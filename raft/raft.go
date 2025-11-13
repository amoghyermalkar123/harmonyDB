package raft

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"harmonydb/raft/proto"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// raft is a consensus protocol
// disk based  states. Log is the basis of replication
// commands sent from clients are converted into logs and totally ordererd
// over available nodes
//
// leaders are starting point for replication
// candidates are next in line leader in case the leader goes unreachable
// followers are next in line candidates

var ErrFailedToReplicate = errors.New("failed to replicate: majority response not received")
var ErrNotALeader = errors.New("not a leader")

type ConsensusState struct {
	// latest term server has seen
	currentTerm int64
	votedFor    int64
	// pastVotes maps term numbers and candidate ids indicating which candidate
	// this node voted for, for a given term
	pastVotes map[int64]int64
	sync.Mutex
}

type Meta struct {
	// last highest commit index known to be comitted
	lastCommitIndex int64

	// last highest commit index applied to state machine
	lastAppliedToSM int64

	nt NodeType
	// for each server, index of the next log entry
	// to send to that server
	nextIndex map[int]int64
	// for each server, index of highest log entry
	// known to be replicated on server
	matchIndex map[int]int64

	sync.RWMutex
}

//var electionTO = time.Duration(10+rand.Intn(10)) * time.Millisecond
//var heartbeatTO = time.Duration(1+rand.Intn(10)) * time.Millisecond

var electionTO = time.Duration(1100+rand.Intn(1000)+rand.Intn(999)) * time.Millisecond
var heartbeatTO = time.Duration(500+rand.Intn(500)) * time.Millisecond

type NodeType int8

const (
	_ NodeType = iota
	Follower
	Candidate
	Leader
)

type LogManager struct {
	logs []*proto.Log
	sync.RWMutex
}

func (lm *LogManager) Append(log *proto.Log) {
	lm.Lock()
	defer lm.Unlock()
	lm.logs = append(lm.logs, log)
}

func (lm *LogManager) GetLastLogID() int64 {
	lm.RLock()
	defer lm.RUnlock()

	if len(lm.logs) == 0 {
		return 1
	}
	return lm.logs[len(lm.logs)-1].Id
}

func (lm *LogManager) NextLogID() int64 {
	lm.RLock()
	defer lm.RUnlock()

	if len(lm.logs) == 0 {
		return 1
	}

	return lm.logs[len(lm.logs)-1].Id + 1
}

// Deprecated
func (lm *LogManager) GetLastLogTerm() int64 {
	lm.RLock()
	defer lm.RUnlock()
	if len(lm.logs) == 0 {
		return 0
	}
	return lm.logs[len(lm.logs)-1].Term
}

func (lm *LogManager) GetLog(index int) *proto.Log {
	lm.RLock()
	defer lm.RUnlock()

	// we do this because Id: 1 is stored at slice index 0
	index = index - 1
	if index < 0 || index >= len(lm.logs) {
		return nil
	}

	return lm.logs[index]
}

func (lm *LogManager) GetLength() int {
	lm.RLock()
	defer lm.RUnlock()
	return len(lm.logs)
}

func (lm *LogManager) GetLogs() []*proto.Log {
	lm.RLock()
	defer lm.RUnlock()
	result := make([]*proto.Log, len(lm.logs))
	copy(result, lm.logs)
	return result
}

func (lm *LogManager) GetLogsAfter(index int64) []*proto.Log {
	lm.RLock()
	defer lm.RUnlock()
	result := make([]*proto.Log, len(lm.logs[index:]))
	copy(result, lm.logs[index:])
	return result
}

func (lm *LogManager) TruncateAfter(index int) {
	lm.Lock()
	defer lm.Unlock()
	if index >= 0 && index < len(lm.logs) {
		lm.logs = lm.logs[:index+1]
	}
}

type node struct {
	proto.UnimplementedRaftServer
	ID              int64
	meta            *Meta
	state           *ConsensusState
	heartbeats      chan *proto.AppendEntries
	heartbeatCloser chan struct{}
	logManager      *LogManager
	cluster         map[int64]proto.RaftClient
	leaderID        int64
	leaderAddress   string // Cached leader HTTP address
	matchIndex      map[int64]int64
	nextIndex       map[int64]int64
	sync.RWMutex
}

