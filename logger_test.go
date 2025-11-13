package harmonydb

import (
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestLogger(t *testing.T) {
	// Initialize logger
	err := InitLogger()
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}
	defer Sync()

	// Test that logger is available
	logger := GetLogger()
	if logger == nil {
		t.Fatal("Logger is nil")
	}

	// Test logging at different levels
	logger.Info("Test info message", zap.String("component", "test"))
	logger.Warn("Test warn message", zap.String("component", "test"))
	logger.Error("Test error message", zap.String("component", "test"), zap.String("test_field", "test_value"))

	// Check that harmony.log file was created
	if _, err := os.Stat("harmony.log"); os.IsNotExist(err) {
		t.Fatal("harmony.log file was not created")
	}

	// Clean up test file
	os.Remove("harmony.log")
}