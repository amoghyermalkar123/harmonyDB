// raft is a consensus protocol
// disk based  states. Log is the basis of replication
// commands sent from clients are converted into logs and totally ordererd
// over available nodes
//
// leaders are starting point for replication
// candidates are next in line leader in case the leader goes unreachable
// followers are next in line candidates
package raft

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"

	"sync"
	"time"

	"harmonydb/raft/proto"
	"harmonydb/wal"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var ErrFailedToReplicate = errors.New("failed to replicate: majority response not received")
var ErrNotALeader = errors.New("not a leader")

const (
	minElectionTimeout = 250 * time.Millisecond
	maxElectionTimeout = 500 * time.Millisecond
	heartbeatTimeout   = 50 * time.Millisecond
)

type ConsensusState struct {
	// latest term server has seen
	currentTerm int64
	// pastVotes maps term numbers and candidate ids indicating which candidate
	// this node voted for, for a given term
	pastVotes map[int64]int64

	sync.Mutex
}

type Meta struct {
	// last highest commit index known to be comitted
	// to a durable storage. In our case, we replicate to majority
	// nodes and when we get a successfull response, we add the log
	// to a WAL which is durable in nature then increment this field
	lastCommitIndex int64
	// last highest commit index applied to state machine
	// which generally means it is in the primary durable storage
	// in our case the harmony KV
	lastAppliedToSM int64
	// type of node, i.e. leader or others
	nt NodeType
	// for each server, index of the next log entry
	// to send to that server
	nextIndex map[int]int64
	// for each server, index of highest log entry
	// known to be replicated on server
	matchIndex map[int]int64

	sync.RWMutex
}

type Raft struct {
	n      *raftNode
	config ClusterConfig
	server *grpc.Server
}

func (n *Raft) Ready() chan ToApply {
	return n.n.applyc
}

type NodeType int8

const (
	_ NodeType = iota
	Follower
	Candidate
	Leader
)

func nodeTypeString(nt NodeType) string {
	switch nt {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	default:
		return "unknown"
	}
}

type ToApply struct {
	Entries []*proto.Log
}

type raftNode struct {
	proto.UnimplementedRaftServer
	ID              int64
	meta            *Meta
	state           *ConsensusState
	heartbeats      chan *proto.AppendEntries
	heartbeatCloser chan struct{}
	applyc          chan ToApply
	logManager      *wal.LogManager
	cluster         map[int64]proto.RaftClient
	leaderID        int64
	matchIndex      map[int64]int64
	nextIndex       map[int64]int64
	logger          *zap.Logger
	sync.RWMutex
}

func newRaftNode(nodeID int64, logger *zap.Logger) *raftNode {
	r := &raftNode{
		ID: nodeID,
		meta: &Meta{
			nt:         Follower,
			nextIndex:  make(map[int]int64),
			matchIndex: make(map[int]int64),
		},
		state: &ConsensusState{
			pastVotes: make(map[int64]int64),
		},
		cluster:    make(map[int64]proto.RaftClient),
		heartbeats: make(chan *proto.AppendEntries),
		applyc:     make(chan ToApply),
		leaderID:   0,
		matchIndex: make(map[int64]int64),
		nextIndex:  make(map[int64]int64),
		logger:     logger,
	}

	r.logManager = wal.NewLM("/tmp/members/" + strconv.FormatInt(r.ID, 10))

	return r
}

func (n *raftNode) CommitIndexAndScheduleForApply(idx int64) {
	n.meta.Lock()
	oldCommitIndex := n.meta.lastCommitIndex
	n.meta.lastCommitIndex = idx
	n.meta.Unlock()

	// :)
	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] committed [idToCommit:%d] [lastCommitted:%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, idx, oldCommitIndex)

	// Apply newly committed entries to the state machine
	if idx > oldCommitIndex {
		entriesToApply := n.logManager.GetLogsAfter(oldCommitIndex)
		n.applyc <- ToApply{
			Entries: entriesToApply,
		}
	}
}

func generateNodeID() int64 {
	return rand.Int63()
}

