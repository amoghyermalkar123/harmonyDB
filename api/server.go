package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"harmonydb"
	"harmonydb/metrics"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type HTTPServer struct {
	db     *harmonydb.DB
	server *http.Server
	port   int
	mu     sync.RWMutex
}

type PutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetRequest struct {
	Key string `json:"key"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

type HealthResponse struct {
	Status  string      `json:"status"`
	Raft    RaftHealth  `json:"raft"`
	Storage StorageHealth `json:"storage"`
}

type RaftHealth struct {
	State       string `json:"state"`
	Term        int64  `json:"term"`
	CommitIndex int64  `json:"commit_index"`
	LeaderID    int64  `json:"leader_id"`
}

type StorageHealth struct {
	TotalPages int `json:"total_pages"`
}


func NewHTTPServer(port int) *HTTPServer {
	return NewHTTPServerWithRaftPort(port, port+1010)
}

func NewHTTPServerWithRaftPort(httpPort, raftPort int) *HTTPServer {
	db, err := harmonydb.Open(raftPort, httpPort)
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}

	return &HTTPServer{
		db:   db,
		port: httpPort,
	}
}

// NewHTTPServerWithDB creates a new HTTP server with an existing database instance
func NewHTTPServerWithDB(db *harmonydb.DB, port int) *HTTPServer {
	return &HTTPServer{
		db:   db,
		port: port,
	}
}

// metricsMiddleware wraps handlers with metrics collection
func (h *HTTPServer) metricsMiddleware(endpoint string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		metrics.HTTPInflightRequests.Inc()
		defer metrics.HTTPInflightRequests.Dec()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next(wrapped, r)

		duration := time.Since(startTime).Seconds()
		status := strconv.Itoa(wrapped.statusCode)

		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, endpoint, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, endpoint).Observe(duration)

		if r.ContentLength > 0 {
			metrics.HTTPRequestSizeBytes.WithLabelValues(r.Method, endpoint).Observe(float64(r.ContentLength))
		}
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (h *HTTPServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", h.metricsMiddleware("/health", h.handleHealth))
	mux.HandleFunc("/put", h.metricsMiddleware("/put", h.handlePut))
	mux.HandleFunc("/get", h.metricsMiddleware("/get", h.handleGet))
	mux.HandleFunc("/leader", h.metricsMiddleware("/leader", h.handleLeader))
	mux.Handle("/metrics", promhttp.Handler())

	h.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", h.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	harmonydb.GetLogger().Info("Starting HTTP API server", zap.String("component", "api"), zap.Int("port", h.port))
	return h.server.ListenAndServe()
}

func (h *HTTPServer) Stop() error {
	// Stop the database (including Raft) first
	if h.db != nil {
		h.db.Stop()
	}

	// Then stop the HTTP server
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.server.Shutdown(ctx)
	}
	return nil
}

func (h *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	raft := h.db.GetRaft()
	leaderID := raft.GetLeaderID()
	term := raft.GetTerm()
	commitIndex := raft.GetCommitIndex()

	// Determine state string
	var stateStr string
	if leaderID == 0 {
		stateStr = "no_leader"
	} else if leaderID == raft.GetConfig().ThisNodeID {
		stateStr = "leader"
	} else {
		stateStr = "follower"
	}

	// Determine overall health status
	status := "healthy"
	if leaderID == 0 {
		status = "degraded"
	}

	response := HealthResponse{
		Status: status,
		Raft: RaftHealth{
			State:       stateStr,
			Term:        term,
			CommitIndex: commitIndex,
			LeaderID:    leaderID,
		},
		Storage: StorageHealth{
			TotalPages: 0, // TODO: expose from BTree
		},
	}

	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handlePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req PutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Success: false, Error: "Invalid JSON body"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Key == "" {
		response := Response{Success: false, Error: "Key cannot be empty"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if err := h.db.Put([]byte(req.Key), []byte(req.Value)); err != nil {
		// Check if this might be a redirection failure or other specific error
		response := Response{Success: false, Error: err.Error()}

		// If the error suggests leadership issues, use 503 Service Unavailable
		if strings.Contains(err.Error(), "leader") || strings.Contains(err.Error(), "redirect") {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}

		json.NewEncoder(w).Encode(response)
		return
	}

	response := Response{Success: true, Message: fmt.Sprintf("Successfully stored key: %s", req.Key)}
	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req GetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Success: false, Error: "Invalid JSON body"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Key == "" {
		response := Response{Success: false, Error: "Key cannot be empty"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	val, err := h.db.Get([]byte(req.Key))
	if err != nil {
		response := Response{Success: false, Error: fmt.Sprintf("Get failed: %v", err)}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := Response{
		Success: true,
		Value:   string(val),
	}

	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handleLeader(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Access the raft instance through the database
	leaderID := h.db.GetLeaderID()
	if leaderID == 0 {
		response := Response{Success: false, Error: "No leader elected"}
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := Response{
		Success: true,
		Value:   fmt.Sprintf("%d", leaderID),
		Message: "Current leader ID",
	}

	json.NewEncoder(w).Encode(response)
}
