package repl

import (
	"fmt"
	"harmonydb/wal"
	"log/slog"
	"net"
	"os"

	proto "harmonydb/repl/proto/repl"

	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

type NodeOptions func(*Node)

func WithWal(wal wal.Storage) NodeOptions {
	return func(n *Node) {
		n.wal = wal
	}
}

func NewRaftNode(configPath string) (*Node, error) {
	return bootstrapRaftNode(configPath)
}

func loadConfig(path string) (*ClusterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ClusterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func bootstrapRaftNode(configPath string, options ...NodeOptions) (*Node, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err, "config_path", configPath)
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Find this node's address from config
	var thisNodeAddr string
	for _, nodeInfo := range config.Cluster.Nodes {
		if nodeInfo.ID == config.ThisNodeID {
			thisNodeAddr = nodeInfo.Address
			break
		}
	}

	if thisNodeAddr == "" {
		slog.Error("node ID not found in cluster config", "node_id", config.ThisNodeID)
		return nil, fmt.Errorf("node ID %d not found in cluster config", config.ThisNodeID)
	}

	// Build peer list (excluding this node)
	var peers []int64
	for _, nodeInfo := range config.Cluster.Nodes {
		if nodeInfo.ID != config.ThisNodeID {
			peers = append(peers, nodeInfo.ID)
		}
	}

	// Initialize WAL if not provided via options
	walPath := fmt.Sprintf("./data/node_%d_wal.log", config.ThisNodeID)
	walStorage := wal.NewLM(walPath)

	node := &Node{
		ID:      config.ThisNodeID,
		address: thisNodeAddr,
		members: make(map[int64]grpc.ClientConnInterface),
		config:  config,
		raft:    NewRaft(config.ThisNodeID, peers),
		propc:   make(chan *proto.Message, 256),
		applyc:  make(chan ToApply, 256),
		tickc:   make(chan struct{}, 128),
		wal:     walStorage,
	}

	// apply optional configs (can override WAL)
	for _, opt := range options {
		opt(node)
	}

	// Connect to all peer nodes
	for _, peer := range config.Cluster.Nodes {
		if peer.ID == node.ID {
			continue
		}

		conn, err := grpc.Dial(peer.Address, grpc.WithInsecure())
		if err != nil {
			slog.Error("failed to connect to peer", "peer_id", peer.ID, "address", peer.Address, "error", err)
			return nil, fmt.Errorf("failed to connect to peer %d at %s: %w", peer.ID, peer.Address, err)
		}

		node.members[peer.ID] = conn
	}

	// start the gRPC server
	if err := node.start(); err != nil {
		slog.Error("failed to start node", "error", err)
		return nil, fmt.Errorf("failed to start node: %w", err)
	}

	// start the run loop
	go node.run()

	return node, nil
}

func (n *Node) start() error {
	n.server = grpc.NewServer()

	// register the raft service
	proto.RegisterRaftServiceServer(n.server, n)

	lis, err := net.Listen("tcp", n.address)
	if err != nil {
		slog.Error("failed to listen", "address", n.address, "error", err)
		return fmt.Errorf("failed to listen on %s: %w", n.address, err)
	}

	go n.server.Serve(lis)

	return nil
}

func (n *Node) stop() {
	if n.server != nil {
		n.server.Stop()
	}

	for _, conn := range n.members {
		// Type assert to *grpc.ClientConn to call Close()
		if cc, ok := conn.(*grpc.ClientConn); ok {
			cc.Close()
		}
	}
}
