package raft

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type testCluster struct {
	t        *testing.T
	nodes    []*Raft
	nodeIDs  []int64
	basePort int
	mu       sync.Mutex
}

func newTestCluster(t *testing.T, n int) *testCluster {
	c := &testCluster{
		t:        t,
		nodes:    make([]*Raft, n),
		nodeIDs:  make([]int64, n),
		basePort: 4000,
	}

	nodeConfigs := make(map[int64]NodeConfig)

	for i := range n {
		nodeID := int64(i + 1)
		c.nodeIDs[i] = nodeID
		nodeConfigs[nodeID] = NodeConfig{
			ID:       nodeID,
			RaftPort: c.basePort + i,
			HTTPPort: c.basePort + 1000 + i,
			Address:  "localhost",
		}
	}

	for i := range n {
		nodeID := c.nodeIDs[i]
		config := ClusterConfig{
			ThisNodeID: nodeID,
			Nodes:      nodeConfigs,
		}
		c.nodes[i] = NewRaftServerWithConfig(config)
	}

	return c
}

func (c *testCluster) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, node := range c.nodes {
		if node != nil {
			node.Stop()
		}
	}
}

func (c *testCluster) waitForLeader(timeout time.Duration) (*Raft, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		for _, node := range c.nodes {
			if node.n.meta.nt == Leader {
				return node, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("no leader elected within timeout")
}

func (c *testCluster) getLeader() *Raft {
	c.mu.Lock()
	defer c.mu.Unlock()

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

func (c *testCluster) getFollowers() []*Raft {
	c.mu.Lock()
	defer c.mu.Unlock()

	var followers []*Raft
	for _, node := range c.nodes {
		node.n.meta.RLock()
		isFollower := node.n.meta.nt == Follower
		node.n.meta.RUnlock()

		if isFollower {
			followers = append(followers, node)
		}
	}
	return followers
}

func (c *testCluster) stopNode(nodeID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, id := range c.nodeIDs {
		if id == nodeID {
			if c.nodes[i] != nil {
				c.nodes[i].Stop()
				return nil
			}
		}
	}
	return fmt.Errorf("node %d not found", nodeID)
}

func (c *testCluster) waitForCommit(nodeID int64, expectedIndex int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	var targetNode *Raft
	for i, id := range c.nodeIDs {
		if id == nodeID {
			targetNode = c.nodes[i]
			break
		}
	}

	if targetNode == nil {
		return fmt.Errorf("node %d not found", nodeID)
	}

	for time.Now().Before(deadline) {
		commitIdx := targetNode.GetCommitIndex()
		if commitIdx >= expectedIndex {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	return fmt.Errorf("commit index did not reach %d within timeout", expectedIndex)
}

func (c *testCluster) waitForAllCommit(expectedIndex int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allCommitted := true
		for _, node := range c.nodes {
			if node.GetCommitIndex() < expectedIndex {
				allCommitted = false
				break
			}
		}
		if allCommitted {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	return fmt.Errorf("not all nodes committed to index %d within timeout", expectedIndex)
}

func (c *testCluster) put(ctx context.Context, key, val []byte) error {
	leader := c.getLeader()
	if leader == nil {
		return fmt.Errorf("no leader available")
	}

	return leader.Put(ctx, key, val)
}

func TestRaftLeaderElection(t *testing.T) {
	// Initialize logger for test - use development config for better test output
	config := zap.NewDevelopmentConfig()
	testLogger, err := config.Build()
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}
	defer testLogger.Sync()

	// Set logger for raft package
	SetLogger(testLogger)

	cl := newTestCluster(t, 3)
	defer cl.shutdown()

	leader, err := cl.waitForLeader(1 * time.Second)
	if err != nil {
		t.Fatal("waitForLeader:", err)
	}

	assert.NotNil(t, leader)

	fmt.Println("====")

	if err := cl.put(context.Background(), []byte("key"), []byte("value")); err != nil {
		t.Fatal("put:", err)
	}

	fmt.Println("==== waiting for commit")

	if err := cl.waitForAllCommit(1, 200*time.Millisecond); err != nil {
		t.Fatal("waitForAllCommit:", err)
	}
}

func TestRaftLogAppend(t *testing.T) {
}

func TestRaftLogCommit(t *testing.T) {
}

func TestRaftLogRecovery(t *testing.T) {
}
