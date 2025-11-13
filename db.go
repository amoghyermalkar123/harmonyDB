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
	return &Db{
		btree:     NewBTree(),
		consensus: raft.NewRaftServerWithConsul(nodePort),
	}, nil
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