func generateNodeID() int64 {
	return rand.Int63()
}

func newNode() *node {
	return &node{
		ID: generateNodeID(),
		meta: &Meta{
			nt:         Follower,
			nextIndex:  make(map[int]int64),
			matchIndex: make(map[int]int64),
		},
		state: &ConsensusState{
			votedFor:  0,
			pastVotes: make(map[int64]int64),
		},
		logManager: &LogManager{
			logs: make([]*proto.Log, 0),
		},
		cluster:    make(map[int64]proto.RaftClient),
		heartbeats: make(chan *proto.AppendEntries),
		leaderID:   0,
		matchIndex: make(map[int64]int64),
		nextIndex:  make(map[int64]int64),
	}
}

// handles incoming log replication and heartbeats
func (n *node) AppendEntriesRPC(ctx context.Context, in *proto.AppendEntries) (*proto.AppendEntriesResponse, error) {
	n.heartbeats <- in

	n.Lock()
	n.leaderID = in.LeaderId
	n.Unlock()

	// just a heartbeat
	if len(in.Entries) == 0 {
		return nil, nil
	}

	getLogger().Info("Starting AppendEntries RPC", zap.String("component", "raft"), zap.Int64("sender", in.LeaderId), zap.String("key", string(in.Entries[0].Data.Key)), zap.String("value", string(in.Entries[0].Data.Value)))

	defer func() {
		getLogger().Warn("Stopping AppendEntries RPC", zap.String("component", "raft"), zap.String("key", string(in.Entries[0].Data.Key)), zap.String("value", string(in.Entries[0].Data.Value)))
	}()

	// revert to a follower state if we get a term number higher than ours
	if n.state.currentTerm < in.Term {
		n.state.currentTerm = in.Term
		n.meta.Lock()
		if n.meta.nt == Leader {
			n.meta.nt = Follower
			getLogger().Warn("Leader transitioned back to follower", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("new_term", in.Term))
		}
		n.meta.Unlock()
	}

	// reject a stale term request
	// the CurrentTerm value we send as part of the response is read by the requester
	// and update their own CurrentTerm
	if n.state.currentTerm > in.Term {
		getLogger().Error("Rejecting append entries due to stale term", zap.String("component", "raft"), zap.Int64("current_term", n.state.currentTerm), zap.Int64("incoming_term", in.Term))
		return &proto.AppendEntriesResponse{
			Success:     false,
			CurrentTerm: n.state.currentTerm,
		}, nil
	}

	// there are gaps in our logs as those compared to the leader's
	if in.PrevLogIdx > n.logManager.GetLastLogID() {
		getLogger().Error("Incoming log ahead of local max", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_max", n.logManager.GetLastLogID()))
		return &proto.AppendEntriesResponse{
			Success: false,
		}, nil
	}

	// make sure everything is consistent upto PrevLogIdx, PrevLogTerm
	if in.PrevLogIdx > 0 {
		if n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm() != in.PrevLogTerm {
			weird := n.logManager.GetLog(int(in.PrevLogIdx))
			getLogger().Error("Inconsistent log state", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_term", weird.GetTerm()), zap.Int64("prev_log_term", in.PrevLogTerm))
			return &proto.AppendEntriesResponse{
				Success: false,
			}, nil
		}
	}

	// now that we know everything is consistent, process the new entries
	for _, entry := range in.Entries {
		existingEntry := n.logManager.GetLog(int(entry.Id))
		if existingEntry != nil {
			if existingEntry.Id == entry.Id && existingEntry.Term != entry.Term {
				// if an entry conflicts at this index, i.e. same ID but different term
				// that means all subsequent entries are going to be incorrect/ conflicting
				// this is because, log matching property ensures that if one entry is correct
				// all preceding must be right.
				//
				// So we delete the entry at this index, and overwrite with the new incoming ones
				n.logManager.TruncateAfter(int(entry.Id - 1))
			} else {
				getLogger().Debug("Skipping existing entry", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id))
				continue
			}
		}

		getLogger().Info("New log entry added", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id), zap.Int64("term", entry.Term))
		n.logManager.Append(entry)
	}

	// update the commit index
	// understanding the requirement for the `min` function
	//
	// if the leader's commit index is ahead than the latest entry from the local
	// logs, we need to be as close to that index as possible but only with the indexes
	// we have present locally, hence we take the Id of the last entry in our local logs
	//
	// if the leader's commit index is behind than the latest entry from the local
	// logs, we need to update the commit index to the leader's commit index, i.e.
	// be on the same page even though we have new logs given to us, this condition
	// tells us that leader for some reason has only committed until a certain Id and
	// that Id is behind what we have in our local logs
	if in.LeaderCommitIdx > n.meta.lastCommitIndex {
		n.meta.lastCommitIndex = min(in.LeaderCommitIdx, n.logManager.GetLastLogID())
	}

	getLogger().Info("AppendEntries completed successfully", zap.String("component", "raft"), zap.Int64("node_id", n.ID))

	return &proto.AppendEntriesResponse{
		CurrentTerm: n.state.currentTerm,
		Success:     true,
	}, nil
}

