# WAL-Raft-Storage Integration Plan for HarmonyDB

## Executive Summary

This document provides a comprehensive analysis of etcd's WAL-Raft-Storage integration patterns and presents a detailed implementation plan for HarmonyDB. Based on the reverse engineering of etcd's architecture, we've identified critical integration patterns that ensure data replication, consistency, and recovery in distributed database systems.

## 1. etcd WAL-Raft-Storage Analysis

### 1.1 Data Flow Pipeline

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐    ┌─────────────┐
│   Client    │───▶│   Raft       │───▶│     WAL     │───▶│   BoltDB    │
│   Request   │    │   Ready()    │    │   Save()    │    │  BatchTx()  │
└─────────────┘    └──────────────┘    └─────────────┘    └─────────────┘
                          │                     │                │
                          ▼                     ▼                ▼
                    ┌──────────────┐    ┌─────────────┐    ┌─────────────┐
                    │  toApply     │    │ HardState + │    │    MVCC     │
                    │  Entries     │    │   Entries   │    │   Store     │
                    │              │    │             │    │ Application │
                    └──────────────┘    └─────────────┘    └─────────────┘
```

### 1.2 Critical Ordering Guarantees

**From `server/etcdserver/raft.go:243-285`**:

1. **Snapshot Order**: `storage.SaveSnap(snapshot)` MUST happen before `storage.Save(hardstate, entries)`
2. **WAL Persistence**: WAL entries are persisted to disk before applying to state machine
3. **Fsync Guarantee**: `storage.Sync()` ensures durability before releasing old data
4. **Memory Update**: `raftStorage.Append(entries)` updates in-memory log after persistence

### 1.3 Key Integration Points

#### A. Raft Ready Processing (`server/etcdserver/raft.go:208-299`)

```go
// Critical sequence from etcd
if !raft.IsEmptySnap(rd.Snapshot) {
    r.storage.SaveSnap(rd.Snapshot)     // 1. Save snapshot to WAL+disk
}

r.storage.Save(rd.HardState, rd.Entries) // 2. Save hardstate + entries to WAL

if !raft.IsEmptySnap(rd.Snapshot) {
    r.storage.Sync()                     // 3. Fsync before releasing
    r.raftStorage.ApplySnapshot()        // 4. Update memory
    r.storage.Release(rd.Snapshot)       // 5. Release old WAL files
}

r.raftStorage.Append(rd.Entries)        // 6. Update in-memory log
```

#### B. Application Pipeline (`server/etcdserver/server.go:1198-1222`)

```go
// Entry application with consistency checks
func (s *EtcdServer) applyEntries(ep *etcdProgress, apply *toApply) {
    firsti := apply.entries[0].Index
    if firsti > ep.appliedi+1 {
        panic("unexpected committed entry index gap")
    }

    // Apply only new entries (handle re-application)
    ents = apply.entries[ep.appliedi+1-firsti:]
    s.apply(ents, &ep.confState, apply.raftAdvancedC)
}
```

#### C. Storage Abstraction (`server/storage/storage.go`)

```go
type Storage interface {
    Save(st raftpb.HardState, ents []raftpb.Entry) error  // WAL persistence
    SaveSnap(snap raftpb.Snapshot) error                  // Snapshot handling
    Close() error                                          // Resource cleanup
    Release(snap raftpb.Snapshot) error                   // WAL file cleanup
    Sync() error                                          // Force fsync
}
```

### 1.4 Recovery Mechanisms

#### A. WAL Recovery (`server/storage/wal/wal.go:469-591`)

1. **Sequential Read**: WAL files read in sequence order
2. **CRC Validation**: Each record validated for corruption
3. **Entry Reconstruction**: Raft entries rebuilt from WAL records
4. **Snapshot Matching**: Validates snapshot consistency with WAL

#### B. Bootstrap Recovery (`server/etcdserver/bootstrap.go`)

1. **WAL Validation**: Ensures WAL integrity before startup
2. **Snapshot Loading**: Loads latest consistent snapshot
3. **Entry Replay**: Replays committed entries from WAL
4. **State Reconstruction**: Rebuilds in-memory state

### 1.5 Consistency Guarantees

#### A. Write Ordering
- WAL persistence → Memory update → Client response
- Snapshot saves before entry saves
- Fsync before WAL file rotation/release

#### B. Crash Recovery
- WAL provides replay capability
- Snapshots provide recovery checkpoints
- CRC ensures data integrity

#### C. Replication Safety
- Leader persists before replication
- Followers persist before acknowledgment
- Committed entries are durable

## 2. HarmonyDB Current State Analysis

### 2.1 Architecture Gaps

**Current HarmonyDB Issues**:

1. **Naive WAL**: Simple in-memory slice without persistence
   ```go
   type LogManager struct {
       logs []*proto.Log    // In-memory only!
       sync.RWMutex
   }
   ```

2. **No Integration**: WAL and Raft operate independently
3. **Missing Recovery**: No crash recovery mechanisms
4. **No Ordering**: No guarantees between WAL/storage/memory
5. **Basic Storage**: File-based storage without transaction guarantees

### 2.2 HarmonyDB Strengths

1. **Raft Foundation**: Basic Raft consensus implemented
2. **gRPC Transport**: Network layer established
3. **B-tree Storage**: Tree-based storage implementation
4. **Observability**: Metrics and monitoring framework

## 3. Integration Design for HarmonyDB

### 3.1 New Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    HarmonyDB Node                        │
├─────────────────────────────────────────────────────────┤
│              gRPC API Server                            │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐   │
│  │    Raft     │◄─┤  Ready()    │─▶│   Application   │   │
│  │  Consensus  │  │  Channel    │  │     Layer       │   │
│  └─────────────┘  └─────────────┘  └─────────────────┘   │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐   │
│  │     WAL     │◄─┤   Storage   │─▶│     B-tree      │   │
│  │ (Persistent)│  │ Abstraction │  │    Engine       │   │
│  └─────────────┘  └─────────────┘  └─────────────────┘   │
├─────────────────────────────────────────────────────────┤
│            File System / Disk Storage                   │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Core Components

#### A. Enhanced WAL Manager

```go
type WAL struct {
    dir         string
    metadata    []byte
    mu          sync.RWMutex
    encoder     *encoder
    decoder     *decoder
    files       []*os.File
    state       raftpb.HardState
    lastIndex   uint64
}

