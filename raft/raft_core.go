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
	"math/rand"

	"sync"
	"time"

	"harmonydb/raft/proto"
	"harmonydb/wal"

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

func newRaftNode(logger *zap.Logger) *raftNode {
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
		logger:     logger,
	}
}

func (n *raftNode) CommitIdx(idx int64) {
	n.meta.Lock()
	oldCommitIndex := n.meta.lastCommitIndex
	n.meta.lastCommitIndex = idx
	n.meta.Unlock()

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
	n.leaderID = in.LeaderId
	n.Unlock()

	// just a heartbeat
	if len(in.Entries) == 0 {
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

	n.logger.Info("Starting AppendEntries RPC", zap.String("component", "raft"), zap.Int64("sender", in.LeaderId), zap.String("key", string(in.Entries[0].Data.Key)), zap.String("value", string(in.Entries[0].Data.Value)))

	// revert to a follower state if we get a term number higher than ours
	if n.state.currentTerm < in.Term {
		n.state.currentTerm = in.Term
		n.meta.Lock()
		if n.meta.nt == Leader {
			n.meta.nt = Follower
			n.logger.Warn("Leader transitioned back to follower", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("new_term", in.Term))
		}
		n.meta.Unlock()
	}

	// reject a stale term request
	// the CurrentTerm value we send as part of the response is read by the requester
	// and update their own CurrentTerm
	if n.state.currentTerm > in.Term {
		n.logger.Error("Rejecting append entries due to stale term", zap.String("component", "raft"), zap.Int64("current_term", n.state.currentTerm), zap.Int64("incoming_term", in.Term))
		return &proto.AppendEntriesResponse{
			Success:     false,
			CurrentTerm: n.state.currentTerm,
		}, nil
	}

	// there are gaps in our logs as those compared to the leader's
	if in.PrevLogIdx > n.logManager.GetLastLogID() {
		n.logger.Error("Incoming log ahead of local max", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_max", n.logManager.GetLastLogID()))
		return &proto.AppendEntriesResponse{
			Success: false,
		}, nil
	}

	// make sure everything is consistent upto PrevLogIdx, PrevLogTerm
	if in.PrevLogIdx > 0 {
		if n.logManager.GetLog(int(in.PrevLogIdx)).GetTerm() != in.PrevLogTerm {
			weird := n.logManager.GetLog(int(in.PrevLogIdx))
			n.logger.Error("Inconsistent log state", zap.String("component", "raft"), zap.Int64("prev_log_idx", in.PrevLogIdx), zap.Int64("local_term", weird.GetTerm()), zap.Int64("prev_log_term", in.PrevLogTerm))
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
				n.logger.Debug("Skipping existing entry", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id))
				continue
			}
		}

		n.logger.Info("New log entry added", zap.String("component", "raft"), zap.Int64("entry_id", entry.Id), zap.Int64("term", entry.Term))
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

	return &proto.AppendEntriesResponse{
		CurrentTerm: n.state.currentTerm,
		Success:     true,
	}, nil
}

// handles incoming election requests
func (n *raftNode) RequestVoteRPC(ctx context.Context, in *proto.RequestVote) (*proto.RequestVoteResponse, error) {
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
func (n *raftNode) replicate(ctx context.Context, key, val []byte, requestID uint64) error {
	n.state.Lock()
	currentTerm := n.state.currentTerm
	n.state.Unlock()

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

	peers := []int64{}
	for k := range n.cluster {
		peers = append(peers, k)
	}

	replicationRounds := 0
	for true {
		var err error
		replicationRounds++

		peers, err = n.sendAppendEntries(peers)
		if peers == nil && err == nil {
			break
		}
	}

	replStatus := 0
	peerStatuses := make(map[int64]bool)

	for peer := range n.cluster {
		idx := n.matchIndex[peer]
		if idx == newlog.Id {
			replStatus += 1
			peerStatuses[peer] = true
		} else {
			peerStatuses[peer] = false
		}
	}

	requiredReplicas := len(n.cluster)/2 + 1

	if replStatus >= requiredReplicas {
		// log has been successfully applied to the state machine,
		// now commit this log.
		n.CommitIdx(newlog.Id)

		n.logger.Info("Log entry successfully replicated", zap.String("component", "raft"), zap.Int64("log_id", newlog.Id), zap.Int64("commit_index", newlog.Id))

		return nil
	}

	return ErrFailedToReplicate
}

// given a list of `peers` send appendEntries rpc to them and return the peers
// who responded with false rpc response. The caller must them to retry
// returns nil array and error when the replication was successfull
func (n *raftNode) sendAppendEntries(peers []int64) ([]int64, error) {
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

		n.logger.Debug("Sending AppendEntries RPC", zap.String("component", "raft"), zap.Int64("prev_log_index", prevLogIndex), zap.Int64("prev_log_term", prevLogTerm))

		resp, err := n.cluster[peer].AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
			Term:            n.state.currentTerm,
			LeaderId:        n.ID,
			PrevLogIdx:      prevLogIndex,
			PrevLogTerm:     prevLogTerm,
			Entries:         n.logManager.GetLogsAfter(n.nextIndex[peer]),
			LeaderCommitIdx: n.meta.lastCommitIndex,
		})

		if err != nil {
			failedPeers = append(failedPeers, peer)
			n.logger.Debug("Peer failed to respond", zap.String("component", "raft"), zap.Int64("peer_id", peer))
		} else {
			// this response means the node is telling us that the log
			// consistency check has failed, so we decrement our nextIndex
			// here
			if !resp.Success && resp.CurrentTerm == 0 {
				n.nextIndex[peer] -= 1
				n.logger.Debug("Log match failed, decremented next index", zap.String("component", "raft"), zap.Int64("peer_id", peer))
				failedPeers = append(failedPeers, peer)
			}

			if resp.CurrentTerm > n.state.currentTerm {
				// transition back to the follower state as we have encountered a term higher
				// than ours indicating a potential leader
				n.state.currentTerm = resp.CurrentTerm
				n.meta.Lock()
				if n.meta.nt == Leader {
					n.meta.nt = Follower
					n.logger.Warn("Leader transitioned back to follower during replication", zap.String("component", "raft"), zap.Int64("node_id", n.ID))
				}
				n.meta.Unlock()
			}

			if resp.Success {
				n.matchIndex[peer] += 1
				n.nextIndex[peer] += 2
				n.logger.Info("Successful RPC, advancing match and next indexes", zap.String("component", "raft"))
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
	electionTimeout := time.Duration(250+r.Intn(1000)) * time.Millisecond
	electionTimeOut := time.NewTicker(electionTimeout)
	n.logger.Info("Starting election routine", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Duration("timeout", electionTimeout))

	for {
		select {
		case <-electionTimeOut.C:
			if n.meta.nt == Leader || len(n.cluster) < 2 {
				newTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
				electionTimeOut.Reset(newTimeout)
				continue
			}

			// Reset with new random timeout after this election attempt
			newTimeout := time.Duration(250+r.Intn(1000)) * time.Millisecond
			electionTimeOut.Reset(newTimeout)

			n.logger.Debug("Election timeout triggered - starting new election",
				zap.String("component", "raft"),
				zap.String("operation", "election"),
				zap.Int64("node_id", n.ID),
				zap.Int64("current_term", n.state.currentTerm))

			// increment currentTerm
			n.state.Lock()
			n.state.currentTerm += 1
			n.state.pastVotes[n.state.currentTerm] = n.ID
			newTerm := n.state.currentTerm
			n.state.Unlock()

			n.logger.Debug("Incremented term and voting for self",
				zap.String("component", "raft"),
				zap.String("operation", "election"),
				zap.Int64("node_id", n.ID),
				zap.Int64("new_term", newTerm))

			// start election
			votes := 1
			rejections := 0
			errors := 0

			for nodeID, node := range n.cluster {
				n.logger.Debug("Requesting vote from peer",
					zap.String("component", "raft"),
					zap.String("operation", "election"),
					zap.Int64("node_id", n.ID),
					zap.Int64("peer_id", nodeID),
					zap.Int64("term", newTerm))

				voteResponse, err := node.RequestVoteRPC(context.TODO(), &proto.RequestVote{
					Term:         newTerm,
					CandidateId:  n.ID,
					LastLogIndex: n.logManager.GetLastLogID(),
					LastLogTerm:  n.logManager.GetLastLogTerm(),
				})

				// TODO: exponential backoff
				if err != nil {
					errors++
					n.logger.Debug("Vote request failed",
						zap.String("component", "raft"),
						zap.String("operation", "election"),
						zap.Int64("node_id", n.ID),
						zap.Int64("peer_id", nodeID),
						zap.Error(err))
					continue
				}

				if voteResponse.VoteGranted {
					votes++
					n.logger.Debug("Vote granted by peer",
						zap.String("component", "raft"),
						zap.String("operation", "election"),
						zap.Int64("node_id", n.ID),
						zap.Int64("peer_id", nodeID),
						zap.Int64("term", newTerm))
				} else {
					rejections++
					n.logger.Debug("Vote rejected by peer",
						zap.String("component", "raft"),
						zap.String("operation", "election"),
						zap.Int64("node_id", n.ID),
						zap.Int64("peer_id", nodeID),
						zap.Int64("term", newTerm),
						zap.Int64("peer_term", voteResponse.Term))
				}
			}

			majority := len(n.cluster)/2 + 1
			n.logger.Debug("Election voting completed",
				zap.String("component", "raft"),
				zap.String("operation", "election"),
				zap.Int64("node_id", n.ID),
				zap.Int64("term", newTerm),
				zap.Int("votes_received", votes),
				zap.Int("votes_needed", majority),
				zap.Int("rejections", rejections),
				zap.Int("errors", errors))

			// elect leader if votes granted by more than half of the nodes
			// present in the cluster
			if votes > len(n.cluster)/2 {
				n.meta.Lock()
				n.meta.nt = Leader
				n.meta.Unlock()

				n.Lock()
				n.leaderID = n.ID
				n.Unlock()

				n.logger.Info("Successfully elected as leader",
					zap.String("component", "raft"),
					zap.String("operation", "election"),
					zap.Int64("node_id", n.ID),
					zap.Int64("term", newTerm),
					zap.Int("votes_received", votes),
					zap.Int("cluster_size", len(n.cluster)+1))

				n.heartbeatCloser = make(chan struct{})
				go n.sendHeartbeats()
			} else {
				n.logger.Debug("Election failed - insufficient votes",
					zap.String("component", "raft"),
					zap.String("operation", "election"),
					zap.Int64("node_id", n.ID),
					zap.Int64("term", newTerm),
					zap.Int("votes_received", votes),
					zap.Int("votes_needed", majority))
			}

		case entry := <-n.heartbeats:
			// TODO: apply comitted entries
			if n.meta.nt == Leader && entry.Term > n.state.currentTerm {
				n.logger.Debug("Stepping down as leader due to higher term heartbeat",
					zap.String("component", "raft"),
					zap.String("operation", "election"),
					zap.Int64("node_id", n.ID),
					zap.Int64("current_term", n.state.currentTerm),
					zap.Int64("heartbeat_term", entry.Term))

				// stop sending heartbeats
				close(n.heartbeatCloser)

				n.meta.Lock()
				n.meta.nt = Follower
				n.logger.Warn("Node transitioned back to follower after failed election", zap.String("component", "raft"), zap.Int64("node_id", n.ID))
				n.meta.Unlock()
			}

			// stay as a follower or a candidate because
			// the leader is sending us the heartbeats
			// resetting the election timeout to represent
			// up-to-date ness
			newTimeout := time.Duration(150+r.Intn(150)) * time.Millisecond
			electionTimeOut.Reset(newTimeout)
			// n.logger.Debug("Node received leader heartbeat", zap.String("component", "raft"), zap.Int64("node_id", n.ID), zap.Int64("leader_term", entry.Term))
		}
	}
}

func (n *raftNode) sendHeartbeats() {
	n.logger.Debug("Starting heartbeat routine",
		zap.String("component", "raft"),
		zap.String("operation", "heartbeat"),
		zap.Int64("node_id", n.ID),
		zap.Duration("interval", heartbeatTO),
		zap.Int("cluster_size", len(n.cluster)))

	heartbeats := time.NewTicker(heartbeatTO)

	for {
		select {
		case _, ok := <-n.heartbeatCloser:
			if !ok {
				return
			}

		case <-heartbeats.C:
			n.state.Lock()
			currentTerm := n.state.currentTerm
			n.state.Unlock()

			n.meta.RLock()
			commitIndex := n.meta.lastCommitIndex
			n.meta.RUnlock()

			lastLogID := n.logManager.GetLastLogID()
			lastLogTerm := n.logManager.GetLastLogTerm()

			successfulHeartbeats := 0
			failedHeartbeats := 0

			for _, node := range n.cluster {
				// Since this is a heartbeat message, we only need
				// to send term and leader id to maintain the leader status
				_, err := node.AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
					Term:            currentTerm,
					LeaderId:        n.ID,
					PrevLogIdx:      lastLogID,
					PrevLogTerm:     lastLogTerm,
					LeaderCommitIdx: commitIndex,
					// We don't need to send `Entries` array here.
				})

				if err != nil {
					failedHeartbeats++
					continue
				}

				successfulHeartbeats++
			}
		}
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
