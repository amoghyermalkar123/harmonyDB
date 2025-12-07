package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"harmonydb/raft/proto"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// TODO: make this a proper WAL implementation
type LogManager struct {
	logs []*proto.Log
	sync.RWMutex

	file *os.File
	path string
}

// caller acquires lock and also opens the file
func (lm *LogManager) decodeLogs() ([]*proto.Log, error) {
	reader := bufio.NewReader(lm.file)
	var logs []*proto.Log

	for {
		var term int64
		if err := binary.Read(reader, binary.LittleEndian, &term); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		var id int64
		if err := binary.Read(reader, binary.LittleEndian, &id); err != nil {
			return nil, err
		}

		var keyLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &keyLen); err != nil {
			return nil, err
		}

		var valueLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &valueLen); err != nil {
			return nil, err
		}

		var opLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &opLen); err != nil {
			return nil, err
		}

		key := make([]byte, keyLen)
		if _, err := io.ReadFull(reader, key); err != nil {
			return nil, err
		}

		value := make([]byte, valueLen)
		if _, err := io.ReadFull(reader, value); err != nil {
			return nil, err
		}

		op := make([]byte, opLen)
		if _, err := io.ReadFull(reader, op); err != nil {
			return nil, err
		}

		logs = append(logs, &proto.Log{
			Term: term,
			Id:   id,
			Data: &proto.Cmd{
				Key:   string(key),
				Value: string(value),
				Op:    string(op),
			},
		})
	}

	return logs, nil
}

func encodeLog(logs []*proto.Log) ([]byte, error) {
	buf := &bytes.Buffer{}

	for _, log := range logs {
		if err := binary.Write(buf, binary.LittleEndian, log.Term); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, log.Id); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, uint32(len(log.Data.GetKey()))); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, uint32(len(log.Data.GetValue()))); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, uint32(len(log.Data.GetOp()))); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, []byte(log.Data.GetKey())); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, []byte(log.Data.GetValue())); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, []byte(log.Data.GetOp())); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func NewLM(path string) *LogManager {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Errorf("create WAL directory: %w", err))
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		panic(fmt.Errorf("open WAL file: %w", err))
	}

	lm := &LogManager{
		logs: make([]*proto.Log, 0),
		file: file,
		path: path,
	}

	// panics if recover fails, no point in booting in invalid state
	if stat, err := file.Stat(); err == nil && stat.Size() > 0 {
		lm.readWAL()
	}

	return lm
}

func (lm *LogManager) Append(log *proto.Log) {
	lm.Lock()
	defer lm.Unlock()

	// durably persisting raft entry
	if err := lm.writeToFile([]*proto.Log{log}); err != nil {
		panic(err)
	}

	// storing in-memory
	lm.logs = append(lm.logs, log)
}

func (lm *LogManager) readWAL() {
	lm.Lock()
	defer lm.Unlock()

	file, err := os.Open(lm.path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logs, err := lm.decodeLogs()
	if err != nil {
		panic(fmt.Errorf("recover: %w", err))
	}

	lm.logs = logs
}

func (lm *LogManager) writeToFile(log []*proto.Log) error {
	data, err := encodeLog(log)
	if err != nil {
		return err
	}

	_, err = lm.file.Write(data)
	if err != nil {
		return err
	}

	if err := lm.file.Sync(); err != nil {
		return fmt.Errorf("writeToFile: sync: %w", err)
	}

	return nil
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

	// Bounds checking to prevent panic
	if index < 0 {
		index = 0
	}
	if index >= int64(len(lm.logs)) {
		return []*proto.Log{}
	}

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
