# HarmonyDB Architecture Visualizations

## 1. System Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         HarmonyDB Node                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐    ┌──────────────┐ │
│  │   Client API    │    │   Raft Layer    │    │  Storage     │ │
│  │                 │    │                 │    │  Interface   │ │
│  │ • Put/Get/Del   │◄──►│ • Leader Elect  │◄──►│ • Save/Apply │ │
│  │ • Query         │    │ • Log Replicate │    │ • Snapshots  │ │
│  │ • Transactions  │    │ • Consensus     │    │ • Recovery   │ │
│  └─────────────────┘    └─────────────────┘    └──────────────┘ │
│           │                       │                      │      │
│           │                       │                      ▼      │
│           │               ┌───────▼──────────┐   ┌──────────────┐ │
│           │               │  Ready Handler   │   │     WAL      │ │
│           │               │                  │   │              │ │
│           └──────────────►│ • Process Ready  │◄──┤ • File Ops   │ │
│                           │ • Ordered Apply  │   │ • CRC Check  │ │
│                           │ • State Machine  │   │ • Recovery   │ │
│                           └──────────────────┘   └──────┬───────┘ │
│                                     │                   │        │
│                                     ▼                   │        │
│                           ┌──────────────────┐          │        │
│                           │    B+ Tree       │          │        │
│                           │                  │◄─────────┘        │
│                           │ • 4KB Pages      │                   │
│                           │ • In-Memory      │                   │
│                           │ • Key-Value      │                   │
│                           └──────────────────┘                   │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      Multi-Node Cluster                        │
├─────────────────────────────────────────────────────────────────┤
│  Node 1 (Leader)     Node 2 (Follower)     Node 3 (Follower)  │
│  ┌─────────────┐     ┌─────────────┐       ┌─────────────┐     │
│  │   Client    │     │             │       │             │     │
│  │     ▲       │     │             │       │             │     │
│  │     │       │     │             │       │             │     │
│  │  ┌──▼──┐    │     │  ┌──────┐   │       │  ┌──────┐   │     │
│  │  │Raft │────┼─────┼──►│Raft  │   │       │  │Raft  │   │     │
│  │  │     │◄───┼─────┼──┤│      │   │       │  │      │   │     │
│  │  └─────┘    │     │  └──────┘   │       │  └──────┘   │     │
│  │     │       │     │     │       │       │     │       │     │
│  │  ┌──▼──┐    │     │  ┌──▼──┐    │       │  ┌──▼──┐    │     │
│  │  │ WAL │    │     │  │ WAL │    │       │  │ WAL │    │     │
│  │  └─────┘    │     │  └─────┘    │       │  └─────┘    │     │
│  │     │       │     │     │       │       │     │       │     │
│  │  ┌──▼──┐    │     │  ┌──▼──┐    │       │  ┌──▼──┐    │     │
│  │  │B+Tr │    │     │  │B+Tr │    │       │  │B+Tr │    │     │
│  │  └─────┘    │     │  └─────┘    │       │  └─────┘    │     │
│  └─────────────┘     └─────────────┘       └─────────────┘     │
│         │                   │                     │           │
│         └───────── Raft RPCs (gRPC) ──────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

## 2. Phase-by-Phase Implementation Flow

