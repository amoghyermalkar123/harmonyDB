# CORRECTED: HarmonyDB WAL-Raft-Storage Implementation Tasks

## ⚠️ DEPENDENCY CORRECTION

The original task ordering had **critical dependency issues** that would require back-and-forth modifications. This corrected version ensures clean sequential implementation.

## Fixed Implementation Flow

```
Phase 1: Core Interfaces    Phase 2: WAL Foundation    Phase 3: Storage Integration
┌─────────────────────┐    ┌────────────────────────┐   ┌──────────────────────┐
│  • Define ALL       │───▶│  • File-based WAL      │──▶│  • Connect WAL +     │
│    Interfaces       │    │  • CRC validation      │   │    B-tree            │
│  • Data Types       │    │  • Recovery logic      │   │  • Snapshots         │
│  • Error Types      │    │  • Standalone tests    │   │  • Transactions      │
└─────────────────────┘    └────────────────────────┘   └──────────────────────┘
          │                           │                              │
          ▼                           ▼                              ▼
Phase 4: Raft Integration     Phase 5: Advanced Features & Testing
┌─────────────────────┐      ┌────────────────────────┐
│  • Ready Processing │      │  • Performance Opts    │
│  • Application Flow │      │  • Failure Recovery    │
│  • Node Integration │      │  • Comprehensive Tests │
└─────────────────────┘      └────────────────────────┘
```

---

## Phase 1: Core Interfaces & Data Types (Week 1)

### 🎯 Goal: Define ALL interfaces and types upfront to avoid later changes

#### Task 1.1: Define Storage Interface Contract

**File**: `storage/interfaces.go`

```go
package storage

import (
    "io"
)

// Core types - define once, use everywhere
type HardState struct {
    Term   uint64 `json:"term"`
    Vote   uint64 `json:"vote"`
    Commit uint64 `json:"commit"`
}

type Entry struct {
    Term  uint64    `json:"term"`
    Index uint64    `json:"index"`
    Type  EntryType `json:"type"`
    Data  []byte    `json:"data"`
}

type EntryType uint8

const (
    EntryNormal EntryType = iota
    EntryConfChange
    EntryConfChangeV2
)

type Snapshot struct {
    Index uint64 `json:"index"`
    Term  uint64 `json:"term"`
    Data  []byte `json:"data"`
}

// Storage interface - ALL operations defined upfront
type Storage interface {
    // WAL operations (Phase 2 will implement)
    Save(state HardState, entries []Entry) error
    SaveSnapshot(snap Snapshot) error
    ReadAll() ([]byte, HardState, []Entry, error)
    Sync() error

    // State machine operations (Phase 3 will implement)
    Apply(entries []Entry) error
    Query(key []byte) ([]byte, error)
    CreateSnapshot() (Snapshot, error)

    // Lifecycle
    Close() error
}

// WAL interface - Phase 2 will implement
type WAL interface {
    Append(record WALRecord) error
    Read(offset uint64) (WALRecord, error)
    ReadAll() ([]WALRecord, error)
    Sync() error
    Close() error
}

type WALRecord struct {
    Type   RecordType `json:"type"`
    Index  uint64     `json:"index"`
    Term   uint64     `json:"term"`
    Data   []byte     `json:"data"`
    CRC    uint32     `json:"crc"`
}

type RecordType uint32

const (
    RecordEntry RecordType = iota + 1
    RecordState
    RecordSnapshot
    RecordCRC
)

// Errors - define all error types upfront
var (
    ErrCRCMismatch    = errors.New("CRC mismatch")
    ErrCorruptRecord  = errors.New("corrupt record")
    ErrNotFound       = errors.New("not found")
    ErrInvalidIndex   = errors.New("invalid index")
)
```

#### Task 1.2: Define Raft Integration Interface

**File**: `node/interfaces.go`

```go
package node

// Raft integration interface - Phase 4 will implement
type RaftNode interface {
    // Core Raft operations
    Propose(data []byte) error
    ProposeConfChange(cc ConfChange) error
    Step(msg Message) error

    // State queries
    Status() Status
    Lead() uint64

    // Event channels
    Ready() <-chan Ready

    // Lifecycle
    Stop()
}

// Ready represents Raft ready state
type Ready struct {
    HardState        HardState
    Entries          []Entry
    Snapshot         Snapshot
    CommittedEntries []Entry
    Messages         []Message
}

// Application interface - Phase 4 will implement
type Application interface {
    Apply(entries []Entry) error
    Snapshot() ([]byte, error)
    Restore(snapshot []byte) error
}
```

#### Task 1.3: Create Test Interfaces

**File**: `testing/interfaces.go`

