package harmonydb

import (
	"context"
	"fmt"
	"harmonydb/metrics"
	"harmonydb/raft"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/codes"
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
	// Check if DATA_DIR environment variable is set, otherwise use current directory
	dataDir := "/var/lib/harmonydb"
	dbPath := fmt.Sprintf("%s/harmony-%d.db", dataDir, clusterConfig.ThisNodeID)

	GetLogger().Info("Opening database",
		zap.String("component", "db"),
		zap.String("path", dbPath),
		zap.Int64("node_id", clusterConfig.ThisNodeID))

	db := &DB{
		kv:         NewBTreeWithPath(dbPath),
		consensus:  raft.NewRaftServerWithConfig(clusterConfig),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	go db.scheduler()

	return db, nil
}

func (db *DB) scheduler() {
	for {
		select {
		// TODO: Make this channel a generic listener on the Db level
		// so that the WAL implementation can use it.
		// In the future we want to add a wal via which entries flow to
		// the schedule which is a fifo queue and adds entries to the underlying
		// kv storage
		case d := <-db.consensus.Ready():
			GetLogger().Debug("Scheduler Triggered",
				zap.String("component", "scheduler"),
				zap.Int("num_entries", len(d.Entries)))

			for i, log := range d.Entries {
				GetLogger().Debug("Applying log entry",
					zap.String("component", "scheduler"),
					zap.Int("index", i),
					zap.String("key", log.Data.Key),
					zap.String("value", log.Data.Value))

				if err := db.kv.put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
					GetLogger().Error("Failed to put in btree",
						zap.String("component", "scheduler"),
						zap.String("key", log.Data.Key),
						zap.Error(err))
					panic(fmt.Sprintf("put: should never panic: (%v)", err))
				}

				GetLogger().Info("Successfully applied log entry",
					zap.String("component", "scheduler"),
					zap.String("key", log.Data.Key),
					zap.String("value", log.Data.Value))
			}
		}
	}
}

func (db *DB) Put(key, val []byte) error {
	ctx, span := metrics.StartSpan(context.TODO(), "db.put")
	defer span.End()

	span.SetAttributes(metrics.KeyAttr(string(key)))
	GetLogger().Debug("Put", zap.String("component", "db"))

	if err := db.consensus.Put(ctx, key, val); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("consensus: %w", err)
	}

	// The scheduler goroutine will apply committed entries from the Ready() channel
	// No need to manually apply here - that's handled asynchronously by the scheduler

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	_, span := metrics.StartSpan(context.TODO(), "db.get")
	defer span.End()

	span.SetAttributes(metrics.KeyAttr(string(key)))

	val, err := db.kv.Get(key)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	return val, nil
}

func (db *DB) GetLeaderID() int64 {
	return db.consensus.GetLeaderID()
}

func (db *DB) GetRaft() *raft.Raft {
	return db.consensus
}

// Raft returns the Raft consensus instance for debug visualization
func (db *DB) Raft() *raft.Raft {
	return db.consensus
}

// BTree returns the B+Tree storage instance for debug visualization
func (db *DB) BTree() *BTree {
	return db.kv
}

// Stop gracefully shuts down the database and raft server
func (db *DB) Stop() {
	if db.consensus != nil {
		db.consensus.Stop()
	}
}