```
Phase 1: Core Interfaces (Week 1)
┌────────────────────────────────────────────────────────────────┐
│ Goal: Define ALL interfaces upfront - no changes later         │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│ storage/interfaces.go                node/interfaces.go        │
│ ┌─────────────────┐                 ┌─────────────────┐       │
│ │ • Storage       │                 │ • RaftNode      │       │
│ │ • WAL           │                 │ • Application   │       │
│ │ • HardState     │                 │ • Ready         │       │
│ │ • Entry         │                 │ • Message       │       │
│ │ • Snapshot      │                 │ • Status        │       │
│ │ • WALRecord     │                 └─────────────────┘       │
│ │ • Error Types   │                                           │
│ └─────────────────┘                                           │
│                                                                │
│ testing/interfaces.go                                          │
│ ┌─────────────────┐                                           │
│ │ • TestCluster   │                                           │
│ │ • TestNode      │                                           │
│ └─────────────────┘                                           │
│                                                                │
│ ✅ Deliverable: Complete interface definitions only            │
└────────────────────────────────────────────────────────────────┘
                                │
                                ▼
Phase 2: WAL Foundation (Week 2)
┌────────────────────────────────────────────────────────────────┐
│ Goal: Implement WAL as standalone component                    │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│ wal/wal.go         wal/encoder.go       wal/decoder.go        │
│ ┌─────────────┐   ┌─────────────┐      ┌─────────────┐       │
│ │ fileWAL     │   │ encoder     │      │ decoder     │       │
│ │             │   │             │      │             │       │
│ │ • Append    │◄──┤ • encode    │      │ • decode    │──────►│
│ │ • ReadAll   │   │ • CRC calc  │      │ • CRC check │       │
│ │ • Sync      │   └─────────────┘      └─────────────┘       │
│ │ • Close     │                                              │
│ └─────────────┘                                              │
│                                                                │
│ wal/wal_test.go                                               │
│ ┌─────────────────┐                                          │
│ │ • Basic Ops     │                                          │
│ │ • CRC Validation│                                          │
│ │ • File Recovery │                                          │
│ └─────────────────┘                                          │
│                                                                │
│ ✅ Deliverable: Working WAL, fully tested, standalone         │
└────────────────────────────────────────────────────────────────┘
                                │
                                ▼
Phase 3: Storage Integration (Week 3)
┌────────────────────────────────────────────────────────────────┐
│ Goal: Connect WAL + B-tree using Phase 1 interfaces           │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│ storage/unified.go                                             │
│ ┌─────────────────────────────────────────────────────────┐   │
│ │                 unifiedStorage                          │   │
│ │                                                         │   │
│ │ ┌─────────────┐                   ┌─────────────────┐   │   │
│ │ │    WAL      │◄─── Save() ──────►│    B+ Tree      │   │   │
│ │ │ (from P2)   │                   │   (existing)    │   │   │
│ │ │             │◄─── Apply() ──────┤                 │   │   │
│ │ │ • Append    │                   │ • Put/Get       │   │   │
│ │ │ • ReadAll   │                   │ • Delete        │   │   │
│ │ │ • Sync      │                   │ • Iterate       │   │   │
│ │ └─────────────┘                   └─────────────────┘   │   │
│ └─────────────────────────────────────────────────────────┘   │
│                                                                │
│ storage/snapshot.go                                            │
│ ┌─────────────────┐                                           │
│ │ • CreateSnapshot│                                           │
│ │ • SaveSnapshot  │                                           │
│ │ • RestoreSnap   │                                           │
│ └─────────────────┘                                           │
│                                                                │
│ ✅ Deliverable: WAL+B-tree working together                   │
└────────────────────────────────────────────────────────────────┘
                                │
                                ▼
Phase 4: Raft Integration (Week 4)
┌────────────────────────────────────────────────────────────────┐
│ Goal: Integrate with existing Raft using Ready pattern        │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│ node/ready_handler.go                                          │
│ ┌─────────────────────────────────────────────────────────┐   │
│ │                Ready Processing                         │   │
│ │                                                         │   │
│ │ Raft Ready ──► 1. Save Snapshot (if any)               │   │
│ │    Channel     2. Save to WAL (HardState + Entries)    │   │
│ │                3. Apply Committed Entries              │   │
│ │                4. Send Messages                        │   │
│ │                                                         │   │
│ │ ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │   │
│ │ │    Raft     │  │   Storage   │  │    Network      │ │   │
│ │ │ (existing)  │  │  (from P3)  │  │   (existing)    │ │   │
│ │ └─────────────┘  └─────────────┘  └─────────────────┘ │   │
│ └─────────────────────────────────────────────────────────┘   │
│                                                                │
│ node/node.go (modify existing)                                │
│ ┌─────────────────┐                                           │
│ │ • Add storage   │                                           │
│ │ • Add ready_h   │                                           │
│ │ • Event loop    │                                           │
│ └─────────────────┘                                           │
│                                                                │
│ ✅ Deliverable: Full Raft-WAL-B+Tree integration             │
└────────────────────────────────────────────────────────────────┘
                                │
                                ▼
Phase 5: Advanced Features (Weeks 5-6)
┌────────────────────────────────────────────────────────────────┐
│ Goal: Production optimizations and comprehensive testing       │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│ wal/batched_wal.go    storage/recovery.go    Comprehensive    │
│ ┌─────────────────┐  ┌─────────────────┐   Testing           │
│ │ • Batch writes  │  │ • WAL replay    │   ┌───────────────┐ │
│ │ • Timer flush   │  │ • Consistency   │   │ • Fault tests │ │
│ │ • Performance   │  │ • Corruption    │   │ • Perf tests  │ │
│ └─────────────────┘  └─────────────────┘   │ • Integration │ │
│                                            │ • End-to-end  │ │
│                                            └───────────────┘ │
│                                                                │
│ ✅ Deliverable: Production-ready system                       │
└────────────────────────────────────────────────────────────────┘
```

