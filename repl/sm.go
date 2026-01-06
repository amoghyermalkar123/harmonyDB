package repl

import (
	"fmt"
	proto "harmonydb/repl/proto/repl"
	"log/slog"
)

// a no i/o pure fsm implementation, caller should take care of io (disk, network)
// typical usage would be: Tick -> Step -> Ready -> Advance
type RaftSM interface {
	Tick()
	Step(m *proto.Message) error
	Ready() <-chan Ready
	Advance()
	// Campaign()
}

type State int

const (
	_ State = iota

	StateFollower
	StateCandidate
	StateLeader
)

// HardState denotes the metadata for the Raft FSM that the caller
// should persist to durable storage so that bootstrapping and rejoining
// the cluster in case of any crashes is reliable
type HardState struct {
	term             int64
	lastCommitIndex  int64
	lastAppliedIndex int64
}

// Ready is the Input/Output boundary between the pure-fsm implementation
// and the code that wraps it to perform I/O
type Ready struct {
	// Messages to send immediately (votes, heartbeats, etc.)
	Messages []*proto.Message
	// Unstable log entries that need WAL persistence
	// These come from raftLog.unstableEntries()
	Entries []*proto.Log
	// Messages to send AFTER persisting Entries to WAL
	MessagesAfterAppend []*proto.Message
	// entries to apply to state machine
	// caller is responsible to persist this in the
	// primary storage (typically a KV)
	// these entries are deemed safe to apply
	// as per the raft protocol, i.e. replicated on the cluster
	// and committed to WAL
	CommittedEntries []*proto.Log
	// persistent state to save, point in time data
	// caller is responsible to persist this
	HardState HardState
}

// A pure FSM, no I/O implementation of the consensus protocol
type Raft struct {
	ID          int64
	currentTerm int64
	votedFor    map[int64]int64
	state       State
	// logical clock used for timeouts such as election timeouts
	// on followers and candidates and heartbeats for leaders
	randomizedTimeout int
	electionTick      int
	heartbeatTick     int

	// configuration
	electionTimeout  int            // base election timeout in ticks
	heartbeatTimeout int            // heartbeat interval in ticks
	clusterSize      int            // total nodes including self
	peers            map[int64]bool // peer IDs (excluding self)

	// based on the state this Raft is in, the step function changes
	// i.e. different for follower, candidate, leader
	// update atomically
	step stepFunc
	// tick function
	tick func()

	// vote tracking (only non-nil in Candidate state)
	votes map[int64]bool

	// message buffers
	msgs            []*proto.Message // messages that don't need WAL persistence (votes, responses, etc.)
	msgsAfterAppend []*proto.Message // messages to send after WAL persistence

	// ready channel
	readyc chan Ready

	// the actual raft log (unstable)
	raftLog *raftLog

	progress map[int64]Progress
}

// Progress tracks the log replication progress per-peer
// only tracked  on a leader
type Progress struct {
	matchIndex int64
	nextIndex  int64
}

type stepFunc func(in *proto.Message) error

func NewRaft(id int64, peers []int64) *Raft {
	peerMap := make(map[int64]bool)
	for _, p := range peers {
		peerMap[p] = true
	}

	r := &Raft{
		ID:               id,
		currentTerm:      0,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 1,
		clusterSize:      len(peers) + 1,
		peers:            peerMap,
		votes:            nil,
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		readyc:           make(chan Ready, 1),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
	}

	r.becomeFollower(0)
	// Add randomization based on node ID to prevent all nodes from starting election simultaneously
	r.electionTick = int(id) % r.electionTimeout
	return r
}

func (r *Raft) Ready() <-chan Ready {
	// Only send Ready if there's something to report
	hasMessages := len(r.msgs) > 0
	hasMsgsAfterAppend := len(r.msgsAfterAppend) > 0
	hasUnstable := r.raftLog.stableIndex < r.raftLog.lastIndex()
	hasCommittedEntries := r.raftLog.commitIndex > r.raftLog.lastApplied

	if hasMessages || hasMsgsAfterAppend || hasUnstable || hasCommittedEntries {
		rd := Ready{
			Messages:            r.msgs,
			Entries:             r.raftLog.volatileEntries(),
			MessagesAfterAppend: r.msgsAfterAppend,
			CommittedEntries:    r.raftLog.committedEntries(),
			HardState: HardState{
				term:             r.currentTerm,
				lastCommitIndex:  r.raftLog.commitIndex,
				lastAppliedIndex: r.raftLog.lastApplied,
			},
		}

		// Clear the buffers since they're now in Ready
		r.msgs = nil
		r.msgsAfterAppend = nil

		// Send on channel (buffered, won't block)
		r.readyc <- rd
	}

	return r.readyc
}

