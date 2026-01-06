package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"harmonydb/api"
)

func main() {
	httpPort := getPortFromEnv()
	raftPort := getRaftPortFromEnv()

	server := api.NewHTTPServerWithRaftPort(httpPort, raftPort)

	if err := server.Start(); err != nil {
		slog.Error("server startup failed", "error", err, "http_port", httpPort, "raft_port", raftPort)
		os.Exit(1)
	}
}

func getPortFromEnv() int {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		// Use basic logging since structured logger isn't initialized yet
		fmt.Printf("Invalid PORT environment variable: %s, using default 8080\n", portStr)
		return 8080
	}

	return port
}

func getRaftPortFromEnv() int {
	portStr := os.Getenv("RAFT_PORT")
	if portStr == "" {
		portStr = "9091"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		// Use basic logging since structured logger isn't initialized yet
		fmt.Printf("Invalid RAFT_PORT environment variable: %s, using default 9091\n", portStr)
		return 9091
	}

	return port
}
