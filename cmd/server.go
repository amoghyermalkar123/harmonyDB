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

	logger := harmonydb.CreateOperationLogger("server", "startup",
		zap.Int("http_port", httpPort),
		zap.Int("raft_port", raftPort),
		zap.String("pid", fmt.Sprintf("%d", os.Getpid())))

	// Set logger for raft package
	raft.SetLogger(harmonydb.GetLogger())
	logger.Debug("Logger initialized and raft logger configured")

	server := api.NewHTTPServerWithRaftPort(httpPort, raftPort)
	logger.Debug("HTTP server instance created")

	logger.Info("Starting HarmonyDB server")

	if err := server.Start(); err != nil {
		harmonydb.LogErrorWithContext(logger, "Server startup failed", err)
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
