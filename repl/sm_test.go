package repl

import (
	"testing"
	"time"

	proto "harmonydb/repl/proto/repl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStepRoutesMessages tests that Step routes messages correctly
func TestStepRoutesMessages(t *testing.T) {
	tests := []struct {
		name    string
		msgType proto.MessageType
	}{
		{"MSG_VOTE", proto.MessageType_MSG_VOTE},
		{"MSG_APP", proto.MessageType_MSG_APP},
		{"MSG_APP_RESP", proto.MessageType_MSG_APP_RESP},
		{"MSG_VOTE_RESP", proto.MessageType_MSG_VOTE_RESP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Raft{
				ID:               1,
				currentTerm:      0,
				votedFor:         make(map[int64]int64),
				state:            StateFollower,
				electionTimeout:  10,
				heartbeatTimeout: 3,
				clusterSize:      3,
				peers:            make(map[int64]bool),
				votes:            make(map[int64]bool),
				msgs:             make([]*proto.Message, 0),
				msgsAfterAppend:  make([]*proto.Message, 0),
				raftLog:          newRaftLog(),
				progress:         make(map[int64]Progress),
				readyc:           make(chan Ready, 1),
			}

			// Initialize as follower to set step and tick functions
			r.becomeFollower(0)

			msg := &proto.Message{
				Type: tt.msgType,
			}

			// Should not panic
			err := r.Step(msg)
			assert.NoError(t, err, "Step should not return error")
		})
	}
}

