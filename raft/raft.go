package raft

import (
	"context"
	"fmt"
	"harmonydb/raft/proto"
	"net"

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

	r := &Raft{
		n:      newRaftNode(),
		config: clusterConfig,
	}

	// Set the node ID to match configuration
	r.n.ID = clusterConfig.ThisNodeID

	// Initialize cluster connections
	r.initializeCluster()

	// Start Raft server
	go r.startWithPort(thisNodeConfig.RaftPort)
	go r.n.startElection()

	getLogger().Info("Started Raft server with static configuration",
		zap.String("component", "raft"),
		zap.Int64("node_id", r.n.ID),
		zap.Int("raft_port", thisNodeConfig.RaftPort),
		zap.Int("http_port", thisNodeConfig.HTTPPort),
		zap.Int("cluster_size", len(clusterConfig.Nodes)))

	return r
}

// Put is a client side interface which is the main entry point for
// log replication
// TODO: add functionality to store the leader info
func (r *Raft) Put(ctx context.Context, key, val []byte) error {
	r.n.meta.RLock()
	isLeader := r.n.meta.nt == Leader
	r.n.meta.RUnlock()

	if !isLeader {
		// TODO: Forward request to leader
		return ErrNotALeader
	}
	return r.n.replicate(ctx, key, val)
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
			getLogger().Warn("Failed to connect to peer",
				zap.String("component", "raft"),
				zap.Int64("peer_id", nodeID),
				zap.String("address", address),
				zap.Error(err))
			continue
		}

		client := proto.NewRaftClient(conn)
		cluster[nodeID] = client

		getLogger().Info("Connected to peer",
			zap.String("component", "raft"),
			zap.Int64("peer_id", nodeID),
			zap.String("address", address))
	}

	r.n.Lock()
	r.n.cluster = cluster

	// Initialize nextIndex for all peers
	if len(r.n.nextIndex) == 0 {
		r.n.nextIndex = make(map[int64]int64)
		for peerID := range cluster {
			r.n.nextIndex[peerID] = 1
		}
	}
	r.n.Unlock()

	getLogger().Info("Cluster initialization complete",
		zap.String("component", "raft"),
		zap.Int("peer_count", len(cluster)))
}

func (r *Raft) startWithPort(port int) {
	r.server = grpc.NewServer()

	proto.RegisterRaftServer(r.server, r.n)

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		getLogger().Fatal("Failed to listen on port", zap.String("component", "raft"), zap.Int("port", port), zap.Error(err))
	}

	getLogger().Info("Raft server listening", zap.String("component", "raft"), zap.Int("port", port))
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
		getLogger().Info("Stopping Raft server", zap.String("component", "raft"))
		r.server.Stop() // Immediate stop, not graceful
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

// GetLogEntry returns a log entry at a specific index
func (r *Raft) GetLogEntry(index int) *proto.Log {
	return r.n.logManager.GetLog(index)
}
