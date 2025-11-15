package harmonydb

import (
	"bytes"
	"context"
	"encoding/json"
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

type Db struct {
	kv         *BTree
	consensus  *raft.Raft
	httpClient *http.Client
}

func Open(raftPort int, httpPort int) (*Db, error) {
	db := &Db{
		kv:         NewBTree(),
		consensus:  raft.NewRaftServerWithConsul(raftPort, httpPort),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	go db.scheduler()

	return db, nil
}

func (db *Db) scheduler() {
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
				if err := db.kv.Put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
					panic(fmt.Sprintf("put: should never panic: (%v)", err))
				}
			}
		}
	}
}

func (db *Db) Put(key, val []byte) error {
	GetLogger().Debug("Put", zap.String("component", "db"))

	if err := db.consensus.Put(context.TODO(), key, val); err != nil {
		if err == raft.ErrNotALeader {
			return db.redirectPutToLeader(key, val)
		}

		return fmt.Errorf("consensus: %w", err)
	}

	lastApplied, lastCommitted := db.consensus.GetLastAppliedLastCommitted()

	// apply all remaining entries since we last applied to the database
	// TODO: this is incorrect, we are adding same key value? no? once? idk change this
	// we can optimize this for batched entries as well
	for i := lastApplied; i <= lastCommitted; i++ {
		GetLogger().Debug("Apply Log", zap.String("component", "db"), zap.Int64("lastApplied", lastApplied), zap.Int64("lastCommitted", lastCommitted))
		if err := db.kv.Put(key, val); err != nil {
			return fmt.Errorf("Put : %w", err)
		}

		db.consensus.IncrementLastApplied()
	}

	return nil
}

func (db *Db) redirectPutToLeader(key, val []byte) error {
	GetLogger().Debug("Redirecting PUT to leader", zap.String("component", "db"))

	// Get leader address
	leaderAddr, err := db.consensus.GetLeaderAddress()
	if err != nil {
		// Check if the error indicates this node is the leader
		if err.Error() == "this node is the leader" {
			return fmt.Errorf("redirect: %w", err)
		}

		return fmt.Errorf("redirect: %w", err)
	}

	// Create PUT request
	putReq := PutRequest{
		Key:   string(key),
		Value: string(val),
	}

	jsonData, err := json.Marshal(putReq)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Send request to leader
	url := fmt.Sprintf("%s/put", leaderAddr)

	resp, err := db.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		GetLogger().Warn("Failed to send PUT request to leader", zap.String("component", "db"), zap.String("leader", leaderAddr), zap.Error(err))
		return fmt.Errorf("redirect: put: %w", err)
	}
	defer resp.Body.Close()

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("redirect: decode: %w", err)
	}

	return nil
}

func (db *Db) Get(key []byte) ([]byte, error) {
	return db.kv.Get(key)
}

func (db *Db) GetLeaderID() int64 {
	return db.consensus.GetLeaderID()
}
