package raft

import (
	"sync"

	"harmonydb/raft/proto"
	"harmonydb/wal"
)

// testLogStore is an in-memory log store for testing
// It wraps wal.LogManager but adds testing-specific capabilities
type testLogStore struct {
	logManager *wal.LogManager
	logs       []*proto.Log
	mu         sync.RWMutex
}

// newTestLogStore creates a new test log store
func newTestLogStore() *testLogStore {
	return &testLogStore{
		logManager: wal.NewLM(),
		logs:       make([]*proto.Log, 0),
	}
}

// append adds a log entry
func (s *testLogStore) append(log *proto.Log) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logManager.Append(log)
	s.logs = append(s.logs, log)
}

// getLog retrieves a log entry by index (1-based)
func (s *testLogStore) getLog(index int) *proto.Log {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index < 1 || index > len(s.logs) {
		return nil
	}
	return s.logs[index-1]
}

// getLogsFrom returns all logs starting from index (1-based)
func (s *testLogStore) getLogsFrom(index int) []*proto.Log {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index < 1 {
		return nil
	}
	if index > len(s.logs) {
		return []*proto.Log{}
	}
	return s.logs[index-1:]
}

// getAllLogs returns all log entries
func (s *testLogStore) getAllLogs() []*proto.Log {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*proto.Log, len(s.logs))
	copy(result, s.logs)
	return result
}

// lastIndex returns the index of the last log entry
func (s *testLogStore) lastIndex() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.logs))
}

// lastTerm returns the term of the last log entry
func (s *testLogStore) lastTerm() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.logs) == 0 {
		return 0
	}
	return s.logs[len(s.logs)-1].Term
}

// truncateAfter removes all log entries after the given index
func (s *testLogStore) truncateAfter(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 {
		index = 0
	}
	if index < len(s.logs) {
		s.logs = s.logs[:index]
		s.logManager.TruncateAfter(index)
	}
}

// clear removes all log entries
func (s *testLogStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = make([]*proto.Log, 0)
	s.logManager = wal.NewLM()
}

// count returns the number of log entries
func (s *testLogStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.logs)
}

// snapshot returns a copy of all logs at this point in time
func (s *testLogStore) snapshot() []*proto.Log {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*proto.Log, len(s.logs))
	copy(result, s.logs)
	return result
}

// contains checks if a log entry with the given index and term exists
func (s *testLogStore) contains(index int64, term int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index < 1 || int(index) > len(s.logs) {
		return false
	}

	log := s.logs[index-1]
	return log.Term == term
}

// getTermAtIndex returns the term of the log entry at the given index
func (s *testLogStore) getTermAtIndex(index int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index < 1 || int(index) > len(s.logs) {
		return 0
	}

	return s.logs[index-1].Term
}
