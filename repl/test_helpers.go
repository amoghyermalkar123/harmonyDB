package repl

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// createTempConfig creates a temporary YAML config file with the given content
func createTempConfig(t *testing.T, content string) string {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "test-config-*.yaml")
	require.NoError(t, err)

	_, err = tmpfile.Write([]byte(content))
	require.NoError(t, err)
	tmpfile.Close()

	t.Cleanup(func() {
		os.Remove(tmpfile.Name())
	})

	return tmpfile.Name()
}

// createSingleNodeConfig creates a config for a single node cluster
func createSingleNodeConfig(t *testing.T, nodeID int64, port int) string {
	t.Helper()

	configYAML := fmt.Sprintf(`cluster:
  nodes:
    - id: %d
      address: "localhost:%d"
this_node_id: %d
`, nodeID, port, nodeID)

	return createTempConfig(t, configYAML)
}

// createMultiNodeConfig creates a config for a multi-node cluster
func createMultiNodeConfig(t *testing.T, thisNodeID int64, nodes []NodeInfo) string {
	t.Helper()

	configYAML := "cluster:\n  nodes:\n"
	for _, node := range nodes {
		configYAML += fmt.Sprintf("    - id: %d\n      address: \"%s\"\n", node.ID, node.Address)
	}
	configYAML += fmt.Sprintf("this_node_id: %d\n", thisNodeID)

	return createTempConfig(t, configYAML)
}

// setupTestNode creates and starts a test node
func setupTestNode(t *testing.T, configPath string) *Node {
	t.Helper()

	node, err := NewRaftNode(configPath)
	require.NoError(t, err)

	err = node.start()
	require.NoError(t, err)

	t.Cleanup(func() {
		node.stop()
	})

	return node
}

// setupTestCluster creates and starts multiple nodes in a cluster
func setupTestCluster(t *testing.T, nodes []NodeInfo) []*Node {
	t.Helper()

	cluster := make([]*Node, 0, len(nodes))

	for _, nodeInfo := range nodes {
		configPath := createMultiNodeConfig(t, nodeInfo.ID, nodes)
		node := setupTestNode(t, configPath)
		cluster = append(cluster, node)
	}

	return cluster
}
