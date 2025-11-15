package framework

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"harmonydb"
	"harmonydb/api"
	"harmonydb/raft"
)

// EmbeddedServer represents a single HarmonyDB server instance running in-process
type EmbeddedServer struct {
	httpPort   int
	raftPort   int
	dataDir    string
	httpServer *api.HTTPServer
	client     *http.Client
	stopCh     chan struct{}
	stopped    bool
	mu         sync.RWMutex
}

// EmbeddedServerConfig holds configuration for creating an embedded server
type EmbeddedServerConfig struct {
	HTTPPort int // If 0, will auto-assign
	RaftPort int // If 0, will auto-assign
	DataDir  string // If empty, will create temp dir
}

// NewEmbeddedServer creates a new embedded HarmonyDB server
func NewEmbeddedServer(t *testing.T, cfg EmbeddedServerConfig) (*EmbeddedServer, error) {
	// Auto-assign ports if not specified
	httpPort := cfg.HTTPPort
	if httpPort == 0 {
		var err error
		httpPort, err = getFreePort()
		if err != nil {
			return nil, fmt.Errorf("failed to get free HTTP port: %w", err)
		}
	}

	raftPort := cfg.RaftPort
	if raftPort == 0 {
		var err error
		raftPort, err = getFreePort()
		if err != nil {
			return nil, fmt.Errorf("failed to get free Raft port: %w", err)
		}
	}

	// Create data directory
	dataDir := cfg.DataDir
	if dataDir == "" {
		tempDir, err := os.MkdirTemp("", "harmonydb-test-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		dataDir = tempDir
	}

	server := &EmbeddedServer{
		httpPort: httpPort,
		raftPort: raftPort,
		dataDir:  dataDir,
		client:   &http.Client{Timeout: 5 * time.Second},
		stopCh:   make(chan struct{}),
	}

	t.Cleanup(func() {
		server.Stop()
	})

	return server, nil
}

// Start starts the embedded server
func (s *EmbeddedServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return fmt.Errorf("server already stopped")
	}

	// Change to data directory
	oldWd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	err = os.Chdir(s.dataDir)
	if err != nil {
		return fmt.Errorf("failed to change to data directory: %w", err)
	}

	// Restore working directory on exit
	defer func() {
		os.Chdir(oldWd)
	}()

	// Initialize logger
	if err := harmonydb.InitLogger(s.httpPort); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Set logger for raft package
	raft.SetLogger(harmonydb.GetLogger())

	// Create HTTP server (which internally creates the DB)
	s.httpServer = api.NewHTTPServerWithRaftPort(s.httpPort, s.raftPort)

	// Start HTTP server in background
	go func() {
		if err := s.httpServer.Start(); err != nil && err != http.ErrServerClosed {
			// Log error but don't panic in test
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	// Wait for server to be ready
	return s.waitReady()
}

// Stop stops the embedded server and cleans up resources
func (s *EmbeddedServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return
	}

	s.stopped = true
	close(s.stopCh)

	if s.httpServer != nil {
		s.httpServer.Stop()
	}

	// Clean up data directory
	if s.dataDir != "" {
		os.RemoveAll(s.dataDir)
	}
}

// HTTPPort returns the HTTP port the server is listening on
func (s *EmbeddedServer) HTTPPort() int {
	return s.httpPort
}

// RaftPort returns the Raft port the server is listening on
func (s *EmbeddedServer) RaftPort() int {
	return s.raftPort
}

// BaseURL returns the base HTTP URL for the server
func (s *EmbeddedServer) BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", s.httpPort)
}

// Client returns an HTTP client configured to talk to this server
func (s *EmbeddedServer) Client() *http.Client {
	return s.client
}

// waitReady waits for the server to be ready to accept requests
func (s *EmbeddedServer) waitReady() error {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("server did not become ready within timeout")
		case <-s.stopCh:
			return fmt.Errorf("server stopped while waiting for ready")
		case <-ticker.C:
			// Try to hit health endpoint
			resp, err := s.client.Get(s.BaseURL() + "/health")
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

// getFreePort finds an available port
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}