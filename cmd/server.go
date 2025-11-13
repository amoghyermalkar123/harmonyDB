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
	port := getPortFromEnv()

	// Initialize global logger
	if err := harmonydb.InitLogger(port); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	defer harmonydb.Logger.Sync()

	// Set logger for raft package
	raft.SetLogger(harmonydb.GetLogger())

	server := api.NewHTTPServer(port)

	harmonydb.GetLogger().Info("Starting HarmonyDB server", zap.String("component", "main"), zap.Int("port", port))

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
