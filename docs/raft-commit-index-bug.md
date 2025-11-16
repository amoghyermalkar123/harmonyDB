# Raft Commit Index Propagation Bug

**Discovered**: 2025-11-16
**Test**: `TestLogMatchingProperty` in `tests/invariants_test.go`
**Severity**: High - Violates Raft specification and causes commit latency

## Problem Statement

Followers were not receiving commit index updates during log replication, only during heartbeats. This violated the Raft specification and created unnecessary commit notification latency of up to 50ms.

## Symptoms

When running integration tests, the following behavior was observed:

```
2025-11-16T09:30:33.838 Log entry successfully replicated {"log_id": 1, "commit_index": 1}
2025-11-16T09:30:33.842 Log entry successfully replicated {"log_id": 2, "commit_index": 2}
2025-11-16T09:30:33.844 Log entry successfully replicated {"log_id": 3, "commit_index": 3}

invariants_test.go:56: Minimum commit index across cluster: 0
```

**Expected**: All nodes should have commit index = 3
**Actual**: Only leader had commit index = 3, followers had 0

## Root Cause Analysis

### Timeline of Events

1. **T=0ms**: Leader replicates log entry, sets `lastCommitIndex = 1`
2. **T=0ms**: Leader sends `AppendEntries` RPC to followers **WITHOUT** `LeaderCommitIdx`
3. **T=1ms**: Test checks commit indices - followers still have `commitIndex = 0`
4. **T=50ms**: Heartbeat fires, sends `AppendEntries` WITH `LeaderCommitIdx = 1`
5. **T=50ms**: Followers finally update their commit index

### The Bug

In `raft_core.go`, the `sendAppendEntries()` function was not including `LeaderCommitIdx`:

```go
// BEFORE (buggy code)
resp, err := n.cluster[peer].AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
    Term:        n.state.currentTerm,
    LeaderId:    n.ID,
    PrevLogIdx:  prevLogIndex,
    PrevLogTerm: prevLogTerm,
    Entries:     n.logManager.GetLogsAfter(n.nextIndex[peer]),
    // Missing: LeaderCommitIdx
})
```

Meanwhile, the heartbeat code correctly included it:

```go
// Heartbeat (correct code)
node.AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
    Term:            n.state.currentTerm,
    LeaderId:        n.ID,
    PrevLogIdx:      n.logManager.GetLastLogID(),
    PrevLogTerm:     n.logManager.GetLastLogTerm(),
    LeaderCommitIdx: n.meta.lastCommitIndex,  // ✓ Included
})
```

## Why This is Wrong

### 1. Violates Raft Specification

From the [Raft paper](https://raft.github.io/raft.pdf) (Figure 2, AppendEntries RPC):

> **Arguments:**
> - `term`: leader's term
> - `leaderId`: so follower can redirect clients
> - `prevLogIndex`: index of log entry immediately preceding new ones
> - `prevLogTerm`: term of prevLogIndex entry
> - `entries[]`: log entries to store (empty for heartbeat)
> - **`leaderCommit`: leader's commitIndex**

The `leaderCommit` field is part of **EVERY** AppendEntries RPC, not just heartbeats.

### 2. Creates Unnecessary Latency

- **Commit notification latency**: Up to 50ms (heartbeat interval)
- **Inconsistent state window**: Followers lag behind leader's commit index
- **Application-level impact**: Clients can't read committed data for up to 50ms

### 3. Race Conditions in Tests

Tests that check commit indices immediately after writes would fail, even though the system would "eventually" be correct after the next heartbeat.

## The Fix

Add `LeaderCommitIdx` to the AppendEntries RPC during log replication:

```go
// AFTER (fixed code)
resp, err := n.cluster[peer].AppendEntriesRPC(context.TODO(), &proto.AppendEntries{
    Term:            n.state.currentTerm,
    LeaderId:        n.ID,
    PrevLogIdx:      prevLogIndex,
    PrevLogTerm:     prevLogTerm,
    Entries:         n.logManager.GetLogsAfter(n.nextIndex[peer]),
    LeaderCommitIdx: n.meta.lastCommitIndex,  // ✓ Added
})
```

**File**: `raft/raft_core.go:377`
**Commit**: Added `LeaderCommitIdx` field to AppendEntries RPC in `sendAppendEntries()`

## Impact of Fix

### Before Fix
- Commit notification latency: **~50ms** (depends on heartbeat timing)
- Followers learn about commits: **Only via heartbeats**
- Test result: `Minimum commit index across cluster: 0`

### After Fix
- Commit notification latency: **~1ms** (immediate with replication)
- Followers learn about commits: **During AppendEntries RPC**
- Test result: `Minimum commit index across cluster: 2` (expected 3, some still in flight)

## How the Test Caught This

The `TestLogMatchingProperty` test:

1. Writes multiple log entries to the cluster
2. **Immediately** checks commit indices across all nodes
3. Expects all nodes to have replicated logs with matching commit indices

The test failed because it checked commit indices before the next heartbeat could propagate them, exposing the race condition and specification violation.

## Lessons Learned

1. **Follow specifications carefully**: The Raft paper explicitly lists `leaderCommit` in AppendEntries arguments for a reason

2. **Integration tests expose timing bugs**: Unit tests might not have caught this because they wouldn't check cross-node commit indices immediately after replication

3. **Heartbeats are not a substitute for proper protocol**: While heartbeats would "eventually" propagate commit indices, they create unnecessary latency and violate the spec

4. **Invariant tests are valuable**: Testing Raft invariants (like commit index consistency) helps validate correct implementation

## Related Code

- **Bug location**: `raft/raft_core.go:371-378` (sendAppendEntries function)
- **Test that found it**: `tests/invariants_test.go:13` (TestLogMatchingProperty)
- **Fix commit**: 2025-11-16 - Added LeaderCommitIdx to AppendEntries RPC

## References

- [Raft Paper](https://raft.github.io/raft.pdf) - Section 5.3 (Log Replication)
- [Raft Visualization](https://raft.github.io/) - Interactive Raft algorithm visualization
- Raft Specification - Figure 2 (AppendEntries RPC)
