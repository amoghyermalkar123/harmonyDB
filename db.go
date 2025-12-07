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
	scheduler  *fifoScheduler
	waiter     Wait
	applyWait  WaitTime
	generator  *Generator
	config     Config
}

type Config struct {
	ProposalTimeout       time.Duration
	LinearizedReadTimeout time.Duration
}

type DbOptions func(*Config)

func Open(raftPort int, httpPort int, opts ...DbOptions) (*DB, error) {
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

	return OpenWithConfig(clusterConfig, opts...)
}

func OpenWithConfig(clusterConfig raft.ClusterConfig, dbOptions ...DbOptions) (*DB, error) {
	// Use node ID to create unique database file for each node
	dbPath := fmt.Sprintf("harmony-%d.db", clusterConfig.ThisNodeID)

	db := &DB{
		kv:         NewBTreeWithPath(dbPath),
		consensus:  raft.NewRaftServerWithConfig(clusterConfig),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     GetStructuredLogger("db"),
		scheduler:  NewFifoScheduler(),
		waiter:     newWaiter(),
		applyWait:  newWaitTime(),
		generator:  NewGenerator(uint16(clusterConfig.ThisNodeID), time.Now()),
	}

	for _, opt := range dbOptions {
		opt(&db.config)
	}

	if db.config.ProposalTimeout == 0 {
		db.config.ProposalTimeout = 100 * time.Millisecond
	}

	if db.config.LinearizedReadTimeout == 0 {
		db.config.LinearizedReadTimeout = 100 * time.Millisecond
	}

	db.bootstrapFromWAL()

	go db.run()

	return db, nil
}

// TODO: add a close method on db

// bootstrapFromWAL is a recovery mechanism that initializes the database from the Write-Ahead Log (WAL).
// no-op when there was no crash before. Call at startup
func (db *DB) bootstrapFromWAL() {
	lastApplied := db.kv.readMeta()

	db.logger.Info("bootstrapFromWAL: starting recovery",
		zap.Int64("lastApplied", lastApplied))

	logsToApply := db.consensus.GetLogsAfter(lastApplied)

	db.logger.Info("bootstrapFromWAL: fetched logs",
		zap.Int("logsCount", len(logsToApply)))

	if len(logsToApply) == 0 {
		db.logger.Info("bootstrapFromWAL: no logs to apply")
		// Even if no new logs to apply, restore commit index from WAL
		// All logs in WAL are committed
		// Only set commit index if lastApplied > 0 (meaning there are committed logs)
		if lastApplied > 0 {
			db.consensus.SetCommitIndex(lastApplied)
			db.logger.Info("bootstrapFromWAL: restored commit index from lastApplied",
				zap.Int64("commitIndex", lastApplied))
		}
		return
	}

	for _, log := range logsToApply {
		db.logger.Info("bootstrapFromWAL: replaying log",
			zap.Int64("logId", log.Id),
			zap.String("key", log.Data.Key))

		if err := db.kv.put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
			panic(fmt.Sprintf("bootstrapFromWAL: put: should never panic: (%v)", err))
		}

		db.consensus.IncrementLastApplied()
	}

	db.kv.updateMeta(db.consensus.GetLastApplied())

	// Set commit index to lastApplied since all WAL entries are committed
	// Use lastApplied instead of GetLastLogID to avoid setting commit index to 1 when there are no logs
	finalLastApplied := db.consensus.GetLastApplied()
	db.consensus.SetCommitIndex(finalLastApplied)

	db.logger.Info("bootstrapFromWAL: recovery complete",
		zap.Int64("finalLastApplied", finalLastApplied),
		zap.Int64("commitIndex", finalLastApplied))
}

// generateCorrelationID creates a unique correlation ID for tracing operations
func generateCorrelationID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (db *DB) applyCommittedEntries(entries *raft.ToApply) error {
	for _, log := range entries.Entries {
		// Apply to B+Tree
		if err := db.kv.put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
			panic(fmt.Sprintf("put: should never panic: (%v)", err))
		}

		// State Machine Application
		//
		// increment last applied to indicate durability of the comitted entries
		// in primary storage
		db.consensus.IncrementLastApplied()

		// Extract request ID and trigger the specific waiter
		requestID := log.Data.RequestId
		if requestID != 0 {
			db.waiter.Trigger(requestID, nil)
		}

		// Also trigger applied index wait for linearizable reads
		db.applyWait.Trigger(log.Id)
	}

	db.kv.updateMeta(db.consensus.GetLastApplied())

	db.logger.Debug("Completed processing all consensus entries",

		zap.String("operation", "applyConsensusEntries"))

	return nil
}

func (db *DB) run() {
	for {
		select {
		case d := <-db.consensus.Ready():
			newjob := newJob("consensus", func(ctx context.Context) {
				db.applyCommittedEntries(&d)
			})

			db.scheduler.AddTask(newjob)
		}
	}
}

func (db *DB) Put(ctx context.Context, key, val []byte) error {
	// timeout for the proposal
	mainCtx, cancel := context.WithTimeout(ctx, db.config.ProposalTimeout)
	defer cancel()

	// generate a unique ID for the operation
	id := db.generator.Next()
	blockingChan := db.waiter.Register(id)

	// propose the entry for consensus with request ID
	if err := db.consensus.Put(mainCtx, key, val, id); err != nil {
		return fmt.Errorf("consensus: %w", err)
	}

	// to ensure linearizability we wait for the blocking channel to be closed
	// indicating that the operation has been applied to the replicated
	// state machine
	//
	// Q. Why not make consensus.Put() as blocking call? Ans: that breaks
	// linearizability. The scheduler is a FIFO queue, so we need to ensure
	// that the operation the current Put() is requesting is applied only
	// when previously scheduled operations have been applied prior. This is
	// how we ensure total order which in turn ensures linearizability.
	select {
	case <-blockingChan:
		// operation has been applied
		return nil
	case <-mainCtx.Done():
		return fmt.Errorf("put timeout: %w", mainCtx.Err())
	}
}

func (db *DB) Get(ctx context.Context, key []byte) ([]byte, error) {
	// timeout for the read
	mainCtx, cancel := context.WithTimeout(ctx, db.config.ProposalTimeout)
	defer cancel()

	commitIdx := db.consensus.GetCommitIndex()

	blockingRead := db.applyWait.Wait(commitIdx)

	select {
	case <-blockingRead:
		value, err := db.kv.Get(key)

		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}

		return value, nil
	case <-mainCtx.Done():
		return nil, fmt.Errorf("get timeout: %w", mainCtx.Err())
	}

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
