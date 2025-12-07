# Mapping etcd WAL to HarmonyDB WAL

## The Key Insight

**etcd's WAL is production-grade with many optimizations. For HarmonyDB v1, we can start much simpler.**

## Direct Mapping

### What You NEED from etcd

| etcd Concept | Why It Matters | HarmonyDB Implementation |
|--------------|----------------|--------------------------|
| **Append-only log** | Durability | Single file, append writes |
| **fsync before ACK** | Safety | Call `file.Sync()` after writes |
| **Recovery on startup** | Crash recovery | Read file, replay entries |
| **Record format** | Serialization | Protobuf or JSON per line |

### What You CAN SKIP (for now)

| etcd Feature | Why etcd Has It | Can Skip Because |
|--------------|-----------------|------------------|
| **File segments (64MB)** | Manage huge logs | Your log is small initially |
| **File pipeline** | Pre-allocation perf | Not performance critical yet |
| **CRC chains** | Corruption detection | Simpler: CRC per record |
| **Multiple record types** | Flexibility | You only need Entry + State |

## Simplified HarmonyDB WAL Design

### Current State (In-Memory)

```go
// wal/wal.go
type LogManager struct {
    logs []*proto.Log
    sync.RWMutex
}

func (lm *LogManager) Append(log *proto.Log) {
    lm.Lock()
    defer lm.Unlock()
    lm.logs = append(lm.logs, log)  // ← Lost on crash!
}
```

### Target State (Persistent)

```go
// wal/wal.go
type LogManager struct {
    logs []*proto.Log
    sync.RWMutex
    
    // NEW: Add these fields
    file *os.File      // The WAL file handle
    path string        // Where the WAL file lives
}

func NewLM(dataDir string) *LogManager {
    path := filepath.Join(dataDir, "wal.log")
    
    // Open or create WAL file
    file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
    if err != nil {
        panic(err) // Handle properly in real code
    }
    
    lm := &LogManager{
        logs: make([]*proto.Log, 0),
        file: file,
        path: path,
    }
    
    // RECOVERY: If file exists and has data, read it
    if stat, _ := file.Stat(); stat.Size() > 0 {
        lm.recover()
    }
    
    return lm
}

func (lm *LogManager) Append(log *proto.Log) error {
    lm.Lock()
    defer lm.Unlock()
    
    // 1. Write to file FIRST (etcd does this)
    if err := lm.writeToFile(log); err != nil {
        return err
    }
    
    // 2. THEN add to memory
    lm.logs = append(lm.logs, log)
    
    return nil
}

func (lm *LogManager) writeToFile(log *proto.Log) error {
    // Serialize the log entry
    data, err := proto.Marshal(log)
    if err != nil {
        return err
    }
    
    // Create a record with length prefix (simple framing)
    // Format: [4 bytes length][data]
    length := uint32(len(data))
    
    // Write length
    if err := binary.Write(lm.file, binary.LittleEndian, length); err != nil {
        return err
    }
    
    // Write data
    if _, err := lm.file.Write(data); err != nil {
        return err
    }
    
    // fsync (this is THE critical part from etcd)
    return lm.file.Sync()
}

func (lm *LogManager) recover() error {
    lm.file.Seek(0, io.SeekStart)
    
    for {
        // Read length
        var length uint32
        if err := binary.Read(lm.file, binary.LittleEndian, &length); err != nil {
            if err == io.EOF {
                break // Done reading
            }
            return err
        }
        
        // Read data
        data := make([]byte, length)
        if _, err := io.ReadFull(lm.file, data); err != nil {
            return err
        }
        
        // Deserialize
        log := &proto.Log{}
        if err := proto.Unmarshal(data, log); err != nil {
            return err
        }
        
        // Add to memory
        lm.logs = append(lm.logs, log)
    }
    
    return nil
}
```

## Step-by-Step Implementation

### Phase 1: Single File WAL (1-2 hours)

**Goal**: Write entries to a single file, recover on restart

```
Files to modify:
  1. wal/wal.go - Add file handle and persist logic
  2. db.go - Pass data directory to NewLM()
```

**Changes**:
```go
// In wal.go
+ file *os.File
+ path string

// In NewLM()
+ Open file for append
+ Call recover() if file exists

// In Append()
+ Marshal entry
+ Write [length][data] to file
+ Call file.Sync()
```

**Test**: Run TestWALRecoverySingleNode (remove t.Skip())

### Phase 2: Error Handling (30 min)

**Goal**: Handle corrupt entries gracefully

```go
func (lm *LogManager) recover() error {
    for {
        // Try to read entry
        log, err := lm.readNextEntry()
        if err == io.EOF {
            break // Normal end
        }
        if err != nil {
            // Corruption detected
            log.Warn("WAL corruption detected", zap.Error(err))
            break // Stop here, don't corrupt memory
        }
        lm.logs = append(lm.logs, log)
    }
}
```

### Phase 3: CRC Validation (1 hour)

**Goal**: Detect corruption with checksums

```go
type WALRecord struct {
    CRC    uint32        // CRC of Data
    Length uint32        // Length of Data
    Data   []byte        // Marshaled proto.Log
}

func (lm *LogManager) writeToFile(log *proto.Log) error {
    data, _ := proto.Marshal(log)
    
    // Calculate CRC
    crc := crc32.ChecksumIEEE(data)
    
    // Write record
    record := WALRecord{
        CRC:    crc,
        Length: uint32(len(data)),
        Data:   data,
    }
    
    // Write to file
    binary.Write(lm.file, binary.LittleEndian, record.CRC)
    binary.Write(lm.file, binary.LittleEndian, record.Length)
    lm.file.Write(record.Data)
    
    return lm.file.Sync()
}
```

### Phase 4 (LATER): Segmentation

Only add this when your WAL file grows > 100MB

## Key Differences from etcd

| Aspect | etcd | HarmonyDB v1 |
|--------|------|--------------|
| **Files** | Multiple segments | Single file |
| **Sync** | Conditional (mustSync) | Always (simpler) |
| **CRC** | Cumulative chain | Per-record |
| **Recovery** | Selective (from snapshot) | Full replay |
| **Records** | 5 types (Entry, State, Meta, CRC, Snap) | 1 type (Entry) |

## Your Current Mental Block - Solved

**Mental Block**: "etcd has segments, file pipeline, CRC chains... where do I start?"

**Solution**: 
1. **Start simpler**: Single file, single record type
2. **Core concept**: Append + fsync before ACK
3. **Recover**: Read file top-to-bottom on startup
4. **Add complexity later**: Segments when needed

## Mapping Your Tests to Implementation

```
TestWALRecoverySingleNode
  → Needs: writeToFile() + recover()
  
TestWALRecoveryMultipleEntries
  → Needs: Same as above (just more entries)
  
TestWALRecoveryWithOverwrites
  → Needs: Same (overwrites handled by B-tree)
  
TestWALRecoveryWithCorruption
  → Needs: CRC validation in recover()
```

## Next Steps

1. **Start here**: Add `file *os.File` to LogManager
2. **Then**: Implement `writeToFile()` with simple framing
3. **Then**: Implement `recover()` to read on startup
4. **Test**: Remove t.Skip() from first test
5. **Iterate**: Add CRC, better error handling

You don't need to build etcd's full WAL. Start with the 20% that gives you 80% of the value.
