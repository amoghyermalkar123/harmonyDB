package raft

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"harmonydb/raft/proto"

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

// var electionTO = time.Duration(601+rand.Intn(999)) * time.Millisecond
// var heartbeatTO = time.Duration(600+rand.Intn(300)) * time.Millisecond

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

	log.Info("[START] AppendEntriesRPC", "sender", in.LeaderId, in.Entries[0].Data.Key, in.Entries[0].Data.Value)

	defer func() {
		log.Warn("[STOP] AppendEntriesRPC", in.Entries[0].Data.Key, in.Entries[0].Data.Value)
	}()

	// revert to a follower state if we get a term number higher than ours
	if n.state.currentTerm < in.Term {
		n.state.currentTerm = in.Term
		n.meta.Lock()
		if n.meta.nt == Leader {
			n.meta.nt = Follower
			log.Warn("Transitioned back to follower")
		}
		n.meta.Unlock()
	}

	// reject a stale term request
	// the CurrentTerm value we send as part of the response is read by the requester
	// and update their own CurrentTerm
	if n.state.currentTerm > in.Term {
		log.Error("[X] rejecting append entries: stale term", n.state.currentTerm, in.Term)
		return &proto.AppendEntriesResponse{
			Success:     false,
			CurrentTerm: n.state.currentTerm,
		}, nil
	}

	// there are gaps in our logs as those compared to the leader's
	if in.PrevLogIdx > n.logManager.GetLastLogID() {
		log.Error("[X] incoming log ahead of local max", in.PrevLogIdx, n.logManager.GetLastLogID())
		return &proto.AppendEntriesResponse{
			Success: false,
		}, nil
	}

	// make sure everything is consistent upto PrevLogIdx, PrevLogTerm
	if in.PrevLogIdx > 0 {
		if n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm() != in.PrevLogTerm {
			log.Error("[X] inconsistent state", n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm(), in.PrevLogTerm)
			return &proto.AppendEntriesResponse{
				Success: false,
			}, nil
		}
	}

	// now that we know everything is consistent, process the new entries
	for _, entry := range in.Entries {
		existingEntry := n.logManager.GetLog(int(entry.Id))
		if existingEntry != nil && existingEntry.Id == entry.Id && existingEntry.Term != entry.Term {
			// if an entry conflicts at this index, i.e. same ID but different term
			// that means all subsequent entries are going to be incorrect/ conflicting
			// this is because, log matching property ensures that if one entry is correct
			// all preceding must be right.
			//
			// So we delete the entry at this index, and overwrite with the new incoming ones
			n.logManager.TruncateAfter(int(entry.Id - 1))
		}

		log.Info("[APPEND] new entry added", entry)
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

	log.Info("[Success] AppendEntries")

	return &proto.AppendEntriesResponse{
		CurrentTerm: n.state.currentTerm,
		Success:     true,
	}, nil
}

// handles incoming election requests
// TODO: for a given term, vote only 1 candidate
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
	log.Info("replicating", string(key), string(val))

	newlog := &proto.Log{
		Term: n.state.currentTerm,
		Id:   n.logManager.NextLogID(),
		Data: &proto.Cmd{
			Op:    "PUT",
			Key:   string(key),
			Value: string(val),
		},
	}

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
		if idx >= newlog.Id {
			replStatus += 1
		}
	}

	if replStatus >= len(n.cluster)/2 {
		// log has been successfully applied to the state machine,
		// now commit this log.
		n.meta.lastCommitIndex = newlog.Id
		log.Info("Successfully Replicated", "CommitIndex", n.meta.lastCommitIndex)
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

		fmt.Println("repl: sending to:", peer)

		prevLogIndex := n.nextIndex[peer]
		var prevLogTerm int64

		lastLog := n.logManager.GetLog(int(prevLogIndex))
		if lastLog != nil {
			prevLogTerm = lastLog.Term
		}

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
			fmt.Printf("append entries rpc: repl: %v", err)
			continue
		}

		// this response means the node is telling us that the log
		// consistency check has failed, so we decrement our nextIndex
		// here
		if !resp.Success && resp.CurrentTerm == 0 {
			n.nextIndex[peer] -= 1
			fmt.Printf("decrementing nextIndex to %d for peer %d", n.nextIndex[peer], peer)
			failedPeers = append(failedPeers, peer)
		}

		if resp.CurrentTerm > n.state.currentTerm {
			// transition back to the follower state as we have encountered a term higher
			// than ours indicating a potential leader
			n.state.currentTerm = resp.CurrentTerm
			n.meta.Lock()
			if n.meta.nt == Leader {
				n.meta.nt = Follower
				log.Warn("Transitioned back to follower")
			}
			n.meta.Unlock()
		}

		if resp.Success {
			n.matchIndex[peer] += 1
			n.nextIndex[peer] += 2
		}
	}

	if len(failedPeers) > 0 {
		return failedPeers, nil
	}

	return nil, nil
}

func (n *node) startElection() {
	electionTimeOut := time.NewTicker(electionTO)
	log.Info("starting election routine")

	for {
		select {
		case <-electionTimeOut.C:
			if n.meta.nt == Leader || len(n.cluster) < 2 {
				electionTimeOut.Reset(electionTO)
				continue
			}

			fmt.Println("leader timed out, standing for new elections")
			electionTimeOut.Reset(electionTO)

			// increment currentTerm
			n.state.Lock()
			n.state.currentTerm += 1
			log.Info("Incremented current term", n.state.currentTerm)
			n.state.Unlock()

			n.state.Lock()
			n.state.pastVotes[n.state.currentTerm] = n.ID
			n.state.Unlock()

			// transition to candiadte
			// start election
			votes := 1
			for ni, node := range n.cluster {
				log.Info("requesting for vote from", ni)
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

			log.Info("calculating votes")

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
				log.Info("transitioned to a follower")

				// stop sending heartbeats
				close(n.heartbeatCloser)

				n.meta.Lock()
				n.meta.nt = Follower
				log.Warn("Transitioned back to follower")
				n.meta.Unlock()
			}

			// stay as a follower or a candidate because
			// the leader is sending us the heartbeats
			// resetting the election timeout to represent
			// up-to-date ness
			electionTimeOut.Reset(electionTO)
			log.Debug("node %d received leader heartbeat\n", n.ID)
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
		fmt.Printf("❌ Failed to initialize Consul discovery: %v\n", err)
		fmt.Println("💡 Make sure Consul is running: consul agent -dev")
		panic(err)
	}

	// Register this node with Consul
	if err := discovery.Register(); err != nil {
		fmt.Printf("❌ Failed to register with Consul: %v\n", err)
		panic(err)
	}

	// Discover and connect to existing peers
	cluster, err := discovery.DiscoverPeers()
	if err != nil {
		fmt.Printf("⚠️  Initial peer discovery failed: %v\n", err)
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
		log.Fatalf("failed to listen on port %d: %v", port, err)
	}

	fmt.Printf("Raft server listening on port %d\n", port)
	serv.Serve(lis)
}

// GetDiscovery returns the Consul discovery instance for cleanup
func (r *Raft) GetDiscovery() *ConsulDiscovery {
	return r.discovery
}

// Put is a client side interface which is the main entry point for
// log replication
// TODO: add functionality to store the leader info
func (r *Raft) Put(ctx context.Context, key, val []byte) error {
	if r.n.meta.nt != Leader {
		// TODO: Forward request to leader
		// return fmt.Errorf("node is not leader")
		return nil
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
