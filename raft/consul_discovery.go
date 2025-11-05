package raft

import (
	"context"
	"fmt"
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
}

func NewConsulDiscovery(nodeID int64, nodePort int) (*ConsulDiscovery, error) {
	config := consul.DefaultConfig()
	config.Address = ConsulAddress

	client, err := consul.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %v", err)
	}

	// Test connection to Consul
	_, err = client.Status().Leader()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to consul at %s: %v", ConsulAddress, err)
	}

	return &ConsulDiscovery{
		client:   client,
		nodeID:   nodeID,
		nodePort: nodePort,
	}, nil
}

func (cd *ConsulDiscovery) Register() error {
	serviceID := fmt.Sprintf("raft-node-%d", cd.nodeID)

	service := &consul.AgentServiceRegistration{
		ID:      serviceID,
		Name:    RaftServiceName,
		Port:    cd.nodePort,
		Address: "127.0.0.1",
		Tags:    []string{"raft", "consensus", fmt.Sprintf("node-id-%d", cd.nodeID)},
		Meta: map[string]string{
			"node_id": strconv.FormatInt(cd.nodeID, 10),
			"version": "1.0",
		},
		Check: &consul.AgentServiceCheck{
			TCP:                            fmt.Sprintf("127.0.0.1:%d", cd.nodePort),
			Interval:                       "10s",
			Timeout:                        "3s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	err := cd.client.Agent().ServiceRegister(service)
	if err != nil {
		return fmt.Errorf("failed to register service: %v", err)
	}

	fmt.Printf("✅ Registered node %d with Consul as %s\n", cd.nodeID, serviceID)
	return nil
}

func (cd *ConsulDiscovery) Deregister() error {
	serviceID := fmt.Sprintf("raft-node-%d", cd.nodeID)
	err := cd.client.Agent().ServiceDeregister(serviceID)
	if err != nil {
		return fmt.Errorf("failed to deregister service: %v", err)
	}
	fmt.Printf("❌ Deregistered node %d from Consul\n", cd.nodeID)
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
			fmt.Printf("⚠️  Failed to connect to peer %d at %s: %v\n", nodeID, address, err)
			continue
		}

		cluster[nodeID] = client
		// fmt.Printf("🔗 Connected to peer %d at %s\n", nodeID, address)
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

func (cd *ConsulDiscovery) WatchForChanges(updateCluster func(map[int64]proto.RaftClient)) {
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			newCluster, err := cd.DiscoverPeers()
			if err != nil {
				fmt.Printf("❌ Error during peer discovery: %v\n", err)
				continue
			}
			updateCluster(newCluster)
		}
	}()
}
