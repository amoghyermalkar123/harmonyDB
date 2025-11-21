# Debug Mode and Visualization Specification

## Overview

This specification defines the debug mode for HarmonyDB that enables real-time visualization of:
1. Raft consensus state and log replication
2. B+Tree structure and operations

## 1. Debug Mode Infrastructure

### 1.1 Configuration

```go
type DebugConfig struct {
    Enabled       bool
    HTTPPort      int    // Debug server port (default: 6060)
    EnableRaft    bool   // Enable Raft visualization
    EnableBTree   bool   // Enable B+Tree visualization
    PollInterval  time.Duration // Data refresh interval (default: 500ms)
}
```

### 1.2 Activation

**Command line:**
```bash
./harmonydb --debug
./harmonydb --debug --debug-port=6060
```

**Programmatic:**
```go
db := harmonydb.NewWithConfig(Config{
    Debug: DebugConfig{
        Enabled:    true,
        HTTPPort:   6060,
        EnableRaft: true,
        EnableBTree: true,
    },
})
```

### 1.3 Debug HTTP Server

When debug mode is enabled, start an HTTP server that serves:
- `/` - Dashboard home with links to all visualizations
- `/api/raft/state` - JSON endpoint for Raft cluster state
- `/api/btree/state` - JSON endpoint for B+Tree structure
- `/raft` - Raft visualization UI
- `/btree` - B+Tree visualization UI
- `/ws/raft` - WebSocket for real-time Raft updates
- `/ws/btree` - WebSocket for real-time B+Tree updates

---

## 2. Raft Visualization

### 2.1 Data Model

The following data structures will be exposed for visualization:

```go
// ClusterState represents the entire cluster's Raft state
type ClusterState struct {
    Nodes      map[int64]*NodeState `json:"nodes"`
    LeaderID   int64                `json:"leader_id"`
    Term       int64                `json:"term"`
    Timestamp  time.Time            `json:"timestamp"`
}

// NodeState represents a single node's Raft state
type NodeState struct {
    NodeID          int64          `json:"node_id"`
    Role            string         `json:"role"` // "leader", "follower", "candidate"
    Term            int64          `json:"term"`
    VotedFor        int64          `json:"voted_for"`
    CommitIndex     int64          `json:"commit_index"`
    LastApplied     int64          `json:"last_applied"`
    LogEntries      []LogEntry     `json:"log_entries"`

    // Leader-specific state
    NextIndex       map[int64]int64 `json:"next_index,omitempty"`
    MatchIndex      map[int64]int64 `json:"match_index,omitempty"`
}

// LogEntry represents a single log entry for visualization
type LogEntry struct {
    ID        int64  `json:"id"`
    Term      int64  `json:"term"`
    Command   string `json:"command"`
    Key       string `json:"key"`
    Value     string `json:"value"`
    Committed bool   `json:"committed"`
    Applied   bool   `json:"applied"`
}
```

### 2.2 Required API Additions to raftNode

Add methods to expose internal state:

```go
// GetDebugState returns the current node state for visualization
func (n *raftNode) GetDebugState() *NodeState {
    n.RLock()
    defer n.RUnlock()

    n.state.Lock()
    defer n.state.Unlock()

    n.meta.RLock()
    defer n.meta.RUnlock()

    role := "follower"
    switch n.meta.nt {
    case Leader:
        role = "leader"
    case Candidate:
        role = "candidate"
    }

    // Build log entries with committed/applied status
    logs := n.logManager.GetLogs()
    entries := make([]LogEntry, len(logs))
    for i, log := range logs {
        entries[i] = LogEntry{
            ID:        log.Id,
            Term:      log.Term,
            Command:   log.Data.Op,
            Key:       log.Data.Key,
            Value:     log.Data.Value,
            Committed: log.Id <= n.meta.lastCommitIndex,
            Applied:   log.Id <= n.meta.lastAppliedToSM,
        }
    }

    state := &NodeState{
        NodeID:      n.ID,
        Role:        role,
        Term:        n.state.currentTerm,
        VotedFor:    n.state.votedFor,
        CommitIndex: n.meta.lastCommitIndex,
        LastApplied: n.meta.lastAppliedToSM,
        LogEntries:  entries,
    }

    // Include leader-specific state
    if role == "leader" {
        state.NextIndex = make(map[int64]int64)
        state.MatchIndex = make(map[int64]int64)
        for k, v := range n.nextIndex {
            state.NextIndex[k] = v
        }
        for k, v := range n.matchIndex {
            state.MatchIndex[k] = v
        }
    }

    return state
}
```

### 2.3 Visualization Features

#### 2.3.1 Cluster Overview
- Visual representation of all nodes in the cluster
- Color-coded by role:
  - Leader: Green
  - Follower: Blue
  - Candidate: Yellow
- Arrows showing replication direction

#### 2.3.2 Log Comparison View
Display side-by-side comparison of logs across nodes:

