package raft

import (
	"fmt"
	"harmonydb/raft/proto"
	"net"
	"strconv"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func NewRaftServerWithConsul(raftPort int, httpPort int) *Raft {
	r := &Raft{
		n: newRaftNode(),
	}

	// Initialize Consul service discovery
	discovery, err := NewConsulDiscovery(r.n.ID, raftPort, httpPort)
	if err != nil {
		getLogger().Error("Failed to initialize Consul discovery", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Int("raft_port", raftPort), zap.Int("http_port", httpPort), zap.Error(err))
		getLogger().Info("Make sure Consul is running: consul agent -dev", zap.String("component", "raft"))
		panic(err)
	}

	// Register this node with Consul
	if err := discovery.Register(); err != nil {
		getLogger().Error("Failed to register with Consul", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Error(err))
		panic(err)
	}
	getLogger().Info("Successfully registered node with Consul", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Int("raft_port", raftPort), zap.Int("http_port", httpPort))

	// Discover and connect to existing peers
	cluster, err := discovery.DiscoverPeers()
	if err != nil {
		getLogger().Warn("Initial peer discovery failed", zap.String("component", "raft"), zap.Int64("node_id", r.n.ID), zap.Error(err))
	}
	r.n.cluster = cluster

	// Watch for cluster changes
	discovery.WatchForChanges(func(newCluster map[int64]proto.RaftClient) {
		r.n.Lock()
		r.n.cluster = newCluster
		// initialize the nextIndex once if not previously initialized
		// hence the len check, no need to update nextIndex as it's managed by
		// the core raft algorithm
		if len(r.n.nextIndex) == 0 {
			for pr := range newCluster {
				// Since our main proto.Log is a 1-indexed array because
				// Id's of the commands start from 1
				r.n.nextIndex[pr] = 1
			}
		}
		r.n.Unlock()
	})

	// Store discovery reference for cleanup
	r.discovery = discovery

	go r.startWithPort(raftPort)
	go r.n.startElection()

	return r
}

func (r *Raft) startWithPort(port int) {
	serv := grpc.NewServer()

	proto.RegisterRaftServer(serv, r.n)

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		getLogger().Fatal("Failed to listen on port", zap.String("component", "raft"), zap.Int("port", port), zap.Error(err))
	}

	getLogger().Info("Raft server listening", zap.String("component", "raft"), zap.Int("port", port))
	serv.Serve(lis)
}

// GetDiscovery returns the Consul discovery instance for cleanup
func (r *Raft) GetDiscovery() *ConsulDiscovery {
	return r.discovery
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

	// Use Consul to discover the leader's address
	services, _, err := r.discovery.client.Health().Service(RaftServiceName, "", true, nil)
	if err != nil {
		return "", fmt.Errorf("failed to query consul for services: %v", err)
	}

	for _, service := range services {
		nodeIDStr, exists := service.Service.Meta["node_id"]
		if !exists {
			continue
		}

		nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
		if err != nil {
			continue
		}

		if nodeID == leaderID {
			// Get HTTP port from service metadata
			httpPortStr, exists := service.Service.Meta["http_port"]
			if !exists {
				return "", fmt.Errorf("http_port not found in leader's service metadata")
			}

			httpPort, err := strconv.Atoi(httpPortStr)
			if err != nil {
				return "", fmt.Errorf("invalid http_port in leader's service metadata: %s", httpPortStr)
			}

			return fmt.Sprintf("http://%s:%d", service.Service.Address, httpPort), nil
		}
	}

	return "", fmt.Errorf("leader address not found in consul")
}