func (r *Raft) Advance() {
	// Mark unstable entries as stable (they've been persisted to WAL)
	if r.raftLog.lastIndex() > r.raftLog.stableIndex {
		r.raftLog.markStableTill(r.raftLog.lastIndex())
	}

	// Mark committed entries as applied
	if r.raftLog.commitIndex > r.raftLog.lastApplied {
		r.raftLog.markAppliedTill(r.raftLog.commitIndex)
	}
}

func (r *Raft) Tick() {
	r.tick()
}

// Step is an incoming message processor which advances the state machine with that message
// the results are available as part of `Ready` and it's important for the caller to call
// `Advance` once a particular Ready is consumed.
func (r *Raft) Step(m *proto.Message) error {
	// Delegate to state-specific step function
	if r.step == nil {
		panic("step function cannot be nil!")
	}

	return r.step(m)
}

// handleAppendEntries processes AppendEntries RPC from leader
func (r *Raft) handleAppendEntries(m *proto.Message) {
	ae := m.GetEntries()
	if ae == nil {
		return
	}

	// Reply false if term < currentTerm
	if ae.GetTerm() < r.currentTerm {
		r.sendAppendResponse(m.GetFrom(), false)
		return
	}

	// If term is higher, update and step down
	if ae.GetTerm() > r.currentTerm {
		r.becomeFollower(ae.GetTerm())
	}

	// Check log consistency: reply false if log doesn't contain entry at prevLogIndex with prevLogTerm
	prevLogIdx := ae.GetPrevLogIdx()
	prevLogTerm := ae.GetPrevLogTerm()

	if prevLogIdx > 0 && (prevLogIdx > r.raftLog.lastIndex() || r.raftLog.term(prevLogIdx) != prevLogTerm) {
		r.sendAppendResponse(m.GetFrom(), false)
		return
	}

	// Append new entries (if any)
	entries := ae.GetEntries()
	if len(entries) > 0 {
		r.raftLog.append(entries...)
	}

	// Update commit index
	leaderCommitIdx := ae.GetLeaderCommitIdx()
	if leaderCommitIdx > r.raftLog.commitIndex {
		r.raftLog.maybeCommit(min(leaderCommitIdx, r.raftLog.lastIndex()))
	}

	// Send response AFTER entries are persisted (if any were appended)
	if len(entries) > 0 {
		r.sendAppendResponseAfterPersist(m.GetFrom(), true)
	}
}

// sendAppendResponse sends an AppendEntries response immediately
func (r *Raft) sendAppendResponse(to int64, success bool) {
	resp := &proto.Message{
		Type: proto.MessageType_MSG_APP_RESP,
		To:   to,
		From: r.ID,
		EntriesResp: &proto.AppendEntriesResponse{
			CurrentTerm: r.currentTerm,
			Success:     success,
		},
	}
	r.msgs = append(r.msgs, resp)
}

// sendAppendResponseAfterPersist queues response to send after WAL persistence
func (r *Raft) sendAppendResponseAfterPersist(to int64, success bool) {
	resp := &proto.Message{
		Type: proto.MessageType_MSG_APP_RESP,
		To:   to,
		From: r.ID,
		EntriesResp: &proto.AppendEntriesResponse{
			CurrentTerm: r.currentTerm,
			Success:     success,
		},
	}
	r.msgsAfterAppend = append(r.msgsAfterAppend, resp)
}

