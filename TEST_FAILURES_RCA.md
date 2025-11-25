# Test Failures - Root Cause Analysis

**Date**: 2025-11-25
**Feature**: Database Quality Assurance (001-db-tests)
**Phase**: User Story 1 - Basic Data Integrity Verification

## Summary

Phase 3 (User Story 1) tests have been implemented but are showing inconsistent failures. Out of 4 tests:
- **TestMultipleKeys**: ✅ PASSING consistently
- **TestBasicPutGet**: ❌ FAILING (intermittent - passed earlier, now failing)
- **TestKeyOverwrite**: ❌ FAILING (intermittent - passed earlier, now failing)
- **TestDataPersistence**: ❌ FAILING (leader election issue)

## Detailed Analysis

### Issue 1: "key not found" errors after successful Put operations

**Affected Tests**: TestBasicPutGet, TestKeyOverwrite

**Symptoms**:
```
Error: key not found
Test: TestBasicPutGet
Messages: Failed to get value for key
```

**Observed Behavior**:
1. Leader election succeeds (confirmed by logs)
2. Put operation returns no error (appears successful)
3. Immediate Get operation fails with "key not found"
4. TestMultipleKeys with 5 keys succeeds consistently
5. TestBasicPutGet with 1 key fails

**Timeline**:
- Initially: All tests passed (TestBasicPutGet passed at 0.56s)
- Later runs: TestBasicPutGet and TestKeyOverwrite fail with "key not found"
- TestMultipleKeys continues to pass

**Root Cause Hypothesis**:

The most likely cause is **asynchronous consensus replication timing**:

1. **Put() returns before data is committed**: The `Put()` method likely returns after submitting to Raft but before the consensus commit completes
2. **Scheduler delay**: The `scheduler()` goroutine in db.go applies entries from the `Ready()` channel asynchronously
3. **Race condition**: Get() is called before the scheduler has applied the Put to the BTree

**Evidence**:
- From db.go:109: "The scheduler goroutine will apply committed entries from the Ready() channel"
- No wait/sync mechanism between Put() returning and data being applied
- TestMultipleKeys succeeds possibly because multiple Puts give more time for first entry to commit

**Why TestMultipleKeys passes**:
- Writes 5 keys in a loop
- By the time it reads, the earlier writes have had time to propagate
- The iteration gives implicit delay for consensus

### Issue 2: Leader election disagreement after restart

**Affected Tests**: TestDataPersistence

**Symptoms**:
```
Error: leader election timeout after 5s
Leader IDs reported: map[1:2 2:1]
Node 1 thinks leader is: 2
Node 2 thinks leader is: 1  
Node 3 thinks leader is: 1
```

**Observed Behavior**:
1. Initial cluster starts fine, leader elected
2. After StopAll() and RestartAll(), nodes disagree on leader
3. Split decision: 2 nodes think leader is 1, 1 node thinks leader is 2
4. No consensus reached within 5 second timeout

**Root Cause Hypothesis**:

**Split-brain scenario after restart**:

1. **Stale term information**: Nodes may retain different Raft term numbers from before restart
2. **Unclean shutdown**: Files may not be fully flushed when Stop() is called
3. **Race in restart**: Nodes restart simultaneously, causing election race conditions
4. **Network partition simulation bug**: The 100ms sleep may not be sufficient for clean state

**Evidence**:
- Logs show nodes have conflicting views of who is leader
- This is a classic Raft split-brain/network partition scenario
- The WaitForLeader helper correctly detects this (requires all nodes to agree)

## Recommendations for Code Fixes

### Fix 1: Wait for consensus commit in Put operations

**Problem**: Put() returns before data is applied to storage

**Solution Options**:

**Option A - Add synchronization to Put()** (Recommended):
```go
// In db.go Put() method, after consensus.Put():
// Wait for entry to be applied
timeout := time.After(2 * time.Second)
for {
    select {
    case <-timeout:
        return fmt.Errorf("timeout waiting for entry to apply")
    default:
        // Check if key exists in storage
        if _, err := db.kv.Get(key); err == nil {
            return nil
        }
        time.Sleep(10 * time.Millisecond)
    }
}
```

**Option B - Add WaitForCommit helper**:
```go
// Add to DB struct:
func (db *DB) WaitForCommit(key []byte, timeout time.Duration) error {
    // Poll until key is readable
}
```

**Option C - Expose Ready() channel**:
- Allow tests to wait for specific log entries to be applied
- More invasive but more precise

### Fix 2: Improve restart reliability

**Problem**: Nodes disagree on leader after cluster restart

**Solution Options**:

**Option A - Longer stabilization delay**:
```go
// In TestDataPersistence, after StopAll():
time.Sleep(500 * time.Millisecond) // Increase from 100ms
```

**Option B - Sequential restart**:
```go
// Restart nodes one at a time with delay:
for i := 0; i < tc.nodeCount; i++ {
    // restart node i
    time.Sleep(200 * time.Millisecond)
}
```

**Option C - Flush data before stop**:
```go
// Add Flush() method to DB:
func (db *DB) Stop() {
    db.kv.Flush() // Ensure all data written to disk
    db.consensus.Stop()
}
```

**Option D - Wait for Raft stable state**:
```go
// After restart, wait for Raft to stabilize:
// Check that all nodes have same term, same log index
```

### Fix 3: Add explicit consensus sync helpers

**Problem**: Tests have no way to wait for consensus completion

**Recommendation**: Add helper methods to DB:

```go
// WaitForConsensus polls until a key is available
func (db *DB) WaitForConsensus(key []byte, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if _, err := db.kv.Get(key); err == nil {
            return nil
        }
        time.Sleep(10 * time.Millisecond)
    }
    return fmt.Errorf("consensus timeout")
}
```

## Test Implementation Status

### Completed (Phase 1-3)
- ✅ T001-T002: Setup phase (db_test.go created with TestMain)
- ✅ T003-T008: Foundational phase (TestCluster infrastructure)
- ✅ T009: TestBasicPutGet (implemented, currently failing)
- ✅ T010: TestMultipleKeys (implemented, passing)
- ✅ T011: TestKeyOverwrite (implemented, currently failing)
- ✅ T012: TestDataPersistence (implemented, failing on restart)

### Helper Methods Added
- ✅ StopAll() - stops all cluster nodes
- ✅ RestartAll() - restarts all cluster nodes
- ⚠️ Need: WaitForConsensus() or similar sync mechanism

## Next Steps

1. **User needs to fix the underlying code issues**:
   - Add synchronization in db.go Put() method
   - Improve Raft restart stability
   - Consider adding flush/sync mechanisms

2. **Alternative: Modify tests to work around limitations**:
   - Add explicit wait after Put() operations
   - Use polling in tests to wait for consensus
   - Increase delays (within 200ms constraint)

3. **Continue implementation**:
   - Once fixes applied, rerun tests
   - Proceed to Phase 4 (User Story 2) - Multi-Node Consistency tests
   - These will require the consensus sync fixes even more critically

## Conclusion

The tests are correctly identifying real issues in the HarmonyDB implementation:

1. **Async consensus without sync mechanism**: Put() returns before data is readable
2. **Restart stability issues**: Nodes disagree on leader after restart
3. **Missing synchronization primitives**: No way for callers to wait for consensus completion

These are legitimate bugs that need code fixes, not test workarounds. The test implementation follows best practices and correctly exposes these issues as per the project requirement: "let tests fail with detailed RCA; user will fix code."
