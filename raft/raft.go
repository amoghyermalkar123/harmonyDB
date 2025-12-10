package raft

import (
	"context"
	"fmt"
	"harmonydb/raft/proto"
	"net"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// NodeConfig represents configuration for a single node
type NodeConfig struct {
	ID       int64
	RaftPort int
	HTTPPort int
	Address  string // hostname/IP address
}

// ClusterConfig represents static cluster configuration
type ClusterConfig struct {
	ThisNodeID int64
	Nodes      map[int64]NodeConfig // nodeID -> node config
}

// NewRaftServerWithConfig creates a new Raft server with static configuration
func NewRaftServerWithConfig(clusterConfig ClusterConfig) *Raft {
	// Find this node's configuration
	thisNodeConfig, exists := clusterConfig.Nodes[clusterConfig.ThisNodeID]
	if !exists {
		panic(fmt.Sprintf("Configuration for node ID %d not found", clusterConfig.ThisNodeID))
	}

	// Use the global logger as default
	nodeLogger := getLogger()

	r := &Raft{
		n:      newRaftNode(clusterConfig.ThisNodeID, nodeLogger),
		config: clusterConfig,
	}

	// Start Raft server first so peers can connect to this node
	go r.startWithPort(thisNodeConfig.RaftPort)

	// Wait briefly for server to start listening
	time.Sleep(100 * time.Millisecond)

	// Initialize cluster connections
	r.initializeCluster()

	// Start election process
	go r.n.startElection()

	return r
}

// NewRaftServerWithLogger creates a new Raft server with custom logger (for testing)
func NewRaftServerWithLogger(clusterConfig ClusterConfig, logger *zap.Logger) *Raft {
	// Find this node's configuration
	thisNodeConfig, exists := clusterConfig.Nodes[clusterConfig.ThisNodeID]
	if !exists {
		panic(fmt.Sprintf("Configuration for node ID %d not found", clusterConfig.ThisNodeID))
	}

	r := &Raft{
		n:      newRaftNode(clusterConfig.ThisNodeID, logger),
		config: clusterConfig,
	}

	// Start Raft server first so peers can connect to this node
	go r.startWithPort(thisNodeConfig.RaftPort)

	// Wait briefly for server to start listening
	time.Sleep(100 * time.Millisecond)

	// Initialize cluster connections
	r.initializeCluster()

	// Start election process
	go r.n.startElection()

	return r
}

// Put is a client side interface which is the main entry point for
// log replication
func (r *Raft) Put(ctx context.Context, key, val []byte, requestID uint64) error {
	r.n.meta.RLock()
	isLeader := r.n.meta.nt == Leader
	r.n.meta.RUnlock()

	if !isLeader {
		return ErrNotALeader
	}

	return r.n.replicate(ctx, key, val, requestID)
}

// initializeCluster establishes connections to peer nodes
func (r *Raft) initializeCluster() {
	cluster := make(map[int64]proto.RaftClient)

	for nodeID, nodeConfig := range r.config.Nodes {
		if nodeID == r.n.ID {
			continue // Skip self
		}

		// Create gRPC connection to peer
		address := fmt.Sprintf("%s:%d", nodeConfig.Address, nodeConfig.RaftPort)
		conn, err := grpc.Dial(address, grpc.WithInsecure())
		if err != nil {
			r.n.logger.Warn("Failed to connect to peer",
				zap.String("component", "raft"),
				zap.Int64("peer_id", nodeID),
				zap.String("address", address),
				zap.Error(err))
			continue
		}

		client := proto.NewRaftClient(conn)
		cluster[nodeID] = client

		r.n.logger.Info("Connected to peer",
			zap.String("component", "raft"),
			zap.Int64("peer_id", nodeID),
			zap.String("address", address))
	}

	r.n.Lock()
	defer r.n.Unlock()

	r.n.cluster = cluster

	// Initialize nextIndex for all peers
	if len(r.n.nextIndex) == 0 {
		r.n.nextIndex = make(map[int64]int64)
		for peerID := range cluster {
			r.n.nextIndex[peerID] = 1
		}
	}

	r.n.logger.Info("Cluster initialization complete",
		zap.String("component", "raft"),
		zap.Int("peer_count", len(cluster)))
}

func (r *Raft) startWithPort(port int) {
	r.server = grpc.NewServer()

	proto.RegisterRaftServer(r.server, r.n)

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		r.n.logger.Fatal("Failed to listen on port", zap.String("component", "raft"), zap.Int("port", port), zap.Error(err))
	}

	r.n.logger.Info("Raft server listening", zap.String("component", "raft"), zap.Int("port", port))
	r.server.Serve(lis)
}

func (r *Raft) GetLeader() proto.RaftClient {
	return r.n.cluster[r.n.leaderID]
}

// GetLeaderID returns the current leader's node ID
func (r *Raft) GetLeaderID() int64 {
	r.n.RLock()
	defer r.n.RUnlock()
	return r.n.leaderID
}

// GetLeaderAddress returns the HTTP address of the current leader
func (r *Raft) GetLeaderAddress() (string, error) {
	leaderID := r.GetLeaderID()
	if leaderID == 0 {
		return "", fmt.Errorf("no leader elected")
	}

	if leaderID == r.n.ID {
		return "", fmt.Errorf("this node is the leader")
	}

	// Use static configuration to find the leader's address
	leaderConfig, exists := r.config.Nodes[leaderID]
	if !exists {
		return "", fmt.Errorf("leader node configuration not found")
	}

	return fmt.Sprintf("http://%s:%d", leaderConfig.Address, leaderConfig.HTTPPort), nil
}

// GetConfig returns the cluster configuration
func (r *Raft) GetConfig() ClusterConfig {
	return r.config
}

// Stop immediately shuts down the Raft server (for testing/partition simulation)
func (r *Raft) Stop() {
	if r.server != nil {
		r.n.logger.Info("Stopping Raft server", zap.String("component", "raft"))
		r.server.Stop() // Immediate stop, not graceful
	}

	// Close WAL file
	if r.n.logManager != nil {
		if err := r.n.logManager.Close(); err != nil {
			r.n.logger.Error("Failed to close WAL file", zap.Error(err))
		}
	}
}

// GetTerm returns the current term
func (r *Raft) GetTerm() int64 {
	r.n.state.Lock()
	defer r.n.state.Unlock()
	return r.n.state.currentTerm
}

// GetCommitIndex returns the current commit index
func (r *Raft) GetCommitIndex() int64 {
	r.n.meta.RLock()
	defer r.n.meta.RUnlock()
	return r.n.meta.lastCommitIndex
}

// SetCommitIndex sets the commit index (used during WAL recovery)
// This directly sets the value without triggering apply logic
func (r *Raft) SetCommitIndex(idx int64) {
	r.n.meta.Lock()
	r.n.meta.lastCommitIndex = idx
	r.n.meta.Unlock()
}

// GetLogEntry returns a log entry at a specific index
func (r *Raft) GetLogEntry(index int) *proto.Log {
	return r.n.logManager.GetLog(index)
}

func (n *Raft) GetLogsAfter(idx int64) []*proto.Log {
	return n.n.logManager.GetLogsAfter(idx)
}

// GetLastLogID returns the ID of the last log entry in the WAL
func (r *Raft) GetLastLogID() int64 {
	return r.n.logManager.GetLastLogID()
}