// handles incoming log replication and heartbeats
func (n *raftNode) AppendEntriesRPC(ctx context.Context, in *proto.AppendEntries) (*proto.AppendEntriesResponse, error) {
	go func() {
		n.heartbeats <- in
	}()

	n.Lock()
	n.leaderID = in.LeaderId
	n.Unlock()

	// just a heartbeat
	if len(in.Entries) == 0 {
		// committed entries from wal go to kv
		if in.LeaderCommitIdx > n.meta.lastCommitIndex {
			// Get entries that need to be applied
			entriesToApply := n.logManager.GetLogsAfter(n.meta.lastCommitIndex)

			// Only dispatch to kv if there are actually entries to apply
			if len(entriesToApply) > 0 {
				// dispatch to kv
				n.applyc <- ToApply{
					// It is safe and rather perfectly viable that we get logs
					// after the `lastCommitIndex` because if we are a follower
					// we know for sure we will have entries after last commit index
					// because the leader only increments it's commit index when
					// majority of nodes replicate on their end and respond back
					// that same commit index comes to us, the follower in the subsequent
					// heartbeat message
					Entries: entriesToApply,
				}
			}
		}

		return nil, nil
	}

	fmt.Printf("[%s] [node:%d] aepc recv() [entries:%d]\n",
		time.Now().String(), n.ID, len(in.Entries))

	// revert to a follower state if we get a term number higher than ours
	if n.state.currentTerm < in.Term {
		n.state.currentTerm = in.Term
		n.meta.Lock()
		if n.meta.nt == Leader {
			n.meta.nt = Follower
		}
		n.meta.Unlock()
	}

	// reject a stale term request
	// the CurrentTerm value we send as part of the response is read by the requester
	// and update their own CurrentTerm
	if n.state.currentTerm > in.Term {
		fmt.Printf("[%s] [node:%d] aepc rejected: stale term [currentTerm:%d] [in.term:%d]\n",
			time.Now().String(), n.ID, n.state.currentTerm, in.Term)
		return &proto.AppendEntriesResponse{
			Success:     false,
			CurrentTerm: n.state.currentTerm,
		}, nil
	}

	// there are gaps in our logs as those compared to the leader's
	if in.PrevLogIdx > n.logManager.GetLastLogID() {
		fmt.Printf("[%s] [node:%d] aepc rejected: log gap [currentTerm:%d] [in.term:%d]\n",
			time.Now().String(), n.ID, n.state.currentTerm, in.Term)
		return &proto.AppendEntriesResponse{
			Success: false,
		}, nil
	}

	// make sure everything is consistent upto PrevLogIdx, PrevLogTerm
	if in.PrevLogIdx > 0 {
		if n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm() != in.PrevLogTerm {
			fmt.Printf("[%s] [node:%d] aepc rejected: log mismatch [currentTerm:%d] [in.term:%d]\n",
				time.Now().String(), n.ID, n.state.currentTerm, in.Term)
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
				continue
			}
		}

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

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] updated commit index to %d\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, n.meta.lastCommitIndex)

	// Apply
	n.CommitIndexAndScheduleForApply(n.meta.lastCommitIndex)

	return &proto.AppendEntriesResponse{
		CurrentTerm: n.state.currentTerm,
		Success:     true,
	}, nil
}

// handles incoming election requests
func (n *raftNode) RequestVoteRPC(ctx context.Context, in *proto.RequestVote) (*proto.RequestVoteResponse, error) {
	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] received vote request from [node:%d][term:%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.CandidateId, in.Term)

	if n.state.currentTerm >= in.Term {
		fmt.Printf("[%s] [node:%d][term:%d][leader:%t] rejected vote for [node:%d] - stale term\n",
			time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.CandidateId)
		return &proto.RequestVoteResponse{
			VoteGranted: false,
			Term:        n.state.currentTerm,
		}, nil
	}

	// we already voted for someone this term
	if _, voted := n.state.pastVotes[in.Term]; voted {
		fmt.Printf("[%s] [node:%d][term:%d][leader:%t] rejected vote for [node:%d] - already voted this term\n",
			time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.CandidateId)
		return &proto.RequestVoteResponse{
			VoteGranted: false,
			Term:        n.state.currentTerm,
		}, nil
	}

	n.state.Lock()
	n.state.pastVotes[in.Term] = in.CandidateId
	n.state.Unlock()

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] logs up-to-date check [in.LastLogTerm:%d] [in.n.logManager.GetLastLogTerm():%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.LastLogTerm, n.logManager.GetLastLogTerm())

	if in.LastLogTerm <= n.logManager.GetLastLogTerm() {
		n.state.Lock()
		n.state.currentTerm = in.Term
		n.state.Unlock()

		fmt.Printf("[%s] [node:%d][term:%d][leader:%t] granted vote to [node:%d]\n",
			time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.CandidateId)
		return &proto.RequestVoteResponse{
			VoteGranted: true,
			Term:        n.state.currentTerm,
		}, nil
	}

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] rejected vote for [node:%d] - log not up-to-date\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, in.CandidateId)
	return &proto.RequestVoteResponse{
		VoteGranted: false,
		Term:        n.state.currentTerm,
	}, nil
}