// handles incoming election requests
func (n *node) RequestVoteRPC(ctx context.Context, in *proto.RequestVote) (*proto.RequestVoteResponse, error) {
	if n.state.currentTerm > in.Term {
		return &proto.RequestVoteResponse{
			VoteGranted: false,
			Term:        n.state.currentTerm,
		}, nil
	}

	if n.state.votedFor == 0 || n.state.votedFor == in.CandidateId {
		if _, voted := n.state.pastVotes[in.Term]; voted {
			// we already voted for someone this term
			return &proto.RequestVoteResponse{
				VoteGranted: false,
				Term:        n.state.currentTerm,
			}, nil
		}

		n.state.Lock()
		n.state.pastVotes[in.Term] = in.CandidateId
		n.state.Unlock()

		if in.LastLogTerm <= n.logManager.GetLastLogTerm() {
			n.state.Lock()
			n.state.currentTerm = in.Term
			n.state.Unlock()

			return &proto.RequestVoteResponse{
				VoteGranted: true,
				Term:        n.state.currentTerm,
			}, nil
		}
	}

	return &proto.RequestVoteResponse{
		VoteGranted: false,
		Term:        n.state.currentTerm,
	}, nil
}

// log replication
// TODO: only allow leader to replicate, otherwise re-route request to it
func (n *node) replicate(ctx context.Context, key, val []byte) error {
	getLogger().Info("Starting replication", zap.String("component", "raft"), zap.String("key", string(key)), zap.String("value", string(val)))

	newlog := &proto.Log{
		Term: n.state.currentTerm,
		Id:   n.logManager.NextLogID(),
		Data: &proto.Cmd{
			Op:    "PUT",
			Key:   string(key),
			Value: string(val),
		},
	}

	getLogger().Debug("Created new log entry", zap.String("component", "raft"), zap.Int64("log_id", newlog.Id), zap.Int64("term", newlog.Term))

	n.logManager.Append(newlog)

	peers := []int64{}
	for k := range n.cluster {
		peers = append(peers, k)
	}

	for true {
		var err error

		peers, err = n.sendAppendEntries(peers)
		if peers == nil && err == nil {
			break
		}
	}

	replStatus := 0

	for peer := range n.cluster {
		idx := n.matchIndex[peer]
		if idx == newlog.Id {
			replStatus += 1
		}
	}

	if replStatus >= len(n.cluster)/2+1 {
		// log has been successfully applied to the state machine,
		// now commit this log.
		n.meta.lastCommitIndex = newlog.Id
		getLogger().Info("Log entry successfully replicated", zap.String("component", "raft"), zap.Int64("log_id", newlog.Id))
		return nil
	}

	return ErrFailedToReplicate
}