type WALEntry struct {
    Type   EntryType  // State, Entry, Snapshot, CRC
    Data   []byte
    CRC    uint32
    Index  uint64
    Term   uint64
}
```

#### B. Storage Abstraction

```go
type Storage interface {
    // WAL operations
    Save(state HardState, entries []Entry) error
    SaveSnapshot(snap Snapshot) error
    ReadAll() ([]byte, HardState, []Entry, error)

    // Lifecycle
    Close() error
    Sync() error
    Release(snap Snapshot) error
}

type storageImpl struct {
    wal     *WAL
    btree   *BTree
    snapDir string
}
```

#### C. Ready Channel Integration

```go
type ReadyHandler struct {
    storage   Storage
    raft      *raft.Node
    btree     *BTree
    applyc    chan ToApply
}

func (rh *ReadyHandler) processReady(rd raft.Ready) {
    // 1. Save snapshot if present
    if !raft.IsEmptySnap(rd.Snapshot) {
        rh.storage.SaveSnapshot(rd.Snapshot)
    }

    // 2. Persist WAL entries
    rh.storage.Save(rd.HardState, rd.Entries)

    // 3. Send to application
    rh.applyc <- ToApply{Entries: rd.CommittedEntries}

    // 4. Update Raft memory
    rh.raft.Advance()
}
```

### 3.3 Data Flow Implementation

#### Write Path:
1. **Client Request** → gRPC handler
2. **Raft Propose** → Add to Raft log
3. **Ready Event** → Process Raft ready state
4. **WAL Persist** → Write to disk atomically
5. **Memory Update** → Update Raft memory state
6. **Apply Channel** → Send to application layer
7. **B-tree Apply** → Update B-tree storage
8. **Client Response** → Return success

#### Read Path:
1. **Client Request** → gRPC handler
2. **Consistency Check** → Verify read index
3. **B-tree Query** → Read from storage
4. **Client Response** → Return data

#### Recovery Path:
1. **WAL Scan** → Read all WAL files
2. **Entry Validation** → Verify CRC checksums
3. **State Rebuild** → Reconstruct Raft state
4. **B-tree Rebuild** → Apply committed entries
5. **Ready State** → Resume normal operation

## 4. Implementation Roadmap

### Phase 1: WAL Foundation (Week 1-2)

#### Task 1.1: Persistent WAL Implementation
- [ ] File-based WAL with segmentation
- [ ] CRC checksum validation
- [ ] Atomic write operations
- [ ] WAL rotation and cleanup

#### Task 1.2: Entry Types and Encoding
- [ ] Define WAL entry types (State, Entry, Snapshot, CRC)
- [ ] Implement binary encoding/decoding
- [ ] Add metadata handling

#### Task 1.3: Basic Recovery
- [ ] WAL file scanning
- [ ] Entry replay functionality
- [ ] Corruption detection and handling

### Phase 2: Storage Integration (Week 3)

#### Task 2.1: Storage Abstraction
- [ ] Define Storage interface
- [ ] Implement storage with WAL + B-tree
- [ ] Add transaction boundaries

#### Task 2.2: Snapshot Management
- [ ] Snapshot creation from B-tree state
- [ ] Snapshot persistence and loading
- [ ] WAL truncation after snapshots

### Phase 3: Raft Integration (Week 4)

#### Task 3.1: Ready Channel Processing
- [ ] Implement Ready event handler
- [ ] Ensure proper ordering (snapshot → WAL → memory)
- [ ] Add fsync guarantees

#### Task 3.2: Application Pipeline
- [ ] Committed entry application
- [ ] Duplicate detection and handling
- [ ] Progress tracking

### Phase 4: Advanced Features (Week 5-6)

#### Task 4.1: Failure Recovery
- [ ] Crash recovery testing
- [ ] Corruption recovery scenarios
- [ ] Network partition handling

#### Task 4.2: Performance Optimization
- [ ] Batch writes
- [ ] Background WAL sync
- [ ] Memory usage optimization

#### Task 4.3: Observability
- [ ] WAL metrics (write rate, file sizes)
- [ ] Recovery metrics
- [ ] Performance monitoring

### Phase 5: Testing and Validation (Week 7-8)

#### Task 5.1: Unit Testing
- [ ] WAL component tests
- [ ] Storage integration tests
- [ ] Recovery scenario tests

#### Task 5.2: Integration Testing
- [ ] Multi-node cluster tests
- [ ] Failure injection testing
- [ ] Performance benchmarks

#### Task 5.3: Chaos Testing
- [ ] Random crash scenarios
- [ ] Disk corruption simulation
- [ ] Network partition testing

## 5. Critical Implementation Details

### 5.1 File Format Design

#### WAL File Structure
```
┌─────────────────┐
│   File Header   │ (Magic + Version + Metadata)
├─────────────────┤
│     Record 1    │ (Type + Length + CRC + Data)
├─────────────────┤
│     Record 2    │
├─────────────────┤
│      ...        │
└─────────────────┘
```

#### Record Format
```go
type Record struct {
    Type   uint32  // EntryType, StateType, etc.
    Length uint32  // Data length
    CRC    uint32  // CRC32 checksum
    Data   []byte  // Payload data
}
```

### 5.2 Concurrency Model

```go
type WAL struct {
    mu      sync.RWMutex  // Protects file operations
    writeMu sync.Mutex    // Serializes writes
    encoder *Encoder      // Thread-safe encoder
}

