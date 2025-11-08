package harmonydb

import (
	"context"
	"fmt"
	"harmonydb/raft"
	"net"
	"os"
)

type Db struct {
	btree     *BTree
	consensus *raft.Raft
}

func Open() (*Db, error) {
	// Try to find an available port
	nodePort := -1
	for i := 0; i < 3; i++ {
		testPort := 8080 + i
		if isPortAvailable(testPort) {
			nodePort = testPort
			break
		}
	}

	if nodePort == -1 {
		fmt.Println("No available ports found in range 8080-8082")
		os.Exit(1)
	}

	return &Db{
		btree:     NewBTree(),
		consensus: raft.NewRaftServerWithConsul(nodePort),
	}, nil
}

func (db *Db) Put(key, val []byte) error {
	if err := db.consensus.Put(context.TODO(), key, val); err != nil {
		return fmt.Errorf("consensus: %w", err)
	}

	lastApplied, lastCommitted := db.consensus.GetLastAppliedLastCommitted()

	// apply all remaining entries since we last applied to the database
	for i := lastApplied; i <= lastCommitted; i++ {
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

func isPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