// handleAppendResponse processes AppendEntries response from followers
func (r *Raft) handleAppendResponse(m *proto.Message) {
	resp := m.GetEntriesResp()
	if resp == nil {
		return
	}

	peerID := m.GetFrom()

	// If response has higher term, step down
	if resp.GetCurrentTerm() > r.currentTerm {
		r.becomeFollower(resp.GetCurrentTerm())
		return
	}

	// Ignore stale responses
	if resp.GetCurrentTerm() < r.currentTerm {
		return
	}

	if resp.GetSuccess() {
		// Update progress for this peer
		prog := r.progress[peerID]
		prog.matchIndex = r.raftLog.lastIndex()
		prog.nextIndex = prog.matchIndex + 1
		r.progress[peerID] = prog

		// Try to advance commitIndex based on majority replication
		// this is called maybeCommit because it has quorum calculation logic
		// which we call everytime this function is called which is for per-peer response
		// so it's an optional advancement because after each peer's response we update the
		// progress tracker and based on that check if quorum is achieved or not, at some
		// point after receiving a peer's response, we might reach consensus at which point
		// the commit index is updated, since this is conditional, hence the name `maybeCommit`
		r.maybeCommit()
	} else {
		// Append failed, decrement nextIndex and retry
		prog := r.progress[peerID]
		if prog.nextIndex > 1 {
			prog.nextIndex--
		}
		r.progress[peerID] = prog
		// TODO: resend AppendEntries with updated nextIndex
	}
}

// maybeCommit tries to advance commitIndex if a majority has replicated entries
// this should only be called when the node is leader, automatically handled in `step`
func (r *Raft) maybeCommit() {
	// Find the highest index that is replicated on a majority
	// Start from commitIndex + 1 and go up to lastIndex
	for index := r.raftLog.commitIndex + 1; index <= r.raftLog.lastIndex(); index++ {
		// Count how many nodes have this index
		replicatedCount := 1 // leader always has it

		for _, prog := range r.progress {
			if prog.matchIndex >= index {
				replicatedCount++
			}
		}

		// Check if we have a majority
		quorum := r.clusterSize/2 + 1
		if replicatedCount >= quorum && r.raftLog.term(index) == r.currentTerm {
			r.raftLog.maybeCommit(index)
		}
	}
}

// handleVoteRequest processes vote requests from other candidates
func (r *Raft) handleVoteRequest(m *proto.Message) {
	voteReq := m.GetVote()
	if voteReq == nil {
		return
	}

	reqTerm := int64(voteReq.GetTerm())
	candidateID := voteReq.GetCandidateId()

	// Reject if requester's term is stale
	if reqTerm < r.currentTerm {
		r.sendVoteResponse(candidateID, false, r.currentTerm)
		return
	}

	// If requester has higher term, update our term and step down
	if reqTerm > r.currentTerm {
		r.becomeFollower(reqTerm)
	}

	// Check if we already voted in this term
	if votedFor, alreadyVoted := r.votedFor[reqTerm]; alreadyVoted {
		if votedFor != candidateID {
			// Already voted for someone else
			r.sendVoteResponse(candidateID, false, r.currentTerm)
			return
		}
		// Already voted for this candidate, grant again (idempotent)
	}

	// TODO(log-matching-property): Check log up-to-dateness
	// For now, just grant the vote

	// Grant vote
	r.votedFor[reqTerm] = candidateID
	r.sendVoteResponse(candidateID, true, r.currentTerm)
}

// sendVoteResponse sends a vote response message
func (r *Raft) sendVoteResponse(to int64, granted bool, term int64) {
	respTerm := int64(term)
	resp := &proto.Message{
		Type: proto.MessageType_MSG_VOTE_RESP,
		To:   to,
		From: r.ID,
		VoteResp: &proto.RequestVoteResponse{
			Term:        respTerm,
			VoteGranted: granted,
		},
	}
	r.msgs = append(r.msgs, resp)
}

// handleVoteResponse processes vote responses and checks for quorum
func (r *Raft) handleVoteResponse(m *proto.Message) {
	voteResp := m.GetVoteResp()
	if voteResp == nil {
		return
	}

	respTerm := voteResp.GetTerm()
	peerID := m.GetFrom()

	// Ignore stale responses
	if respTerm < r.currentTerm {
		return
	}

	// If response has higher term, step down
	if respTerm > r.currentTerm {
		r.becomeFollower(respTerm)
		return
	}

	// Record the vote from this peer
	r.votes[peerID] = voteResp.GetVoteGranted()

	// Count granted votes
	granted := 0
	for _, isGranted := range r.votes {
		if isGranted {
			granted++
		}
	}

	// Check for quorum
	quorum := r.clusterSize/2 + 1
	if granted >= quorum {
		r.becomeLeader()
	}
}

