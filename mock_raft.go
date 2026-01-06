package harmonydb

import (
	"context"
	"harmonydb/repl"
	proto "harmonydb/repl/proto/repl"
	"harmonydb/wal"
	"sync"
)

// MockRaftNode is a mock implementation of repl.RaftNode for testing
type MockRaftNode struct {
	mu          sync.Mutex
	ID          int64
	applyc      chan repl.ToApply
	processFunc func(ctx context.Context, msg *proto.Message) (*proto.Message, error)
	tickCount   int
}

func NewMockRaftNode(id int64) *MockRaftNode {
	return &MockRaftNode{
		ID:     id,
		applyc: make(chan repl.ToApply, 256),
	}
}

func (m *MockRaftNode) Tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickCount++
}

func (m *MockRaftNode) Process(ctx context.Context, msg *proto.Message) (*proto.Message, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, msg)
	}
	return &proto.Message{}, nil
}

func (m *MockRaftNode) Apply() chan repl.ToApply {
	return m.applyc
}

func (m *MockRaftNode) GetWAL() wal.Storage {
	return nil
}

// SetProcessFunc allows tests to customize the behavior of Process
func (m *MockRaftNode) SetProcessFunc(f func(ctx context.Context, msg *proto.Message) (*proto.Message, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processFunc = f
}

// SimulateApply simulates applying entries (as if they were committed by Raft)
func (m *MockRaftNode) SimulateApply(entries []*proto.Log, commitIndex int64) {
	m.applyc <- repl.ToApply{
		Entries:     entries,
		CommitIndex: commitIndex,
	}
}

// GetTickCount returns the number of times Tick was called
func (m *MockRaftNode) GetTickCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tickCount
}
