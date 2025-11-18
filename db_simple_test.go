package harmonydb

import (
	"harmonydb/raft"
	"os"
	"testing"
	"time"
)

func TestSimpleClusterSetup(t *testing.T) {
	basePort := 60000
	numNodes := 5

	// Build cluster configuration for 5 nodes
	clusterConfig := raft.ClusterConfig{
		Nodes: make(map[int64]raft.NodeConfig),
	}

	for i := 0; i < numNodes; i++ {
		nodeID := int64(i + 1)
		clusterConfig.Nodes[nodeID] = raft.NodeConfig{
			ID:       nodeID,
			RaftPort: basePort + (i * 2),
			HTTPPort: basePort + (i * 2) + 1,
			Address:  "localhost",
		}
	}

	// Create all nodes
	nodes := make([]*DB, numNodes)
	for i := 0; i < numNodes; i++ {
		nodeID := int64(i + 1)
		nodeConfig := clusterConfig
		nodeConfig.ThisNodeID = nodeID

		t.Logf("Creating node %d...", nodeID)
		db, err := OpenWithConfig(nodeConfig)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", nodeID, err)
		}

		if db == nil {
			t.Fatalf("DB is nil for node %d", nodeID)
		}

		nodes[i] = db
		t.Logf("Node %d created successfully", nodeID)

		// Small delay between nodes
		time.Sleep(300 * time.Millisecond)
	}

	t.Log("All nodes created, waiting for leader election...")
	time.Sleep(5 * time.Second)

	// Check leader
	leaderID := nodes[0].GetLeaderID()
	t.Logf("Leader ID: %d", leaderID)

	// Cleanup
	for i, node := range nodes {
		if node != nil {
			t.Logf("Stopping node %d...", i+1)
			node.Stop()
		}
	}

	// Cleanup files
	for i := 0; i < numNodes; i++ {
		os.Remove("harmony-" + string(rune(i+1)) + ".db")
	}

	t.Log("Test completed")
}
