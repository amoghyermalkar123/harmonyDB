package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Cluster represents a cluster of HarmonyDB servers
type Cluster struct {
	servers []*EmbeddedServer
	size    int
	t       *testing.T
}

// ClusterConfig holds configuration for creating a cluster
type ClusterConfig struct {
	Size int // Number of servers in the cluster
}

// NewCluster creates a new embedded cluster
func NewCluster(t *testing.T, cfg ClusterConfig) *Cluster {
	if cfg.Size <= 0 {
		cfg.Size = 3 // Default cluster size
	}

	cluster := &Cluster{
		servers: make([]*EmbeddedServer, 0, cfg.Size),
		size:    cfg.Size,
		t:       t,
	}

	// Create servers
	for i := range cfg.Size {
		server, err := NewEmbeddedServer(t, EmbeddedServerConfig{})
		if err != nil {
			t.Fatalf("Failed to create server %d: %v", i, err)
		}
		cluster.servers = append(cluster.servers, server)
	}

	// Start all servers
	for i, server := range cluster.servers {
		if err := server.Start(); err != nil {
			t.Fatalf("Failed to start server %d: %v", i, err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		cluster.Terminate()
	})

	return cluster
}

// Servers returns all servers in the cluster
func (c *Cluster) Servers() []*EmbeddedServer {
	return c.servers
}

// Server returns a specific server by index
func (c *Cluster) Server(index int) *EmbeddedServer {
	if index < 0 || index >= len(c.servers) {
		c.t.Fatalf("Invalid server index %d, cluster has %d servers", index, len(c.servers))
	}
	return c.servers[index]
}

// Size returns the number of servers in the cluster
func (c *Cluster) Size() int {
	return c.size
}

// Terminate stops all servers in the cluster
func (c *Cluster) Terminate() {
	for _, server := range c.servers {
		server.Stop()
	}
}

// WaitForLeader waits for a leader to be elected and returns the leader index
func (c *Cluster) WaitForLeader(timeout time.Duration) (int, error) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		for i, server := range c.servers {
			leaderID, err := c.getServerLeaderID(server)
			if err == nil && leaderID != 0 {
				// Found a leader, verify it's consistent across servers
				if c.verifyLeaderConsistency(leaderID) {
					return i, nil
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return -1, fmt.Errorf("no leader elected within timeout")
}

// MustProgress verifies that the cluster can make progress by performing a write operation
func (c *Cluster) MustProgress(key, value string) error {
	// Given the leader redirection issues we're seeing, let's implement a retry-based approach
	// that accounts for the fact that leader election may be in flux

	maxRetries := 10
	retryDelay := 500 * time.Millisecond

	for attempt := range maxRetries {
		// Try each server in sequence
		for i, server := range c.servers {
			err := c.putKeyValue(server, key, value)
			if err == nil {
				c.t.Logf("Successfully wrote %s=%s to server %d on attempt %d", key, value, i, attempt+1)
				return nil
			}

			if err.Error() == "server unavailable (redirection failed)" {
				continue // Try next server
			}

			c.t.Logf("Server %d attempt %d failed: %v", i, attempt+1, err)
		}

		// If all servers failed this round, wait and try again
		if attempt < maxRetries-1 {
			c.t.Logf("All servers failed on attempt %d, waiting %v before retry", attempt+1, retryDelay)
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("cluster could not make progress after %d attempts", maxRetries)
}

// getServerLeaderID gets the leader ID from a server
func (c *Cluster) getServerLeaderID(server *EmbeddedServer) (int64, error) {
	resp, err := server.Client().Get(server.BaseURL() + "/leader")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("leader endpoint returned %d", resp.StatusCode)
	}

	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("failed to decode leader response: %w", err)
	}

	if !response["success"].(bool) {
		return 0, fmt.Errorf("no leader elected")
	}

	// Parse leader ID from value field
	valueStr, ok := response["value"].(string)
	if !ok {
		return 0, fmt.Errorf("invalid leader ID format")
	}

	var leaderID int64
	if _, err := fmt.Sscanf(valueStr, "%d", &leaderID); err != nil {
		return 0, fmt.Errorf("failed to parse leader ID: %w", err)
	}

	return leaderID, nil
}

// verifyLeaderConsistency checks that all servers agree on the leader
func (c *Cluster) verifyLeaderConsistency(leaderID int64) bool {
	for _, server := range c.servers {
		reportedLeaderID, err := c.getServerLeaderID(server)
		if err != nil || reportedLeaderID != leaderID {
			return false
		}
	}
	return true
}

// putKeyValue performs a PUT operation on a server
func (c *Cluster) putKeyValue(server *EmbeddedServer, key, value string) error {
	putData := map[string]string{
		"key":   key,
		"value": value,
	}
	jsonData, err := json.Marshal(putData)
	if err != nil {
		return fmt.Errorf("failed to marshal PUT data: %w", err)
	}

	resp, err := server.Client().Post(server.BaseURL()+"/put", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// If we get 503 (Service Unavailable), it means the server tried to redirect but failed
	// In this case, we should try to handle this at the test level by finding the actual leader
	if resp.StatusCode == 503 {
		return fmt.Errorf("server unavailable (redirection failed)")
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("PUT request failed with status %d", resp.StatusCode)
	}

	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode PUT response: %w", err)
	}

	if !response["success"].(bool) {
		errorMsg, _ := response["error"].(string)
		return fmt.Errorf("PUT request failed: %s", errorMsg)
	}

	return nil
}
