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

	"sync"
	"time"

	"harmonydb/metrics"
	"harmonydb/raft/proto"
	"harmonydb/wal"

	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var ErrFailedToReplicate = errors.New("failed to replicate: majority response not received")
var ErrNotALeader = errors.New("not a leader")

type ConsensusState struct {
	// latest term server has seen
	currentTerm int64
	// who we voted for in the current term
	votedFor int64
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

var electionTO = time.Duration(150+rand.Intn(150)) * time.Millisecond
var heartbeatTO = time.Duration(50) * time.Millisecond

// var electionTO = time.Duration(1100+rand.Intn(1000)+rand.Intn(999)) * time.Millisecond
// var heartbeatTO = time.Duration(500+rand.Intn(500)) * time.Millisecond

type NodeType int8

const (
	_ NodeType = iota
	Follower
	Candidate
	Leader
)

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
	sync.RWMutex
}

func newRaftNode() *raftNode {
	return &raftNode{
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
		logManager: wal.NewLM(),
		cluster:    make(map[int64]proto.RaftClient),
		heartbeats: make(chan *proto.AppendEntries),
		applyc:     make(chan ToApply),
		leaderID:   0,
		matchIndex: make(map[int64]int64),
		nextIndex:  make(map[int64]int64),
	}
}

func (n *raftNode) CommitIdx(idx int64) {
	n.meta.Lock()
	oldCommitIndex := n.meta.lastCommitIndex
	n.meta.lastCommitIndex = idx
	n.meta.Unlock()

	// Update metrics
	nodeID := fmt.Sprintf("%d", n.ID)
	metrics.RaftCommitIndex.WithLabelValues(nodeID).Set(float64(idx))

	// Apply newly committed entries to the state machine
	if idx > oldCommitIndex {
		n.applyc <- ToApply{
			Entries: n.logManager.GetLogsAfter(oldCommitIndex),
		}
	}
}

func generateNodeID() int64 {
	return rand.Int63()
}

// handles incoming log replication and heartbeats
func (n *raftNode) AppendEntriesRPC(ctx context.Context, in *proto.AppendEntries) (*proto.AppendEntriesResponse, error) {
	n.heartbeats <- in

	n.Lock()
	oldLeaderID := n.leaderID
	n.leaderID = in.LeaderId
	n.Unlock()

	nodeID := fmt.Sprintf("%d", n.ID)

	// Track leader changes
	if oldLeaderID != in.LeaderId && in.LeaderId != 0 {
		metrics.RaftLeaderChangesTotal.WithLabelValues(nodeID).Inc()
		metrics.RaftLeaderID.WithLabelValues(nodeID).Set(float64(in.LeaderId))
	}

	// just a heartbeat
	if len(in.Entries) == 0 {
		metrics.RaftAppendEntriesTotal.WithLabelValues(nodeID, "heartbeat", "success").Inc()
		// committed entries from wal go to kv
		if in.LeaderCommitIdx > n.meta.lastCommitIndex {
			// dispatch to kv
			n.applyc <- ToApply{
				// It is safe and rather perfectly viable that we get logs
				// after the `lastCommitIndex` because if we are a follower
				// we know for sure we will have entries after last commit index
				// because the leader only increments it's commit index when
				// majority of nodes replicate on their end and respond back
				// that same commit index comes to us, the follower in the subsequent
				// heartbeat message
				Entries: n.logManager.GetLogsAfter(n.meta.lastCommitIndex),
			}
		}

		return nil, nil
	}

	getLogger().Info("Starting AppendEntries RPC", zap.String("component", "raft"), zap.Int64("sender", in.LeaderId), zap.String("key", string(in.Entries[0].Data.Key)), zap.String("value", string(in.Entries[0].Data.Value)))

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
		metrics.RaftAppendEntriesTotal.WithLabelValues(nodeID, "replication", "rejected_stale_term").Inc()
		return &proto.AppendEntriesResponse{
			Success:     false,
			CurrentTerm: n.state.currentTerm,
		}, nil
	}

	// there are gaps in our logs as those compared to the leader's
	if in.PrevLogIdx > n.logManager.GetLastLogID() {
		getLogger().Error("Incoming log ahead of local max", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_max", n.logManager.GetLastLogID()))
		metrics.RaftAppendEntriesTotal.WithLabelValues(nodeID, "replication", "log_gap").Inc()
		return &proto.AppendEntriesResponse{
			Success: false,
		}, nil
	}

	// make sure everything is consistent upto PrevLogIdx, PrevLogTerm
	if in.PrevLogIdx > 0 {
		if n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm() != in.PrevLogTerm {
			weird := n.logManager.GetLog(int(in.PrevLogIdx))
			getLogger().Error("Inconsistent log state", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_term", weird.GetTerm()), zap.Int64("prev_log_term", in.PrevLogTerm))
			metrics.RaftAppendEntriesTotal.WithLabelValues(nodeID, "replication", "inconsistent").Inc()
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
				metrics.RaftLogTruncationsTotal.WithLabelValues(nodeID).Inc()
			} else {
				getLogger().Debug("Skipping existing entry", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id))
				continue
			}
		}

		getLogger().Info("New log entry added", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id), zap.Int64("term", entry.Term))
		n.logManager.Append(entry)
	}

	// Update log entries metric
	metrics.RaftLogEntries.WithLabelValues(nodeID).Set(float64(n.logManager.GetLength()))
	metrics.RaftAppendEntriesTotal.WithLabelValues(nodeID, "replication", "success").Inc()

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

	return &proto.AppendEntriesResponse{
		CurrentTerm: n.state.currentTerm,
		Success:     true,
	}, nil
}