// given a list of `peers` send appendEntries rpc to them and return the peers
// who responded with false rpc response. The caller must them to retry
// returns nil array and error when the replication was successfull
func (n *node) sendAppendEntries(peers []int64) ([]int64, error) {
	// TODO: this is incorrect log term calculation, if the previous
	// entries weren't replicated properly, ..

	failedPeers := []int64{}

	for _, peer := range peers {
		if peer == n.ID {
			continue
		}

		prevLogIndex := n.nextIndex[peer] - 1
		var prevLogTerm int64

		lastLog := n.logManager.GetLog(int(prevLogIndex))
		if lastLog != nil {
			prevLogTerm = lastLog.Term
		}

		getLogger().Debug("Sending AppendEntries RPC", zap.String("component", "raft"), zap.Int64("prev_log_index", prevLogIndex), zap.Int64("prev_log_term", prevLogTerm))

		resp, err := n.cluster[peer].AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
			Term:        n.state.currentTerm,
			LeaderId:    n.ID,
			PrevLogIdx:  prevLogIndex,
			PrevLogTerm: prevLogTerm,
			Entries:     n.logManager.GetLogsAfter(n.nextIndex[peer]),
		})

		// TODO: decrement nextIndex when log consistency check fails
		// i.e. line 242
		if err != nil {
			failedPeers = append(failedPeers, peer)
			getLogger().Debug("Peer failed to respond", zap.String("component", "raft"), zap.Int64("peer_id", peer))
		} else {
			// this response means the node is telling us that the log
			// consistency check has failed, so we decrement our nextIndex
			// here
			if !resp.Success && resp.CurrentTerm == 0 {
				n.nextIndex[peer] -= 1
				getLogger().Debug("Log match failed, decremented next index", zap.String("component", "raft"), zap.Int64("peer_id", peer))
				failedPeers = append(failedPeers, peer)
			}

			if resp.CurrentTerm > n.state.currentTerm {
				// transition back to the follower state as we have encountered a term higher
				// than ours indicating a potential leader
				n.state.currentTerm = resp.CurrentTerm
				n.meta.Lock()
				if n.meta.nt == Leader {
					n.meta.nt = Follower
					getLogger().Warn("Leader transitioned back to follower during replication", zap.String("component", "raft"), zap.Int64("node_id", n.ID))
				}
				n.meta.Unlock()
			}

			if resp.Success {
				n.matchIndex[peer] += 1
				n.nextIndex[peer] += 2
				getLogger().Info("Successful RPC, advancing match and next indexes", zap.String("component", "raft"))
			}

			getLogger().Debug("Updated indexes", zap.String("component", "raft"))
		}
	}

	if len(failedPeers) > 0 {
		return failedPeers, nil
	}

	return nil, nil
}

func (n *node) startElection() {
	electionTimeOut := time.NewTicker(electionTO)
	getLogger().Info("Starting election routine", zap.String("component", "raft"), zap.Int64("node_id", n.ID))

	for {
		select {
		case <-electionTimeOut.C:
			if n.meta.nt == Leader || len(n.cluster) < 2 {
				electionTimeOut.Reset(electionTO)
				continue
			}

			fmt.Println("leader timed out, standing for new elections", len(n.cluster))
			electionTimeOut.Reset(electionTO)

			// increment currentTerm
			n.state.Lock()
			n.state.currentTerm += 1
			getLogger().Info("Incremented current term for election", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("term", n.state.currentTerm))
			n.state.Unlock()

			n.state.Lock()
			n.state.pastVotes[n.state.currentTerm] = n.ID
			n.state.Unlock()

			// transition to candiadte
			// start election
			votes := 1
			for ni, node := range n.cluster {
				getLogger().Info("Requesting vote from peer", zap.String("component", "raft"), zap.Int64("peer_id", ni), zap.Int64("term", n.state.currentTerm))
				voteResponse, err := node.RequestVoteRPC(context.TODO(), &proto.RequestVote{
					Term:         n.state.currentTerm,
					CandidateId:  n.ID,
					LastLogIndex: n.logManager.GetLastLogID(),
					LastLogTerm:  n.logManager.GetLastLogTerm(),
				})

				if err != nil {
					// TODO: better error handling
					fmt.Printf("request vote rpc: %v", err)
					continue
				}

				if voteResponse.VoteGranted {
					votes += 1
				}

			}

			getLogger().Info("Calculating election votes", zap.String("component", "raft"), zap.Int("votes_received", votes), zap.Int("required_majority", len(n.cluster)/2+1))

			// elect leader if votes granted by more than half of the nodes
			// present in the cluster
			if votes > len(n.cluster)/2 {
				n.meta.Lock()
				n.meta.nt = Leader
				n.meta.Unlock()

				n.heartbeatCloser = make(chan struct{})

				fmt.Printf("node %d is now leader, with term: %d\n", n.ID, n.state.currentTerm)
				fmt.Printf("starting heartbeats...")

				go n.sendHeartbeats()
			}
		case entry := <-n.heartbeats:
			// TODO: apply comitted entries
			if n.meta.nt == Leader && entry.Term > n.state.currentTerm {
				getLogger().Info("Node transitioned to follower after election", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("new_term", n.state.currentTerm))

				// stop sending heartbeats
				close(n.heartbeatCloser)

				n.meta.Lock()
				n.meta.nt = Follower
				getLogger().Warn("Node transitioned back to follower after failed election", zap.String("component", "raft"), zap.Int64("node_id", n.ID))
				n.meta.Unlock()
			}

			// stay as a follower or a candidate because
			// the leader is sending us the heartbeats
			// resetting the election timeout to represent
			// up-to-date ness
			electionTimeOut.Reset(electionTO)
			getLogger().Debug("Node received leader heartbeat", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("leader_term", entry.Term))
		}
	}
}

