package repl

import (
	proto "harmonydb/repl/proto/repl"
)

type raftLog struct {
	// in-memory log entries (index 0 is unused, entries start at index 1)
	entries []*proto.Log

	// volatile state
	// it's persisted via the Ready.HardState mechanism by the consumer
	// of the raft package
	commitIndex int64 // index of highest log entry known to be committed
	lastApplied int64 // index of highest log entry applied to state machine

	// stableIndex tracks the highest log entry that has been persisted to WAL
	// entries[1:stableIndex] are persisted, entries[stableIndex+1:] are unstable
	stableIndex int64
}

func newRaftLog() *raftLog {
	return &raftLog{
		entries:     make([]*proto.Log, 1), // index 0 unused
		commitIndex: 0,
		lastApplied: 0,
	}
}

// lastIndex returns the index of the last entry in the log
func (l *raftLog) lastIndex() int64 {
	if len(l.entries) <= 1 {
		return 0
	}
	return l.entries[len(l.entries)-1].Id
}

// lastTerm returns the term of the last entry in the log
func (l *raftLog) lastTerm() int64 {
	if len(l.entries) <= 1 {
		return 0
	}
	return l.entries[len(l.entries)-1].Term
}

// append adds new entries to the log
func (l *raftLog) append(entries ...*proto.Log) {
	if len(entries) == 0 {
		return
	}
	l.entries = append(l.entries, entries...)
}

// term returns the term of entry at index i
func (l *raftLog) term(i int64) int64 {
	if i == 0 || i > l.lastIndex() {
		return 0
	}
	// entries are 1-indexed
	return l.entries[i].Term
}

// committedEntries returns all entries that are committed but not yet applied
func (l *raftLog) committedEntries() []*proto.Log {
	if l.commitIndex <= l.lastApplied {
		return nil
	}

	result := make([]*proto.Log, 0, l.commitIndex-l.lastApplied)
	for i := l.lastApplied + 1; i <= l.commitIndex; i++ {
		if int(i) < len(l.entries) {
			result = append(result, l.entries[i])
		}
	}
	return result
}

// maybeCommit attempts to advance commitIndex
func (l *raftLog) maybeCommit(index int64) bool {
	if index > l.commitIndex && index <= l.lastIndex() {
		l.commitIndex = index
		return true
	}
	return false
}

// markAppliedTill marks entries up to index as applied to state machine
func (l *raftLog) markAppliedTill(index int64) {
	if index > l.lastApplied {
		l.lastApplied = index
	}
}

// markStableTill marks entries up to index as persisted to WAL
func (l *raftLog) markStableTill(index int64) {
	if index > l.stableIndex && index <= l.lastIndex() {
		l.stableIndex = index
	}
}

// volatileEntries returns entries that haven't been persisted to WAL yet
func (l *raftLog) volatileEntries() []*proto.Log {
	if l.stableIndex >= l.lastIndex() {
		return nil
	}

	result := make([]*proto.Log, 0)
	for i := l.stableIndex + 1; i <= l.lastIndex(); i++ {
		if int(i) < len(l.entries) {
			result = append(result, l.entries[i])
		}
	}
	return result
}
