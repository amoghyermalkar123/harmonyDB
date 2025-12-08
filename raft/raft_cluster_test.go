package raft

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
)

// testRaftCluster manages a cluster of raft nodes for testing
type testRaftCluster struct {
	nodes   []*Raft
	configs []ClusterConfig
	t       *testing.T
}

// newTestRaftCluster creates a new test cluster with the specified number of nodes
func newTestRaftCluster(t *testing.T, nodeCount int, basePort int) *testRaftCluster {
	if nodeCount < 1 {
		t.Fatalf("Invalid node count: %d", nodeCount)
	}

	tc := &testRaftCluster{
		nodes:   make([]*Raft, 0, nodeCount),
		configs: make([]ClusterConfig, 0, nodeCount),
		t:       t,
	}

	// Build cluster configuration
	nodesConfig := make(map[int64]NodeConfig)
	for i := 0; i < nodeCount; i++ {
		nodeID := int64(i + 1)
		nodesConfig[nodeID] = NodeConfig{
			ID:       nodeID,
			RaftPort: basePort + (i * 2),
			HTTPPort: basePort + (i * 2) + 1,
			Address:  "localhost",
		}
	}

	// Create logger for testing
	logger, _ := zap.NewDevelopment()

	// Create and start each node
	for i := 0; i < nodeCount; i++ {
		nodeID := int64(i + 1)

		clusterConfig := ClusterConfig{
			ThisNodeID: nodeID,
			Nodes:      nodesConfig,
		}

		tc.configs = append(tc.configs, clusterConfig)

		// Create raft instance
		raft := NewRaftServerWithLogger(clusterConfig, logger)
		tc.nodes = append(tc.nodes, raft)
	}

	t.Logf("Started %d-node raft cluster", nodeCount)

	// Register cleanup
	t.Cleanup(func() {
		tc.cleanup()
	})

	return tc
}

// cleanup stops all nodes
func (tc *testRaftCluster) cleanup() {
	tc.t.Logf("Stopping %d raft nodes", len(tc.nodes))
	for i, node := range tc.nodes {
		if node != nil {
			node.Stop()
			tc.t.Logf("Stopped raft node %d", i+1)
		}
	}
}

// waitForLeader polls until a leader is elected or timeout occurs
func (tc *testRaftCluster) waitForLeader(timeout time.Duration) (int64, error) {
	deadline := time.Now().Add(timeout)
	sleepDuration := 50 * time.Millisecond

	for time.Now().Before(deadline) {
		// Check all nodes for leader
		for _, node := range tc.nodes {
			if leaderID := node.GetLeaderID(); leaderID != 0 {
				// Verify all nodes agree on this leader
				allAgree := true
				for _, n := range tc.nodes {
					if n.GetLeaderID() != leaderID {
						allAgree = false
						break
					}
				}
				if allAgree {
					tc.t.Logf("Leader elected: node %d", leaderID)
					return leaderID, nil
				}
			}
		}

		time.Sleep(sleepDuration)
		if sleepDuration < 200*time.Millisecond {
			sleepDuration *= 2
		}
	}

	// Gather diagnostic info
	leaderIDs := make(map[int64]int)
	for i, node := range tc.nodes {
		leaderID := node.GetLeaderID()
		leaderIDs[leaderID]++
		tc.t.Logf("Node %d thinks leader is: %d", i+1, leaderID)
	}

	return 0, fmt.Errorf("leader election timeout after %v. Leader IDs: %v", timeout, leaderIDs)
}

// getLeader returns the current leader node
func (tc *testRaftCluster) getLeader() *Raft {
	for _, node := range tc.nodes {
		if node.GetLeaderID() == node.n.ID {
			return node
		}
	}
	return nil
}

// getNode returns node by ID
func (tc *testRaftCluster) getNode(id int64) *Raft {
	for _, node := range tc.nodes {
		if node.n.ID == id {
			return node
		}
	}
	return nil
}

// TestLeaderElection tests that a leader is elected in a 3-node cluster
func TestLeaderElection(t *testing.T) {
	cluster := newTestRaftCluster(t, 3, 20000)

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("Leader election failed: %v", err)
	}

	// Verify exactly one leader
	leaderCount := 0
	for _, node := range cluster.nodes {
		node.n.meta.RLock()
		if node.n.meta.nt == Leader {
			leaderCount++
		}
		node.n.meta.RUnlock()
	}

	if leaderCount != 1 {
		t.Fatalf("Expected exactly 1 leader, got %d", leaderCount)
	}

	// Verify all nodes agree on the same leader
	for i, node := range cluster.nodes {
		if node.GetLeaderID() != leaderID {
			t.Fatalf("Node %d disagrees on leader: expected %d, got %d",
				i+1, leaderID, node.GetLeaderID())
		}
	}

	t.Logf("Leader election successful: node %d is leader", leaderID)
}