```go
package testing

// Test helpers that all phases can use
type TestCluster interface {
    Start(nodeCount int) error
    Stop() error
    GetNode(id uint64) TestNode
    PartitionNode(id uint64)
    UnpartitionNode(id uint64)
    KillNode(id uint64)
    RestartNode(id uint64)
}

type TestNode interface {
    Put(key, value []byte) error
    Get(key []byte) ([]byte, error)
    Delete(key []byte) error
    IsLeader() bool
    GetTerm() uint64
}
```

**✅ Phase 1 Deliverable**: Complete interface definitions with zero implementation. All subsequent phases implement these interfaces without modification.

---

## Phase 2: WAL Foundation (Week 2)

### 🎯 Goal: Implement WAL interface from Phase 1 as standalone component

#### Task 2.1: Implement WAL File Operations

**File**: `wal/wal.go`

```go
package wal

import (
    "harmonydb/storage" // Import interfaces from Phase 1
)

// Implements storage.WAL interface defined in Phase 1
type fileWAL struct {
    dir       string
    files     []*os.File
    encoder   *encoder
    decoder   *decoder
    mu        sync.RWMutex
    nextSeq   uint64
}

// NewWAL creates WAL implementing storage.WAL interface
func NewWAL(dir string) (storage.WAL, error) {
    // Implementation details...
}

// Append implements storage.WAL.Append
func (w *fileWAL) Append(record storage.WALRecord) error {
    // File operations, encoding, CRC calculation
}

// ReadAll implements storage.WAL.ReadAll
func (w *fileWAL) ReadAll() ([]storage.WALRecord, error) {
    // File scanning, decoding, validation
}
```

#### Task 2.2: Implement Encoding/Decoding

**Files**: `wal/encoder.go`, `wal/decoder.go`

```go
// encoder.go
type encoder struct {
    w   io.Writer
    crc hash.Hash32
}

func (e *encoder) encode(rec storage.WALRecord) error {
    // Encode using types from Phase 1
}

// decoder.go
type decoder struct {
    r   io.Reader
    crc hash.Hash32
}

func (d *decoder) decode() (storage.WALRecord, error) {
    // Decode using types from Phase 1
}
```

#### Task 2.3: Standalone WAL Tests

**File**: `wal/wal_test.go`

```go
func TestWALBasicOperations(t *testing.T) {
    // Test WAL using interfaces from Phase 1
    wal := NewWAL("/tmp/test")

    record := storage.WALRecord{
        Type: storage.RecordEntry,
        Data: []byte("test"),
    }

    err := wal.Append(record)
    assert.NoError(t, err)

    records, err := wal.ReadAll()
    assert.NoError(t, err)
    assert.Len(t, records, 1)
}
```

**✅ Phase 2 Deliverable**: Fully functional WAL that implements Phase 1 interfaces. Can be tested independently of Raft/Storage.

---

## Phase 3: Storage Integration (Week 3)

### 🎯 Goal: Connect WAL with B-tree using interfaces from Phase 1

#### Task 3.1: Implement Unified Storage

**File**: `storage/unified.go`

```go
package storage

// Implements Storage interface from Phase 1
type unifiedStorage struct {
    wal   WAL    // Use WAL interface from Phase 1, implemented in Phase 2
    btree *btree.Tree
    mu    sync.RWMutex

    appliedIndex uint64
    appliedTerm  uint64
}

// NewUnifiedStorage creates storage implementing Storage interface
func NewUnifiedStorage(walDir, btreeDir string) (Storage, error) {
    // Uses WAL from Phase 2
    w, err := wal.NewWAL(walDir)
    if err != nil {
        return nil, err
    }

    bt, err := btree.Open(btreeDir)  // Uses existing B-tree
    if err != nil {
        return nil, err
    }

    return &unifiedStorage{
        wal:   w,
        btree: bt,
    }, nil
}

// Save implements Storage.Save using WAL from Phase 2
func (s *unifiedStorage) Save(state HardState, entries []Entry) error {
    // Convert to WAL records and use WAL.Append
    for _, entry := range entries {
        record := WALRecord{
            Type:  RecordEntry,
            Index: entry.Index,
            Term:  entry.Term,
            Data:  entry.Data,
        }
        if err := s.wal.Append(record); err != nil {
            return err
        }
    }
    return s.wal.Sync()
}

// Apply implements Storage.Apply using B-tree
func (s *unifiedStorage) Apply(entries []Entry) error {
    // Apply to B-tree state machine
}
```

#### Task 3.2: Add Snapshot Support

**File**: `storage/snapshot.go`

```go
// Implements snapshot operations using interfaces from Phase 1
func (s *unifiedStorage) CreateSnapshot() (Snapshot, error) {
    // Serialize B-tree state
}

func (s *unifiedStorage) SaveSnapshot(snap Snapshot) error {
    // Use WAL to save snapshot
    record := WALRecord{
        Type: RecordSnapshot,
        Data: snap.Data,
    }
    return s.wal.Append(record)
}
```

