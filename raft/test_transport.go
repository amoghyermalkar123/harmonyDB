package raft

import (
	"context"
	"fmt"
	"sync"

	"harmonydb/raft/proto"

	"google.golang.org/grpc"
)

// testTransport is an in-memory transport for testing
// It allows full control over network connectivity for partition testing
type testTransport struct {
	id    int64
	node  *raftNode // Reference to the actual Raft node
	peers map[int64]*testTransport
	mu    sync.RWMutex
}

// newTestTransport creates a new test transport
func newTestTransport(id int64) *testTransport {
	return &testTransport{
		id:    id,
		peers: make(map[int64]*testTransport),
	}
}

// connect establishes a connection to a peer
func (t *testTransport) connect(peerID int64, peer *testTransport) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers[peerID] = peer
}

// disconnect removes a connection to a peer
func (t *testTransport) disconnect(peerID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.peers, peerID)
}

// disconnectAll removes all peer connections
func (t *testTransport) disconnectAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers = make(map[int64]*testTransport)
}

// isConnected checks if connected to a peer
func (t *testTransport) isConnected(peerID int64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.peers[peerID]
	return ok
}

// getPeer gets a connected peer
func (t *testTransport) getPeer(peerID int64) (*testTransport, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	peer, ok := t.peers[peerID]
	if !ok {
		return nil, fmt.Errorf("not connected to peer %d", peerID)
	}
	return peer, nil
}

// createClientForPeer creates a RaftClient that communicates with a specific peer
func (t *testTransport) createClientForPeer(peerID int64) proto.RaftClient {
	return &testRaftClient{
		sourceTransport: t,
		targetID:        peerID,
	}
}

// testRaftClient implements proto.RaftClient for in-memory testing
// It routes RPC calls to the actual Raft node through the test transport
type testRaftClient struct {
	sourceTransport *testTransport
	targetID        int64
}

// AppendEntriesRPC implements the AppendEntries RPC for testing
// It calls the actual RPC handler on the target node
func (c *testRaftClient) AppendEntriesRPC(ctx context.Context, req *proto.AppendEntries, opts ...grpc.CallOption) (*proto.AppendEntriesResponse, error) {
	// Get target transport
	c.sourceTransport.mu.RLock()
	targetTrans, ok := c.sourceTransport.peers[c.targetID]
	c.sourceTransport.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("not connected to peer %d", c.targetID)
	}

	// Ensure target node is set
	if targetTrans.node == nil {
		return nil, fmt.Errorf("target node not initialized for peer %d", c.targetID)
	}

	// Call actual RPC handler on target node
	return targetTrans.node.AppendEntriesRPC(ctx, req)
}

// RequestVoteRPC implements the RequestVote RPC for testing
// It calls the actual RPC handler on the target node
func (c *testRaftClient) RequestVoteRPC(ctx context.Context, req *proto.RequestVote, opts ...grpc.CallOption) (*proto.RequestVoteResponse, error) {
	// Get target transport
	c.sourceTransport.mu.RLock()
	targetTrans, ok := c.sourceTransport.peers[c.targetID]
	c.sourceTransport.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("not connected to peer %d", c.targetID)
	}

	// Ensure target node is set
	if targetTrans.node == nil {
		return nil, fmt.Errorf("target node not initialized for peer %d", c.targetID)
	}

	// Call actual RPC handler on target node
	return targetTrans.node.RequestVoteRPC(ctx, req)
}
