package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"harmonydb"
	"harmonydb/raft"

	"go.uber.org/zap"
)

// RaftStateProvider interface for getting Raft debug state
type RaftStateProvider interface {
	GetDebugState() *raft.DebugNodeState
	GetClusterDebugState() *raft.DebugClusterState
}

// BTreeStateProvider interface for getting B+Tree debug state
type BTreeStateProvider interface {
	GetDebugState() *harmonydb.DebugBTreeState
}

// Server represents the debug visualization server
type Server struct {
	raft   RaftStateProvider
	btree  BTreeStateProvider
	config Config
	logger *zap.Logger
}

// NewServer creates a new debug server
func NewServer(raft RaftStateProvider, btree BTreeStateProvider, config Config) *Server {
	logger, _ := zap.NewProduction()
	return &Server{
		raft:   raft,
		btree:  btree,
		config: config,
		logger: logger,
	}
}

// Start starts the debug HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/raft", s.handleRaft)
	mux.HandleFunc("/btree", s.handleBTree)

	// API endpoints
	mux.HandleFunc("/api/raft/state", s.handleRaftState)
	mux.HandleFunc("/api/btree/state", s.handleBTreeState)

	// SSE endpoints for real-time updates
	mux.HandleFunc("/sse/raft", s.handleRaftSSE)
	mux.HandleFunc("/sse/btree", s.handleBTreeSSE)

	addr := fmt.Sprintf(":%d", s.config.HTTPPort)
	s.logger.Info("Debug server starting",
		zap.String("address", addr),
		zap.String("dashboard", fmt.Sprintf("http://localhost:%d", s.config.HTTPPort)),
	)

	return http.ListenAndServe(addr, mux)
}

// handleDashboard serves the main dashboard page
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := Dashboard().Render(r.Context(), &buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(buf.Bytes())
}

// handleRaft serves the Raft visualization page
func (s *Server) handleRaft(w http.ResponseWriter, r *http.Request) {
	state := s.raft.GetClusterDebugState()
	var buf bytes.Buffer
	if err := RaftPage(state).Render(r.Context(), &buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(buf.Bytes())
}

// handleBTree serves the B+Tree visualization page
func (s *Server) handleBTree(w http.ResponseWriter, r *http.Request) {
	state := s.btree.GetDebugState()
	var buf bytes.Buffer
	if err := BTreePage(state).Render(r.Context(), &buf); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(buf.Bytes())
}

// handleRaftState returns Raft state as JSON
func (s *Server) handleRaftState(w http.ResponseWriter, r *http.Request) {
	state := s.raft.GetClusterDebugState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// handleBTreeState returns B+Tree state as JSON
func (s *Server) handleBTreeState(w http.ResponseWriter, r *http.Request) {
	state := s.btree.GetDebugState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// handleRaftSSE handles Server-Sent Events for Raft state updates
func (s *Server) handleRaftSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := s.raft.GetClusterDebugState()
			var buf bytes.Buffer
			if err := RaftContent(state).Render(context.Background(), &buf); err != nil {
				continue
			}

			// Send as SSE event
			fmt.Fprintf(w, "event: raft-update\n")
			fmt.Fprintf(w, "data: %s\n\n", buf.String())
			flusher.Flush()
		}
	}
}

// handleBTreeSSE handles Server-Sent Events for B+Tree state updates
func (s *Server) handleBTreeSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := s.btree.GetDebugState()
			var buf bytes.Buffer
			if err := BTreeContent(state).Render(context.Background(), &buf); err != nil {
				continue
			}

			// Send as SSE event
			fmt.Fprintf(w, "event: btree-update\n")
			fmt.Fprintf(w, "data: %s\n\n", buf.String())
			flusher.Flush()
		}
	}
}
