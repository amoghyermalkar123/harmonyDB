package harmonydb

import (
	"fmt"
	"harmonydb/raft"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain provides setup and teardown for all tests in this package
func TestMain(m *testing.M) {
	// Setup: Could add global test setup here if needed

	// Run tests
	exitCode := m.Run()

	// Teardown: Could add global cleanup here if needed

	// Exit with test result code
	os.Exit(exitCode)
}

// TestCluster manages a cluster of database nodes for testing
type TestCluster struct {
	nodes     []*DB
	dataDir   string
	t         *testing.T
	nodeCount int
	configs   map[int64]raft.ClusterConfig
}

// NewTestCluster creates a new test cluster with the specified number of nodes
// nodeCount must be between 3 (minimum) and 6 (maximum)
func NewTestCluster(t *testing.T, nodeCount int) *TestCluster {
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

	tc := &TestCluster{
		nodes:     make([]*DB, 0, nodeCount),
		dataDir:   dataDir,
		t:         t,
		nodeCount: nodeCount,
		configs:   make(map[int64]raft.ClusterConfig),
	}

	// Register cleanup
	t.Cleanup(func() {
		tc.Cleanup()
	})

	return tc
}

// Cleanup removes all test data and stops all nodes
func (tc *TestCluster) Cleanup() {
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

// Start initializes and starts all nodes in the cluster
func (tc *TestCluster) Start() {
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

// WaitForLeader polls until a leader is elected or timeout occurs
// Maximum sleep per iteration is 200ms as per project constraints
func (tc *TestCluster) WaitForLeader(timeout time.Duration) (int64, error) {
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

// StopAll stops all nodes in the cluster
func (tc *TestCluster) StopAll() {
	tc.t.Logf("Stopping all %d nodes", len(tc.nodes))
	for i, node := range tc.nodes {
		if node != nil {
			node.Stop()
			tc.t.Logf("Stopped node %d", i+1)
		}
	}
}

// RestartAll restarts all nodes in the cluster
func (tc *TestCluster) RestartAll() error {
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

// =============================================================================
// Phase 3: User Story 1 - Basic Data Integrity Verification
// =============================================================================

// TestBasicPutGet validates that a single key-value pair can be written and read
// in a 3-node cluster
func TestBasicPutGet(t *testing.T) {
	// Create and start 3-node cluster (minimum required)
	cluster := NewTestCluster(t, 3)
	cluster.Start()

	// Wait for leader election
	leaderID, err := cluster.WaitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")
	t.Logf("Leader elected: node %d", leaderID)

	// Write a key-value pair
	key := []byte("test-key")
	value := []byte("test-value")

	leader := cluster.nodes[leaderID-1]
	err = leader.Put(key, value)
	require.NoError(t, err, "Failed to put key-value pair")
	t.Logf("Successfully wrote key=%s, value=%s", key, value)

	// Read back the value
	retrievedValue, err := leader.Get(key)
	require.NoError(t, err, "Failed to get value for key")

	// Verify the value matches
	assert.Equal(t, value, retrievedValue,
		"Retrieved value does not match written value.\nExpected: %s\nGot: %s",
		value, retrievedValue)

	t.Logf("Successfully verified key-value pair")
}

// TestMultipleKeys validates that multiple key-value pairs can be written and read
// correctly without data mixing
func TestMultipleKeys(t *testing.T) {
	// Create and start 3-node cluster
	cluster := NewTestCluster(t, 3)
	cluster.Start()

	// Wait for leader election
	leaderID, err := cluster.WaitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write multiple key-value pairs
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
		"key4": "value4",
		"key5": "value5",
	}

	for k, v := range testData {
		err := leader.Put([]byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Successfully wrote %d key-value pairs", len(testData))

	// Read back all values and verify
	for k, expectedValue := range testData {
		retrievedValue, err := leader.Get([]byte(k))
		require.NoError(t, err, "Failed to get value for key=%s", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Value mismatch for key=%s.\nExpected: %s\nGot: %s",
			k, expectedValue, retrievedValue)
	}

	t.Logf("Successfully verified all %d key-value pairs", len(testData))
}

// TestKeyOverwrite validates that overwriting an existing key replaces the value completely
func TestKeyOverwrite(t *testing.T) {
	// Create and start 3-node cluster
	cluster := NewTestCluster(t, 3)
	cluster.Start()

	// Wait for leader election
	leaderID, err := cluster.WaitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]
	key := []byte("overwrite-key")

	// Write initial value
	initialValue := []byte("initial-value")
	err = leader.Put(key, initialValue)
	require.NoError(t, err, "Failed to put initial value")
	t.Logf("Wrote initial value: %s", initialValue)

	// Verify initial value
	retrievedValue, err := leader.Get(key)
	require.NoError(t, err, "Failed to get initial value")
	assert.Equal(t, initialValue, retrievedValue, "Initial value mismatch")

	// Overwrite with new value
	newValue := []byte("new-value-that-overwrites")
	err = leader.Put(key, newValue)
	require.NoError(t, err, "Failed to overwrite value")
	t.Logf("Overwrote with new value: %s", newValue)

	// Verify new value
	retrievedValue, err = leader.Get(key)
	require.NoError(t, err, "Failed to get overwritten value")
	assert.Equal(t, newValue, retrievedValue,
		"Overwritten value mismatch.\nExpected: %s\nGot: %s",
		newValue, retrievedValue)

	// Ensure old value is completely gone
	assert.NotEqual(t, initialValue, retrievedValue,
		"Old value still present after overwrite")

	t.Logf("Successfully verified key overwrite")
}

// TestDataPersistence validates that data survives cluster restart
func TestDataPersistence(t *testing.T) {
	t.Skip("Skipping: WAL not implemented")
	// Create and start 3-node cluster
	cluster := NewTestCluster(t, 3)
	cluster.Start()

	// Wait for leader election
	leaderID, err := cluster.WaitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write test data
	testData := map[string]string{
		"persist-key1": "persist-value1",
		"persist-key2": "persist-value2",
		"persist-key3": "persist-value3",
	}

	for k, v := range testData {
		err := leader.Put([]byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Wrote %d key-value pairs before restart", len(testData))

	// Give a moment for consensus to propagate
	time.Sleep(100 * time.Millisecond)

	// Stop all nodes
	cluster.StopAll()

	// Restart all nodes
	err = cluster.RestartAll()
	require.NoError(t, err, "Failed to restart cluster")

	// Wait for new leader election
	leaderID, err = cluster.WaitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after restart")
	t.Logf("New leader elected after restart: node %d", leaderID)

	// Verify all data persisted
	newLeader := cluster.nodes[leaderID-1]
	for k, expectedValue := range testData {
		retrievedValue, err := newLeader.Get([]byte(k))
		require.NoError(t, err, "Failed to get persisted key=%s", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Persisted value mismatch for key=%s.\nExpected: %s\nGot: %s",
			k, expectedValue, retrievedValue)
	}

	t.Logf("Successfully verified data persistence across cluster restart")
}
