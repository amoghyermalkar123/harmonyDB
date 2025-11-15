# HarmonyDB Testing Suite

A comprehensive testing framework for HarmonyDB's distributed database system, covering unit tests, integration tests, and distributed system validation.

## Overview

The testing suite provides three levels of testing:

1. **Unit Tests** - Test individual components in isolation
2. **Integration Tests** - Test component interactions and persistence
3. **Distributed Tests** - Test distributed system properties using real containers

## Quick Start

```bash
# Run all unit tests
make test

# Run integration tests
make test-integration

# Run distributed tests (requires Docker)
make test-distributed

# Run everything
make test-all
```

## Testing Architecture

### Directory Structure

```
tests/
├── testutils/           # Shared test utilities
│   ├── cluster.go       # Test cluster management
│   ├── harness.go       # Test harness for distributed testing
│   └── helpers.go       # Common test helpers
├── integration/         # Integration tests
│   ├── btree_persistence_test.go
│   └── raft_consensus_test.go
├── distributed/         # Distributed system tests
│   ├── cluster_test.go  # Multi-node cluster tests
│   └── chaos_test.go    # Chaos engineering tests
└── configs/             # Test configurations
    ├── test-3-node.yaml
    ├── test-5-node.yaml
    ├── test-chaos.yaml
    └── test-performance.yaml
```

## Test Categories

### Unit Tests

Located in the main package files (`*_test.go`):

- `btree_test.go` - B+ tree operations
- `node_test.go` - Node structure and operations
- `raft/raft_test.go` - Raft consensus mechanisms

```bash
# Run all unit tests
make test-unit

# Run with race detection
make test-unit-race

# Run with coverage
make test-unit-coverage
```

### Integration Tests

Test component interactions with real file I/O and persistence:

```bash
# Run integration tests
make test-integration

# Run specific component tests
make test-btree    # B-tree specific
make test-raft     # Raft specific
```

**Build Tag**: `//go:build integration`

### Distributed Tests

Test distributed system properties using Docker containers:

```bash
# Run distributed tests
make test-distributed

# Run specific scenarios
make test-3-node              # 3-node cluster
make test-5-node              # 5-node cluster
make test-leader-failover     # Leader election
make test-network-partition   # Network splits
make test-chaos               # Chaos engineering
```

**Build Tag**: `//go:build distributed`

## Test Utilities

### TestCluster

Manages Docker-based test clusters:

```go
// Create a 3-node cluster
cluster := testutils.RequireCluster(t, 3)

// Get cluster nodes
nodes := cluster.GetNodes()

// Simulate failures
cluster.StopNode(1)
cluster.StartNode(1)

// Network partitions
cluster.SimulateNetworkPartition([]int{1, 2}, 10*time.Second)
```

### TestHarness

Provides distributed testing operations:

```go
harness := testutils.NewTestHarness(t, cluster)

// Wait for leader election
leader := harness.AssertLeaderExists(30 * time.Second)

// Put data to leader
harness.PutValueToLeader("key", "value")

// Verify eventual consistency
harness.AssertEventualConsistency("key", "value", 30*time.Second)

// Test concurrent operations
operations := []func() error{...}
harness.ConcurrentOperations(operations)
```

## Distributed Testing Features

### Leader Election Testing

```bash
make test-leader-failover
```

Tests:
- Initial leader election
- Leader failure detection
- New leader election
- Split vote scenarios

### Data Replication Testing

```bash
make test-distributed
```

Tests:
- Write to leader, read from followers
- Eventual consistency verification
- Log replication mechanics
- Commit index advancement

### Fault Tolerance Testing

```bash
make test-node-recovery
make test-network-partition
```

Tests:
- Single node failures
- Majority/minority partitions
- Network healing
- Data consistency after recovery

### Chaos Engineering

```bash
make test-chaos
```

Tests:
- Random node failures
- Network partitions
- Leader chaos (frequent failovers)
- High load with failures
- Recovery verification

