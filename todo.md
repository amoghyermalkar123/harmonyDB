# HarmonyDB TODO

## Learning-Driven Development Plan

### Phase 0: QoL
- [x] Add logging for debugging (✅ Implemented with zap)
- [ ] **Performance Monitoring & Instrumentation**
  - Add Prometheus client library to dependencies
  - Create metrics package with key performance indicators
  - Add /metrics endpoint to HTTP server
  - Instrument Raft operations with metrics (elections, log replication, state transitions)
  - Instrument database operations with metrics (Put/Get latency, throughput)
  - Instrument API operations with metrics (request duration, status codes)
  - Create Docker Compose for Prometheus + Grafana
  - Create Grafana dashboards (cluster health, performance, system resources)
  - Add pprof endpoints for profiling (CPU, memory, goroutines)
  - Implement load testing framework (k6 scripts, custom Go clients)
  - Add distributed tracing with OpenTelemetry/Jaeger


### Phase 1: Core Distributed Features (High Impact)
- [ ] **WAL Implementation** - Learn durability guarantees
  - Write-ahead logging for crash recovery
  - Log replay mechanisms
  - Durability vs performance tradeoffs
- [ ] **Log Compaction** - Essential for any real Raft deployment
  - Snapshot creation and restoration
  - Log truncation logic
  - Memory management for large logs
- [ ] **Network Partition Testing** - Understand CAP theorem practically
  - Partition simulation framework
  - Split-brain prevention validation
  - Quorum-based operation testing

### Phase 2: Transaction Support
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

### Phase 3: Performance & Scale
- [ ] **Buffer Pool Management** - Critical for database performance
  - LRU/Clock replacement policies
  - Page pinning and unpinning
  - Memory pressure handling
- [ ] **Concurrent B+ Tree** - Learn about database concurrency
  - Latch-based concurrency control
  - Lock coupling strategies
  - Read-write lock optimization
- [ ] **Query Optimization** - Understand how databases think
  - SQL parser implementation
  - Cost-based optimization
  - Join algorithm selection

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
  - Different isolation levels implementation
  - Eventual consistency for read replicas
  - Linearizability guarantees

### Advanced Storage Engine Features
- [ ] **MVCC (Multi-Version Concurrency Control)**
  - Version chain management
  - Garbage collection strategies
  - Snapshot isolation
- [ ] **Storage Optimizations**
  - Page-level compression
  - Bloom filters for negative lookups
  - Secondary index support
- [ ] **Advanced Query Processing**
  - Join algorithms (nested loop, hash, merge)
  - Index selection and statistics
  - Parallel query execution

## Immediate Issues (Fix These First)

### Critical Bugs
- [ ] Fix B+ tree root reassignment bug in `btree.go:119`
- [ ] Fix child page fetching in `btree.go:141`
- [ ] Implement missing decode functions in `node.go:269-271`
- [ ] Fix format string errors in `cmd/dbclient.go:57,69`

### Missing Features
- [ ] Add Raft tests (currently no raft package tests)
- [ ] Implement proper leader forwarding for non-leader nodes
- [ ] Complete second goroutine in dbclient
- [ ] Add integration tests for B+ tree and Raft together

### Code Quality
- [ ] Add godoc comments for public APIs
- [ ] More granular error types
- [ ] Better error handling throughout codebase
