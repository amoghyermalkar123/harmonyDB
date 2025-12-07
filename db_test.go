package harmonydb

import (
	"context"
	"fmt"
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

// =============================================================================
//  Basic Data Integrity Verification
// =============================================================================

// TestLinearizableWritePath validates that a single key-value pair can be written and read
// in a 3-node cluster
func TestLinearizableWritePath(t *testing.T) {
	// Create and start 3-node cluster (minimum required)
	cluster := newTestCluster(t, 3)
	cluster.start()

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")
	t.Logf("Leader elected: node %d", leaderID)

	// Write a key-value pair
	key := []byte("test-key")
	value := []byte("test-value")

	leader := cluster.nodes[leaderID-1]
	err = leader.Put(context.Background(), key, value)
	require.NoError(t, err, "Failed to put key-value pair")
	t.Logf("Successfully wrote key=%s, value=%s", key, value)

	// Read back the value
	retrievedValue, err := leader.Get(context.Background(), key)
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
	cluster := newTestCluster(t, 3)
	cluster.start()

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
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
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Successfully wrote %d key-value pairs", len(testData))

	// Read back all values and verify
	for k, expectedValue := range testData {
		retrievedValue, err := leader.Get(context.Background(), []byte(k))
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
	cluster := newTestCluster(t, 3)
	cluster.start()

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]
	key := []byte("overwrite-key")

	// Write initial value
	initialValue := []byte("initial-value")
	err = leader.Put(context.Background(), key, initialValue)
	require.NoError(t, err, "Failed to put initial value")
	t.Logf("Wrote initial value: %s", initialValue)

	// Verify initial value
	retrievedValue, err := leader.Get(context.Background(), key)
	require.NoError(t, err, "Failed to get initial value")
	assert.Equal(t, initialValue, retrievedValue, "Initial value mismatch")

	// Overwrite with new value
	newValue := []byte("new-value-that-overwrites")
	err = leader.Put(context.Background(), key, newValue)
	require.NoError(t, err, "Failed to overwrite value")
	t.Logf("Overwrote with new value: %s", newValue)

	// Verify new value
	retrievedValue, err = leader.Get(context.Background(), key)
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
	cluster := newTestCluster(t, 3)
	cluster.start()

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write test data
	testData := map[string]string{
		"persist-key1": "persist-value1",
		"persist-key2": "persist-value2",
		"persist-key3": "persist-value3",
	}

	for k, v := range testData {
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Wrote %d key-value pairs before restart", len(testData))

	// Give a moment for consensus to propagate
	time.Sleep(100 * time.Millisecond)

	// Stop all nodes
	cluster.stopAll()

	// Restart all nodes
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster")

	// Wait for new leader election
	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after restart")
	t.Logf("New leader elected after restart: node %d", leaderID)

	// Verify all data persisted
	newLeader := cluster.nodes[leaderID-1]
	for k, expectedValue := range testData {
		retrievedValue, err := newLeader.Get(context.Background(), []byte(k))
		require.NoError(t, err, "Failed to get persisted key=%s", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Persisted value mismatch for key=%s.\nExpected: %s\nGot: %s",
			k, expectedValue, retrievedValue)
	}

	t.Logf("Successfully verified data persistence across cluster restart")
}

// =============================================================================
//  WAL Data Recovery Tests
// =============================================================================

// TestWALRecoverySingleNode validates that a single node can recover its state
// from the WAL after an unclean shutdown
func TestWALRecoverySingleNode(t *testing.T) {
	// t.Skip("Skipping: WAL persistence not implemented")

	// Create and start a single-node cluster
	cluster := newTestCluster(t, 3)
	cluster.start()

	// Wait for leader election
	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write test data
	testData := map[string]string{
		"recovery-key1": "recovery-value1",
		"recovery-key2": "recovery-value2",
		"recovery-key3": "recovery-value3",
	}

	for k, v := range testData {
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Wrote %d key-value pairs to WAL", len(testData))

	// Simulate unclean shutdown - stop without proper cleanup
	cluster.stopAll()
	t.Logf("Simulated unclean shutdown")

	// Restart the node - should recover from WAL
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart node")

	// Wait for leader election
	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after recovery")

	// Verify all data recovered from WAL
	newLeader := cluster.nodes[leaderID-1]
	for k, expectedValue := range testData {
		retrievedValue, err := newLeader.Get(context.Background(), []byte(k))
		require.NoError(t, err, "Failed to get recovered key=%s", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Recovered value mismatch for key=%s.\nExpected: %s\nGot: %s",
			k, expectedValue, retrievedValue)
	}

	t.Logf("Successfully verified WAL recovery for single node")
}

// TestWALRecoveryMultipleEntries validates that the WAL can recover a large
// number of entries in the correct order
func TestWALRecoveryMultipleEntries(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write a large number of sequential entries
	numEntries := 100
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("seq-key-%03d", i))
		value := []byte(fmt.Sprintf("seq-value-%03d", i))

		err := leader.Put(context.Background(), key, value)
		require.NoError(t, err, "Failed to put entry %d", i)
	}
	t.Logf("Wrote %d sequential entries to WAL", numEntries)

	// Stop and restart
	cluster.stopAll()
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster")

	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after recovery")

	// Verify all entries recovered in correct order
	newLeader := cluster.nodes[leaderID-1]
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("seq-key-%03d", i))
		expectedValue := []byte(fmt.Sprintf("seq-value-%03d", i))

		retrievedValue, err := newLeader.Get(context.Background(), key)
		require.NoError(t, err, "Failed to get entry %d after recovery", i)

		assert.Equal(t, expectedValue, retrievedValue,
			"Entry %d value mismatch after recovery", i)
	}

	t.Logf("Successfully verified %d entries recovered in correct order", numEntries)
}

