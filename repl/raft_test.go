//go:build integration
// +build integration

package repl

import (
	"context"
	"testing"
	"time"

	proto "harmonydb/repl/proto/repl"

	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"
)

// TestLeaderElection tests that a node can successfully become a leader
// when it times out and sends vote requests to peers
func TestLeaderElection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a 3-node cluster where node 1 will become leader
	node1 := createTestNode(1, []int64{2, 3}, ctrl)

	// Get mock WAL and set up expectations (no WAL writes during election)
	mockWAL := node1.wal.(*MockStorage)
	mockWAL.EXPECT().Append(gomock.Any()).Times(0)

	// Set up expectations for vote requests
	// Node 1 will send vote requests to nodes 2 and 3
	for peerID := range node1.members {
		mockConn := node1.members[peerID].(*MockClientConn)

		// Expect vote request to be sent and simulate granting the vote
		mockConn.EXPECT().
			Invoke(gomock.Any(), proto.RaftService_Process_FullMethodName, gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
				msg := args.(*proto.Message)
				if msg.Type == proto.MessageType_MSG_VOTE {
					t.Logf("Peer %d received vote request from node %d (term=%d)", peerID, msg.From, msg.GetVote().GetTerm())

					// Simulate the peer granting the vote by sending a response back
					// Send the vote response back to node1
					go func() {
						time.Sleep(20 * time.Millisecond) // Simulate network delay
						// Use a new context instead of the call's context which may be cancelled
						newCtx := context.Background()
						node1.Process(newCtx, createVoteGrantMessage(peerID, msg.From, int64(msg.GetVote().GetTerm())))
					}()
				}
				return nil
			}).
			AnyTimes()
	}

	// Start the run loop
	go node1.run()

	// Get initial state
	raft := node1.raft.(*Raft)
	initialState := raft.state
	t.Logf("Initial state: %v", initialState)

	if initialState != StateFollower {
		t.Fatalf("Expected initial state to be Follower, got %v", initialState)
	}

	// Trigger election by ticking past election timeout
	for range 5 {
		node1.Tick()
	}

	// Give time for election to complete and vote responses to be processed
	time.Sleep(50 * time.Millisecond)

	// Check state after election
	finalState := raft.state
	t.Logf("Final state: %v (current term: %d)", finalState, raft.currentTerm)

	// With vote responses being sent back, node 1 should become leader
	if finalState != StateLeader {
		t.Errorf("Expected state to be Leader after receiving votes, got %v", finalState)
	}

	// Verify that node voted for itself
	if raft.votedFor[raft.currentTerm] != node1.ID {
		t.Errorf("Expected node to vote for itself (node %d), got vote for node %d",
			node1.ID, raft.votedFor[raft.currentTerm])
	}

	// Verify the node is in term 1 (incremented from initial term 0)
	if raft.currentTerm != 1 {
		t.Errorf("Expected term to be 1, got %d", raft.currentTerm)
	}
}