## 3. Data Flow: Write Operation

```
Client Write Request Flow
─────────────────────────

1. Client Request
   ┌─────────┐     PUT(key, value)     ┌─────────────┐
   │ Client  │────────────────────────►│ Node (API)  │
   └─────────┘                         └─────────────┘
                                              │
                                              ▼
2. Raft Propose
   ┌─────────────┐     Propose(data)   ┌─────────────┐
   │ Node (API)  │────────────────────►│ Raft Leader │
   └─────────────┘                     └─────────────┘
                                              │
                                              ▼
3. Log Replication
   ┌─────────────┐                     ┌─────────────┐
   │   Follower  │◄────AppendEntries───┤ Raft Leader │
   │    Node     │                     └─────────────┘
   └─────────────┘                            │
         │                                    ▼
         ▼                             ┌─────────────┐
   ┌─────────────┐                     │   Follower  │
   │ Majority    │◄────AppendEntries───┤    Node     │
   │ Consensus   │                     └─────────────┘
   └─────────────┘
         │
         ▼
4. Ready Processing (Leader)
   ┌─────────────────────────────────────────────────────────────┐
   │                    Ready Channel                            │
   │                                                             │
   │ Ready {                                                     │
   │   HardState: term=5, vote=1, commit=10                     │
   │   Entries: [Entry{index=10, term=5, data="PUT k1 v1"}]     │
   │   CommittedEntries: [Entry{index=9, term=5, data="..."}]   │
   │   Messages: [AppendEntriesReply{...}]                      │
   │ }                                                           │
   └─────────────────────────────────────────────────────────────┘
         │
         ▼
5. Ordered Persistence (etcd ordering)
   ┌─────────────────────────────────────────────────────────────┐
   │              Ready Handler Processing                       │
   │                                                             │
   │ Step 1: Save Snapshot (if present)                         │
   │ ┌─────────────┐                                             │
   │ │ Snapshot    │──► WAL.Append(RecordSnapshot)               │
   │ └─────────────┘                                             │
   │                                                             │
   │ Step 2: Save HardState + Entries                           │
   │ ┌─────────────┐                                             │
   │ │ HardState   │──► WAL.Append(RecordState)                 │
   │ │ Entries     │──► WAL.Append(RecordEntry) for each        │
   │ └─────────────┘                                             │
   │                                                             │
   │ Step 3: Apply Committed Entries                            │
   │ ┌─────────────┐                                             │
   │ │ Committed   │──► B+Tree.Put(key, value)                  │
   │ │ Entries     │                                             │
   │ └─────────────┘                                             │
   └─────────────────────────────────────────────────────────────┘
         │
         ▼
6. Persistence Layer
   ┌─────────────────────────────────────────────────────────────┐
   │                    WAL (Write-Ahead Log)                   │
   │                                                             │
   │ File: /data/wal/00000001.wal                               │
   │ ┌─────────────────────────────────────────────────────────┐ │
   │ │ Record 1: RecordState    | CRC | term=5 vote=1 commit=9 │ │
   │ │ Record 2: RecordEntry    | CRC | index=10 term=5 PUT... │ │
   │ │ Record 3: RecordEntry    | CRC | index=11 term=5 DEL... │ │
   │ └─────────────────────────────────────────────────────────┘ │
   └─────────────────────────────────────────────────────────────┘
         │
         ▼
   ┌─────────────────────────────────────────────────────────────┐
   │                   B+ Tree (State Machine)                  │
   │                                                             │
   │ In-Memory Pages (4KB each)                                 │
   │ ┌─────────────────────────────────────────────────────────┐ │
   │ │ Root Page                                               │ │
   │ │ ┌─────┬─────┬─────┬─────┐                               │ │
   │ │ │ k1  │ k2  │ k3  │ ... │                               │ │
   │ │ │ v1  │ v2  │ v3  │ ... │                               │ │
   │ │ └─────┴─────┴─────┴─────┘                               │ │
   │ └─────────────────────────────────────────────────────────┘ │
   └─────────────────────────────────────────────────────────────┘
         │
         ▼
7. Client Response
   ┌─────────┐        Success         ┌─────────────┐
   │ Client  │◄──────────────────────┤ Node (API)  │
   └─────────┘                       └─────────────┘
```