// TestReadyChannelPattern tests the Ready/Advance pattern
func TestReadyChannelPattern(t *testing.T) {
	r := &Raft{
		ID:               1,
		currentTerm:      0,
		votedFor:         make(map[int64]int64),
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

	// 1. Trigger campaign
	r.campaign()

	// 2. Messages should be accumulated
	assert.Len(t, r.msgs, 2, "should have 2 messages accumulated")

	// 3. Call Ready() to get messages
	readyChan := r.Ready()

	select {
	case rd := <-readyChan:
		assert.Len(t, rd.Messages, 2, "should have 2 messages in Ready")
		assert.Empty(t, r.msgs, "msgs should be cleared after Ready")

	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for Ready")
	}

	// 4. Call Advance()
	r.Advance()

	// 6. Subsequent Ready() should not send anything (no new messages)
	select {
	case <-r.Ready():
		t.Error("Ready() should not send anything when no new messages")
	case <-time.After(50 * time.Millisecond):
		// Expected - no ready
	}
}

// Integration test scenarios

func TestSingleNodeElectionScenario(t *testing.T) {
	t.Skip("Full election logic not yet implemented")

	// Scenario: Single node cluster should become leader immediately
	// 1. Create node with no peers
	// 2. Trigger campaign
	// 3. Should transition to leader without waiting for votes
}

func TestThreeNodeElectionScenario(t *testing.T) {
	t.Skip("Full election logic not yet implemented")

	// Scenario: 3-node cluster, one node becomes leader
	// 1. Node 1 triggers campaign (term 1)
	// 2. Sends vote requests to nodes 2 and 3
	// 3. Node 2 and 3 grant votes
	// 4. Node 1 receives 2 votes (quorum = 2/3)
	// 5. Node 1 becomes leader
}

func TestElectionTimeoutScenario(t *testing.T) {
	t.Skip("Tick functions not yet implemented")

	// Scenario: Follower election timeout triggers campaign
	// 1. Node starts as follower
	// 2. Tick repeatedly until randomizedTimeout reached
	// 3. Should automatically trigger campaign
	// 4. State should be Candidate
	// 5. Term should be incremented
}

func TestHeartbeatPreventsElectionScenario(t *testing.T) {
	t.Skip("Heartbeat handling not yet implemented")

	// Scenario: Heartbeats reset follower election timer
	// 1. Follower ticks several times (but not timeout)
	// 2. Receives heartbeat from leader
	// 3. Election tick should reset to 0
	// 4. Continue ticking, should not trigger election early
}

func TestVoteGrantingScenario(t *testing.T) {
	r := &Raft{
		ID:              1,
		currentTerm:     0,
		votedFor:        make(map[int64]int64),
		state:           StateFollower,
		electionTimeout: 10,
		peers:           make(map[int64]bool),
		msgs:            make([]*proto.Message, 0),
		msgsAfterAppend: make([]*proto.Message, 0),
		raftLog:         newRaftLog(),
		progress:        make(map[int64]Progress),
		readyc:          make(chan Ready, 1),
	}
	r.becomeFollower(0)

	// Create vote request from candidate 2 in term 1
	term := uint64(1)
	voteReq := &proto.Message{
		Type: proto.MessageType_MSG_VOTE,
		Vote: &proto.RequestVote{
			Term:         &term,
			CandidateId:  2,
			LastLogIndex: 0,
			LastLogTerm:  0,
		},
	}

	// Process vote request
	err := r.Step(voteReq)
	require.NoError(t, err)

	// Check that we voted for candidate 2
	assert.Equal(t, int64(2), r.votedFor[1], "should vote for candidate 2 in term 1")

	// Check that vote response was generated
	require.Len(t, r.msgs, 1, "should have 1 vote response message")

	resp := r.msgs[0]
	assert.Equal(t, proto.MessageType_MSG_VOTE_RESP, resp.Type, "should be MSG_VOTE_RESP")

	voteResp := resp.GetVoteResp()
	assert.True(t, voteResp.GetVoteGranted(), "vote should be granted")
}

func TestVoteRejectionAlreadyVotedScenario(t *testing.T) {
	t.Skip("Vote granting logic not yet implemented")

	// Scenario: Follower rejects vote if already voted
	// 1. Follower votes for candidate A in term 1
	// 2. Follower receives vote request from candidate B in term 1
	// 3. Follower rejects vote (already voted for A)
	// 4. Sends vote response with voteGranted=false
}

func TestCandidateStepsDownOnHigherTermScenario(t *testing.T) {
	t.Skip("Step down logic not yet implemented")

	// Scenario: Candidate discovers higher term and steps down
	// 1. Node is candidate in term 1
	// 2. Receives message with term 2
	// 3. Node steps down to follower
	// 4. Updates term to 2
	// 5. Clears vote tracking
}

func TestLeaderSendsHeartbeatsScenario(t *testing.T) {
	t.Skip("Leader heartbeat logic not yet implemented")

	// Scenario: Leader sends periodic heartbeats
	// 1. Node becomes leader
	// 2. Tick repeatedly until heartbeatTimeout
	// 3. Leader generates heartbeat messages
	// 4. Messages available via Ready()
	// 5. Heartbeat tick resets
}

func TestSplitVoteScenario(t *testing.T) {
	t.Skip("Full election logic not yet implemented")

	// Scenario: Split vote causes election restart
	// 1. 4-node cluster, two candidates start simultaneously
	// 2. Each gets 2 votes (including self)
	// 3. No quorum (need 3/4)
	// 4. Election timeout occurs
	// 5. Both restart campaign with higher term
}

func TestLogReplicationScenario(t *testing.T) {
	// Scenario: Leader replicates log entries to followers
	// 1. Node becomes leader
	// 2. Leader receives proposal with log entry
	// 3. Leader appends to local log
	// 4. Leader generates AppendEntries for peers
	// 5. Verify messages are queued in msgsAfterAppend

	r := &Raft{
		ID:               1,
		currentTerm:      1,
		votedFor:         make(map[int64]int64),
		state:            StateLeader,
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

	// Create a proposal message with a log entry
	entry := &proto.Log{
		Data: &proto.Cmd{
			Op:        "PUT",
			Key:       "key1",
			Value:     "value1",
			RequestId: 123,
		},
	}

	proposal := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		Entries: &proto.AppendEntries{
			Entries: []*proto.Log{entry},
		},
	}

	// Step the proposal through the leader
	err := r.Step(proposal)
	require.NoError(t, err)

	// Verify entry was appended to local log
	assert.Equal(t, int64(1), r.raftLog.lastIndex(), "lastIndex should be 1")

	// Verify entry has correct term and ID
	assert.Equal(t, int64(1), r.raftLog.entries[1].Term, "entry term should be 1")
	assert.Equal(t, int64(1), r.raftLog.entries[1].Id, "entry id should be 1")

	// Verify AppendEntries messages generated for peers
	require.Len(t, r.msgsAfterAppend, 2, "should have 2 AppendEntries messages for 2 peers")

	// Verify messages are properly formed
	for _, msg := range r.msgsAfterAppend {
		assert.Equal(t, proto.MessageType_MSG_APP, msg.Type, "message type should be MSG_APP")

		ae := msg.GetEntries()
		require.NotNil(t, ae, "AppendEntries should not be nil")

		assert.Equal(t, int64(1), ae.Term, "term should be 1")
		assert.Equal(t, int64(1), ae.LeaderId, "leaderId should be 1")
		assert.Len(t, ae.Entries, 1, "should have 1 entry")
	}

	// Verify Ready contains the unstable entries and messages
	readyChan := r.Ready()
	select {
	case rd := <-readyChan:
		assert.Len(t, rd.Entries, 1, "Ready should contain 1 unstable entry")
		assert.Len(t, rd.MessagesAfterAppend, 2, "Ready should contain 2 AppendEntries messages for peers")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for Ready")
	}
}

func TestFollowerAppendEntriesScenario(t *testing.T) {
	// Scenario: Follower receives and appends log entries from leader
	// 1. Node is follower
	// 2. Receives AppendEntries with new entry
	// 3. Appends to local log
	// 4. Sends success response

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

	// Create AppendEntries message from leader
	entry := &proto.Log{
		Term: 1,
		Id:   1,
		Data: &proto.Cmd{
			Op:        "PUT",
			Key:       "key1",
			Value:     "value1",
			RequestId: 123,
		},
	}

	appendMsg := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		From: 1,
		To:   2,
		Entries: &proto.AppendEntries{
			Term:            1,
			LeaderId:        1,
			PrevLogIdx:      0,
			PrevLogTerm:     0,
			Entries:         []*proto.Log{entry},
			LeaderCommitIdx: 0,
		},
	}

	// Process the AppendEntries
	err := r.Step(appendMsg)
	require.NoError(t, err)

	// Verify entry was appended to local log
	assert.Equal(t, int64(1), r.raftLog.lastIndex(), "lastIndex should be 1")

	// Verify response was queued in msgsAfterAppend
	require.Len(t, r.msgsAfterAppend, 1, "should have 1 response message")

	resp := r.msgsAfterAppend[0]
	assert.Equal(t, proto.MessageType_MSG_APP_RESP, resp.Type, "message type should be MSG_APP_RESP")

	respData := resp.GetEntriesResp()
	require.NotNil(t, respData, "AppendEntriesResponse should not be nil")

	assert.True(t, respData.Success, "response should indicate success")
	assert.Equal(t, int64(1), respData.CurrentTerm, "currentTerm should be 1")
}