```
Node 1 (Leader)     Node 2 (Follower)   Node 3 (Follower)
+----------------+  +----------------+  +----------------+
| [1] T:1 PUT k1 |  | [1] T:1 PUT k1 |  | [1] T:1 PUT k1 |
| [2] T:1 PUT k2 |  | [2] T:1 PUT k2 |  | [2] T:1 PUT k2 |
| [3] T:2 PUT k3 |  | [3] T:2 PUT k3 |  |                |
| [4] T:2 PUT k4 |  |                |  |                |
+----------------+  +----------------+  +----------------+
Commit: 2           Commit: 2           Commit: 2
NextIdx: -          NextIdx: 3          NextIdx: 3
MatchIdx: -         MatchIdx: 2         MatchIdx: 2
```

#### 2.3.3 Leader State View
For the leader node, display:
- `nextIndex[peer]` - Next log index to send to each follower
- `matchIndex[peer]` - Highest log index known to be replicated on each follower
- Visual progress bars showing replication status per follower

#### 2.3.4 Log Entry Detail
Click on any log entry to see:
- Term number
- Log ID
- Full command (Op, Key, Value)
- Committed status
- Applied to state machine status

### 2.4 Implementation Approach

**Recommended: Web-based visualization with embedded server**

Files to create:
- `debug/server.go` - HTTP server and WebSocket handlers
- `debug/raft_handler.go` - Raft state collection and endpoints
- `debug/static/` - Embedded HTML/CSS/JS for visualization

Technology choices:
- Backend: Go net/http with gorilla/websocket
- Frontend: Vanilla JS with D3.js for visual graphs
- Data transport: JSON over WebSocket for real-time updates

---

## 3. B+Tree Visualization

### 3.1 Data Model

```go
// BTreeState represents the entire tree structure
type BTreeState struct {
    TotalPages    int           `json:"total_pages"`
    TreeHeight    int           `json:"tree_height"`
    TotalKeys     int           `json:"total_keys"`
    RootNode      *NodeViz      `json:"root"`
    Timestamp     time.Time     `json:"timestamp"`
}

// NodeViz represents a node for visualization
type NodeViz struct {
    FileOffset   uint64      `json:"file_offset"`
    IsLeaf       bool        `json:"is_leaf"`
    IsDirty      bool        `json:"is_dirty"`
    KeyCount     int         `json:"key_count"`
    Keys         []string    `json:"keys"`
    Children     []*NodeViz  `json:"children,omitempty"`     // For internal nodes
    Values       []string    `json:"values,omitempty"`       // For leaf nodes (truncated)
    Utilization  float64     `json:"utilization"`            // Percentage of capacity used
}

// BTreeOperation represents a tracked operation
type BTreeOperation struct {
    Type       string    `json:"type"`       // "get", "put", "split"
    Key        string    `json:"key"`
    Timestamp  time.Time `json:"timestamp"`
    NodePath   []uint64  `json:"node_path"`  // File offsets traversed
    Duration   float64   `json:"duration_ms"`
}
```

### 3.2 Required API Additions to BTree

```go
// GetDebugState returns the current tree structure for visualization
func (b *BTree) GetDebugState() *BTreeState {
    return &BTreeState{
        TotalPages:   len(b.cache),
        TreeHeight:   b.calculateHeight(),
        TotalKeys:    b.countKeys(),
        RootNode:     b.nodeToViz(b.root),
        Timestamp:    time.Now(),
    }
}

// nodeToViz recursively converts a node to visualization format
func (b *BTree) nodeToViz(n *Node) *NodeViz {
    if n == nil {
        return nil
    }

    viz := &NodeViz{
        FileOffset:  n.fileOffset,
        IsLeaf:      n.isLeaf,
        IsDirty:     n.isDirty,
        KeyCount:    len(n.offsets),
    }

    if n.isLeaf {
        viz.Utilization = float64(len(n.offsets)) / float64(maxLeafNodeCells) * 100
        viz.Keys = make([]string, len(n.offsets))
        viz.Values = make([]string, len(n.offsets))
        for i, ofs := range n.offsets {
            viz.Keys[i] = string(n.leafCell[ofs].key)
            // Truncate values for display
            val := string(n.leafCell[ofs].val)
            if len(val) > 20 {
                val = val[:20] + "..."
            }
            viz.Values[i] = val
        }
    } else {
        viz.Utilization = float64(len(n.offsets)) / float64(maxInternalNodeCells) * 100
        viz.Keys = make([]string, len(n.offsets))
        viz.Children = make([]*NodeViz, len(n.offsets))
        for i, ofs := range n.offsets {
            viz.Keys[i] = string(n.internalCell[ofs].key)
            childNode := b.fetch(n.internalCell[ofs].fileOffset)
            viz.Children[i] = b.nodeToViz(childNode)
        }
    }

    return viz
}

// calculateHeight returns the tree height
func (b *BTree) calculateHeight() int {
    height := 0
    node := b.root
    for node != nil && !node.isLeaf {
        height++
        if len(node.offsets) > 0 {
            node = b.fetch(node.internalCell[node.offsets[0]].fileOffset)
        } else {
            break
        }
    }
    return height + 1 // Include leaf level
}

// countKeys returns total number of keys in the tree
func (b *BTree) countKeys() int {
    return b.countKeysInNode(b.root)
}

func (b *BTree) countKeysInNode(n *Node) int {
    if n == nil {
        return 0
    }
    if n.isLeaf {
        return len(n.offsets)
    }
    count := 0
    for _, ofs := range n.offsets {
        child := b.fetch(n.internalCell[ofs].fileOffset)
        count += b.countKeysInNode(child)
    }
    return count
}
```

