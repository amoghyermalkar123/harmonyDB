package raft

import (
	"go.uber.org/zap"
)

var logger *zap.Logger

func SetLogger(l *zap.Logger) {
	logger = l
}

func getLogger() *zap.Logger {
	if logger == nil {
		// Fallback to a no-op logger if not set
		logger = zap.NewNop()
	}
	return logger
}