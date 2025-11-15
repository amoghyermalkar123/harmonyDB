package raft

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"harmonydb/raft/proto"

	consul "github.com/hashicorp/consul/api"
	"google.golang.org/grpc"
)

const (
	RaftServiceName = "harmonydb-raft"
	ConsulAddress   = "127.0.0.1:8500" // Default Consul address
)

type ConsulDiscovery struct {
	client   *consul.Client
	nodeID   int64
	nodePort int
	httpPort int
}

func NewConsulDiscovery(nodeID int64, nodePort int, httpPort int) (*ConsulDiscovery, error) {
	config := consul.DefaultConfig()

	// Use environment variable if available
	if consulAddr := os.Getenv("CONSUL_ADDR"); consulAddr != "" {
		config.Address = consulAddr
	} else {
		config.Address = ConsulAddress
	}

	client, err := consul.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %v", err)
	}

	// Test connection to Consul
	_, err = client.Status().Leader()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to consul at %s: %v", config.Address, err)
	}

	return &ConsulDiscovery{
		client:   client,
		nodeID:   nodeID,
		nodePort: nodePort,
		httpPort: httpPort,
	}, nil
}

func (cd *ConsulDiscovery) Register() error {
	serviceID := fmt.Sprintf("raft-node-%d", cd.nodeID)

	// Get the actual IP address of this container/host
	nodeAddress, err := cd.getNodeAddress()
	if err != nil {
		return fmt.Errorf("failed to get node address: %v", err)
	}

	service := &consul.AgentServiceRegistration{
		ID:      serviceID,
		Name:    RaftServiceName,
		Port:    cd.nodePort,
		Address: nodeAddress,
		Tags:    []string{"raft", "consensus", fmt.Sprintf("node-id-%d", cd.nodeID)},
		Meta: map[string]string{
			"node_id":   strconv.FormatInt(cd.nodeID, 10),
			"http_port": strconv.Itoa(cd.httpPort),
			"version":   "1.0",
		},
		Check: &consul.AgentServiceCheck{
			TCP:                            fmt.Sprintf("%s:%d", nodeAddress, cd.nodePort),
			Interval:                       "10s",
			Timeout:                        "3s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	err = cd.client.Agent().ServiceRegister(service)
	if err != nil {
		return fmt.Errorf("failed to register service: %v", err)
	}

	// Note: we can't import harmonydb.GetLogger() here due to circular import
	// The caller should log this registration
	return nil
}

func (cd *ConsulDiscovery) Deregister() error {
	serviceID := fmt.Sprintf("raft-node-%d", cd.nodeID)
	err := cd.client.Agent().ServiceDeregister(serviceID)
	if err != nil {
		return fmt.Errorf("failed to deregister service: %v", err)
	}
	return nil
}

func (cd *ConsulDiscovery) DiscoverPeers() (map[int64]proto.RaftClient, error) {
	services, _, err := cd.client.Health().Service(RaftServiceName, "", true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query consul for services: %v", err)
	}

	cluster := make(map[int64]proto.RaftClient)

	for _, service := range services {
		nodeIDStr, exists := service.Service.Meta["node_id"]
		if !exists {
			continue
		}

		nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
		if err != nil {
			continue
		}

		// Skip self
		if nodeID == cd.nodeID {
			continue
		}

		address := fmt.Sprintf("%s:%d", service.Service.Address, service.Service.Port)
		client, err := cd.connectToPeer(address)
		if err != nil {
			continue
		}

		cluster[nodeID] = client
	}

	return cluster, nil
}

func (cd *ConsulDiscovery) connectToPeer(address string) (proto.RaftClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, err
	}

	return proto.NewRaftClient(conn), nil
}

func (cd *ConsulDiscovery) getNodeAddress() (string, error) {
	// Check if NODE_ADDRESS environment variable is set (useful for containerized environments)
	if nodeAddr := os.Getenv("NODE_ADDRESS"); nodeAddr != "" {
		return nodeAddr, nil
	}

	// In containerized environments, use the container's hostname as the address
	if hostname, err := os.Hostname(); err == nil {
		// Check if hostname looks like a container ID (12 char hex)
		if len(hostname) == 12 {
			// This looks like a Docker container, return hostname
			return hostname, nil
		}
	}

	// Fallback: determine the local IP that would be used to reach consul
	conn, err := net.Dial("udp", "consul:8500")
	if err != nil {
		// Final fallback to localhost (for local development)
		return "127.0.0.1", nil
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

func (cd *ConsulDiscovery) WatchForChanges(updateCluster func(map[int64]proto.RaftClient)) {
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			newCluster, err := cd.DiscoverPeers()
			if err != nil {
				continue
			}
			updateCluster(newCluster)
		}
	}()
}