### 3.3 Visualization Features

#### 3.3.1 Tree Structure View
Interactive tree diagram showing:
- Hierarchical node layout
- Internal nodes with separator keys
- Leaf nodes with key-value pairs
- Node utilization (color gradient: green=low, yellow=medium, red=high)
- Click to expand/collapse subtrees

#### 3.3.2 Node Detail Panel
Click on any node to display:
- File offset (page location)
- Node type (leaf/internal)
- Dirty status
- All keys in order
- For leaf nodes: all values
- For internal nodes: child pointers

#### 3.3.3 Operation Tracing
Track and visualize recent operations:
- GET operations: Highlight search path
- PUT operations: Highlight insertion path
- SPLIT operations: Animate the split process

Display as a timeline of recent operations with:
- Operation type
- Key involved
- Nodes traversed
- Time taken

#### 3.3.4 Statistics Panel
- Total pages allocated
- Tree height
- Total keys stored
- Average node utilization
- Split count (leaf/internal)
- Cache hit ratio

### 3.4 Implementation Approach

**Recommended: D3.js tree visualization**

Features:
- Zoomable/pannable tree view
- Collapsible nodes for large trees
- Animated transitions for operations
- Search functionality to locate keys

---

## 4. Implementation Plan

### Phase 1: Core Infrastructure
1. Create `debug/` package
2. Implement debug HTTP server
3. Add configuration and command-line flags
4. Create WebSocket infrastructure for real-time updates

### Phase 2: Raft Visualization
1. Add `GetDebugState()` to raftNode
2. Implement `/api/raft/state` endpoint
3. Create Raft visualization UI
4. Add WebSocket updates for state changes

### Phase 3: B+Tree Visualization
1. Add `GetDebugState()` to BTree
2. Implement `/api/btree/state` endpoint
3. Create B+Tree visualization UI with D3.js
4. Add operation tracing hooks

### Phase 4: Polish
1. Add unified dashboard
2. Implement operation replay/stepping
3. Add export functionality (JSON/PNG)
4. Performance optimization for large datasets

---

## 5. File Structure

```
harmonydb/
  debug/
    server.go           # HTTP/WebSocket server
    raft_handler.go     # Raft state endpoints
    btree_handler.go    # B+Tree state endpoints
    config.go           # Debug configuration
    static/
      index.html        # Dashboard
      raft.html         # Raft visualization
      btree.html        # B+Tree visualization
      css/
        style.css
      js/
        raft-viz.js     # Raft visualization logic
        btree-viz.js    # B+Tree visualization (D3.js)
        websocket.js    # WebSocket client
```

---

## 6. Usage Examples

### Starting in Debug Mode

```go
// In cmd/server.go
func main() {
    debug := flag.Bool("debug", false, "Enable debug mode")
    debugPort := flag.Int("debug-port", 6060, "Debug server port")
    flag.Parse()

    config := harmonydb.Config{
        // ... existing config
        Debug: harmonydb.DebugConfig{
            Enabled:  *debug,
            HTTPPort: *debugPort,
        },
    }

    db := harmonydb.NewWithConfig(config)
    // ...
}
```

### Accessing the Visualization

```bash
# Start server in debug mode
./harmonydb --debug

# Access visualizations
# Dashboard: http://localhost:6060/
# Raft:      http://localhost:6060/raft
# B+Tree:    http://localhost:6060/btree
# APIs:      http://localhost:6060/api/raft/state
#            http://localhost:6060/api/btree/state
```

### Programmatic Access

```go
// Get Raft state
resp, _ := http.Get("http://localhost:6060/api/raft/state")
var state debug.ClusterState
json.NewDecoder(resp.Body).Decode(&state)

// Get B+Tree state
resp, _ := http.Get("http://localhost:6060/api/btree/state")
var btree debug.BTreeState
json.NewDecoder(resp.Body).Decode(&btree)
```

---

## 7. Acceptance Criteria

### Debug Mode
- [ ] Database can be started with `--debug` flag
- [ ] Debug HTTP server starts on configurable port
- [ ] Server serves both API endpoints and static UI

### Raft Visualization
- [ ] Can view all nodes in cluster with their roles
- [ ] Can see log entries on each node with committed/applied status
- [ ] Leader's nextIndex and matchIndex are displayed
- [ ] Real-time updates via WebSocket
- [ ] Side-by-side log comparison view

### B+Tree Visualization
- [ ] Can view tree structure hierarchically
- [ ] Can click nodes to see details
- [ ] Node utilization is color-coded
- [ ] Operation paths are traceable
- [ ] Statistics panel shows tree metrics

### Performance
- [ ] Debug mode has minimal impact on normal operations
- [ ] Visualization handles trees with 1000+ keys
- [ ] WebSocket updates throttled appropriately