## 4. Data Flow: Read Operation

```
Client Read Request Flow
────────────────────────

1. Client Request
   ┌─────────┐     GET(key)           ┌─────────────┐
   │ Client  │──────────────────────►│ Node (API)  │
   └─────────┘                       └─────────────┘
                                            │
                                            ▼
2. Leadership Check
   ┌─────────────────────────────────────────────────────┐
   │                Node Decision                        │
   │                                                     │
   │ if node.IsLeader() {                               │
   │     // Read from local B+Tree                      │
   │     return btree.Get(key)                          │
   │ } else {                                           │
   │     // Redirect to leader OR                       │
   │     // Read from local (if stale reads allowed)    │
   │     return redirect_to_leader()                    │
   │ }                                                  │
   └─────────────────────────────────────────────────────┘
                          │
                          ▼
3. Local Read (Leader or Stale)
   ┌─────────────────────────────────────────────────────────────┐
   │                    B+ Tree Query                           │
   │                                                             │
   │ ┌─────────────────────────────────────────────────────────┐ │
   │ │                Root Page                                │ │
   │ │ ┌─────┬─────┬─────┬─────┐                               │ │
   │ │ │ k1  │ k5  │ k9  │ ... │  Search: key = "k3"           │ │
   │ │ │ptr1 │ptr2 │ptr3 │ ... │          └─► "k1" < "k3" < "k5" │ │
   │ │ └─────┴─────┴─────┴─────┘              Follow ptr1      │ │
   │ └─────────────────────────────────────────────────────────┘ │
   │                          │                                  │
   │                          ▼                                  │
   │ ┌─────────────────────────────────────────────────────────┐ │
   │ │                Leaf Page                                │ │
   │ │ ┌─────┬─────┬─────┬─────┐                               │ │
   │ │ │ k1  │ k2  │ k3  │ k4  │  Found: k3 = v3               │ │
   │ │ │ v1  │ v2  │ v3  │ v4  │         └─► Return v3         │ │
   │ │ └─────┴─────┴─────┴─────┘                               │ │
   │ └─────────────────────────────────────────────────────────┘ │
   └─────────────────────────────────────────────────────────────┘
                          │
                          ▼
4. Client Response
   ┌─────────┐        value          ┌─────────────┐
   │ Client  │◄──────────────────────┤ Node (API)  │
   └─────────┘                       └─────────────┘
```

## 5. Recovery Flow

