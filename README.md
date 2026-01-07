# HarmonyDB

A toy distributed key-value database built in Go with Raft consensus.

## Features

- Key-value storage with B+ tree for page management
- Distributed consensus using Raft protocol
- HTTP API for database operations
- FIFO scheduling for request handling

## Usage

```go
db, err := harmonydb.Open(raftPort, httpPort)
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Put key-value pair
err = db.Put("key", "value")

// Get value by key
value, err := db.Get("key")
```

## Build

```bash
go build
```

## Test

### Unit tests
```bash
make test
```

### Integration tests
```bash
make test-integration
```