// log replication
// TODO: only allow leader to replicate, otherwise re-route request to it
func (n *raftNode) replicate(ctx context.Context, key, val []byte, requestID uint64) error {
	n.state.Lock()
	currentTerm := n.state.currentTerm
	n.state.Unlock()

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] replicate start\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID)

	logID := n.logManager.NextLogID()

	newlog := &proto.Log{
		Term: currentTerm,
		Id:   logID,
		Data: &proto.Cmd{
			Op:        "PUT",
			Key:       string(key),
			Value:     string(val),
			RequestId: requestID,
		},
	}

	n.logManager.Append(newlog)

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] [nextLog:%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, logID)

	peers := []int64{}

	for k := range n.cluster {
		peers = append(peers, k)
	}

	peers, err := n.sendAppendEntries(ctx, peers)
	if err != nil {
		panic(fmt.Errorf("unreachable state %w", err))
	}

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] replication complete [nextLog:%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, logID)

	replStatus := 1

	for peer := range n.cluster {
		idx := n.matchIndex[peer]
		if idx == newlog.Id {
			replStatus += 1
		}
	}

	requiredReplicas := len(n.cluster)/2 + 1

	if replStatus >= requiredReplicas {
		fmt.Printf("[%s] [node:%d][term:%d][leader:%t] quorum achieved [nextLog:%d]\n",
			time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, logID)

		n.CommitIndexAndScheduleForApply(newlog.Id)

		return nil
	}

	fmt.Printf("[%s] [node:%d][term:%d][leader:%t] quorum failed [nextLog:%d]\n",
		time.Now().String(), n.ID, n.state.currentTerm, n.leaderID == n.ID, logID)

	return ErrFailedToReplicate
}

// given a list of `peers` send appendEntries rpc to them and return the peers
// who responded with false rpc response. The caller must them to retry
// returns nil array and error when the replication was successfull
func (n *raftNode) sendAppendEntries(ctx context.Context, peers []int64) ([]int64, error) {
	failedPeers := []int64{}

	for _, peer := range peers {
		fmt.Println("sending AE rpc")
		if peer == n.ID {
			continue
		}

		// Check if peer connection exists
		if n.cluster[peer] == nil {
			n.logger.Warn("peer connection not available",
				zap.Int64("peer_id", peer))
			failedPeers = append(failedPeers, peer)
			continue
		}

		prevLogIndex := n.nextIndex[peer] - 1
		var prevLogTerm int64

		lastLog := n.logManager.GetLog(int(prevLogIndex))
		if lastLog != nil {
			prevLogTerm = lastLog.Term
		}

		resp, err := n.cluster[peer].AppendEntriesRPC(ctx, &proto.AppendEntries{
			Term:            n.state.currentTerm,
			LeaderId:        n.ID,
			PrevLogIdx:      prevLogIndex,
			PrevLogTerm:     prevLogTerm,
			Entries:         n.logManager.GetLogsAfter(n.nextIndex[peer]),
			LeaderCommitIdx: n.meta.lastCommitIndex,
		})

		if err != nil {
			n.logger.Warn("AppendEntries RPC failed",
				zap.Int64("peer_id", peer),
				zap.Error(err))
			failedPeers = append(failedPeers, peer)
			continue
		}

		fmt.Printf("AE RPC response from peer %d: Success=%v, CurrentTerm=%d\n", peer, resp.Success, resp.CurrentTerm)

		// this response means the node is telling us that the log
		// consistency check has failed, so we decrement our nextIndex
		// here
		if !resp.Success && resp.CurrentTerm == 0 {
			n.nextIndex[peer] -= 1
			failedPeers = append(failedPeers, peer)
			fmt.Printf("Peer %d failed log consistency check\n", peer)
		}

		if resp.CurrentTerm > n.state.currentTerm {
			// transition back to the follower state as we have encountered a term higher
			// than ours indicating a potential leader
			n.state.currentTerm = resp.CurrentTerm
			n.meta.Lock()
			if n.meta.nt == Leader {
				n.meta.nt = Follower
			}
			n.meta.Unlock()
		}

		if resp.Success {
			// Get the logs that were sent in this AppendEntries RPC
			sentLogs := n.logManager.GetLogsAfter(n.nextIndex[peer])
			if len(sentLogs) > 0 {
				// Update matchIndex to the ID of the last log that was successfully replicated
				lastReplicatedLogID := sentLogs[len(sentLogs)-1].Id
				n.matchIndex[peer] = lastReplicatedLogID
				n.nextIndex[peer] = lastReplicatedLogID + 1
				fmt.Printf("Peer %d successfully replicated log %d\n", peer, lastReplicatedLogID)
			}
		}

	}

	if len(failedPeers) > 0 {
		return failedPeers, nil
	}

	return nil, nil
}

