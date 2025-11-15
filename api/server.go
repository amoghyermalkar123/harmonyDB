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
	db     *harmonydb.Db
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

	harmonydb.GetLogger().Info("Starting HTTP API server", zap.String("component", "api"), zap.Int("port", h.port))
	return h.server.ListenAndServe()
}

func (h *HTTPServer) Stop() error {
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
		harmonydb.GetLogger().Error("PUT operation failed", zap.String("component", "api"), zap.String("key", req.Key), zap.Error(err))

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