```
Node Startup & Recovery Flow
───────────────────────────

1. Node Startup
   ┌─────────────┐      Start()       ┌─────────────┐
   │   System    │──────────────────►│    Node     │
   └─────────────┘                   └─────────────┘
                                            │
                                            ▼
2. WAL Recovery
   ┌─────────────────────────────────────────────────────────────┐
   │                  WAL.ReadAll()                             │
   │                                                             │
   │ Scan WAL files: 00000001.wal, 00000002.wal, ...           │
   │                                                             │
   │ ┌─────────────────────────────────────────────────────────┐ │
   │ │ Record 1: RecordState    | CRC ✓| term=1 vote=0        │ │
   │ │ Record 2: RecordEntry    | CRC ✓| index=1 term=1 PUT.. │ │
   │ │ Record 3: RecordEntry    | CRC ✓| index=2 term=1 DEL.. │ │
   │ │ Record 4: RecordSnapshot | CRC ✓| index=100 term=3     │ │
   │ │ Record 5: RecordState    | CRC ✓| term=3 vote=2        │ │
   │ │ Record 6: RecordEntry    | CRC ✓| index=101 term=3     │ │
   │ │ ...                                                     │ │
   │ └─────────────────────────────────────────────────────────┘ │
   └─────────────────────────────────────────────────────────────┘
                          │
                          ▼
3. State Reconstruction
   ┌─────────────────────────────────────────────────────────────┐
   │               Recovery Processing                           │
   │                                                             │
   │ For each WAL record:                                       │
   │   switch record.Type {                                     │
   │   case RecordState:                                        │
   │     raft.hardState = record.HardState                      │
   │   case RecordSnapshot:                                     │
   │     btree.RestoreFromSnapshot(record.Data)                 │
   │     appliedIndex = record.Index                            │
   │   case RecordEntry:                                        │
   │     if record.Index > appliedIndex {                       │
   │       btree.Apply(record.Entry)                            │
   │       appliedIndex = record.Index                          │
   │     }                                                      │
   │   }                                                        │
   └─────────────────────────────────────────────────────────────┘
                          │
                          ▼
4. Raft State Recovery
   ┌─────────────────────────────────────────────────────────────┐
   │                 Raft Initialization                        │
   │                                                             │
   │ raftState = RaftState{                                     │
   │   currentTerm:  lastHardState.Term,                        │
   │   votedFor:     lastHardState.Vote,                        │
   │   commitIndex:  lastHardState.Commit,                      │
   │   lastApplied:  appliedIndex,                              │
   │   log:          recoveredEntries,                          │
   │   state:        Follower, // Start as follower             │
   │ }                                                          │
   │                                                             │
   │ Start election timeout...                                  │
   └─────────────────────────────────────────────────────────────┘
                          │
                          ▼
5. Ready for Operations
   ┌─────────┐     Operations       ┌─────────────┐
   │ Client  │◄───────────────────►│ Recovered   │
   └─────────┘                     │    Node     │
                                   └─────────────┘
```

## 6. Component Interaction Matrix

```
                 │  WAL  │ B+Tree│ Raft │ Client│ Ready │ Storage│
─────────────────┼───────┼───────┼──────┼───────┼───────┼────────┤
WAL              │   -   │   ×   │  W→R │   ×   │  R→W  │  S→W   │
B+Tree           │   ×   │   -   │  ×   │  C→B  │  R→B  │  S→B   │
Raft             │  R→W  │   ×   │  -   │  C→R  │  R→R  │   ×    │
Client           │   ×   │  C→B  │ C→R  │   -   │   ×   │  C→S   │
Ready Handler    │  R→W  │  R→B  │ R→R  │   ×   │   -   │  R→S   │
Storage          │  S→W  │  S→B  │  ×   │  C→S  │  R→S  │   -    │

Legend:
→ : Direction of dependency/call
× : No direct interaction
- : Self (same component)

W = WAL, B = B+Tree, R = Raft, C = Client, S = Storage

Key Interactions:
• Client → Raft: PUT/GET requests via Propose()
• Client → B+Tree: Direct GET for reads (leader only)
• Client → Storage: Unified interface for operations
• Raft → Ready: Produces Ready events via channel
• Ready → WAL: Saves HardState + Entries
• Ready → B+Tree: Applies committed entries
• Ready → Storage: Orchestrates persistence
• Storage → WAL: Manages WAL operations
• Storage → B+Tree: Manages state machine
```

## 7. Error Handling Flow

```
Error Handling & Fault Tolerance
───────────────────────────────

1. WAL Write Failure
   Write Op ──► WAL.Append() ──► Disk Error
                     │
                     ▼
            ┌─────────────────┐
            │ Error Recovery  │
            │                 │
            │ • Retry write   │
            │ • Mark WAL bad  │
            │ • Switch file   │
            │ • Report error  │
            └─────────────────┘
                     │
                     ▼
            Reject client request

2. Raft Partition
   Leader ──X──Network──X── Follower
      │                        │
      ▼                        ▼
   ┌─────────┐              ┌─────────┐
   │ Timeout │              │ Timeout │
   │ Step    │              │ Step    │
   │ Down    │              │ Down    │
   └─────────┘              └─────────┘
      │                        │
      ▼                        ▼
   Reject                   Start
   Writes                   Election

3. Corruption Detection
   WAL Read ──► CRC Check ──► Mismatch
                    │
                    ▼
           ┌─────────────────┐
           │ Corruption      │
           │ Handling        │
           │                 │
           │ • Stop at error │
           │ • Request snap  │
           │ • Rebuild from  │
           │   last good     │
           └─────────────────┘
```