**✅ Phase 3 Deliverable**: Unified storage using WAL from Phase 2 and existing B-tree. No changes to previous phases required.

---

## Phase 4: Raft Integration (Week 4)

### 🎯 Goal: Integrate with existing Raft using interfaces from Phase 1

#### Task 4.1: Implement Ready Handler

**File**: `node/ready_handler.go`

```go
package node

import (
    "harmonydb/storage" // Use interfaces from Phase 1
)

type readyHandler struct {
    storage storage.Storage  // Use Storage interface from Phase 1
    raft    RaftNode        // Use RaftNode interface from Phase 1
}

func (rh *readyHandler) processReady(ready Ready) error {
    // Use exact etcd ordering

    // 1. Save snapshot before entries
    if !isEmptySnapshot(ready.Snapshot) {
        if err := rh.storage.SaveSnapshot(ready.Snapshot); err != nil {
            return err
        }
    }

    // 2. Save to WAL
    if err := rh.storage.Save(ready.HardState, ready.Entries); err != nil {
        return err
    }

    // 3. Apply committed entries
    if len(ready.CommittedEntries) > 0 {
        if err := rh.storage.Apply(ready.CommittedEntries); err != nil {
            return err
        }
    }

    return nil
}
```

#### Task 4.2: Integrate with Existing Node

**File**: `node/node.go` (modify existing)

```go
// Add to existing Node struct
type Node struct {
    // Existing fields...

    // New fields using Phase 1 interfaces
    storage      storage.Storage
    readyHandler *readyHandler
    applyc       chan []storage.Entry
}

func (n *Node) run() {
    for {
        select {
        case ready := <-n.raft.Ready():
            // Use ready handler from Phase 4
            if err := n.readyHandler.processReady(ready); err != nil {
                n.logger.Error("process ready", zap.Error(err))
            }

        // Existing select cases...
        }
    }
}
```

**✅ Phase 4 Deliverable**: Full Raft integration using components from previous phases. No modifications to WAL or Storage layers needed.

---

## Phase 5: Advanced Features & Testing (Weeks 5-6)

### 🎯 Goal: Add optimizations and comprehensive testing

#### Task 5.1: Performance Optimizations

**File**: `wal/batched_wal.go`

```go
// Wrapper around Phase 2 WAL that adds batching
type batchedWAL struct {
    underlying storage.WAL  // Use WAL from Phase 2
    batchSize  int
    buffer     []storage.WALRecord
    flushTimer *time.Timer
}

// Implements same storage.WAL interface
func (bw *batchedWAL) Append(record storage.WALRecord) error {
    // Add batching logic around underlying WAL
}
```

#### Task 5.2: Failure Recovery

**File**: `storage/recovery.go`

```go
func (s *unifiedStorage) Recover() error {
    // Use WAL.ReadAll from Phase 2
    records, err := s.wal.ReadAll()
    if err != nil {
        return err
    }

    // Apply records using Storage.Apply from Phase 3
    var entries []Entry
    for _, record := range records {
        if record.Type == RecordEntry {
            entries = append(entries, recordToEntry(record))
        }
    }

    return s.Apply(entries)
}
```

**✅ Phase 5 Deliverable**: Production-ready system with all optimizations. Built entirely on stable foundations from previous phases.

---

## ✅ Dependency Validation

### Phase Dependencies (No Back-and-Forth):

1. **Phase 1** → Standalone interfaces (no dependencies)
2. **Phase 2** → Uses Phase 1 interfaces only (no implementation details)
3. **Phase 3** → Uses Phase 1 interfaces + Phase 2 WAL implementation
4. **Phase 4** → Uses Phase 1 interfaces + Phase 3 storage implementation
5. **Phase 5** → Wraps/extends previous phases (no core changes)

### Critical Success Factors:

✅ **No Interface Changes**: Phase 1 defines ALL interfaces permanently
✅ **Clean Dependencies**: Each phase only depends on interfaces, not implementations
✅ **Standalone Testing**: Each phase can be tested independently
✅ **Zero Refactoring**: No need to modify previous phases

### Implementation Validation:

```bash
# After Phase 1: Interfaces compile
go build ./storage/interfaces.go

# After Phase 2: WAL works standalone
go test ./wal/...

# After Phase 3: Storage integration works
go test ./storage/...

# After Phase 4: Full system works
go test ./node/...

# Phase 5: Optimizations and production readiness
go test ./...
```

This corrected plan ensures **linear progression** with **zero back-and-forth modifications**. Each phase builds cleanly on the previous one using stable interfaces defined upfront.