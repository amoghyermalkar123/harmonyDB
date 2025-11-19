# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

HarmonyDB is a read-efficient, B+ tree based database with distributed systems capabilities. The project focuses on understanding database internals and distributed systems, specifically implementing:

1. **B+ Tree Storage Engine** - Custom B+ tree implementation for efficient data storage and retrieval
2. **Raft Consensus Protocol** - Distributed consensus implementation for data replication and consistency

## Architecture

### Core Components

**Database Layer (`/` root)**
- `db.go` - Main database interface (currently minimal)
- `btree.go` - B+ tree implementation with in-memory page store
- `node.go` - B+ tree node structure with 4KB page size, supports both leaf and internal nodes

**Raft Consensus Layer (`/raft/`)**
- `raft.go` - Complete Raft implementation with LogManager abstraction for thread-safe log operations
- `proto/` - Protocol buffer definitions for Raft RPC (AppendEntries, RequestVote)
- `cmd/main.go` - Example Raft server usage

### Key Architecture Details

**B+ Tree Structure:**
- Page size: 4KB (constant `pageSize = 4096`)
- Max leaf node cells: 9, Max internal node cells: 290
- Uses `store` for page management with in-memory slice of nodes
- Node serialization follows format: `| type | nkeys | pointers | offsets | key-values |`

**Raft Implementation:**
- Uses `LogManager` for thread-safe log operations (prevents panics on empty logs)
- Supports all three node states: Follower, Candidate, Leader
- gRPC-based communication between nodes
- Random 64-bit node ID generation using `rand.Int63()`
- **Service Discovery**: In-memory registry for automatic peer discovery

**Service Discovery Architecture:**
- `ServiceRegistry`: Global registry for node registration and discovery
- `ClusterManager`: Handles peer connections and cluster updates
- `NodeInfo`: Contains node ID, address, and port information
- Automatic peer discovery every 10 seconds
- No hardcoded peer lists required

**Node Communication:**
- Protocol buffers define `RequestVote` and `AppendEntries` RPCs
- Entries contain term, ID, and command data (PUT operations)
- Leader election and log replication follow standard Raft protocol
- Only leaders accept Put requests; followers reject with error

## Development Commands

### Building and Testing
```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./raft
go test .

# Run single test
go test -run TestSpecificTest

# Build main raft server
go build ./raft/cmd

# Run raft server example
go run ./raft/cmd/main.go
```

### Protocol Buffer Generation
```bash
# Generate Go code from .proto files (when proto files are modified)
cd raft
protoc --go_out=. --go-grpc_out=. raft.proto
```

### Go Module Management
```bash
# Download dependencies
go mod download

# Tidy dependencies
go mod tidy
```

## Testing Framework

Uses `github.com/stretchr/testify` for assertions:
- `btree_test.go` - B+ tree functionality tests
- `node_test.go` - Node structure and operations tests
- `db_test.go` - Database interface tests

Test files use standard Go testing patterns with testify assertions like `assert.True()`, `assert.Greater()`, etc.

## Important Implementation Notes

### LogManager Safety
The Raft implementation uses a `LogManager` abstraction that provides thread-safe operations and prevents panics when accessing empty logs. Key methods:
- `GetLastLogID()` and `GetLastLogTerm()` return 0 for empty logs
- `Append()`, `GetLog()`, `TruncateAfter()` are all thread-safe
- Always use LogManager methods instead of direct slice access

### B+ Tree Node Operations
- Nodes support split operations when exceeding capacity
- Use `offsets` array for ordered access to cells
- Distinguish between leaf cells (key-value) and internal cells (key-pointer)

### Development Focus
Per the readme, the project prioritizes understanding database internals and distributed systems over low-level implementation details like custom B+ tree algorithms.