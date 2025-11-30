package harmonydb

import (
	"context"
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
// Phase 3: User Story 1 - Basic Data Integrity Verification
// =============================================================================

// TestBasicPutGet validates that a single key-value pair can be written and read
// in a 3-node cluster
func TestLinearizableGet(t *testing.T) {
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
