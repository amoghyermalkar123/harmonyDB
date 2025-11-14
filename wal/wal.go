package wal

import (
	"harmonydb/raft/proto"
	"sync"
)

// TODO: make this a proper WAL implementation
type LogManager struct {
	logs []*proto.Log
	sync.RWMutex
}

func NewLM() *LogManager {
	return &LogManager{
		logs: make([]*proto.Log, 0),
	}
}

func (lm *LogManager) Append(log *proto.Log) {
	lm.Lock()
	defer lm.Unlock()
	lm.logs = append(lm.logs, log)
}

func (lm *LogManager) GetLastLogID() int64 {
	lm.RLock()
	defer lm.RUnlock()

	if len(lm.logs) == 0 {
		return 1
	}
	return lm.logs[len(lm.logs)-1].Id
}

func (lm *LogManager) NextLogID() int64 {
	lm.RLock()
	defer lm.RUnlock()

	if len(lm.logs) == 0 {
		return 1
	}

	return lm.logs[len(lm.logs)-1].Id + 1
}

// Deprecated
func (lm *LogManager) GetLastLogTerm() int64 {
	lm.RLock()
	defer lm.RUnlock()
	if len(lm.logs) == 0 {
		return 0
	}
	return lm.logs[len(lm.logs)-1].Term
}

func (lm *LogManager) GetLog(index int) *proto.Log {
	lm.RLock()
	defer lm.RUnlock()

	// we do this because Id: 1 is stored at slice index 0
	index = index - 1
	if index < 0 || index >= len(lm.logs) {
		return nil
	}

	return lm.logs[index]
}

func (lm *LogManager) GetLength() int {
	lm.RLock()
	defer lm.RUnlock()
	return len(lm.logs)
}

func (lm *LogManager) GetLogs() []*proto.Log {
	lm.RLock()
	defer lm.RUnlock()
	result := make([]*proto.Log, len(lm.logs))
	copy(result, lm.logs)
	return result
}

func (lm *LogManager) GetLogsAfter(index int64) []*proto.Log {
	lm.RLock()
	defer lm.RUnlock()
	result := make([]*proto.Log, len(lm.logs[index:]))
	copy(result, lm.logs[index:])
	return result
}

func (lm *LogManager) TruncateAfter(index int) {
	lm.Lock()
	defer lm.Unlock()
	if index >= 0 && index < len(lm.logs) {
		lm.logs = lm.logs[:index+1]
	}
}