func (n *node) sendHeartbeats() {
	if n.meta.nt != Leader {
		return
	}

	fmt.Println("dbg", heartbeatTO)
	heartbeats := time.NewTicker(heartbeatTO)

	for {
		select {
		case _, ok := <-n.heartbeatCloser:
			if !ok {
				heartbeats.Stop()
				return
			}
		case <-heartbeats.C:
			for _, node := range n.cluster {
				// Since this is a heartbeat message, we only need
				// to send term and leader id to maintain the leader status
				_, err := node.AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
					Term:        n.state.currentTerm,
					LeaderId:    n.ID,
					PrevLogIdx:  n.logManager.GetLastLogID(),
					PrevLogTerm: n.logManager.GetLastLogTerm(),
					// We don't need to send `Entries` array here.
					LeaderCommitIdx: n.meta.lastCommitIndex,
				})

				if err != nil {
					fmt.Printf("append entries rpc: heartbeat : %v", err)
					continue
				}
			}
		}
	}
}

type Raft struct {
	n         *node
	discovery *ConsulDiscovery
}

func NewRaftServerWithConsul(port int) *Raft {
	r := &Raft{
		n: newNode(),
	}

	// Initialize Consul service discovery
	discovery, err := NewConsulDiscovery(r.n.ID, port)
	if err != nil {
		getLogger().Error("Failed to initialize Consul discovery", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Int("port", port), zap.Error(err))
		getLogger().Info("Make sure Consul is running: consul agent -dev", zap.String("component", "raft"))
		panic(err)
	}

	// Register this node with Consul
	if err := discovery.Register(); err != nil {
		getLogger().Error("Failed to register with Consul", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Error(err))
		panic(err)
	}
	getLogger().Info("Successfully registered node with Consul", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Int("port", port))

	// Discover and connect to existing peers
	cluster, err := discovery.DiscoverPeers()
	if err != nil {
		getLogger().Warn("Initial peer discovery failed", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Error(err))
	}
	r.n.cluster = cluster

	// Watch for cluster changes
	discovery.WatchForChanges(func(newCluster map[int64]proto.RaftClient) {
		r.n.Lock()
		r.n.cluster = newCluster
		// initialize the nextIndex once if not previously initialized
		// hence the len check, no need to update nextIndex as it's managed by
		// the core raft algorithm
		if len(r.n.nextIndex) == 0 {
			for pr := range newCluster {
				// Since our main proto.Log is a 1-indexed array because
				// Id's of the commands start from 1
				r.n.nextIndex[pr] = 1
			}
		}
		r.n.Unlock()
	})

	// Store discovery reference for cleanup
	r.discovery = discovery

	go r.startWithPort(port)
	go r.n.startElection()

	return r
}

// Deprecated: Use NewRaftServerWithConsul instead
func NewRaftServerWithConfig(port int, peers []string) *Raft {
	return NewRaftServerWithConsul(port)
}

// Deprecated: Use NewRaftServerWithConsul instead
func NewRaftServerWithDiscovery(port int) *Raft {
	return NewRaftServerWithConsul(port)
}

func (r *Raft) startWithPort(port int) {
	serv := grpc.NewServer()

	proto.RegisterRaftServer(serv, r.n)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		getLogger().Fatal("Failed to listen on port", zap.String("component", "raft"), zap.Int("port", port), zap.Error(err))
	}

	getLogger().Info("Raft server listening", zap.String("component", "raft"), zap.Int("port", port))
	serv.Serve(lis)
}

