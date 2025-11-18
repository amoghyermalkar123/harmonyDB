package harmonydb

import (
	"context"
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
			GetLogger().Debug("Scheduler Triggered")

			for _, log := range d.Entries {
				if err := db.kv.put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
					panic(fmt.Sprintf("put: should never panic: (%v)", err))
				}
			}
		}
	}
}

func (db *DB) Put(key, val []byte) error {
	GetLogger().Debug("Put", zap.String("component", "db"))

	if err := db.consensus.Put(context.TODO(), key, val); err != nil {
		return fmt.Errorf("consensus: %w", err)
	}

	// The scheduler goroutine will apply committed entries from the Ready() channel
	// No need to manually apply here - that's handled asynchronously by the scheduler

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	return db.kv.Get(key)
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