// TestWALRecoveryWithOverwrites validates that the WAL correctly recovers
// the final state when keys are overwritten multiple times
func TestWALRecoveryWithOverwrites(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write and overwrite the same key multiple times
	key := []byte("overwrite-recovery-key")

	values := []string{
		"version-1",
		"version-2",
		"version-3",
		"version-4-final",
	}

	for i, val := range values {
		err := leader.Put(context.Background(), key, []byte(val))
		require.NoError(t, err, "Failed to put version %d", i+1)
		t.Logf("Wrote version %d: %s", i+1, val)
	}

	// Stop and restart
	cluster.stopAll()
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster")

	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after recovery")

	// Verify only the final value is present
	newLeader := cluster.nodes[leaderID-1]
	retrievedValue, err := newLeader.Get(context.Background(), key)
	require.NoError(t, err, "Failed to get key after recovery")

	finalValue := []byte(values[len(values)-1])
	assert.Equal(t, finalValue, retrievedValue,
		"After recovery, expected final value %s, got %s", finalValue, retrievedValue)

	// Ensure old values are not present
	for i := 0; i < len(values)-1; i++ {
		assert.NotEqual(t, []byte(values[i]), retrievedValue,
			"Old value %s should not be present after recovery", values[i])
	}

	t.Logf("Successfully verified final state after multiple overwrites")
}

// TestWALRecoveryPartialWrite validates recovery when a write operation
// was in progress during shutdown
func TestWALRecoveryPartialWrite(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write some committed data
	committedData := map[string]string{
		"committed-1": "value-1",
		"committed-2": "value-2",
	}

	for k, v := range committedData {
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put committed key=%s", k)
	}
	t.Logf("Wrote %d committed entries", len(committedData))

	// In a real scenario, we would simulate a partial write here
	// For now, we test that committed data survives

	// Stop and restart
	cluster.stopAll()
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster")

	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after recovery")

	// Verify committed data is intact
	newLeader := cluster.nodes[leaderID-1]
	for k, expectedValue := range committedData {
		retrievedValue, err := newLeader.Get(context.Background(), []byte(k))
		require.NoError(t, err, "Failed to get committed key=%s after recovery", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Committed value mismatch for key=%s after recovery", k)
	}

	t.Logf("Successfully verified committed data survived partial write scenario")
}

// TestWALRecoveryEmptyLog validates that recovery works correctly when
// the WAL is empty (new node or fresh start)
func TestWALRecoveryEmptyLog(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")
	t.Logf("Initial leader elected: node %d", leaderID)

	// Immediately restart without writing anything
	cluster.stopAll()
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster with empty WAL")

	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after restart with empty WAL")

	// Verify we can still write and read
	leader := cluster.nodes[leaderID-1]
	key := []byte("post-empty-recovery")
	value := []byte("test-value")

	err = leader.Put(context.Background(), key, value)
	require.NoError(t, err, "Failed to put after empty WAL recovery")

	retrievedValue, err := leader.Get(context.Background(), key)
	require.NoError(t, err, "Failed to get after empty WAL recovery")

	assert.Equal(t, value, retrievedValue, "Value mismatch after empty WAL recovery")

	t.Logf("Successfully verified recovery from empty WAL")
}

// TestWALRecoveryMultipleRestarts validates that data persists across
// multiple restart cycles
func TestWALRecoveryMultipleRestarts(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Accumulate data across multiple restart cycles
	allData := make(map[string]string)

	for cycle := 0; cycle < 3; cycle++ {
		// Write data in this cycle
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("cycle-%d-key-%d", cycle, i)
			value := fmt.Sprintf("cycle-%d-value-%d", cycle, i)

			err := leader.Put(context.Background(), []byte(key), []byte(value))
			require.NoError(t, err, "Failed to put in cycle %d", cycle)

			allData[key] = value
		}
		t.Logf("Cycle %d: wrote 5 entries", cycle)

		// Restart cluster
		cluster.stopAll()
		err = cluster.restartAll()
		require.NoError(t, err, "Failed to restart in cycle %d", cycle)

		leaderID, err = cluster.waitForLeader(5 * time.Second)
		require.NoError(t, err, "Failed to elect leader after restart in cycle %d", cycle)

		leader = cluster.nodes[leaderID-1]
	}

	// Verify all data from all cycles
	for k, expectedValue := range allData {
		retrievedValue, err := leader.Get(context.Background(), []byte(k))
		require.NoError(t, err, "Failed to get key=%s after multiple restarts", k)

		assert.Equal(t, []byte(expectedValue), retrievedValue,
			"Value mismatch for key=%s after multiple restarts", k)
	}

	t.Logf("Successfully verified %d entries across 3 restart cycles", len(allData))
}