// GetDiscovery returns the Consul discovery instance for cleanup
func (r *Raft) GetDiscovery() *ConsulDiscovery {
	return r.discovery
}

func (r *Raft) GetLeader() proto.RaftClient {
	return r.n.cluster[r.n.leaderID]
}

// GetLeaderID returns the current leader's node ID
func (r *Raft) GetLeaderID() int64 {
	r.n.RLock()
	defer r.n.RUnlock()
	return r.n.leaderID
}

// GetLeaderAddress returns the HTTP address of the current leader
func (r *Raft) GetLeaderAddress() (string, error) {
	leaderID := r.GetLeaderID()
	if leaderID == 0 {
		return "", fmt.Errorf("no leader elected")
	}

	if leaderID == r.n.ID {
		return "", fmt.Errorf("this node is the leader")
	}

	// Use Consul to discover the leader's address
	services, _, err := r.discovery.client.Health().Service(RaftServiceName, "", true, nil)
	if err != nil {
		return "", fmt.Errorf("failed to query consul for services: %v", err)
	}

	for _, service := range services {
		nodeIDStr, exists := service.Service.Meta["node_id"]
		if !exists {
			continue
		}

		nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
		if err != nil {
			continue
		}

		if nodeID == leaderID {
			// Assuming HTTP server runs on port+1000 (you may need to adjust this)
			httpPort := service.Service.Port + 1000
			return fmt.Sprintf("http://%s:%d", service.Service.Address, httpPort), nil
		}
	}

	return "", fmt.Errorf("leader address not found in consul")
}

// Put is a client side interface which is the main entry point for
// log replication
// TODO: add functionality to store the leader info
func (r *Raft) Put(ctx context.Context, key, val []byte) error {
	r.n.meta.RLock()
	isLeader := r.n.meta.nt == Leader
	r.n.meta.RUnlock()

	if !isLeader {
		// TODO: Forward request to leader
		return ErrNotALeader
	}
	return r.n.replicate(ctx, key, val)
}

func (r *Raft) GetLastAppliedLastCommitted() (int64, int64) {
	return r.n.meta.lastAppliedToSM, r.n.meta.lastCommitIndex
}

func (r *Raft) IncrementLastApplied() {
	r.n.meta.Lock()
	r.n.meta.lastAppliedToSM++
	r.n.meta.Unlock()
}

// GetStatus returns the status of the Raft node for monitoring
func (n *node) GetStatus(ctx context.Context, req *proto.GetStatusRequest) (*proto.GetStatusResponse, error) {
	n.state.Lock()
	currentTerm := n.state.currentTerm
	n.state.Unlock()

	n.meta.RLock()
	nodeState := ""
	switch n.meta.nt {
	case Leader:
		nodeState = "Leader"
	case Candidate:
		nodeState = "Candidate"
	case Follower:
		nodeState = "Follower"
	default:
		nodeState = "Unknown"
	}
	lastApplied := n.meta.lastAppliedToSM
	lastCommitted := n.meta.lastCommitIndex
	n.meta.RUnlock()

	return &proto.GetStatusResponse{
		CurrentTerm:   currentTerm,
		NodeState:     nodeState,
		LastApplied:   lastApplied,
		LastCommitted: lastCommitted,
		NodeId:        n.ID,
	}, nil
}

// GetLogs returns all logs for monitoring
func (n *node) GetLogs(ctx context.Context, req *proto.GetLogsRequest) (*proto.GetLogsResponse, error) {
	logs := n.logManager.GetLogs()
	return &proto.GetLogsResponse{
		Logs: logs,
	}, nil
}

// GetNodeType returns the current node type (Leader, Follower, Candidate)
func (r *Raft) GetNodeType() NodeType {
	r.n.meta.RLock()
	defer r.n.meta.RUnlock()
	return r.n.meta.nt
}

// GetCurrentTerm returns the current term
func (r *Raft) GetCurrentTerm() int64 {
	r.n.state.Lock()
	defer r.n.state.Unlock()
	return r.n.state.currentTerm
}

// String method for NodeType
func (nt NodeType) String() string {
	switch nt {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}
