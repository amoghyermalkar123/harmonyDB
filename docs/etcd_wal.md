# etcd Write-Ahead Log (WAL) Explained

This document explains how etcd's Write-Ahead Log (WAL) works, including file structure, write operations, recovery, and integration with Raft.

## Table of Contents

1. [What is WAL?](#what-is-wal)
2. [File Structure](#file-structure)
3. [Write Flow](#write-flow)
4. [Read/Recovery Flow](#read-recovery-flow)
5. [Integration with Raft](#integration-with-raft)
6. [Code References](#code-references)

---

## What is WAL?

The Write-Ahead Log (WAL) is etcd's **durability layer** that ensures data is not lost even if the process crashes.

### Purpose

```
┌─────────────────────────────────────────────────────────────────────┐
│                     WHY WAL EXISTS                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│ Problem: What happens if etcd crashes during a write?              │
│                                                                     │
│ Without WAL:                                                        │
│   Client → Raft → [CRASH] → Data lost!                            │
│                                                                     │
│ With WAL:                                                           │
│   Client → Raft → WAL (fsync to disk) → [CRASH]                   │
│   On restart: Read WAL → Recover Raft state → Resume              │
│                                                                     │
│ Key Guarantee:                                                      │
│   Once Raft commits an entry, it survives crashes                  │
│   WAL ensures committed entries are persisted to disk              │
└─────────────────────────────────────────────────────────────────────┘
```

### WAL vs BoltDB

```
┌─────────────────────────────────────────────────────────────────────┐
│                       WAL vs BoltDB                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│ WAL (Write-Ahead Log)                                               │
│ ─────────────────────                                               │
│ Purpose:      Raft durability (log persistence)                    │
│ What:         Raft entries + Raft state (term, vote, commit)      │
│ Format:       Append-only sequential log files                     │
│ When:         BEFORE Raft commits (during consensus)               │
│ Location:     <data-dir>/member/wal/                               │
│ Performance:  Sequential writes (very fast)                         │
│ Recovery:     Used on startup to rebuild Raft state                │
│                                                                     │
│ BoltDB (State Machine Storage)                                      │
│ ────────────────────────────────                                    │
│ Purpose:      Application state (key-value store)                  │
│ What:         Applied KV pairs, leases, auth, etc.                 │
│ Format:       B+tree database file                                 │
│ When:         AFTER Raft commits and applies                       │
│ Location:     <data-dir>/member/snap/db                            │
│ Performance:  Random access (slower than WAL)                       │
│ Recovery:     Used for serving client reads                         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

Timeline of a Write:
══════════════════════

T0: Client sends Put("foo", "bar")
    
T1: Raft proposes entry
    
T2: WAL.Save(entry)  ← Raft log persisted to disk
    [Entry now durable in WAL]
    
T3: Raft replicates to followers
    Followers also write to their WALs
    
T4: Quorum acknowledges
    Raft commits entry
    
T5: Apply loop applies entry to BoltDB
    [KV pair now in state machine]

T6: Client receives response


Key Insight: WAL happens at T2 (before commit)
             BoltDB happens at T5 (after commit)
```

---

## File Structure

### Directory Layout

```
<data-dir>/
└── member/
    ├── wal/                          ← WAL directory
    │   ├── 0000000000000000-0000000000000000.wal   ← First segment
    │   ├── 0000000000000001-0000000000000021.wal   ← Second segment
    │   └── 0000000000000002-0000000000000031.wal   ← Current segment
    └── snap/
        └── db                        ← BoltDB file
```

### WAL File Naming

```
Format: {sequence:016x}-{index:016x}.wal

┌─────────────────────────────────────────────────────────────────────┐
│ Example: 0000000000000002-0000000000000031.wal                      │
│          ^^^^^^^^^^^^^^^^  ^^^^^^^^^^^^^^^^                         │
│          Sequence Number   First Raft Index                         │
│          (file number)     (in this file)                           │
└─────────────────────────────────────────────────────────────────────┘

Sequence:
  - Increments each time a new WAL file is created
  - Starts at 0 for the first file
  - Monotonically increasing

Index:
  - The first Raft log index that will be written to this file
  - Used during recovery to find the right file to start from

Example Timeline:
━━━━━━━━━━━━━━━━━

T0: Create first WAL file
    0000000000000000-0000000000000000.wal
    (seq=0, startIndex=0)

T1: Write entries 0-32 (assuming each ~2MB)
    File size reaches 64MB

T2: Cut (create new segment)
    0000000000000001-0000000000000021.wal
    (seq=1, startIndex=33 in hex = 0x21)

T3: Write entries 33-80
    File size reaches 64MB

T4: Cut again
    0000000000000002-0000000000000051.wal
    (seq=2, startIndex=81 in hex = 0x51)
```

### Record Format

Each WAL file contains a sequence of records:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         WAL RECORD FORMAT                           │
└─────────────────────────────────────────────────────────────────────┘

Physical Layout:
━━━━━━━━━━━━━━━━

┌──────────────┬─────────────────────────────────────┬─────────┐
│ Length Field │     Record Protobuf                 │ Padding │
│  (8 bytes)   │     (variable)                      │ (0-7)   │
└──────────────┴─────────────────────────────────────┴─────────┘
      │                      │
      │                      └─> Contains: Type, CRC, Data
      │
      └─> Lower 56 bits: record size
          Upper 8 bits: padding info

Record Protobuf (walpb.Record):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

message Record {
  int64  type = 1;   // MetadataType, EntryType, StateType, etc.
  uint32 crc  = 2;   // CRC32 of all previous records
  bytes  data = 3;   // Payload (varies by type)
}


Record Types:
━━━━━━━━━━━━━

1. MetadataType (type=1)
   ┌──────────────────────────────────────────────────────────┐
   │ Data: Arbitrary metadata bytes                          │
   │ Purpose: Application-specific metadata (cluster info)   │
   │ When: Written at WAL creation                           │
   └──────────────────────────────────────────────────────────┘

2. EntryType (type=2)
   ┌──────────────────────────────────────────────────────────┐
   │ Data: raftpb.Entry (marshaled)                          │
   │ Purpose: Raft log entries                               │
   │ When: Every Save() call with entries                    │
   │                                                          │
   │ Entry contains:                                          │
   │   - Index: Raft log index                               │
   │   - Term: Raft term                                     │
   │   - Type: EntryNormal, EntryConfChange                  │
   │   - Data: Application payload (Put, Delete, etc.)       │
   └──────────────────────────────────────────────────────────┘

3. StateType (type=3)
   ┌──────────────────────────────────────────────────────────┐
   │ Data: raftpb.HardState (marshaled)                      │
   │ Purpose: Raft's persistent state                        │
   │ When: Every Save() call                                 │
   │                                                          │
   │ HardState contains:                                      │
   │   - Term: Current term                                  │
   │   - Vote: Who we voted for                              │
   │   - Commit: Committed index                             │
   └──────────────────────────────────────────────────────────┘

4. CrcType (type=4)
   ┌──────────────────────────────────────────────────────────┐
   │ Data: (empty)                                            │
   │ CRC: Cumulative CRC32 of all previous records           │
   │ Purpose: Detect corruption                               │
   │ When: Periodically during writes                         │
   └──────────────────────────────────────────────────────────┘

5. SnapshotType (type=5)
   ┌──────────────────────────────────────────────────────────┐
   │ Data: walpb.Snapshot (Index, Term)                      │
   │ Purpose: Record that snapshot was taken at this point   │
   │ When: After creating a Raft snapshot                    │
   └──────────────────────────────────────────────────────────┘


Example WAL File Contents:
━━━━━━━━━━━━━━━━━━━━━━━━━━━

File: 0000000000000000-0000000000000000.wal
┌─────────────────────────────────────────────────────────────┐
│ [MetadataType]                                              │
│   data: cluster_id=abc123, node_id=1                        │
│                                                              │
│ [CrcType]                                                    │
│   crc: 0x12345678                                           │
│                                                              │
│ [EntryType]                                                  │
│   data: Entry{Index: 1, Term: 1, Data: Put("a", "1")}      │
│                                                              │
│ [StateType]                                                  │
│   data: HardState{Term: 1, Vote: 1, Commit: 0}             │
│                                                              │
│ [EntryType]                                                  │
│   data: Entry{Index: 2, Term: 1, Data: Put("b", "2")}      │
│                                                              │
│ [StateType]                                                  │
│   data: HardState{Term: 1, Vote: 1, Commit: 1}             │
│                                                              │
│ [CrcType]                                                    │
│   crc: 0x87654321                                           │
│                                                              │
│ ... more entries ...                                         │
└─────────────────────────────────────────────────────────────┘
```

### Alignment and CRC

```
┌─────────────────────────────────────────────────────────────────────┐
│                      ALIGNMENT & CORRUPTION DETECTION               │
└─────────────────────────────────────────────────────────────────────┘

8-byte Alignment:
━━━━━━━━━━━━━━━━━

Every record is padded to 8-byte boundary

Example:
  Record size: 13 bytes
  Padding needed: 8 - (13 % 8) = 3 bytes
  Total on disk: 16 bytes (8 + 13 + 3)

Why: Prevents torn writes
  - Disk sectors are typically 512 bytes or 4KB
  - 8-byte alignment ensures length field is atomic
  - If crash during write, we detect incomplete record


CRC (Cyclic Redundancy Check):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Cumulative CRC across all records:

┌──────────────────────────────────────────────────────────────┐
│ Record 1: [Entry]                                            │
│   crc = CRC32(record1_data)                                  │
│                                                              │
│ Record 2: [State]                                            │
│   crc = CRC32(previous_crc || record2_data)                 │
│                                                              │
│ Record 3: [CRC checkpoint]                                   │
│   crc = cumulative_crc_so_far                               │
│                                                              │
│ Record 4: [Entry]                                            │
│   crc = CRC32(crc_from_record3 || record4_data)             │
└──────────────────────────────────────────────────────────────┘

During recovery:
  - Recalculate CRC while reading
  - Compare with stored CRC
  - If mismatch: corruption detected
  - Return ErrCRCMismatch
```

---

## Write Flow

### High-Level Write Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    WAL WRITE FLOW                                   │
└─────────────────────────────────────────────────────────────────────┘

Raft Thread                    WAL                      Disk
     │                          │                         │
     │ 1. raft.Ready()          │                         │
     │    ents: [e1, e2, e3]    │                         │
     │    state: HardState{...} │                         │
     │                          │                         │
     │ 2. wal.Save(state, ents) │                         │
     │─────────────────────────>│                         │
     │                          │                         │
     │                          │ 3. Encode entries       │
     │                          │    [EntryType] e1       │
     │                          │    [EntryType] e2       │
     │                          │    [EntryType] e3       │
     │                          │                         │
     │                          │ 4. Encode state         │
     │                          │    [StateType] HardState│
     │                          │                         │
     │                          │ 5. Check if must sync   │
     │                          │    (state changed?)     │
     │                          │                         │
     │                          │ 6. fsync()              │
     │                          │────────────────────────>│
     │                          │                         │ Write to disk
     │                          │                         │ Flush buffers
     │                          │<────────────────────────│
     │                          │ 7. fsync complete       │
     │                          │                         │
     │ <─────────────────────────│                         │
     │ 8. Save() returns        │                         │
     │                          │                         │
     ▼                          ▼                         ▼
```

### Detailed Save() Flow

```
Location: server/storage/wal/wal.go:956

func (w *WAL) Save(st raftpb.HardState, ents []raftpb.Entry) error {
  w.mu.Lock()
  defer w.mu.Unlock()

  // Fast path: nothing to save
  if raft.IsEmptyHardState(st) && len(ents) == 0 {
    return nil
  }

  // Determine if we MUST fsync
  mustSync := raft.MustSync(st, w.state, len(ents))
  // Returns true if:
  //   - HardState changed (term, vote, or commit)
  //   - OR we have entries to save

  // STEP 1: Write all entries
  for i := range ents {
    if err := w.saveEntry(&ents[i]); err != nil {
      return err
    }
  }

  // STEP 2: Write hard state
  if err := w.saveState(&st); err != nil {
    return err
  }

  // STEP 3: Check file size
  curOff, err := w.tail().Seek(0, io.SeekCurrent)
  if err != nil {
    return err
  }

  // STEP 4: Sync or cut
  if curOff < SegmentSizeBytes {
    // File not full yet
    if mustSync {
      return w.sync()  // fsync to disk
    }
    return nil  // No sync needed (batching)
  }

  // File full - create new segment
  return w.cut()
}


STEP 1: saveEntry()
━━━━━━━━━━━━━━━━━━━

func (w *WAL) saveEntry(e *raftpb.Entry) error {
  // Marshal the entry
  b := pbutil.MustMarshal(e)
  
  // Create record
  rec := &walpb.Record{
    Type: EntryType,
    Data: b,
  }
  
  // Encode to file
  if err := w.encoder.encode(rec); err != nil {
    return err
  }
  
  // Update last entry index
  w.enti = e.Index
  return nil
}


STEP 2: saveState()
━━━━━━━━━━━━━━━━━━━

func (w *WAL) saveState(s *raftpb.HardState) error {
  if raft.IsEmptyHardState(*s) {
    return nil
  }
  
  // Update WAL's copy of state
  w.state = *s
  
  // Marshal the hard state
  b := pbutil.MustMarshal(s)
  
  // Create record
  rec := &walpb.Record{
    Type: StateType,
    Data: b,
  }
  
  // Encode to file
  return w.encoder.encode(rec)
}


STEP 3: sync()
━━━━━━━━━━━━━━

func (w *WAL) sync() error {
  if w.encoder != nil {
    if err := w.encoder.flush(); err != nil {
      return err
    }
  }
  
  start := time.Now()
  
  // fsync the current WAL file
  err := w.tail().Sync()
  
  duration := time.Since(start)
  
  // Warn if sync is slow
  if duration > warnSyncDuration {
    w.lg.Warn(
      "slow fdatasync",
      zap.Duration("took", duration),
      zap.Duration("expected-duration", warnSyncDuration),
    )
  }
  
  walFsyncLatencyHistogram.Observe(duration.Seconds())
  
  return err
}


STEP 4: cut() - Create New Segment
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (w *WAL) cut() error {
  // Sync current file before cutting
  if err := w.sync(); err != nil {
    return err
  }
  
  // Close encoder
  off, err := w.tail().Seek(0, io.SeekCurrent)
  if err != nil {
    return err
  }
  
  // Create new WAL file
  // Name: {seq+1}-{enti+1}.wal
  newSeq := w.seq() + 1
  newIndex := w.enti + 1
  
  fpath := filepath.Join(w.dir, walName(newSeq, newIndex))
  
  // Use file pipeline (pre-allocated file)
  f, err := w.fp.Open()
  if err != nil {
    return err
  }
  
  // Lock the new file
  w.locks = append(w.locks, f)
  
  // Create new encoder
  w.encoder = newEncoder(f, ...)
  
  // Write CRC checkpoint
  if err := w.saveCrc(prevCrc); err != nil {
    return err
  }
  
  // Write metadata to new file
  if err := w.encoder.encode(&walpb.Record{
    Type: MetadataType,
    Data: w.metadata,
  }); err != nil {
    return err
  }
  
  return w.sync()
}
```

### When Does fsync Happen?

```
┌─────────────────────────────────────────────────────────────────────┐
│                      FSYNC DECISION LOGIC                           │
└─────────────────────────────────────────────────────────────────────┘

func MustSync(st, prevst HardState, entsnum int) bool {
  // Must sync if hard state changed
  return entsnum != 0 || 
         st.Vote != prevst.Vote ||
         st.Term != prevst.Term
}

Scenarios:
━━━━━━━━━━

1. New entries + state unchanged
   → mustSync = true (because entsnum > 0)
   → fsync()

2. No entries + state unchanged
   → mustSync = false
   → return (no-op, fast path)

3. State changed (vote or term changed)
   → mustSync = true
   → fsync()

4. Commit index changed (but vote/term same)
   → mustSync = false
   → Can batch, no immediate fsync
   
   NOTE: Commit is in HardState but doesn't trigger fsync
         because it's just updating local state
         The actual entries were already fsync'd earlier


Why This Matters:
━━━━━━━━━━━━━━━━━

Raft safety requires:
  ✅ New entries must be durable before acknowledging
  ✅ Vote changes must be durable (can't vote twice)
  ✅ Term changes must be durable (can't go backwards)
  
  ❌ Commit index changes don't need immediate sync
     (commit is derived from replicated entries)
```

---

## Read/Recovery Flow

### Recovery on Startup

```
┌─────────────────────────────────────────────────────────────────────┐
│                   WAL RECOVERY FLOW                                 │
└─────────────────────────────────────────────────────────────────────┘

Startup                     WAL                        Raft
   │                         │                          │
   │ 1. wal.Open(snap)       │                          │
   │────────────────────────>│                          │
   │                         │                          │
   │                         │ 2. Find WAL files        │
   │                         │    >= snap.Index         │
   │                         │                          │
   │                         │ 3. Open files            │
   │                         │    Acquire locks         │
   │                         │                          │
   │ <────────────────────────│ 4. Return WAL handle    │
   │                         │                          │
   │ 5. wal.ReadAll()        │                          │
   │────────────────────────>│                          │
   │                         │                          │
   │                         │ 6. Read all records      │
   │                         │    - Metadata            │
   │                         │    - Entries             │
   │                         │    - HardState           │
   │                         │    - Validate CRCs       │
   │                         │                          │
   │ <────────────────────────│ 7. Return data          │
   │ metadata, state, ents   │                          │
   │                         │                          │
   │ 8. raft.Restore(state, ents)                       │
   │───────────────────────────────────────────────────>│
   │                         │                          │
   │                         │                     9. Rebuild
   │                         │                        Raft log
   │                         │                          │
   │ <───────────────────────────────────────────────────│
   │ 10. Resume normal ops   │                          │
   ▼                         ▼                          ▼
```

### Detailed Open() Flow

```
Location: server/storage/wal/wal.go:344

func Open(lg *zap.Logger, dirpath string, snap walpb.Snapshot) (*WAL, error) {
  // Call openAtIndex with write=true
  w, err := openAtIndex(lg, dirpath, snap, true)
  if err != nil {
    return nil, err
  }
  
  // Open directory for fsync
  w.dirFile, err = fileutil.OpenDir(w.dir)
  if err != nil {
    return nil, err
  }
  
  return w, nil
}


func openAtIndex(lg, dirpath, snap, write) (*WAL, error) {
  // STEP 1: Find WAL files that contain snap.Index or later
  names, nameIndex, err := selectWALFiles(lg, dirpath, snap)
  //
  // Given: snap.Index = 50
  // Files in directory:
  //   0000000000000000-0000000000000000.wal  (entries 0-32)
  //   0000000000000001-0000000000000021.wal  (entries 33-80)
  //   0000000000000002-0000000000000051.wal  (entries 81-...)
  //
  // Returns:
  //   names = [file1, file2]  (second and third files)
  //   nameIndex = 1  (start from second file)

  // STEP 2: Open files for reading
  rs, ls, closer, err := openWALFiles(lg, dirpath, names, nameIndex, write)
  // rs = file readers
  // ls = locked files (if write=true)
  // closer = function to close all readers

  // STEP 3: Create WAL handle
  w := &WAL{
    lg:        lg,
    dir:       dirpath,
    start:     snap,                    // Remember where we started
    decoder:   NewDecoder(rs...),       // Chain of file readers
    readClose: closer,
    locks:     ls,                      // Locked files (for write mode)
  }

  if write {
    // In write mode, keep files locked and ready for appending
    w.readClose = nil
    w.fp = newFilePipeline(lg, w.dir, SegmentSizeBytes)
  }

  return w, nil
}
```

### Detailed ReadAll() Flow

```
Location: server/storage/wal/wal.go:469

func (w *WAL) ReadAll() (
  metadata []byte,
  state raftpb.HardState,
  ents []raftpb.Entry,
  err error,
) {
  w.mu.Lock()
  defer w.mu.Unlock()

  if w.decoder == nil {
    return nil, state, nil, ErrDecoderNotFound
  }

  var match bool  // Did we find the starting snapshot?
  rec := &walpb.Record{}

  // Read records one by one
  for err = decoder.Decode(rec); err == nil; err = decoder.Decode(rec) {
    switch rec.Type {
    
    case EntryType:
      // Unmarshal Raft entry
      e := MustUnmarshalEntry(rec.Data)
      
      // Only include entries AFTER the snapshot
      if e.Index > w.start.Index {
        // Append to entries slice
        offset := e.Index - w.start.Index - 1
        ents = append(ents[:offset], e)
      }
      
      w.enti = e.Index

    case StateType:
      // Unmarshal Raft hard state
      state = MustUnmarshalState(rec.Data)

    case MetadataType:
      // Check metadata consistency
      if metadata != nil && !bytes.Equal(metadata, rec.Data) {
        return nil, state, nil, ErrMetadataConflict
      }
      metadata = rec.Data

    case CrcType:
      // Validate CRC
      crc := decoder.LastCRC()
      if crc != 0 && rec.Validate(crc) != nil {
        return nil, state, nil, ErrCRCMismatch
      }
      decoder.UpdateCRC(rec.Crc)

    case SnapshotType:
      // Unmarshal snapshot marker
      var snap walpb.Snapshot
      pbutil.MustUnmarshal(&snap, rec.Data)
      
      // Found our starting snapshot
      if snap.Index == w.start.Index {
        if snap.Term != w.start.Term {
          return nil, state, nil, ErrSnapshotMismatch
        }
        match = true
      }

    default:
      return nil, state, nil, fmt.Errorf("unexpected type %d", rec.Type)
    }
  }

  // Check that we read everything successfully
  if err != io.EOF && err != io.ErrUnexpectedEOF {
    return nil, state, nil, err
  }

  // If we specified a snapshot, we must find it
  if !match {
    return nil, state, nil, ErrSnapshotNotFound
  }

  return metadata, state, ents, nil
}


Example Recovery Scenario:
━━━━━━━━━━━━━━━━━━━━━━━━━━

Snapshot: Index=50, Term=3

WAL contains:
  [Snapshot] Index=50, Term=3         ← Match! Start here
  [Entry] Index=51, Term=3, Data=...  → Include
  [Entry] Index=52, Term=3, Data=...  → Include
  [State] Term=3, Vote=1, Commit=52   → Latest state
  [Entry] Index=53, Term=3, Data=...  → Include
  [Entry] Index=54, Term=4, Data=...  → Include (new term!)
  [State] Term=4, Vote=1, Commit=54   → Latest state (updated)

Returns:
  metadata: [cluster info]
  state: HardState{Term: 4, Vote: 1, Commit: 54}
  ents: [Entry{51}, Entry{52}, Entry{53}, Entry{54}]

Raft then:
  1. Sets term = 4, vote = 1, commit = 54
  2. Appends entries 51-54 to its log
  3. Applies entries 51-54 to state machine (if not already applied)
```

### File Selection Logic

```
┌─────────────────────────────────────────────────────────────────────┐
│                   WAL FILE SELECTION                                │
└─────────────────────────────────────────────────────────────────────┘

Problem: Given snapshot index, which WAL files should we read?

Directory:
  0000000000000000-0000000000000000.wal  → starts at index 0
  0000000000000001-0000000000000021.wal  → starts at index 33
  0000000000000002-0000000000000051.wal  → starts at index 81

Scenario 1: snap.Index = 0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Search: Binary search for file with Index <= 0
  Found: File 0 (0000000000000000-0000000000000000.wal)
  Read: All 3 files (start from beginning)

Scenario 2: snap.Index = 50
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Search: Binary search for file with Index <= 50
  Found: File 1 (0000000000000001-0000000000000021.wal)
         (starts at 33, which is <= 50)
  Read: Files 1 and 2 (skip file 0, it's before snapshot)

Scenario 3: snap.Index = 100
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Search: Binary search for file with Index <= 100
  Found: File 2 (0000000000000002-0000000000000051.wal)
         (starts at 81, which is <= 100)
  Read: Only file 2

Why This Works:
━━━━━━━━━━━━━━━
  - File names encode the first index in the file
  - Binary search finds the file containing snap.Index
  - Read from that file onwards
  - Skip older files (before snapshot)
```

---

## Integration with Raft

### Where WAL Fits in Raft Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                RAFT + WAL INTEGRATION                               │
└─────────────────────────────────────────────────────────────────────┘

Raft Storage Interface:
━━━━━━━━━━━━━━━━━━━━━━━━

type Storage interface {
  InitialState() (HardState, ConfState, error)
  Entries(lo, hi, maxSize) ([]Entry, error)
  Term(i uint64) (uint64, error)
  LastIndex() (uint64, error)
  FirstIndex() (uint64, error)
  Snapshot() (Snapshot, error)
}

etcd implements this with:
  - MemoryStorage (in-memory Raft log)
  - WAL (persistent Raft log)


Integration Points:
━━━━━━━━━━━━━━━━━━━

1. Startup (raftNode.go)
   ┌──────────────────────────────────────────────────────────┐
   │ func (r *raftNode) start() {                            │
   │   // Open WAL                                            │
   │   w, err := wal.Open(lg, waldir, snapshot)              │
   │                                                          │
   │   // Read WAL                                            │
   │   metadata, state, ents, err := w.ReadAll()             │
   │                                                          │
   │   // Restore Raft from WAL                              │
   │   r.storage.ApplySnapshot(snapshot)                     │
   │   r.storage.SetHardState(state)                         │
   │   r.storage.Append(ents)                                │
   │                                                          │
   │   // Start Raft with recovered state                    │
   │   r.node = raft.StartNode(config, peers)                │
   │ }                                                        │
   └──────────────────────────────────────────────────────────┘

2. Normal Operation (raftNode.go)
   ┌──────────────────────────────────────────────────────────┐
   │ for {                                                    │
   │   select {                                               │
   │   case rd := <-r.node.Ready():                           │
   │     // 1. Save to WAL FIRST (durability)                │
   │     if err := r.wal.Save(rd.HardState, rd.Entries); err │
   │                                                          │
   │     // 2. Send messages to peers                        │
   │     r.transport.Send(rd.Messages)                        │
   │                                                          │
   │     // 3. Apply committed entries                       │
   │     r.publishEntries(rd.CommittedEntries)                │
   │                                                          │
   │     // 4. Tell Raft we're done                          │
   │     r.node.Advance()                                     │
   │   }                                                      │
   │ }                                                        │
   └──────────────────────────────────────────────────────────┘

3. Snapshot Coordination (raftNode.go)
   ┌──────────────────────────────────────────────────────────┐
   │ func (r *raftNode) saveSnap(snap raftpb.Snapshot) {    │
   │   // 1. Save Raft snapshot to disk                      │
   │   if err := r.snapshotter.SaveSnap(snap); err != nil    │
   │                                                          │
   │   // 2. Tell WAL about snapshot                         │
   │   walSnap := walpb.Snapshot{                             │
   │     Index: snap.Metadata.Index,                          │
   │     Term:  snap.Metadata.Term,                           │
   │   }                                                      │
   │   if err := r.wal.SaveSnapshot(walSnap); err != nil     │
   │                                                          │
   │   // 3. Release old WAL files                           │
   │   if err := r.wal.ReleaseLockTo(snap.Metadata.Index)    │
   │ }                                                        │
   └──────────────────────────────────────────────────────────┘
```

### WAL Write Timeline

```
Client Request → Raft Flow
═══════════════════════════════════════════════════════════════════════

T0: Client: Put("foo", "bar")
    
T1: Leader receives proposal
    raft.Propose(data)
    
T2: Raft creates entry
    Entry{Index: 100, Term: 5, Data: Put("foo", "bar")}
    
T3: Raft returns Ready()
    rd.Entries = [Entry{100}]
    rd.HardState = {Term: 5, Vote: 1, Commit: 99}
    
T4: wal.Save(rd.HardState, rd.Entries)  ← WAL WRITE
    ┌──────────────────────────────────────────┐
    │ Write EntryType record                   │
    │ Write StateType record                   │
    │ fsync() to disk                          │
    └──────────────────────────────────────────┘
    Entry 100 is now DURABLE on leader
    
T5: Send to followers
    transport.Send(rd.Messages)
    
T6: Followers receive entry
    Each follower:
      - wal.Save(HardState{}, [Entry{100}])  ← Follower WAL write
      - fsync() to disk
      - Send ACK to leader
    
T7: Leader receives quorum ACKs
    Raft commits entry 100
    rd.CommittedEntries = [Entry{100}]
    
T8: wal.Save(HardState{Commit: 100}, [])
    Update commit index in WAL
    
T9: Apply entry to BoltDB
    kv.Put("foo", "bar")
    
T10: Client receives response


Key Points:
━━━━━━━━━━━

1. WAL write happens at T4 (BEFORE sending to followers)
2. Each follower also writes to WAL (T6)
3. WAL write is synchronous (fsync)
4. Only AFTER WAL write do we send to peers
5. This ensures we never lose accepted entries
```

---

## Code References

### Core WAL Files

| File | Description |
|------|-------------|
| `server/storage/wal/wal.go` | Main WAL implementation |
| `server/storage/wal/encoder.go` | Record encoding |
| `server/storage/wal/decoder.go` | Record decoding |
| `server/storage/wal/file_pipeline.go` | Pre-allocation of WAL files |
| `server/storage/wal/doc.go` | Package documentation |

### Key Functions

| Function | File | Line | Description |
|----------|------|------|-------------|
| Create | wal.go | ~110 | Create new WAL |
| Open | wal.go | 344 | Open existing WAL for read/write |
| OpenForRead | wal.go | 357 | Open for read-only |
| Save | wal.go | 956 | Save entries and state |
| SaveSnapshot | wal.go | 994 | Record snapshot marker |
| ReadAll | wal.go | 469 | Read all records |
| sync | wal.go | ~1040 | fsync to disk |
| cut | wal.go | ~1070 | Create new segment |

### Constants

```go
// Segment size: 64MB
SegmentSizeBytes int64 = 64 * 1000 * 1000

// Warn if fsync takes longer than 1 second
warnSyncDuration = time.Second

// Record types
MetadataType  int64 = 1
EntryType     int64 = 2
StateType     int64 = 3
CrcType       int64 = 4
SnapshotType  int64 = 5
```

---

## Summary

### What is WAL?

```
┌─────────────────────────────────────────────────────────────────────┐
│ WAL is etcd's durability mechanism for Raft                        │
│                                                                     │
│ Purpose:                                                            │
│   - Persist Raft log entries before committing                     │
│   - Persist Raft state (term, vote, commit)                        │
│   - Enable recovery after crash                                    │
│                                                                     │
│ Format:                                                             │
│   - Append-only log files (segments)                               │
│   - 64MB per segment                                                │
│   - Each record: [Length][Type|CRC|Data][Padding]                  │
│                                                                     │
│ Guarantees:                                                         │
│   - fsync() before acknowledging writes                            │
│   - CRC validation on read                                         │
│   - Atomic segment creation                                        │
│                                                                     │
│ Performance:                                                        │
│   - Sequential writes (fast)                                       │
│   - Batching when possible                                         │
│   - Pre-allocated files (file pipeline)                            │
└─────────────────────────────────────────────────────────────────────┘
```

### Critical Design Decisions

1. **Append-only**: Never modify existing records, only append
2. **fsync on state changes**: Durability for critical Raft state
3. **Segmented files**: Easier to manage than single huge file
4. **CRC checking**: Detect corruption early
5. **File pipeline**: Pre-allocate next file to avoid allocation delays
6. **8-byte alignment**: Prevent torn writes

### Common Patterns

```
Write Pattern:
  Raft Ready → Save to WAL → fsync → Send to peers

Recovery Pattern:
  Open WAL → ReadAll → Restore Raft → Resume

Snapshot Pattern:
  Take snapshot → SaveSnapshot to WAL → Release old WAL files
```
