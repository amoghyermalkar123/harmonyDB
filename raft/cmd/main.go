package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"harmonydb/raft"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	// Try to find an available port
	nodePort := -1
	for i := 0; i < 3; i++ {
		testPort := 8080 + i
		if isPortAvailable(testPort) {
			nodePort = testPort
			break
		}
	}

	if nodePort == -1 {
		fmt.Println("No available ports found in range 8080-8082")
		os.Exit(1)
	}

	fmt.Printf("Starting Raft node on port %d (PID: %d)\n", nodePort, os.Getpid())

	r := raft.NewRaftServerWithConsul(nodePort)

	// Wait for cluster to stabilize
	time.Sleep(3 * time.Second)

	// Generate random test data periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				key := fmt.Sprintf("key_%d", rand.Intn(100))
				value := fmt.Sprintf("value_%d_%d", nodePort, time.Now().Unix())

				err := r.Put(context.TODO(), []byte(key), []byte(value))
				if err != nil {
					fmt.Printf("[Node %d] Error: %v\n", nodePort, err)
				}
			}
		}
	}()

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("[Node %d] Node is running... Press Ctrl+C to stop\n", nodePort)

	// Wait for shutdown signal
	<-c
	fmt.Printf("\n[Node %d] Shutting down gracefully...\n", nodePort)

	// Cleanup: Deregister from Consul
	if r.GetDiscovery() != nil {
		r.GetDiscovery().Deregister()
	}

	fmt.Printf("[Node %d] Shutdown complete\n", nodePort)
}

func isPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