// Reads can happen concurrently
func (w *WAL) ReadAll() ([]Entry, error) {
    w.mu.RLock()
    defer w.mu.RUnlock()
    // Read logic
}

// Writes must be serialized
func (w *WAL) Save(entries []Entry) error {
    w.writeMu.Lock()
    defer w.writeMu.Unlock()
    // Write logic
}
```

### 5.3 Error Handling Strategy

1. **Corruption Detection**: CRC validation on all reads
2. **Partial Write Recovery**: Truncate incomplete records
3. **File System Errors**: Graceful degradation
4. **Disk Full**: Proper error propagation
5. **Recovery Boundaries**: Clear recovery points

### 5.4 Performance Considerations

1. **Batch Writes**: Group multiple entries per fsync
2. **Pre-allocation**: Preallocate WAL file space
3. **Background Sync**: Async fsync with proper ordering
4. **Memory Mapping**: Consider mmap for large files
5. **Compression**: Optional compression for large entries

## 6. Testing Strategy

### 6.1 Unit Tests
- WAL append/read correctness
- CRC validation
- File rotation logic
- Recovery from various corruption scenarios

### 6.2 Integration Tests
- End-to-end write → WAL → recovery
- Multi-node consensus with WAL
- Snapshot creation and recovery
- Performance under load

### 6.3 Failure Scenarios
- Process crash during write
- Disk corruption
- Partial file writes
- Clock skew and ordering issues

### 6.4 Performance Tests
- Write throughput benchmarks
- Recovery time measurements
- Memory usage profiling
- Disk space utilization

## 7. Success Metrics

### 7.1 Correctness
- [ ] Zero data loss in crash scenarios
- [ ] Consistent recovery from any failure point
- [ ] Proper ordering guarantees maintained

### 7.2 Performance
- [ ] WAL write latency < 1ms (p99)
- [ ] Recovery time < 10s for 1GB WAL
- [ ] Memory usage growth < 10MB/hour

### 7.3 Reliability
- [ ] Handle 10⁶ crash/recovery cycles
- [ ] Survive arbitrary corruption patterns
- [ ] Maintain availability during member failures

## Conclusion

This implementation plan provides a comprehensive roadmap for integrating WAL, Raft, and storage systems in HarmonyDB based on proven patterns from etcd. The phased approach ensures incremental progress while maintaining system stability. Critical attention to ordering guarantees, recovery mechanisms, and performance optimization will result in a production-ready distributed database system.

The key insight from etcd's architecture is that the integration between these components is just as important as the individual component implementation. Proper ordering, atomicity, and recovery guarantees are what make a distributed database reliable and consistent.