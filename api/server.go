package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"harmonydb"

	"go.uber.org/zap"
)

type HTTPServer struct {
	db     *harmonydb.DB
	server *http.Server
	port   int
	mu     sync.RWMutex
	logger *zap.Logger
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

func NewHTTPServer(port int) *HTTPServer {
	return NewHTTPServerWithRaftPort(port, port+1010)
}

func NewHTTPServerWithRaftPort(httpPort, raftPort int) *HTTPServer {
	db, err := harmonydb.Open(raftPort, httpPort)
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}

	return &HTTPServer{
		db:     db,
		port:   httpPort,
		logger: harmonydb.GetStructuredLogger("api"),
	}
}

// NewHTTPServerWithDB creates a new HTTP server with an existing database instance
func NewHTTPServerWithDB(db *harmonydb.DB, port int) *HTTPServer {
	return &HTTPServer{
		db:     db,
		port:   port,
		logger: harmonydb.GetStructuredLogger("api"),
	}
}

func (h *HTTPServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/put", h.handlePut)
	mux.HandleFunc("/get", h.handleGet)
	mux.HandleFunc("/leader", h.handleLeader)

	h.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", h.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	h.logger.Info("Starting HTTP API server",
		zap.String("operation", "start"),
		zap.Int("port", h.port))
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
	response := Response{Success: true, Message: "healthy"}
	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handlePut(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	h.logger.Debug("Received PUT request",
		zap.String("operation", "put"),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.UserAgent()))

	if r.Method != http.MethodPost {
		h.logger.Debug("Method not allowed",
			zap.String("operation", "put"),
			zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req PutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		harmonydb.LogErrorWithContext(h.logger, "Failed to decode JSON request", err,
			zap.String("operation", "put"),
			zap.String("remote_addr", r.RemoteAddr),
			harmonydb.DurationFromTime(start))
		response := Response{Success: false, Error: "Invalid JSON body"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Key == "" {
		h.logger.Debug("Empty key provided",
			zap.String("operation", "put"),
			zap.String("remote_addr", r.RemoteAddr))
		response := Response{Success: false, Error: "Key cannot be empty"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if err := h.db.Put(r.Context(), []byte(req.Key), []byte(req.Value)); err != nil {
		response := Response{Success: false, Error: err.Error()}

		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "leader") || strings.Contains(err.Error(), "redirect") {
			statusCode = http.StatusServiceUnavailable
		}

		harmonydb.LogErrorWithContext(h.logger, "PUT operation failed", err,
			zap.String("operation", "put"),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("key", req.Key),
			zap.Int("value_size", len(req.Value)),
			zap.Int("status_code", statusCode),
			harmonydb.DurationFromTime(start))

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
		return
	}

	h.logger.Debug("PUT operation completed successfully",
		zap.String("operation", "put"),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("key", req.Key),
		zap.Int("value_size", len(req.Value)),
		harmonydb.DurationFromTime(start))

	response := Response{Success: true, Message: fmt.Sprintf("Successfully stored key: %s", req.Key)}
	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	h.logger.Debug("Received GET request",
		zap.String("operation", "get"),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.UserAgent()))

	if r.Method != http.MethodPost {
		h.logger.Debug("Method not allowed",
			zap.String("operation", "get"),
			zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req GetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		harmonydb.LogErrorWithContext(h.logger, "Failed to decode JSON request", err,
			zap.String("operation", "get"),
			zap.String("remote_addr", r.RemoteAddr),
			harmonydb.DurationFromTime(start))
		response := Response{Success: false, Error: "Invalid JSON body"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Key == "" {
		h.logger.Debug("Empty key provided",
			zap.String("operation", "get"),
			zap.String("remote_addr", r.RemoteAddr))
		response := Response{Success: false, Error: "Key cannot be empty"}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	val, err := h.db.Get(r.Context(), []byte(req.Key))
	if err != nil {
		harmonydb.LogErrorWithContext(h.logger, "GET operation failed", err,
			zap.String("operation", "get"),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("key", req.Key),
			harmonydb.DurationFromTime(start))
		response := Response{Success: false, Error: fmt.Sprintf("Get failed: %v", err)}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	h.logger.Debug("GET operation completed successfully",
		zap.String("operation", "get"),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("key", req.Key),
		zap.Int("value_size", len(val)),
		harmonydb.DurationFromTime(start))

	response := Response{
		Success: true,
		Value:   string(val),
	}

	json.NewEncoder(w).Encode(response)
}

func (h *HTTPServer) handleLeader(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	h.logger.Debug("Received leader request",
		zap.String("operation", "leader"),
		zap.String("remote_addr", r.RemoteAddr))

	if r.Method != http.MethodGet {
		h.logger.Debug("Method not allowed",
			zap.String("operation", "leader"),
			zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	leaderID := h.db.GetLeaderID()
	if leaderID == 0 {
		h.logger.Debug("No leader elected",
			zap.String("operation", "leader"),
			zap.String("remote_addr", r.RemoteAddr),
			harmonydb.DurationFromTime(start))
		response := Response{Success: false, Error: "No leader elected"}
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
		return
	}

	h.logger.Debug("Leader request completed successfully",
		zap.String("operation", "leader"),
		zap.String("remote_addr", r.RemoteAddr),
		zap.Int64("leader_id", leaderID),
		harmonydb.DurationFromTime(start))

	response := Response{
		Success: true,
		Value:   fmt.Sprintf("%d", leaderID),
		Message: "Current leader ID",
	}

	json.NewEncoder(w).Encode(response)
}
