package raft

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"harmonydb/raft/proto"
)

// testCluster manages a cluster of raft nodes for testing
type testCluster struct {
	t          *testing.T
	nodes      []*Raft
	transports []*testTransport
	logs       []*testLogStore
	config     ClusterConfig
	mu         sync.Mutex
	failedCh   chan struct{}
	failed     bool
	startTime  time.Time
	leaderCh   chan int64 // notifies when a leader is elected
}

// newTestCluster creates a new test cluster with n nodes
func newTestCluster(t *testing.T, n int) *testCluster {
	c := &testCluster{
		t:        t,
		failedCh: make(chan struct{}),
		leaderCh: make(chan int64, 10),
	}

	// Create nodes
	for i := 0; i < n; i++ {
		nodeID := int64(i + 1)

		// Create test transport
		trans := newTestTransport(nodeID)
		c.transports = append(c.transports, trans)

		// Create test log store
		logStore := newTestLogStore()
		c.logs = append(c.logs, logStore)

		// Note: Raft creation will be done after wiring up transports
	}

	// Wire up transports (fully connected by default)
	c.fullyConnect()

	c.startTime = time.Now()
	return c
}

// bootstrap initializes the cluster with bootstrapped raft nodes
func (c *testCluster) bootstrap() {
	// Build cluster configuration
	config := ClusterConfig{
		Nodes: make(map[int64]NodeConfig),
	}

	for i := range c.transports {
		nodeID := int64(i + 1)
		config.Nodes[nodeID] = NodeConfig{
			ID:       nodeID,
			RaftPort: 5000 + i,
			HTTPPort: 8000 + i,
			Address:  fmt.Sprintf("test-node-%d", nodeID),
		}
	}

	// Create raft nodes
	for i, trans := range c.transports {
		nodeID := int64(i + 1)
		nodeConfig := config
		nodeConfig.ThisNodeID = nodeID

		// Create raft node with test transport and log store
		raft := c.createTestRaft(nodeID, nodeConfig, trans, c.logs[i])
		c.nodes = append(c.nodes, raft)
	}

	// Now that all nodes are created, update cluster connections
	// This is important because nodes need to reference each other
	for i, raft := range c.nodes {
		trans := c.transports[i]
		// Update cluster map with proper client references
		for peerID := range trans.peers {
			raft.n.cluster[peerID] = trans.createClientForPeer(peerID)
		}
	}

	c.config = config
}

// createTestRaft creates a raft node for testing
func (c *testCluster) createTestRaft(id int64, config ClusterConfig, trans *testTransport, logStore *testLogStore) *Raft {
	// Create raft node manually for testing
	meta := &Meta{
		nt:         Follower,
		nextIndex:  make(map[int]int64),
		matchIndex: make(map[int]int64),
	}

	state := &ConsensusState{
		pastVotes: make(map[int64]int64),
	}

	node := &raftNode{
		ID:              id,
		meta:            meta,
		state:           state,
		heartbeats:      make(chan *proto.AppendEntries, 100),
		heartbeatCloser: make(chan struct{}),
		applyc:          make(chan ToApply, 100),
		logManager:      logStore.logManager,
		cluster:         make(map[int64]proto.RaftClient),
		leaderID:        0,
		matchIndex:      make(map[int64]int64),
		nextIndex:       make(map[int64]int64),
	}

	// Set the node reference in the transport
	trans.node = node

	// Wire up cluster connections through test transport
	for peerID := range trans.peers {
		// Create a client that targets this specific peer using the updated transport
		node.cluster[peerID] = trans.createClientForPeer(peerID)
	}

	raft := &Raft{
		n:      node,
		config: config,
	}

	// Start election timer - this is the key to making tests work!
	go node.startElection()

	return raft
}

// fullyConnect connects all transports to each other
func (c *testCluster) fullyConnect() {
	for i, t1 := range c.transports {
		for j, t2 := range c.transports {
			if i != j {
				t1.connect(t2.id, t2)
			}
		}
	}
}

// disconnect isolates a node from the cluster
func (c *testCluster) disconnect(nodeID int64) {
	idx := nodeID - 1
	trans := c.transports[idx]
	trans.disconnectAll()

	// Disconnect others from this node
	for i, t := range c.transports {
		if int64(i) != idx {
			t.disconnect(nodeID)
		}
	}
}

// reconnect reconnects a node to the cluster
func (c *testCluster) reconnect(nodeID int64) {
	idx := nodeID - 1
	trans := c.transports[idx]

	// Reconnect to all peers
	for i, t := range c.transports {
		if int64(i) != idx {
			trans.connect(t.id, t)
			t.connect(trans.id, trans)
		}
	}
}

