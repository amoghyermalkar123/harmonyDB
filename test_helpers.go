package harmonydb

import (
	"fmt"
	"harmonydb/raft"
	"os"
	"testing"
	"time"
)

// testCluster manages a cluster of database nodes for testing
type testCluster struct {
	nodes     []*DB
	dataDir   string
	t         *testing.T
	nodeCount int
	configs   map[int64]raft.ClusterConfig
}

// newTestCluster creates a new test cluster with the specified number of nodes
// nodeCount must be between 3 (minimum) and 6 (maximum)
func newTestCluster(t *testing.T, nodeCount int) *testCluster {
	// Validate node count
	if nodeCount < 3 {
		t.Fatalf("HarmonyDB requires minimum 3 nodes, got %d", nodeCount)
	}
	if nodeCount > 6 {
		t.Fatalf("Maximum cluster size is 6 nodes, got %d", nodeCount)
	}

	// Create temporary directory for test data
	dataDir, err := os.MkdirTemp("", "harmonydb-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	tc := &testCluster{
		nodes:     make([]*DB, 0, nodeCount),
		dataDir:   dataDir,
		t:         t,
		nodeCount: nodeCount,
		configs:   make(map[int64]raft.ClusterConfig),
	}

	// Register cleanup
	t.Cleanup(func() {
		tc.cleanup()
	})

	return tc
}

// cleanup removes all test data and stops all nodes
func (tc *testCluster) cleanup() {
	// Stop all running nodes
	for _, node := range tc.nodes {
		if node != nil {
			node.Stop()
		}
	}

	// Remove temporary directory
	if tc.dataDir != "" {
		os.RemoveAll(tc.dataDir)
	}
}

// allocatePorts allocates sequential ports for cluster nodes
// Returns raft and http ports for each node
func allocatePorts(nodeCount int, testName string) ([]int, []int) {
	// Use a high base port to avoid conflicts
	// Hash test name to get a unique offset
	basePort := 50000
	for _, c := range testName {
		basePort += int(c)
	}
	basePort = basePort%10000 + 50000

	raftPorts := make([]int, nodeCount)
	httpPorts := make([]int, nodeCount)

	for i := 0; i < nodeCount; i++ {
		raftPorts[i] = basePort + (i * 2)
		httpPorts[i] = basePort + (i * 2) + 1
	}

	return raftPorts, httpPorts
}

// start initializes and starts all nodes in the cluster
func (tc *testCluster) start() {
	// Allocate ports
	raftPorts, httpPorts := allocatePorts(tc.nodeCount, tc.t.Name())

	// Build cluster configuration
	nodesConfig := make(map[int64]raft.NodeConfig)
	for i := 0; i < tc.nodeCount; i++ {
		nodeID := int64(i + 1)
		nodesConfig[nodeID] = raft.NodeConfig{
			ID:       nodeID,
			RaftPort: raftPorts[i],
			HTTPPort: httpPorts[i],
			Address:  "localhost",
		}
	}

	// Create and start each node
	for i := 0; i < tc.nodeCount; i++ {
		nodeID := int64(i + 1)

		clusterConfig := raft.ClusterConfig{
			ThisNodeID: nodeID,
			Nodes:      nodesConfig,
		}

		tc.configs[nodeID] = clusterConfig

		// Create database instance
		db, err := OpenWithConfig(clusterConfig)
		if err != nil {
			tc.t.Fatalf("Failed to start node %d: %v", nodeID, err)
		}

		tc.nodes = append(tc.nodes, db)
	}

	tc.t.Logf("Started %d-node cluster", tc.nodeCount)
}

// waitForLeader polls until a leader is elected or timeout occurs
// Maximum sleep per iteration is 200ms as per project constraints
func (tc *testCluster) waitForLeader(timeout time.Duration) (int64, error) {
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

		// Sleep with max 200ms per iteration
		if sleepDuration > 200*time.Millisecond {
			sleepDuration = 200 * time.Millisecond
		}
		time.Sleep(sleepDuration)
		sleepDuration = min(sleepDuration*2, 200*time.Millisecond)
	}

	// Gather diagnostic info for failure message
	leaderIDs := make(map[int64]int)
	for i, node := range tc.nodes {
		leaderID := node.GetLeaderID()
		leaderIDs[leaderID]++
		tc.t.Logf("Node %d thinks leader is: %d", i+1, leaderID)
	}

	return 0, fmt.Errorf("leader election timeout after %v. Leader IDs reported: %v", timeout, leaderIDs)
}

// stopAll stops all nodes in the cluster
func (tc *testCluster) stopAll() {
	tc.t.Logf("Stopping all %d nodes", len(tc.nodes))
	for i, node := range tc.nodes {
		if node != nil {
			node.Stop()
			tc.t.Logf("Stopped node %d", i+1)
		}
	}
}

// restartAll restarts all nodes in the cluster
func (tc *testCluster) restartAll() error {
	tc.t.Logf("Restarting all %d nodes", tc.nodeCount)

	// Create new node instances with the same configs
	newNodes := make([]*DB, 0, tc.nodeCount)

	for i := 0; i < tc.nodeCount; i++ {
		nodeID := int64(i + 1)
		config := tc.configs[nodeID]

		db, err := OpenWithConfig(config)
		if err != nil {
			return fmt.Errorf("failed to restart node %d: %w", nodeID, err)
		}

		newNodes = append(newNodes, db)
		tc.t.Logf("Restarted node %d", nodeID)
	}

	tc.nodes = newNodes
	tc.t.Logf("All nodes restarted successfully")
	return nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
