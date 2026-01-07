# HarmonyDB TODO

## Learning-Driven Development Plan

### Phase 1: Core Distributed Features (High Impact)
- [X] **WAL Implementation** - Learn durability guarantees
  - Write-ahead logging for crash recovery
  - Log replay mechanisms (recovery)
- [X] **Linearizability**
  - Linearizable reads
  - Linearizable writes
- [ ] **Log Compaction** - Essential for any real Raft deployment
  - Snapshot creation and restoration
  - Log truncation logic
  - Memory management for large logs
- [ ] **Network Partition Testing** - Understand CAP theorem practically
  - Partition simulation framework
  - Split-brain prevention validation
  - Quorum-based operation testing

### Phase 2: Performance & Scale
- [ ] **Buffer Pool Management** - Critical for database performance
  - LRU/Clock replacement policies
  - Page pinning and unpinning
  - Memory pressure handling
- [ ] **Concurrent B+ Tree** - Learn about database concurrency
  - Latch-based concurrency control
  - Lock coupling strategies
  - Read-write lock optimization
  
### Phase 3: Transaction Support
- [ ] **Basic Transactions with ACID Properties**
  - Begin/Commit/Rollback semantics
  - Atomicity guarantees
  - Transaction isolation levels
- [ ] **Deadlock Detection and Prevention**
  - Wait-for graph implementation
  - Deadlock detection algorithms
  - Prevention strategies
- [ ] **2PC for Distributed Transactions**
  - Two-phase commit protocol
  - Coordinator and participant roles
  - Failure recovery mechanisms
- [ ] **Linearizable Feature**
  - Different isolation levels implementation
  - Eventual consistency for read replicas
  - Linearizability guarantees

### Advanced Distributed Systems Features
- [ ] **Partition Tolerance & Split-Brain Prevention**
  - Quorum-based operations beyond leader election
  - Network partition simulation/testing
  - Proper cluster membership changes
- [ ] **Advanced Raft Features**
  - Read-only queries with linearizable reads
  - Leadership transfer mechanism
  - Configuration changes with joint consensus
- [ ] **Consistency Models**

### Advanced Storage Engine Features (Optional)
- [ ] **MVCC (Multi-Version Concurrency Control)**
  - Version chain management
  - Garbage collection strategies
  - Snapshot isolation
- [ ] **Storage Optimizations**
  - Page-level compression
  - Bloom filters for negative lookups
