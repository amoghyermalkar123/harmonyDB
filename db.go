package harmonydb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"harmonydb/raft"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type PutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

type DB struct {
	kv         *BTree
	consensus  *raft.Raft
	httpClient *http.Client
	logger     *zap.Logger
}

func Open(raftPort int, httpPort int) (*DB, error) {
	nodeID := int64(1)
	clusterConfig := raft.ClusterConfig{
		ThisNodeID: nodeID,
		Nodes: map[int64]raft.NodeConfig{
			nodeID: {
				ID:       nodeID,
				RaftPort: raftPort,
				HTTPPort: httpPort,
				Address:  "localhost",
			},
		},
	}

	return OpenWithConfig(clusterConfig)
}

func OpenWithConfig(clusterConfig raft.ClusterConfig) (*DB, error) {
	// Use node ID to create unique database file for each node
	dbPath := fmt.Sprintf("harmony-%d.db", clusterConfig.ThisNodeID)

	db := &DB{
		kv:         NewBTreeWithPath(dbPath),
		consensus:  raft.NewRaftServerWithConfig(clusterConfig),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     GetStructuredLogger("db"),
	}

	go db.scheduler()

	return db, nil
}

// generateCorrelationID creates a unique correlation ID for tracing operations
func generateCorrelationID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (db *DB) scheduler() {
	db.logger.Debug("Starting database scheduler", zap.String("operation", "scheduler"))

	for {
		select {
		// TODO: Make this channel a generic listener on the Db level
		// so that the WAL implementation can use it.
		// In the future we want to add a wal via which entries flow to
		// the schedule which is a fifo queue and adds entries to the underlying
		// kv storage
		case d := <-db.consensus.Ready():
			db.logger.Debug("Scheduler triggered - processing consensus entries",
				zap.String("operation", "scheduler"),
				zap.Int("entry_count", len(d.Entries)))

			for i, log := range d.Entries {
				start := time.Now()
				if err := db.kv.put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
					LogErrorWithContext(db.logger,
						"Fatal error applying consensus entry - this should never happen", err,
						zap.String("operation", "scheduler"),
						zap.Int("entry_index", i),
						zap.String("key", log.Data.Key),
						zap.Int("value_size", len(log.Data.Value)),
						DurationFromTime(start))
					panic(fmt.Sprintf("put: should never panic: (%v)", err))
				}

				db.logger.Debug("Applied consensus entry successfully",
					zap.String("operation", "scheduler"),
					zap.Int("entry_index", i),
					zap.String("key", log.Data.Key),
					zap.Int("value_size", len(log.Data.Value)),
					DurationFromTime(start))
			}

			db.logger.Debug("Completed processing all consensus entries",
				zap.String("operation", "scheduler"))
		}
	}
}

func (db *DB) Put(key, val []byte) error {
	correlationID := generateCorrelationID()
	start := time.Now()

	db.logger.Debug("Starting put operation",
		zap.String("operation", "put"),
		zap.String("key", string(key)),
		zap.Int("value_size", len(val)),
		CorrelationID(correlationID))

	if err := db.consensus.Put(context.TODO(), key, val); err != nil {
		LogErrorWithContext(db.logger, "Put operation failed in consensus layer", err,
			zap.String("operation", "put"),
			zap.String("key", string(key)),
			zap.Int("value_size", len(val)),
			CorrelationID(correlationID),
			DurationFromTime(start))
		return fmt.Errorf("consensus: %w", err)
	}

	db.logger.Debug("Put operation completed",
		zap.String("operation", "put"),
		zap.String("key", string(key)),
		zap.Int("value_size", len(val)),
		CorrelationID(correlationID),
		DurationFromTime(start))

	// The scheduler goroutine will apply committed entries from the Ready() channel
	// No need to manually apply here - that's handled asynchronously by the scheduler

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	correlationID := generateCorrelationID()
	start := time.Now()

	db.logger.Debug("Starting get operation",
		zap.String("operation", "get"),
		zap.String("key", string(key)),
		CorrelationID(correlationID))

	value, err := db.kv.Get(key)

	if err != nil {
		LogErrorWithContext(db.logger, "Get operation failed", err,
			zap.String("operation", "get"),
			zap.String("key", string(key)),
			CorrelationID(correlationID),
			DurationFromTime(start))
		return nil, err
	}

	db.logger.Debug("Get operation completed",
		zap.String("operation", "get"),
		zap.String("key", string(key)),
		zap.Int("value_size", len(value)),
		CorrelationID(correlationID),
		DurationFromTime(start))

	return value, nil
}

func (db *DB) GetLeaderID() int64 {
	return db.consensus.GetLeaderID()
}

func (db *DB) GetRaft() *raft.Raft {
	return db.consensus
}

// Stop gracefully shuts down the database and raft server
func (db *DB) Stop() {
	if db.consensus != nil {
		db.consensus.Stop()
	}
}
