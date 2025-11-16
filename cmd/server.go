package main

import (
	"fmt"
	"os"
	"strconv"

	"harmonydb"
	"harmonydb/api"
	"harmonydb/raft"

	"go.uber.org/zap"
)

func main() {
	httpPort := getPortFromEnv()
	raftPort := getRaftPortFromEnv()

	// Initialize global logger
	if err := harmonydb.InitLogger(httpPort, false); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	defer harmonydb.Logger.Sync()

	// Set logger for raft package
	raft.SetLogger(harmonydb.GetLogger())

	server := api.NewHTTPServerWithRaftPort(httpPort, raftPort)

	harmonydb.GetLogger().Info("Starting HarmonyDB server", zap.String("component", "main"), zap.Int("http_port", httpPort), zap.Int("raft_port", raftPort))

	if err := server.Start(); err != nil {
		harmonydb.GetLogger().Error("Server error", zap.String("component", "main"), zap.Error(err))
	}
}

func getPortFromEnv() int {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
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
		fmt.Printf("Invalid RAFT_PORT environment variable: %s, using default 9091\n", portStr)
		return 9091
	}

	return port
}
