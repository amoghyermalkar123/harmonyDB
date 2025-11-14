package harmonydb

import (
	"context"
	"fmt"
	"harmonydb/raft"

	"go.uber.org/zap"
)

type Db struct {
	btree     *BTree
	consensus *raft.Raft
}

func Open(nodePort int) (*Db, error) {
	db := &Db{
		btree:     NewBTree(),
		consensus: raft.NewRaftServerWithConsul(nodePort),
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
				if err := db.btree.Put([]byte(log.Data.Key), []byte(log.Data.Value)); err != nil {
					panic(fmt.Sprintf("put: should never panic: (%v)", err))
				}
			}
		}
	}
}

func (db *Db) Put(key, val []byte) error {
	GetLogger().Debug("Put", zap.String("component", "db"))

	if err := db.consensus.Put(context.TODO(), key, val); err != nil {
		// TODO: leader req redirection
		return fmt.Errorf("consensus: %w", err)
	}

	lastApplied, lastCommitted := db.consensus.GetLastAppliedLastCommitted()

	GetLogger().Debug("Consensus Success", zap.String("component", "db"), zap.Int64("lastApplied", lastApplied), zap.Int64("lastCommitted", lastCommitted))

	// apply all remaining entries since we last applied to the database
	// TODO: this is incorrect, we are adding same key value? no? once? idk change this
	// we can optimize this for batched entries as well
	for i := lastApplied; i <= lastCommitted; i++ {
		GetLogger().Debug("Apply Log", zap.String("component", "db"), zap.Int64("lastApplied", lastApplied), zap.Int64("lastCommitted", lastCommitted))
		if err := db.btree.Put(key, val); err != nil {
			return fmt.Errorf("Put : %w", err)
		}

		db.consensus.IncrementLastApplied()
	}

	return nil
}

func (db *Db) Get(key []byte) ([]byte, error) {
	return db.btree.Get(key)
}