// handles incoming election requests
func (n *raftNode) RequestVoteRPC(ctx context.Context, in *proto.RequestVote) (*proto.RequestVoteResponse, error) {
	nodeID := fmt.Sprintf("%d", n.ID)

	if n.state.currentTerm > in.Term {
		metrics.RaftRequestVoteTotal.WithLabelValues(nodeID, "rejected_stale_term").Inc()
		return &proto.RequestVoteResponse{
			VoteGranted: false,
			Term:        n.state.currentTerm,
		}, nil
	}

	// Check if we already voted for someone else this term
	if _, voted := n.state.pastVotes[in.Term]; voted {
		if n.state.pastVotes[in.Term] != in.CandidateId {
			// we already voted for someone else this term
			metrics.RaftRequestVoteTotal.WithLabelValues(nodeID, "already_voted").Inc()
			return &proto.RequestVoteResponse{
				VoteGranted: false,
				Term:        n.state.currentTerm,
			}, nil
		}
	}

	// Check if candidate's log is at least as up-to-date as ours
	// Raft determines which log is more up-to-date by comparing term of last entry
	// If terms are equal, compare log length
	candidateLogOK := false
	lastLogTerm := n.logManager.GetLastLogTerm()
	lastLogID := n.logManager.GetLastLogID()

	if in.LastLogTerm > lastLogTerm {
		candidateLogOK = true
	} else if in.LastLogTerm == lastLogTerm && in.LastLogIndex >= lastLogID {
		candidateLogOK = true
	}

	if candidateLogOK {
		n.state.Lock()
		n.state.currentTerm = in.Term
		n.state.pastVotes[in.Term] = in.CandidateId
		n.state.votedFor = in.CandidateId
		n.state.Unlock()

		metrics.RaftVotesGrantedTotal.WithLabelValues(nodeID).Inc()
		metrics.RaftRequestVoteTotal.WithLabelValues(nodeID, "granted").Inc()
		metrics.RaftTerm.WithLabelValues(nodeID).Set(float64(in.Term))

		return &proto.RequestVoteResponse{
			VoteGranted: true,
			Term:        n.state.currentTerm,
		}, nil
	}

	metrics.RaftRequestVoteTotal.WithLabelValues(nodeID, "rejected").Inc()
	return &proto.RequestVoteResponse{
		VoteGranted: false,
		Term:        n.state.currentTerm,
	}, nil
}

// log replication
// TODO: only allow leader to replicate, otherwise re-route request to it
func (n *raftNode) replicate(ctx context.Context, key, val []byte) error {
	ctx, span := metrics.StartSpan(ctx, "raft.replicate")
	defer span.End()

	startTime := time.Now()
	nodeID := fmt.Sprintf("%d", n.ID)

	span.SetAttributes(
		metrics.NodeIDAttr(n.ID),
		metrics.TermAttr(n.state.currentTerm),
		metrics.KeyAttr(string(key)),
	)

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

	n.logManager.Append(newlog)
	metrics.RaftLogEntries.WithLabelValues(nodeID).Set(float64(n.logManager.GetLength()))

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
		n.CommitIdx(newlog.Id)

		duration := time.Since(startTime).Seconds()
		metrics.RaftReplicationDuration.WithLabelValues(nodeID).Observe(duration)
		metrics.RaftCommitLatency.WithLabelValues(nodeID).Observe(duration)

		getLogger().Info("Log entry successfully replicated", zap.String("component", "raft"), zap.Int64("log_id", newlog.Id), zap.Int64("commit_index", newlog.Id))

		span.SetAttributes(metrics.LogIDAttr(newlog.Id))
		return nil
	}

	metrics.RaftReplicationDuration.WithLabelValues(nodeID).Observe(time.Since(startTime).Seconds())
	span.RecordError(ErrFailedToReplicate)
	span.SetStatus(codes.Error, ErrFailedToReplicate.Error())
	return ErrFailedToReplicate
}