func randomTimeout(minVal time.Duration) <-chan time.Time {
	if minVal == 0 {
		return nil
	}
	extra := time.Duration(rand.Int63()) % minVal
	return time.After(minVal + extra)
}

func (n *raftNode) startElection() {
	electionTimeOut := randomTimeout(500 * time.Millisecond)

	for {
		select {
		case <-electionTimeOut:
			// fmt.Printf("[%s] [node:%d][leader:%t] election trigger\n", time.Now().String(), n.ID, n.leaderID == n.ID)

			if n.meta.nt == Leader {
				electionTimeOut = randomTimeout(600 * time.Millisecond)
				continue
			}

			electionTimeOut = randomTimeout(800 * time.Millisecond)

			// increment currentTerm
			n.state.Lock()
			n.state.currentTerm += 1
			n.state.pastVotes[n.state.currentTerm] = n.ID
			newTerm := n.state.currentTerm
			n.state.Unlock()

			// start election
			// grant vote to self
			votes := 1

			// request votes for election
			for nid, node := range n.cluster {
				voteResponse, err := node.RequestVoteRPC(context.TODO(), &proto.RequestVote{
					Term:         newTerm,
					CandidateId:  n.ID,
					LastLogIndex: n.logManager.GetLastLogID(),
					LastLogTerm:  n.logManager.GetLastLogTerm(),
				})

				// TODO: exponential backoff and transition cluster to read-only
				if err != nil {
					fmt.Printf("[%s] [node:%d][leader:%t] request to [node:%d] failed\n", time.Now().String(), n.ID, n.leaderID == n.ID, nid)
					continue
				}

				if voteResponse.VoteGranted {
					fmt.Printf("[%s] [node:%d][leader:%t] received vote from [node:%d]\n", time.Now().String(), n.ID, n.leaderID == n.ID, nid)
					votes++
				}
			}

			// elect leader if votes granted by more than half of the nodes
			// present in the cluster
			if votes > len(n.cluster)/2 {
				fmt.Printf("[%s] [node:%d][leader:%t] became leader\n", time.Now().String(), n.ID, n.leaderID == n.ID)

				n.meta.Lock()
				n.meta.nt = Leader
				n.meta.Unlock()

				n.Lock()
				n.leaderID = n.ID
				n.Unlock()

				n.heartbeatCloser = make(chan struct{})
				go n.sendHeartbeats()
			}

		case entry := <-n.heartbeats:
			if n.meta.nt == Leader && entry.Term > n.state.currentTerm {
				// stop sending heartbeats
				close(n.heartbeatCloser)

				fmt.Printf("[%s] [node:%d][leader:%t] became follower\n", time.Now().String(), n.ID, n.leaderID == n.ID)
				n.meta.Lock()

				n.meta.nt = Follower
				n.meta.Unlock()
			}

			// stay as a follower or a candidate because
			// the leader is sending us the heartbeats
			// resetting the election timeout to represent
			// up-to-date ness
			electionTimeOut = randomTimeout(150 * time.Millisecond)
		}
	}
}

func (n *raftNode) sendHeartbeats() {
	heartbeats := time.NewTicker(heartbeatTimeout)

	n.heartbeatRPC()

	for {
		select {
		case _, ok := <-n.heartbeatCloser:
			if !ok {
				return
			}

		case <-heartbeats.C:
			n.heartbeatRPC()
		}
	}
}

func (n *raftNode) heartbeatRPC() {
	// fmt.Printf("[%s] [node:%d][leader:%t] hb\n", time.Now().String(), n.ID, n.leaderID == n.ID)
	n.state.Lock()
	currentTerm := n.state.currentTerm
	n.state.Unlock()

	n.meta.RLock()
	commitIndex := n.meta.lastCommitIndex
	n.meta.RUnlock()

	lastLogID := n.logManager.GetLastLogID()
	lastLogTerm := n.logManager.GetLastLogTerm()

	for _, node := range n.cluster {
		node.AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
			Term:            currentTerm,
			LeaderId:        n.ID,
			PrevLogIdx:      lastLogID,
			PrevLogTerm:     lastLogTerm,
			LeaderCommitIdx: commitIndex,
		})
	}
}

func (r *Raft) GetLastAppliedLastCommitted() (int64, int64) {
	return r.n.meta.lastAppliedToSM, r.n.meta.lastCommitIndex
}

func (r *Raft) GetLastApplied() int64 {
	return r.n.meta.lastAppliedToSM
}

func (r *Raft) IncrementLastApplied() {
	r.n.meta.Lock()
	r.n.meta.lastAppliedToSM++
	r.n.meta.Unlock()
}
