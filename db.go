package harmonydb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"harmonydb/repl"
	proto "harmonydb/repl/proto/repl"
	"harmonydb/scheduler"
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
	kv              *BTree
	raft            repl.RaftNode
	httpClient      *http.Client
	logger          *zap.Logger
	scheduler       *scheduler.FifoScheduler
	waiter          Wait
	applyWait       WaitTime
	generator       *Generator
	config          Config
	lastCommitIndex int64
}

type Config struct {
	ProposalTimeout       time.Duration
	LinearizedReadTimeout time.Duration
}

type DbOptions func(*Config)

func Open(raftPort int, httpPort int, opts ...DbOptions) (*DB, error) {
	// Determine config path based on raft port
	configPath := fmt.Sprintf("./repl/cluster-node%d.yaml", (raftPort-5001)+1)

	rn, err := repl.NewRaftNode(configPath)
	if err != nil {
		return nil, fmt.Errorf("opendb with config: %w", err)
	}

	// Default db path based on node ID
	dbPath := fmt.Sprintf("./data/node_%d.db", rn.ID)

	db := &DB{
		kv:         NewBTreeWithPath(dbPath),
		raft:       rn,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     GetStructuredLogger("db"),
		scheduler:  scheduler.NewFifoScheduler(),
		waiter:     newWaiter(),
		applyWait:  newWaitTime(),
		generator:  NewGenerator(uint16(rn.ID), time.Now()),
	}

	for _, opt := range opts {
		opt(&db.config)
	}

	if db.config.ProposalTimeout == 0 {
		db.config.ProposalTimeout = 100 * time.Millisecond
	}

	if db.config.LinearizedReadTimeout == 0 {
		db.config.LinearizedReadTimeout = 100 * time.Millisecond
	}

	// Replay WAL entries to recover B+tree state
	// This needs to happen BEFORE starting applyLoop
	walStorage := rn.GetWAL()
	if walStorage != nil {
		if err := ReplayWAL(db.kv, walStorage); err != nil {
			return nil, fmt.Errorf("replay WAL: %w", err)
		}
	}

	go db.applyLoop()

	// start the ticker (100ms tick interval)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			db.raft.Tick()
		}
	}()

	return db, nil
}

// generateCorrelationID creates a unique correlation ID for tracing operations
func generateCorrelationID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (db *DB) applyLoop() {
	for {
		select {
		case ta := <-db.raft.Apply():
			db.scheduler.AddTask(scheduler.NewJob("applier", func(ctx context.Context) {
				var lastAppliedIdx int64

				for _, e := range ta.Entries {
					if err := db.kv.put([]byte(e.GetData().GetKey()), []byte(e.GetData().GetValue())); err != nil {
						panic(fmt.Sprintf("applier %s", err.Error()))
					}

					// (linearizability) trigger write waiters
					db.waiter.Trigger(e.GetData().RequestId, nil)

					lastAppliedIdx = e.GetId()
				}

				// Update metadata with lastApplied and commitIndex for recovery
				if len(ta.Entries) > 0 {
					db.kv.updateMeta(lastAppliedIdx, ta.CommitIndex)
				}

				// (linearizability) update and trigger read waiters
				db.lastCommitIndex = ta.CommitIndex
				db.applyWait.Trigger(ta.CommitIndex)
			}))
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

	// create the proposal message
	msg := &proto.Message{
		Type: proto.MessageType_MSG_APP,
		Entries: &proto.AppendEntries{
			Entries: []*proto.Log{
				{
					Data: &proto.Cmd{
						Op:        "PUT",
						Key:       string(key),
						Value:     string(val),
						RequestId: id,
					},
				},
			},
		},
	}

	// propose the entry for consensus with request ID
	if _, err := db.raft.Process(mainCtx, msg); err != nil {
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
	mainCtx, cancel := context.WithTimeout(ctx, db.config.LinearizedReadTimeout)
	defer cancel()

	// (linearizability) wait for all committed entries to be applied
	// this ensures we read the most recent state
	commitIdx := db.lastCommitIndex
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