// given a list of `peers` send appendEntries rpc to them and return the peers
// who responded with false rpc response. The caller must them to retry
// returns nil array and error when the replication was successfull
func (n *raftNode) sendAppendEntries(peers []int64) ([]int64, error) {
	// TODO: this is incorrect log term calculation, if the previous
	// entries weren't replicated properly, ..

	failedPeers := []int64{}
	nodeID := fmt.Sprintf("%d", n.ID)

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

		peerID := fmt.Sprintf("%d", peer)
		rpcStart := time.Now()

		resp, err := n.cluster[peer].AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
			Term:            n.state.currentTerm,
			LeaderId:        n.ID,
			PrevLogIdx:      prevLogIndex,
			PrevLogTerm:     prevLogTerm,
			Entries:         n.logManager.GetLogsAfter(n.nextIndex[peer]),
			LeaderCommitIdx: n.meta.lastCommitIndex,
		})

		metrics.RaftAppendEntriesDuration.WithLabelValues(nodeID, peerID).Observe(time.Since(rpcStart).Seconds())

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

		}
	}

	if len(failedPeers) > 0 {
		return failedPeers, nil
	}

	return nil, nil
}

func (n *raftNode) startElection() {
	// Use random election timeout for each node to prevent split votes
	// Use node ID as part of the seed to ensure different nodes get different timeouts
	r := rand.New(rand.NewSource(time.Now().UnixNano() + n.ID))
	electionTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
	electionTimeOut := time.NewTicker(electionTimeout)
	nodeID := fmt.Sprintf("%d", n.ID)

	// Initialize state metrics
	metrics.RaftState.WithLabelValues(nodeID).Set(float64(Follower))
	metrics.RaftClusterSize.WithLabelValues(nodeID).Set(float64(len(n.cluster)))

	getLogger().Info("Starting election routine", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Duration("timeout", electionTimeout))

	for {
		select {
		case <-electionTimeOut.C:
			if n.meta.nt == Leader || len(n.cluster) < 2 {
				newTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
				electionTimeOut.Reset(newTimeout)
				continue
			}

			// Reset with new random timeout after this election attempt
			newTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
			electionTimeOut.Reset(newTimeout)

			// increment currentTerm
			n.state.Lock()
			n.state.currentTerm += 1
			n.state.Unlock()

			n.state.Lock()
			n.state.pastVotes[n.state.currentTerm] = n.ID
			n.state.Unlock()

			// Update metrics for candidate state
			metrics.RaftState.WithLabelValues(nodeID).Set(float64(Candidate))
			metrics.RaftTerm.WithLabelValues(nodeID).Set(float64(n.state.currentTerm))

			electionStart := time.Now()

			// transition to candiadte
			// start election
			votes := 1
			for _, node := range n.cluster {
				voteResponse, err := node.RequestVoteRPC(context.TODO(), &proto.RequestVote{
					Term:         n.state.currentTerm,
					CandidateId:  n.ID,
					LastLogIndex: n.logManager.GetLastLogID(),
					LastLogTerm:  n.logManager.GetLastLogTerm(),
				})

				if err != nil {
					continue
				}

				if voteResponse.VoteGranted {
					votes += 1
				}

			}

			// elect leader if votes granted by majority of the cluster
			// Total cluster size is peers + self
			clusterSize := len(n.cluster) + 1
			majority := clusterSize/2 + 1
			if votes >= majority {
				n.meta.Lock()
				n.meta.nt = Leader
				n.meta.Unlock()

				n.Lock()
				n.leaderID = n.ID
				n.Unlock()

				// Record election metrics
				metrics.RaftElectionsTotal.WithLabelValues(nodeID, "won").Inc()
				metrics.RaftElectionDuration.WithLabelValues(nodeID).Observe(time.Since(electionStart).Seconds())
				metrics.RaftState.WithLabelValues(nodeID).Set(float64(Leader))
				metrics.RaftLeaderID.WithLabelValues(nodeID).Set(float64(n.ID))
				metrics.RaftLeaderChangesTotal.WithLabelValues(nodeID).Inc()

				n.heartbeatCloser = make(chan struct{})

				go n.sendHeartbeats()
			} else {
				// Election lost
				metrics.RaftElectionsTotal.WithLabelValues(nodeID, "lost").Inc()
				metrics.RaftState.WithLabelValues(nodeID).Set(float64(Follower))
			}
		case entry := <-n.heartbeats:
			// TODO: apply comitted entries
			if n.meta.nt == Leader && entry.Term > n.state.currentTerm {
				// stop sending heartbeats
				close(n.heartbeatCloser)

				n.meta.Lock()
				n.meta.nt = Follower
				getLogger().Warn("Node transitioned back to follower after failed election", zap.String("component", "raft"), zap.Int64("node_id", n.ID))
				n.meta.Unlock()

				metrics.RaftState.WithLabelValues(nodeID).Set(float64(Follower))
				metrics.RaftLeaderID.WithLabelValues(nodeID).Set(float64(entry.LeaderId))
			}

			// stay as a follower or a candidate because
			// the leader is sending us the heartbeats
			// resetting the election timeout to represent
			// up-to-date ness
			newTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
			electionTimeOut.Reset(newTimeout)
			getLogger().Debug("Node received leader heartbeat", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("leader_term", entry.Term))
		}
	}
}