// TestWALRecoveryConsistencyAcrossNodes validates that all nodes in a cluster
// recover to the same consistent state from their WALs
func TestWALRecoveryConsistencyAcrossNodes(t *testing.T) {
	t.Skip("Skipping: WAL persistence not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write data that should replicate to all nodes
	testData := map[string]string{
		"consistent-key1": "consistent-value1",
		"consistent-key2": "consistent-value2",
		"consistent-key3": "consistent-value3",
	}

	for k, v := range testData {
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}
	t.Logf("Wrote %d entries to leader", len(testData))

	// Wait for replication
	time.Sleep(200 * time.Millisecond)

	// Stop all nodes
	cluster.stopAll()

	// Restart all nodes - each should recover from its own WAL
	err = cluster.restartAll()
	require.NoError(t, err, "Failed to restart cluster")

	leaderID, err = cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader after recovery")

	// Verify all nodes have consistent data
	for nodeIdx, node := range cluster.nodes {
		t.Logf("Verifying node %d", nodeIdx+1)

		for k, expectedValue := range testData {
			retrievedValue, err := node.Get(context.Background(), []byte(k))
			require.NoError(t, err, "Node %d: failed to get key=%s", nodeIdx+1, k)

			assert.Equal(t, []byte(expectedValue), retrievedValue,
				"Node %d: value mismatch for key=%s", nodeIdx+1, k)
		}
	}

	t.Logf("Successfully verified WAL recovery consistency across all nodes")
}

// TestWALRecoveryWithCorruption validates that the system handles
// corrupted WAL entries gracefully
func TestWALRecoveryWithCorruption(t *testing.T) {
	t.Skip("Skipping: WAL persistence and corruption detection not implemented")

	cluster := newTestCluster(t, 3)
	cluster.start()

	leaderID, err := cluster.waitForLeader(5 * time.Second)
	require.NoError(t, err, "Failed to elect leader")

	leader := cluster.nodes[leaderID-1]

	// Write some valid data
	validData := map[string]string{
		"valid-key1": "valid-value1",
		"valid-key2": "valid-value2",
	}

	for k, v := range validData {
		err := leader.Put(context.Background(), []byte(k), []byte(v))
		require.NoError(t, err, "Failed to put key=%s", k)
	}

	// In a real implementation, we would:
	// 1. Stop the node
	// 2. Corrupt the WAL file (flip some bits, truncate, etc.)
	// 3. Restart and verify recovery behavior
	//
	// Expected behavior:
	// - WAL should detect corruption via CRC or checksum
	// - Recovery should stop at the corruption point
	// - Valid entries before corruption should be recovered
	// - System should log corruption error clearly

	t.Logf("Corruption handling test placeholder - implement with WAL")
}

// =============================================================================
//  Recovery Tests
// =============================================================================
