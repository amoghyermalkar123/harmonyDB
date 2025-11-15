package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClusterBasic tests basic cluster creation and health
func TestClusterBasic(t *testing.T) {
	cluster := NewCluster(t, ClusterConfig{Size: 3})

	// Verify cluster size
	assert.Equal(t, 3, cluster.Size(), "Cluster should have 3 servers")
	assert.Len(t, cluster.Servers(), 3, "Should have 3 server instances")

	// Test server access
	server0 := cluster.Server(0)
	assert.NotNil(t, server0, "Server 0 should exist")

	// Verify each server is healthy
	for i := range cluster.Size() {
		server := cluster.Server(i)
		resp, err := server.Client().Get(server.BaseURL() + "/health")
		require.NoError(t, err, "Server %d health check failed", i)
		defer resp.Body.Close()
		assert.Equal(t, 200, resp.StatusCode, "Server %d should be healthy", i)
	}
}

// TestClusterLeaderElection tests leader election in the cluster
func TestClusterLeaderElection(t *testing.T) {
	cluster := NewCluster(t, ClusterConfig{Size: 3})

	// Wait for leader election with realistic timeout (Raft election timeout is ~1-3 seconds,
	// plus time for peer discovery through Consul)
	leaderIndex, err := cluster.WaitForLeader(0)
	require.NoError(t, err, "Leader should be elected within timeout")
	assert.GreaterOrEqual(t, leaderIndex, 0, "Valid leader index should be returned")
	assert.Less(t, leaderIndex, cluster.Size(), "Leader index should be within cluster bounds")

	t.Logf("Leader elected at index %d", leaderIndex)
}

// TestClusterProgress tests that the cluster can make progress
func TestClusterProgress(t *testing.T) {
	cluster := NewCluster(t, ClusterConfig{Size: 3})

	// Wait for leader election first with realistic timeout
	_, err := cluster.WaitForLeader(0)
	require.NoError(t, err, "Leader should be elected before testing progress")

	// Test cluster can make progress
	err = cluster.MustProgress("test_key", "test_value")
	assert.NoError(t, err, "Cluster should be able to make progress")
}
