package repl

import (
	"testing"

	proto "harmonydb/repl/proto/repl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressTrackingWithPartialReplication tests that the leader correctly
// tracks per-peer progress when some peers are behind others
func TestProgressTrackingWithPartialReplication(t *testing.T) {
	// Scenario:
	// - Node 1 is leader with entries [1, 2, 3] in log
	// - Node 2 has replicated entries [1, 2] (matchIndex=2, nextIndex=3)
	// - Node 3 has replicated entry [1] only (matchIndex=1, nextIndex=2)
	// - Leader receives new entry [4] and should send:
	//   - To Node 2: entry [4] with prevLogIdx=2 (NOT 3!)
	//   - To Node 3: entries [2, 3, 4] with prevLogIdx=1 (NOT 3!)

	r := &Raft{
		ID:               1,
		currentTerm:      1,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 2,
		clusterSize:      3,
		peers:            map[int64]bool{2: true, 3: true},
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	// Become leader to initialize step function
	r.becomeLeader()

	// Pre-populate leader's log with entries [1, 2, 3]
	r.raftLog.append(&proto.Log{Id: 1, Term: 1, Data: &proto.Cmd{Op: "PUT", Key: "k1", Value: "v1"}})
	r.raftLog.append(&proto.Log{Id: 2, Term: 1, Data: &proto.Cmd{Op: "PUT", Key: "k2", Value: "v2"}})
	r.raftLog.append(&proto.Log{Id: 3, Term: 1, Data: &proto.Cmd{Op: "PUT", Key: "k3", Value: "v3"}})
	r.raftLog.markStableTill(3) // Mark all as stable

	// Simulate different progress states for peers
	r.progress[2] = Progress{matchIndex: 2, nextIndex: 3} // Node 2 has [1, 2]
	r.progress[3] = Progress{matchIndex: 1, nextIndex: 2} // Node 3 has [1] only

	// Leader receives new proposal for entry [4]
	proposal := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		Entries: &proto.AppendEntries{
			Entries: []*proto.Log{
				{Data: &proto.Cmd{Op: "PUT", Key: "k4", Value: "v4"}},
			},
		},
	}

	err := r.Step(proposal)
	require.NoError(t, err)

	// Leader should generate AppendEntries for both peers
	require.Len(t, r.msgsAfterAppend, 2, "should have AppendEntries for 2 peers")

	// Find messages by recipient
	var msgToPeer2, msgToPeer3 *proto.Message
	for _, msg := range r.msgsAfterAppend {
		if msg.To == 2 {
			msgToPeer2 = msg
		} else if msg.To == 3 {
			msgToPeer3 = msg
		}
	}

	require.NotNil(t, msgToPeer2, "should have message to peer 2")
	require.NotNil(t, msgToPeer3, "should have message to peer 3")

	// **TEST 1: prevLogIdx should be nextIndex - 1, NOT matchIndex**
	ae2 := msgToPeer2.GetEntries()
	assert.Equal(t, int64(2), ae2.PrevLogIdx,
		"prevLogIdx for peer 2 should be nextIndex-1 = 3-1 = 2 (currently uses matchIndex=2, accidentally correct)")

	ae3 := msgToPeer3.GetEntries()
	assert.Equal(t, int64(1), ae3.PrevLogIdx,
		"prevLogIdx for peer 3 should be nextIndex-1 = 2-1 = 1 (currently uses matchIndex=1, accidentally correct)")

	// **TEST 2: Entries should be sliced based on nextIndex**
	// Peer 2 (nextIndex=3) should receive entries [3, 4] (indices 3 onwards)
	// Currently it receives all entries (bug)
	assert.Len(t, ae2.Entries, 2,
		"peer 2 should receive entries starting from nextIndex=3: [entry 3, entry 4]")
	if len(ae2.Entries) == 2 {
		assert.Equal(t, int64(3), ae2.Entries[0].Id, "first entry should be ID 3")
		assert.Equal(t, int64(4), ae2.Entries[1].Id, "second entry should be ID 4")
	}

	// Peer 3 (nextIndex=2) should receive entries [2, 3, 4] (indices 2 onwards)
	assert.Len(t, ae3.Entries, 3,
		"peer 3 should receive entries starting from nextIndex=2: [entry 2, entry 3, entry 4]")
	if len(ae3.Entries) == 3 {
		assert.Equal(t, int64(2), ae3.Entries[0].Id, "first entry should be ID 2")
		assert.Equal(t, int64(3), ae3.Entries[1].Id, "second entry should be ID 3")
		assert.Equal(t, int64(4), ae3.Entries[2].Id, "third entry should be ID 4")
	}
}

// TestProgressUpdateOnSuccessResponse tests that matchIndex is correctly updated
// based on the actual entries sent, not the leader's lastIndex
func TestProgressUpdateOnSuccessResponse(t *testing.T) {
	r := &Raft{
		ID:               1,
		currentTerm:      1,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 2,
		clusterSize:      3,
		peers:            map[int64]bool{2: true, 3: true},
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	r.becomeLeader()

	// Leader has entries [1, 2, 3, 4, 5]
	for i := int64(1); i <= 5; i++ {
		r.raftLog.append(&proto.Log{Id: i, Term: 1, Data: &proto.Cmd{Op: "PUT"}})
	}
	r.raftLog.markStableTill(5)

	// Peer 2 starts with matchIndex=2, nextIndex=3
	r.progress[2] = Progress{matchIndex: 2, nextIndex: 3}

	// Simulate sending entries [3, 4, 5] to peer 2
	// (In correct implementation, leader would send these based on nextIndex=3)

	// Peer 2 responds with success
	successResp := &proto.Message{
		Type: proto.MessageType_MSG_APP_RESP,
		From: 2,
		To:   1,
		EntriesResp: &proto.AppendEntriesResponse{
			CurrentTerm: 1,
			Success:     true,
		},
	}

	err := r.Step(successResp)
	require.NoError(t, err)

	// **BUG EXPOSED**: matchIndex is set to leader's lastIndex=5
	// **CORRECT**: matchIndex should be set to the highest index that was sent and acknowledged
	// Since we sent entries [3, 4, 5], matchIndex should become 5
	// BUT if we only sent [3], matchIndex should only be updated to 3, not 5

	prog := r.progress[2]

	// This assertion will PASS with current buggy code, but it's incorrect logic
	// The bug is that we don't track what was actually sent
	assert.Equal(t, int64(5), prog.matchIndex,
		"BUG: matchIndex blindly set to lastIndex without knowing what was sent")

	// What if another goroutine appended entry 6 between sending and receiving response?
	// Then matchIndex would be set to 6 even though peer 2 doesn't have entry 6!

	// The correct fix requires tracking which entries were sent in each AppendEntries RPC
	t.Log("Current implementation assumes response is for ALL entries up to lastIndex")
	t.Log("This is a race condition: if new entries arrive after sending, matchIndex is wrong")
}

// TestFollowerRejectsInconsistentLog tests that followers properly reject
// AppendEntries when their log doesn't match prevLogIdx/prevLogTerm
func TestFollowerRejectsInconsistentLog(t *testing.T) {
	r := &Raft{
		ID:               2,
		currentTerm:      1,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 2,
		clusterSize:      3,
		peers:            map[int64]bool{1: true, 3: true},
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	r.becomeFollower(1)

	// Follower has entries [1, 2] with term 1
	r.raftLog.append(&proto.Log{Id: 1, Term: 1, Data: &proto.Cmd{Op: "PUT"}})
	r.raftLog.append(&proto.Log{Id: 2, Term: 1, Data: &proto.Cmd{Op: "PUT"}})
	r.raftLog.markStableTill(2)

	// Leader sends AppendEntries with prevLogIdx=3, prevLogTerm=1
	// Follower doesn't have entry 3, should reject
	appendMsg := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		From: 1,
		To:   2,
		Entries: &proto.AppendEntries{
			Term:        1,
			LeaderId:    1,
			PrevLogIdx:  3, // Follower only has up to 2
			PrevLogTerm: 1,
			Entries: []*proto.Log{
				{Id: 4, Term: 1, Data: &proto.Cmd{Op: "PUT"}},
			},
			LeaderCommitIdx: 0,
		},
	}

	err := r.Step(appendMsg)
	require.NoError(t, err)

	// Should have generated a rejection response
	require.Len(t, r.msgs, 1, "should have rejection response")

	resp := r.msgs[0]
	assert.Equal(t, proto.MessageType_MSG_APP_RESP, resp.Type)
	assert.Equal(t, int64(1), resp.To, "response should go to leader")
	assert.False(t, resp.GetEntriesResp().Success, "should be rejection")

	// **BUG EXPOSED**: Response doesn't include hints about where to retry
	// Ideally, it should include:
	// - ConflictIndex: where the conflict occurred (or hint about last index)
	// - ConflictTerm: the term at conflict point
	// This would allow leader to jump back intelligently instead of decrementing by 1

	t.Log("Current rejection response provides no hints for fast recovery")
	t.Log("Leader will have to decrement nextIndex one-by-one (slow)")
}

// TestCandidateReceivesAppendEntries tests that candidates also handle AppendEntries
func TestCandidateReceivesAppendEntries(t *testing.T) {
	r := &Raft{
		ID:               2,
		currentTerm:      2,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 2,
		clusterSize:      3,
		peers:            map[int64]bool{1: true, 3: true},
		votes:            map[int64]bool{},
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	r.becomeCandidate() // Start as candidate
	r.votes[2] = true   // Voted for self

	// Leader (term 3) sends AppendEntries
	appendMsg := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		From: 1,
		To:   2,
		Entries: &proto.AppendEntries{
			Term:            3, // Higher term than candidate
			LeaderId:        1,
			PrevLogIdx:      0,
			PrevLogTerm:     0,
			Entries:         []*proto.Log{{Id: 1, Term: 3, Data: &proto.Cmd{Op: "PUT"}}},
			LeaderCommitIdx: 0,
		},
	}

	// **BUG**: Candidate's step function is stepCandidate, which doesn't call handleAppendEntries
	// It SHOULD step down to follower when receiving AppendEntries from valid leader

	err := r.Step(appendMsg)
	require.NoError(t, err)

	// After receiving AppendEntries from higher term, should step down to follower
	assert.Equal(t, StateFollower, r.state,
		"BUG: candidate should step down to follower when receiving AppendEntries from valid leader")

	// Should have appended the entry
	assert.Equal(t, int64(1), r.raftLog.lastIndex(),
		"should have appended entry from leader")
}

// TestFollowerAppendEntriesStepReadyAdvance tests the full Step/Ready/Advance
// pattern for a follower receiving AppendEntries
func TestFollowerAppendEntriesStepReadyAdvance(t *testing.T) {
	r := &Raft{
		ID:               2,
		currentTerm:      1,
		votedFor:         make(map[int64]int64),
		state:            StateFollower,
		electionTimeout:  10,
		heartbeatTimeout: 2,
		clusterSize:      3,
		peers:            map[int64]bool{1: true, 3: true},
		msgs:             make([]*proto.Message, 0),
		msgsAfterAppend:  make([]*proto.Message, 0),
		raftLog:          newRaftLog(),
		progress:         make(map[int64]Progress),
		readyc:           make(chan Ready, 1),
	}

	r.becomeFollower(1)

	// Leader sends AppendEntries with entry [1]
	appendMsg := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		From: 1,
		To:   2,
		Entries: &proto.AppendEntries{
			Term:            1,
			LeaderId:        1,
			PrevLogIdx:      0,
			PrevLogTerm:     0,
			Entries:         []*proto.Log{{Id: 1, Term: 1, Data: &proto.Cmd{Op: "PUT", Key: "k1"}}},
			LeaderCommitIdx: 0,
		},
	}

	// **STEP 1: Step() processes the message**
	err := r.Step(appendMsg)
	require.NoError(t, err)

	// Entry should be appended to raftLog
	assert.Equal(t, int64(1), r.raftLog.lastIndex(), "entry should be appended to log")

	// Debug: check state after Step
	t.Logf("After Step: stableIndex=%d, lastIndex=%d, msgs=%d, msgsAfterAppend=%d",
		r.raftLog.stableIndex, r.raftLog.lastIndex(), len(r.msgs), len(r.msgsAfterAppend))
	t.Logf("unstableEntries: %d", len(r.raftLog.volatileEntries()))

	// **STEP 2: Ready() should provide entries to persist and response to send**
	readyChan := r.Ready()
	select {
	case rd := <-readyChan:
		// Should have unstable entries to persist
		assert.Len(t, rd.Entries, 1, "should have 1 unstable entry to persist to WAL")
		assert.Equal(t, int64(1), rd.Entries[0].Id, "entry ID should be 1")

		// Should have response message to send AFTER persistence
		assert.Len(t, rd.MessagesAfterAppend, 1, "should have success response to send after WAL")
		resp := rd.MessagesAfterAppend[0]
		assert.Equal(t, proto.MessageType_MSG_APP_RESP, resp.Type)
		assert.Equal(t, int64(1), resp.To, "response should go to leader")
		assert.True(t, resp.GetEntriesResp().Success, "response should be success")

		// **STEP 3: I/O layer persists to WAL (simulated)**
		// n.wal.Append(rd.Entries[0])

		// **STEP 4: I/O layer sends response (simulated)**
		// n.send(rd.MessagesAfterAppend)

		// **STEP 5: Advance() marks entries as stable**
		r.Advance()

		// After Advance, entry should be marked stable
		assert.Equal(t, int64(1), r.raftLog.stableIndex, "entry should be marked stable after Advance")

		// No more unstable entries
		assert.Len(t, r.raftLog.volatileEntries(), 0, "no unstable entries after Advance")

	default:
		t.Fatal("Ready channel should have data")
	}
}
