package harmonydb

import (
	"context"
	proto "harmonydb/repl/proto/repl"
	"harmonydb/scheduler"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) (*DB, *MockRaftNode) {
	mockRaft := NewMockRaftNode(1)
	db := &DB{
		kv:              NewBTree(),
		raft:            mockRaft,
		logger:          GetStructuredLogger("db_test"),
		scheduler:       scheduler.NewFifoScheduler(),
		waiter:          newWaiter(),
		applyWait:       newWaitTime(),
		generator:       NewGenerator(1, time.Now()),
		config:          Config{ProposalTimeout: 100 * time.Millisecond, LinearizedReadTimeout: 100 * time.Millisecond},
		lastCommitIndex: 0,
	}
	go db.applyLoop()
	return db, mockRaft
}

func TestPut_Linearizable(t *testing.T) {
	db, mockRaft := newTestDB(t)

	mockRaft.SetProcessFunc(func(ctx context.Context, msg *proto.Message) (*proto.Message, error) {
		entries := msg.GetEntries().GetEntries()
		if len(entries) > 0 {
			go func() {
				time.Sleep(10 * time.Millisecond)
				mockRaft.SimulateApply([]*proto.Log{{Id: 1, Term: 1, Data: entries[0].GetData()}}, 1)
			}()
		}
		return &proto.Message{}, nil
	})

	err := db.Put(context.Background(), []byte("key"), []byte("value"))
	require.NoError(t, err)

	val, err := db.kv.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, "value", string(val))
}

func TestPut_Timeout(t *testing.T) {
	db, mockRaft := newTestDB(t)
	mockRaft.SetProcessFunc(func(ctx context.Context, msg *proto.Message) (*proto.Message, error) {
		return &proto.Message{}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := db.Put(ctx, []byte("key"), []byte("value"))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "timeout"))
}

func TestGet_Linearizable(t *testing.T) {
	db, _ := newTestDB(t)

	db.kv.put([]byte("key"), []byte("value"))
	db.lastCommitIndex = 5
	db.applyWait.Trigger(5)

	val, err := db.Get(context.Background(), []byte("key"))
	require.NoError(t, err)
	assert.Equal(t, "value", string(val))
}

func TestGet_Timeout(t *testing.T) {
	db, _ := newTestDB(t)
	db.lastCommitIndex = 100

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := db.Get(ctx, []byte("key"))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "timeout"))
}