## Performance Testing

```bash
# Run benchmarks
make benchmark

# Run performance-specific tests
make test-performance
```

Performance tests measure:
- Operation latency (P50, P90, P95, P99)
- Throughput under load
- Memory usage patterns
- Recovery times

## Configuration

### Test Configurations

Test scenarios are defined in YAML files:

- `test-3-node.yaml` - Standard 3-node setup
- `test-5-node.yaml` - 5-node fault tolerance
- `test-chaos.yaml` - Chaos engineering parameters
- `test-performance.yaml` - Performance benchmarks

### Docker Configuration

The test suite uses:
- `Dockerfile` - Container image for HarmonyDB nodes
- `docker-compose-test.yml` - Multi-node test setup
- Testcontainers - Programmatic container management

## CI/CD Integration

### Standard CI Pipeline

```bash
make ci  # Unit + integration tests
```

### Full CI Pipeline

```bash
make ci-full  # All tests including distributed
```

### Coverage Reporting

```bash
make ci-coverage  # Generate coverage reports
```

## Development Workflow

### Quick Development Cycle

```bash
make dev-test  # Format + vet + fast tests
```

### Watch Mode

```bash
make watch-test  # Re-run tests on file changes
```

### Test-Driven Development

```bash
# Write failing test
go test ./... -run TestNewFeature

# Implement feature
# ...

# Verify test passes
make test
```

## Troubleshooting

### Docker Issues

```bash
# Check Docker status
docker --version
docker info

# Clean up test containers
make test-clean

# Diagnose test environment
make test-doctor
```

### Test Failures

```bash
# Run with verbose output
make test-verbose

# Run specific test
go test -v -run TestSpecificFunction ./...

# Run with race detection
make test-race
```

### Performance Issues

```bash
# Run only fast tests
make test-short

# Profile test execution
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
```

## Best Practices

### Writing Tests

1. **Use table-driven tests** for multiple scenarios
2. **Run tests in parallel** with `t.Parallel()`
3. **Use testify assertions** for clear error messages
4. **Clean up resources** with `t.Cleanup()`

### Distributed Testing

1. **Wait for convergence** before assertions
2. **Use timeouts** for eventual consistency
3. **Test failure scenarios** not just happy paths
4. **Verify invariants** across all nodes

### Performance Testing

1. **Measure baselines** before optimizations
2. **Test under load** to find bottlenecks
3. **Monitor resource usage** during tests
4. **Use benchmarks** for regression detection

## Example Test Cases

### Basic Functionality

```go
func TestBasicReplication(t *testing.T) {
    cluster := testutils.RequireCluster(t, 3)
    harness := testutils.NewTestHarness(t, cluster)

    // Wait for leader
    leader := harness.AssertLeaderExists(30 * time.Second)

    // Write data
    err := harness.PutValueToLeader("test_key", "test_value")
    require.NoError(t, err)

    // Verify replication
    harness.AssertEventualConsistency("test_key", "test_value", 15*time.Second)
}
```

### Fault Tolerance

```go
func TestNodeFailure(t *testing.T) {
    cluster := testutils.RequireCluster(t, 3)
    harness := testutils.NewTestHarness(t, cluster)

    // Stop minority of nodes
    cluster.StopNode(2)

    // System should still work
    err := harness.PutValueToLeader("failure_test", "still_works")
    require.NoError(t, err)

    // Restart node
    cluster.StartNode(2)

    // Verify recovery
    harness.AssertEventualConsistency("failure_test", "still_works", 30*time.Second)
}
```

## Getting Help

- Check the [main README](README.md) for project overview
- See [CLAUDE.md](CLAUDE.md) for development guidelines
- Use `make help` for available commands
- Use `make test-examples` for testing examples

## Contributing

1. Write tests for new features
2. Ensure all tests pass before submitting PRs
3. Add integration tests for new distributed features
4. Update documentation for new test patterns