func (r *Raft) isLead() bool {
	return r.state == StateLeader
}

func (r *Raft) replicate(m *proto.Message) error {
	if !r.isLead() {
		return fmt.Errorf("node:%d is not a leader", r.ID)
	}

	// Get entries from the proposal message
	entries := m.GetEntries().GetEntries()
	if len(entries) == 0 {
		return nil
	}

	// Assign term and sequential IDs to new entries
	nextID := r.raftLog.lastIndex() + 1
	for _, entry := range entries {
		entry.Term = r.currentTerm
		entry.Id = nextID
		nextID++
	}

	// Append entries to leader's local log
	r.raftLog.append(entries...)

	// Generate AppendEntries messages for all peers
	// These will be sent AFTER persisting their embedded entries to WAL
	for peerID := range r.peers {
		prog := r.progress[peerID]

		// prevLogIdx should be nextIndex - 1
		prevLogIdx := prog.nextIndex - 1
		prevLogTerm := int64(0)
		if prevLogIdx > 0 {
			prevLogTerm = r.raftLog.term(prevLogIdx)
		}

		// Slice entries based on peer's nextIndex
		// Only send entries from nextIndex onwards
		var entriesToSend []*proto.Log
		for i := prog.nextIndex; i <= r.raftLog.lastIndex(); i++ {
			if int(i) < len(r.raftLog.entries) {
				entriesToSend = append(entriesToSend, r.raftLog.entries[i])
			}
		}

		appendMsg := &proto.Message{
			Type: proto.MessageType_MSG_APP,
			To:   peerID,
			From: r.ID,
			Entries: &proto.AppendEntries{
				Term:            r.currentTerm,
				LeaderId:        r.ID,
				PrevLogIdx:      prevLogIdx,
				PrevLogTerm:     prevLogTerm,
				Entries:         entriesToSend,
				LeaderCommitIdx: r.raftLog.commitIndex,
			},
		}

		r.msgsAfterAppend = append(r.msgsAfterAppend, appendMsg)
	}

	return nil
}

func (r *Raft) campaign() {
	// Transition to candidate state
	r.becomeCandidate()

	// Increment term
	r.currentTerm++
	// vote for yourself for the new term
	r.votedFor[r.currentTerm] = r.ID
	// update the current vote tracker
	r.votes[r.ID] = true

	slog.Info("starting campaign", "node_id", r.ID, "term", r.currentTerm)

	// Single node cluster - become leader immediately
	if r.clusterSize == 1 {
		r.becomeLeader()
		return
	}

	// Generate RequestVote messages for all peers
	for peerID := range r.peers {
		term := uint64(r.currentTerm)
		voteMsg := &proto.Message{
			Type: proto.MessageType_MSG_VOTE,
			To:   peerID,
			From: r.ID,
			Vote: &proto.RequestVote{
				Term:         &term,
				CandidateId:  r.ID,
				LastLogIndex: r.raftLog.lastIndex(),
				LastLogTerm:  r.raftLog.lastTerm(),
			},
		}

		r.msgs = append(r.msgs, voteMsg)
	}
}

func (r *Raft) stepFollower(m *proto.Message) error {
	switch m.Type {
	case proto.MessageType_MSG_VOTE:
		// Check if this is internal trigger or external vote request
		voteReq := m.GetVote()
		if voteReq == nil {
			// Internal trigger to start election
			r.campaign()
		} else {
			// External vote request from another candidate
			r.handleVoteRequest(m)
		}
	case proto.MessageType_MSG_APP, proto.MessageType_MSG_HEARTBEAT:
		// AppendEntries from leader - reset election timer
		r.electionTick = 0
		r.handleAppendEntries(m)
	}
	return nil
}

func (r *Raft) stepCandidate(m *proto.Message) error {
	switch m.Type {
	case proto.MessageType_MSG_VOTE:
		voteReq := m.GetVote()
		if voteReq == nil {
			// Internal trigger - election timeout, restart campaign
			r.campaign()
		} else {
			// External vote request from another candidate
			r.handleVoteRequest(m)
		}
	case proto.MessageType_MSG_VOTE_RESP:
		r.handleVoteResponse(m)
	case proto.MessageType_MSG_APP, proto.MessageType_MSG_HEARTBEAT:
		// AppendEntries from leader - reset election timer
		r.electionTick = 0
		r.handleAppendEntries(m)
	}
	return nil
}