// partition creates a network partition
// near nodes can communicate with each other, far nodes can communicate with each other,
// but near and far cannot communicate
func (c *testCluster) partition(nearIDs, farIDs []int64) {
	// Disconnect all connections first
	for _, trans := range c.transports {
		trans.disconnectAll()
	}

	// Reconnect near nodes
	for _, id1 := range nearIDs {
		for _, id2 := range nearIDs {
			if id1 != id2 {
				t1 := c.transports[id1-1]
				t2 := c.transports[id2-1]
				t1.connect(t2.id, t2)
			}
		}
	}

	// Reconnect far nodes
	for _, id1 := range farIDs {
		for _, id2 := range farIDs {
			if id1 != id2 {
				t1 := c.transports[id1-1]
				t2 := c.transports[id2-1]
				t1.connect(t2.id, t2)
			}
		}
	}
}

// getNode returns the node with the given ID
func (c *testCluster) getNode(id int64) *Raft {
	for _, node := range c.nodes {
		if node.n.ID == id {
			return node
		}
	}
	return nil
}

// leader waits for a leader to be elected and returns it
func (c *testCluster) leader() *Raft {
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			c.t.Fatal("timeout waiting for leader")
			return nil
		case <-ticker.C:
			for _, node := range c.nodes {
				node.n.meta.RLock()
				isLeader := node.n.meta.nt == Leader
				node.n.meta.RUnlock()

				if isLeader {
					return node
				}
			}
		}
	}
}

// getLeader returns the current leader, or nil if none
func (c *testCluster) getLeader() *Raft {
	for _, node := range c.nodes {
		node.n.meta.RLock()
		isLeader := node.n.meta.nt == Leader
		node.n.meta.RUnlock()

		if isLeader {
			return node
		}
	}
	return nil
}

// followers waits for n-1 followers
func (c *testCluster) followers() []*Raft {
	expected := len(c.nodes) - 1
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			c.t.Fatalf("timeout waiting for %d followers", expected)
			return nil
		case <-ticker.C:
			var followers []*Raft
			for _, node := range c.nodes {
				node.n.meta.RLock()
				isFollower := node.n.meta.nt == Follower
				node.n.meta.RUnlock()

				if isFollower {
					followers = append(followers, node)
				}
			}

			if len(followers) == expected {
				return followers
			}
		}
	}
}

// waitForReplication waits until all nodes have at least n log entries
func (c *testCluster) waitForReplication(n int) {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			c.t.Fatal("timeout waiting for replication")
			return
		case <-ticker.C:
			allReplicated := true
			for _, logStore := range c.logs {
				logCount := logStore.logManager.GetLength()
				if logCount < n {
					allReplicated = false
					break
				}
			}

			if allReplicated {
				return
			}
		}
	}
}

// ensureSame verifies all logs are identical
func (c *testCluster) ensureSame() {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			c.logDifferences()
			c.t.Fatal("timeout waiting for log consistency")
			return
		case <-ticker.C:
			if c.logsConsistent() {
				return
			}
		}
	}
}

// logsConsistent checks if all logs are identical
func (c *testCluster) logsConsistent() bool {
	if len(c.logs) == 0 {
		return true
	}

	first := c.logs[0]
	firstLogs := first.logManager.GetLogs()

	for i := 1; i < len(c.logs); i++ {
		other := c.logs[i]
		otherLogs := other.logManager.GetLogs()

		if len(firstLogs) != len(otherLogs) {
			return false
		}

		for j := range firstLogs {
			if !logsEqual(firstLogs[j], otherLogs[j]) {
				return false
			}
		}
	}

	return true
}

// logDifferences prints log differences for debugging
func (c *testCluster) logDifferences() {
	for i, logStore := range c.logs {
		logs := logStore.logManager.GetLogs()
		c.t.Logf("Node %d has %d log entries", i+1, len(logs))
		for j, log := range logs {
			c.t.Logf("  [%d] term=%d, index=%d, op=%s", j, log.Term, log.Id, log.Data.Op)
		}
	}
}

// logsEqual compares two log entries
func logsEqual(a, b *proto.Log) bool {
	if a.Term != b.Term || a.Id != b.Id {
		return false
	}
	if a.Data == nil && b.Data == nil {
		return true
	}
	if a.Data == nil || b.Data == nil {
		return false
	}
	return a.Data.Op == b.Data.Op &&
		a.Data.Key == b.Data.Key &&
		a.Data.Value == b.Data.Value
}

// ensureLeader verifies that all nodes agree on the leader
func (c *testCluster) ensureLeader(expectedID int64) {
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			c.t.Fatalf("timeout waiting for all nodes to agree on leader %d", expectedID)
			return
		case <-ticker.C:
			allAgree := true
			for _, node := range c.nodes {
				node.n.RLock()
				leaderID := node.n.leaderID
				node.n.RUnlock()

				if leaderID != expectedID {
					allAgree = false
					break
				}
			}

			if allAgree {
				return
			}
		}
	}
}

// shutdown shuts down all nodes in the cluster
func (c *testCluster) shutdown() {
	for _, node := range c.nodes {
		node.n.shutdown()
	}
}

// waitFor waits for a condition with timeout
func (c *testCluster) waitFor(condition func() bool, timeout time.Duration, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.t.Fatal(msg)
			return
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// logf logs a message
func (c *testCluster) logf(format string, args ...interface{}) {
	c.t.Logf(format, args...)
}
