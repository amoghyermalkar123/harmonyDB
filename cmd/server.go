package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"harmonydb"
	"harmonydb/api"
	"harmonydb/debug"
	"harmonydb/raft"

	"go.uber.org/zap"
)

func main() {
	httpPort := getPortFromEnv()
	raftPort := getRaftPortFromEnv()
	nodeID := getNodeIDFromEnv()
	debugMode := getDebugModeFromEnv()
	debugPort := getDebugPortFromEnv()

	// Initialize global logger
	if err := harmonydb.InitLogger(httpPort, false); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	defer harmonydb.Logger.Sync()

	// Set logger for raft package
	raft.SetLogger(harmonydb.GetLogger())

	// Define the full cluster configuration
	// All nodes must know about all other nodes
	clusterConfig := raft.ClusterConfig{
		ThisNodeID: nodeID,
		Nodes: map[int64]raft.NodeConfig{
			1: {ID: 1, RaftPort: 9091, HTTPPort: 8081, Address: "localhost"},
			2: {ID: 2, RaftPort: 9092, HTTPPort: 8082, Address: "localhost"},
			3: {ID: 3, RaftPort: 9093, HTTPPort: 8083, Address: "localhost"},
		},
	}

	db, err := harmonydb.OpenWithConfig(clusterConfig)
	if err != nil {
		harmonydb.GetLogger().Fatal("Failed to open database", zap.Error(err))
	}

	server := api.NewHTTPServerWithDB(db, httpPort)

	// Start debug server if enabled
	if debugMode {
		debugConfig := debug.Config{
			Enabled:      true,
			HTTPPort:     debugPort,
			EnableRaft:   true,
			EnableBTree:  true,
			PollInterval: 500 * time.Millisecond,
		}

		debugServer := debug.NewServer(db.Raft(), db.BTree(), debugConfig)

		go func() {
			harmonydb.GetLogger().Info("Starting debug server",
				zap.String("component", "debug"),
				zap.Int("port", debugPort),
				zap.String("url", fmt.Sprintf("http://localhost:%d", debugPort)))

			if err := debugServer.Start(); err != nil {
				harmonydb.GetLogger().Error("Debug server error",
					zap.String("component", "debug"),
					zap.Error(err))
			}
		}()
	}

	harmonydb.GetLogger().Info("Starting HarmonyDB server",
		zap.String("component", "main"),
		zap.Int64("node_id", nodeID),
		zap.Int("http_port", httpPort),
		zap.Int("raft_port", raftPort),
		zap.Bool("debug_mode", debugMode))

	if err := server.Start(); err != nil {
		harmonydb.GetLogger().Error("Server error", zap.String("component", "main"), zap.Error(err))
	}
}

func getDebugModeFromEnv() bool {
	debugStr := os.Getenv("DEBUG")
	return debugStr == "true" || debugStr == "1"
}

func getDebugPortFromEnv() int {
	portStr := os.Getenv("DEBUG_PORT")
	if portStr == "" {
		portStr = "6060"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		fmt.Printf("Invalid DEBUG_PORT environment variable: %s, using default 6060\n", portStr)
		return 6060
	}

	return port
}

func getNodeIDFromEnv() int64 {
	idStr := os.Getenv("NODE_ID")
	if idStr == "" {
		idStr = "1"
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		fmt.Printf("Invalid NODE_ID environment variable: %s, using default 1\n", idStr)
		return 1
	}

	return id
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