func (r *Raft) stepLeader(m *proto.Message) error {
	switch m.Type {
	case proto.MessageType_MSG_HEARTBEAT:
		r.broadcastHeartbeat()
	case proto.MessageType_MSG_APP:
		// Replicate log entries
		if err := r.replicate(m); err != nil {
			return fmt.Errorf("replicate: %w", err)
		}
	case proto.MessageType_MSG_APP_RESP:
		r.handleAppendResponse(m)
	}

	return nil
}

func (r *Raft) broadcastHeartbeat() {
	commitIndex := r.raftLog.commitIndex

	// Send heartbeat (empty AppendEntries) to all peers
	for peerID := range r.peers {
		// For now, send the same prevLogIdx to all peers
		prevLogIdx := r.progress[peerID].matchIndex
		prevLogTerm := r.raftLog.term(prevLogIdx)

		heartbeat := &proto.Message{
			Type: proto.MessageType_MSG_APP,
			To:   peerID,
			From: r.ID,
			Entries: &proto.AppendEntries{
				Term:            r.currentTerm,
				LeaderId:        r.ID,
				PrevLogIdx:      prevLogIdx,
				PrevLogTerm:     prevLogTerm,
				Entries:         nil, // empty for heartbeat
				LeaderCommitIdx: commitIndex,
			},
		}
		r.msgs = append(r.msgs, heartbeat)
	}
}

func (r *Raft) becomeFollower(term int64) {
	r.state = StateFollower
	r.currentTerm = term
	r.step = r.stepFollower
	r.tick = r.tickFollower
	r.electionTick = 0
	// Set randomized timeout to prevent split votes
	if r.electionTimeout > 0 {
		r.randomizedTimeout = r.electionTimeout + (r.electionTick % r.electionTimeout)
	}
	r.votes = nil // clear any vote tracking
	slog.Info("became follower", "node_id", r.ID, "term", term)
}

func (r *Raft) becomeCandidate() {
	r.state = StateCandidate
	r.step = r.stepCandidate
	r.tick = r.tickCandidate
	r.electionTick = 0
	// Set randomized timeout for election
	if r.electionTimeout > 0 {
		r.randomizedTimeout = r.electionTimeout + (r.electionTick % r.electionTimeout)
	}
	r.votes = make(map[int64]bool) // initialize vote tracking
}

func (r *Raft) becomeLeader() {
	r.state = StateLeader
	r.step = r.stepLeader
	r.tick = r.tickLeader
	r.heartbeatTick = 0
	r.votes = nil // clear vote tracking

	// Initialize progress tracking for all peers
	lastIndex := r.raftLog.lastIndex()
	for peerID := range r.peers {
		r.progress[peerID] = Progress{
			matchIndex: 0,
			nextIndex:  lastIndex + 1,
		}
	}
	slog.Info("became leader", "node_id", r.ID, "term", r.currentTerm)
}

func (r *Raft) tickFollower() {
	r.electionTick++

	if r.electionTick >= r.randomizedTimeout {
		r.electionTick = 0
		// Trigger election by stepping with internal MSG_VOTE
		if err := r.Step(&proto.Message{Type: proto.MessageType_MSG_VOTE}); err != nil {
			slog.Error("error occurred during election", "node_id", r.ID, "error", err)
		}
	}
}

func (r *Raft) tickCandidate() {
	r.electionTick++

	if r.electionTick >= r.randomizedTimeout {
		r.electionTick = 0
		// Election timeout - restart campaign
		if err := r.Step(&proto.Message{Type: proto.MessageType_MSG_VOTE}); err != nil {
			fmt.Printf("error occurred during election: %v\n", err)
		}
	}
}

func (r *Raft) tickLeader() {
	r.heartbeatTick++

	if r.heartbeatTick >= r.heartbeatTimeout {
		r.heartbeatTick = 0
		// Trigger heartbeat send
		if err := r.Step(&proto.Message{Type: proto.MessageType_MSG_HEARTBEAT}); err != nil {
			fmt.Printf("error occurred during heartbeat: %v\n", err)
		}
	}
}
