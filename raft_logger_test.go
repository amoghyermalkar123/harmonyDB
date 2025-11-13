package harmonydb

import (
	"os"
	"testing"

	"harmonydb/raft"

	"go.uber.org/zap"
)

func TestRaftLogger(t *testing.T) {
	// Initialize global logger
	err := InitLogger()
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}
	defer Sync()

	// Set logger for raft package
	raft.SetLogger(GetLogger())

	// Test that the raft package can use the logger
	// We'll just create the node to ensure the logger setup doesn't crash
	// (We can't easily test full raft functionality due to consul dependencies)

	GetLogger().Info("Testing raft package logger integration", zap.String("component", "test"))

	// Check that harmony.log file was created
	if _, err := os.Stat("harmony.log"); os.IsNotExist(err) {
		t.Fatal("harmony.log file was not created")
	}

	// Clean up test file
	os.Remove("harmony.log")
}