func (n *raftNode) sendHeartbeats() {
	if n.meta.nt != Leader {
		return
	}

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
					continue
				}
			}
		}
	}
}

func (r *Raft) GetLastAppliedLastCommitted() (int64, int64) {
	return r.n.meta.lastAppliedToSM, r.n.meta.lastCommitIndex
}

func (r *Raft) IncrementLastApplied() {
	r.n.meta.Lock()
	r.n.meta.lastAppliedToSM++
	r.n.meta.Unlock()
}

// GetDebugState returns the current node state for visualization
func (r *Raft) GetDebugState() *DebugNodeState {
	return r.n.getDebugState()
}

// GetClusterDebugState returns the cluster state for visualization
func (r *Raft) GetClusterDebugState() *DebugClusterState {
	nodeState := r.n.getDebugState()

	return &DebugClusterState{
		Nodes: map[int64]*DebugNodeState{
			r.n.ID: nodeState,
		},
		LeaderID:  r.n.leaderID,
		Term:      r.n.state.currentTerm,
		Timestamp: time.Now(),
	}
}

// getDebugState returns the current node state for visualization
func (n *raftNode) getDebugState() *DebugNodeState {
	n.RLock()
	defer n.RUnlock()

	n.state.Lock()
	defer n.state.Unlock()

	n.meta.RLock()
	defer n.meta.RUnlock()

	role := "follower"
	switch n.meta.nt {
	case Leader:
		role = "leader"
	case Candidate:
		role = "candidate"
	}

	// Build log entries with committed/applied status
	logs := n.logManager.GetLogs()
	entries := make([]DebugLogEntry, len(logs))
	for i, log := range logs {
		entries[i] = DebugLogEntry{
			ID:        log.Id,
			Term:      log.Term,
			Command:   log.Data.Op,
			Key:       log.Data.Key,
			Value:     log.Data.Value,
			Committed: log.Id <= n.meta.lastCommitIndex,
			Applied:   log.Id <= n.meta.lastAppliedToSM,
		}
	}

	state := &DebugNodeState{
		NodeID:      n.ID,
		Role:        role,
		Term:        n.state.currentTerm,
		VotedFor:    n.state.votedFor,
		CommitIndex: n.meta.lastCommitIndex,
		LastApplied: n.meta.lastAppliedToSM,
		LogEntries:  entries,
	}

	// Include leader-specific state
	if role == "leader" {
		state.NextIndex = make(map[int64]int64)
		state.MatchIndex = make(map[int64]int64)
		for k, v := range n.nextIndex {
			state.NextIndex[k] = v
		}
		for k, v := range n.matchIndex {
			state.MatchIndex[k] = v
		}
	}

	return state
}

// Debug state types for visualization
type DebugClusterState struct {
	Nodes     map[int64]*DebugNodeState `json:"nodes"`
	LeaderID  int64                     `json:"leader_id"`
	Term      int64                     `json:"term"`
	Timestamp time.Time                 `json:"timestamp"`
}

type DebugNodeState struct {
	NodeID      int64           `json:"node_id"`
	Role        string          `json:"role"`
	Term        int64           `json:"term"`
	VotedFor    int64           `json:"voted_for"`
	CommitIndex int64           `json:"commit_index"`
	LastApplied int64           `json:"last_applied"`
	LogEntries  []DebugLogEntry `json:"log_entries"`
	NextIndex   map[int64]int64 `json:"next_index,omitempty"`
	MatchIndex  map[int64]int64 `json:"match_index,omitempty"`
}

type DebugLogEntry struct {
	ID        int64  `json:"id"`
	Term      int64  `json:"term"`
	Command   string `json:"command"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Committed bool   `json:"committed"`
	Applied   bool   `json:"applied"`
}
