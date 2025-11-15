package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUtil provides common testing utilities for embedded tests
type TestUtil struct {
	cluster *Cluster
	t       *testing.T
}

// NewTestUtil creates a new test utility instance
func NewTestUtil(t *testing.T, cluster *Cluster) *TestUtil {
	return &TestUtil{
		cluster: cluster,
		t:       t,
	}
}

// WaitForClusterReady waits for the cluster to be ready (leader elected and healthy)
func (tu *TestUtil) WaitForClusterReady(timeout time.Duration) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Wait for leader election
	leaderIndex, err := tu.cluster.WaitForLeader(timeout)
	require.NoError(tu.t, err, "Cluster should elect a leader")
	tu.t.Logf("Leader elected at index %d", leaderIndex)

	// Verify all nodes are healthy
	for i := 0; i < tu.cluster.Size(); i++ {
		server := tu.cluster.Server(i)
		resp, err := server.Client().Get(server.BaseURL() + "/health")
		require.NoError(tu.t, err, "Server %d should be healthy", i)
		resp.Body.Close()
		assert.Equal(tu.t, http.StatusOK, resp.StatusCode, "Server %d health check", i)
	}
}

// PutKey performs a PUT operation and verifies it succeeds
func (tu *TestUtil) PutKey(key, value string) {
	err := tu.cluster.MustProgress(key, value)
	require.NoError(tu.t, err, "PUT operation should succeed for key=%s, value=%s", key, value)
}

// GetKey performs a GET operation and returns the value
func (tu *TestUtil) GetKey(key string) string {
	// Try GET on each server until one succeeds (following the same pattern as PUT)
	for i, server := range tu.cluster.Servers() {
		value, err := tu.getKeyFromServer(server, key)
		if err == nil {
			tu.t.Logf("Successfully read %s from server %d", key, i)
			return value
		}
	}
	tu.t.Fatalf("Failed to GET key %s from any server", key)
	return ""
}

// VerifyKeyValue performs a GET and verifies the value matches
func (tu *TestUtil) VerifyKeyValue(key, expectedValue string) {
	actualValue := tu.GetKey(key)
	assert.Equal(tu.t, expectedValue, actualValue, "Value for key %s should match", key)
}

// GetLeaderServer returns the server that is currently the leader
func (tu *TestUtil) GetLeaderServer() *EmbeddedServer {
	leaderIndex, err := tu.cluster.WaitForLeader(10 * time.Second)
	require.NoError(tu.t, err, "Should be able to identify leader")
	return tu.cluster.Server(leaderIndex)
}

// WaitForValueReplication waits for a key-value pair to be replicated across the cluster
func (tu *TestUtil) WaitForValueReplication(key, expectedValue string, timeout time.Duration) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allMatch := true
		for _, server := range tu.cluster.Servers() {
			value, err := tu.getKeyFromServer(server, key)
			if err != nil || value != expectedValue {
				allMatch = false
				break
			}
		}
		if allMatch {
			tu.t.Logf("Key %s successfully replicated with value %s", key, expectedValue)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	tu.t.Fatalf("Key %s was not replicated with value %s within timeout", key, expectedValue)
}

// Helper method to get a key from a specific server
func (tu *TestUtil) getKeyFromServer(server *EmbeddedServer, key string) (string, error) {
	getData := map[string]string{
		"key": key,
	}
	jsonData, err := json.Marshal(getData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal GET data: %w", err)
	}

	resp, err := server.Client().Post(server.BaseURL()+"/get", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET request failed with status %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode GET response: %w", err)
	}

	if !response["success"].(bool) {
		errorMsg, _ := response["error"].(string)
		return "", fmt.Errorf("GET request failed: %s", errorMsg)
	}

	value, ok := response["value"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	return value, nil
}
