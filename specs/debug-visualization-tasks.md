# Debug Visualization - Implementation Tasks

## Phase 1: Infrastructure Setup

### 1.1 Install Dependencies
- [ ] Install templui CLI: `go install github.com/templui/templui/cmd/templui@latest`
- [ ] Install templ: `go get github.com/a-h/templ`
- [ ] Verify installations: `templui -v`

### 1.2 Initialize templui
- [ ] Run `templui init` in project root
- [ ] Configure `.templui.json`:
  - Components directory: `debug/components`
  - Utils directory: `debug/utils`
  - JS directory: `debug/assets/js`

### 1.3 Add Required Components
- [ ] `templui add tabs`
- [ ] `templui add table`
- [ ] `templui add card`
- [ ] `templui add badge`
- [ ] `templui add progress`
- [ ] `templui add accordion`
- [ ] `templui add code`
- [ ] `templui add tooltip`
- [ ] `templui add button`

### 1.4 Create Directory Structure
- [ ] `mkdir -p debug/templates/components`
- [ ] `mkdir -p debug/assets/css`
- [ ] `mkdir -p debug/assets/js`

### 1.5 Create Base Layout
- [ ] Create `debug/templates/layout.templ`
  - HTML5 boilerplate
  - Tailwind CSS link
  - HTMX script
  - templui scripts (popover, code)
  - Navigation header

### 1.6 Create Debug Server
- [ ] Create `debug/server.go`
  - Server struct with Raft and BTree providers
  - NewServer constructor
  - Start method with routing
- [ ] Create `debug/config.go`
  - DebugConfig struct
  - Default values
- [ ] Create `debug/state.go`
  - RaftStateProvider interface
  - BTreeStateProvider interface
  - ClusterState, NodeState, LogEntry types
  - BTreeState, NodeViz types

### 1.7 Implement Basic Routing
- [ ] GET `/` - Dashboard handler
- [ ] GET `/raft` - Raft page handler
- [ ] GET `/btree` - B+Tree page handler
- [ ] GET `/api/raft/state` - JSON API
- [ ] GET `/api/btree/state` - JSON API
- [ ] GET `/sse/raft` - SSE endpoint
- [ ] GET `/sse/btree` - SSE endpoint
- [ ] Static file serving for `/assets/`

### 1.8 Setup Tailwind CSS
- [ ] Create `debug/assets/css/input.css` with templui imports
- [ ] Configure `tailwind.config.js` for templ files
- [ ] Build command: `npx tailwindcss -i ... -o ...`

### 1.9 Add HTMX
- [ ] Download htmx.min.js to `debug/assets/js/`
- [ ] Download htmx sse extension

---

## Phase 2: Raft Visualization

### 2.1 Add GetDebugState to raftNode
- [ ] Implement `GetDebugState() *NodeState` in `raft/raft_core.go`
  - Lock state, meta, node
  - Determine role string
  - Build LogEntry slice with committed/applied status
  - Include nextIndex/matchIndex for leader
- [ ] Add `GetDebugState() *ClusterState` to Raft struct
  - Aggregate state from this node
  - (Note: Initially single-node view, cluster view requires cross-node communication)

### 2.2 Create Raft Templates

#### 2.2.1 Main Page
- [ ] Create `debug/templates/raft.templ`
  - RaftPage templ function
  - Tabs: Cluster Overview, Log Comparison, Leader State
  - HTMX SSE connection

#### 2.2.2 Cluster Overview Component
- [ ] Create `debug/templates/components/cluster_overview.templ`
  - Grid layout for node cards
  - Responsive (1/2/3 columns)

#### 2.2.3 Node Card Component
- [ ] Create `debug/templates/components/node_card.templ`
  - Card with header (ID + role badge)
  - Content: Term, CommitIndex, LastApplied, LogCount
  - Role badge colors: Leader=primary, Follower=secondary, Candidate=outline

#### 2.2.4 Log Comparison Component
- [ ] Create `debug/templates/components/log_comparison.templ`
  - Side-by-side node cards
  - Each with LogTable

#### 2.2.5 Log Table Component
- [ ] Create `debug/templates/components/log_table.templ`
  - Table with columns: ID, Term, Key, Status
  - Status badge: Committed=primary, Pending=outline
  - Scroll for long logs

#### 2.2.6 Leader State Component
- [ ] Create `debug/templates/components/leader_state.templ`
  - Card for leader only
  - Per-peer: nextIndex, matchIndex
  - Progress bar showing replication %

### 2.3 Implement Raft Handlers
- [ ] `handleRaft` - Render RaftPage template
- [ ] `handleRaftState` - Return JSON
- [ ] `handleRaftSSE` - SSE with 500ms updates
  - Set headers (Content-Type, Cache-Control, Connection)
  - Ticker loop
  - Render updated component to buffer
  - Send as SSE data

### 2.4 Test Raft Visualization
- [ ] Start 3-node cluster in debug mode
- [ ] Verify cluster overview shows all nodes
- [ ] Verify role badges update on leader election
- [ ] Verify log table updates on Put operations
- [ ] Verify leader state shows replication progress

---

## Phase 3: B+Tree Visualization

### 3.1 Add GetDebugState to BTree
- [ ] Implement `GetDebugState() *BTreeState` in `btree.go`
  - TotalPages from cache length
  - TreeHeight via calculateHeight()
  - TotalKeys via countKeys()
  - RootNode via nodeToViz()
- [ ] Implement `calculateHeight() int`
  - Traverse from root to leaf
- [ ] Implement `countKeys() int`
  - Recursive count of leaf keys
- [ ] Implement `nodeToViz(n *Node) *NodeViz`
  - FileOffset, IsLeaf, IsDirty, KeyCount
  - Keys array
  - Values array (for leaf, truncated to 20 chars)
  - Children array (recursive for internal)
  - Utilization percentage

### 3.2 Create B+Tree Templates

#### 3.2.1 Main Page
- [ ] Create `debug/templates/btree.templ`
  - BTreePage templ function
  - Grid: 1/4 stats sidebar, 3/4 tree view
  - HTMX SSE connection

#### 3.2.2 Statistics Card
- [ ] Create `debug/templates/components/btree_stats.templ`
  - Total Pages
  - Tree Height
  - Total Keys
  - (Future: Split counts, cache hit ratio)

#### 3.2.3 Tree Node Accordion
- [ ] Create `debug/templates/components/tree_node.templ`
  - TreeNodeAccordion templ function
  - Accordion trigger: type badge, offset, key count, utilization bar
  - Accordion content: NodeDetails + recursive children
  - Indentation via border-left + padding

#### 3.2.4 Node Details Component
- [ ] Create `debug/templates/components/node_details.templ`
  - Leaf: Table with Key/Value
  - Internal: Separator keys as badges

### 3.3 Implement B+Tree Handlers
- [ ] `handleBTree` - Render BTreePage template
- [ ] `handleBTreeState` - Return JSON
- [ ] `handleBTreeSSE` - SSE with 500ms updates

### 3.4 Test B+Tree Visualization
- [ ] Insert 20+ keys to trigger splits
- [ ] Verify tree structure shows hierarchy
- [ ] Verify accordion expands/collapses
- [ ] Verify utilization bars reflect capacity
- [ ] Verify statistics update in real-time

---

## Phase 4: Integration & Polish

### 4.1 Create Dashboard
- [ ] Create `debug/templates/dashboard.templ`
  - Welcome message
  - Links to /raft and /btree
  - Quick stats summary (cluster size, total keys)
  - System info (node ID, uptime)

### 4.2 Add Command-line Flags
- [ ] Update `cmd/server.go`
  - `--debug` flag (bool)
  - `--debug-port` flag (int, default 6060)
- [ ] Start debug server conditionally
- [ ] Log debug server URL on startup

### 4.3 Integrate with Main Server
- [ ] Modify Raft struct to implement RaftStateProvider
- [ ] Modify BTree struct to implement BTreeStateProvider
- [ ] Pass providers to debug.NewServer
- [ ] Start debug server in goroutine

### 4.4 Add Navigation
- [ ] Header component with nav links
- [ ] Highlight current page
- [ ] Node ID display

### 4.5 Performance Optimization
- [ ] Throttle SSE updates (configurable interval)
- [ ] Limit tree depth for large trees (expand on demand)
- [ ] Truncate log entries display (pagination)

### 4.6 Error Handling
- [ ] Handle missing leader gracefully
- [ ] Handle empty tree state
- [ ] SSE reconnection on disconnect

### 4.7 Testing
- [ ] Unit tests for GetDebugState methods
- [ ] Integration test: start server, fetch /api/raft/state
- [ ] Manual test: 3-node cluster, visualize replication
- [ ] Manual test: 1000 keys, verify B+Tree performance

### 4.8 Documentation
- [ ] Update CLAUDE.md with debug mode instructions
- [ ] Add usage examples to README
- [ ] Document API endpoints

---

## Detailed Task Estimates

| Phase | Task Count | Estimated Hours |
|-------|------------|-----------------|
| Phase 1: Infrastructure | 28 | 8-10 |
| Phase 2: Raft Viz | 16 | 6-8 |
| Phase 3: B+Tree Viz | 14 | 6-8 |
| Phase 4: Polish | 16 | 4-6 |
| **Total** | **74** | **24-32** |

---

## Files to Create Summary

```
debug/
  server.go
  config.go
  state.go
  handlers.go
  templates/
    layout.templ
    dashboard.templ
    raft.templ
    btree.templ
    components/
      cluster_overview.templ
      node_card.templ
      log_comparison.templ
      log_table.templ
      leader_state.templ
      btree_stats.templ
      tree_node.templ
      node_details.templ
  assets/
    css/
      input.css
    js/
      htmx.min.js
```

---

## Quick Start Commands

```bash
# Phase 1 setup
go install github.com/templui/templui/cmd/templui@latest
go get github.com/a-h/templ
templui init
templui add tabs table card badge progress accordion code tooltip button

# Generate and build
templ generate
npx tailwindcss -i debug/assets/css/input.css -o debug/assets/css/output.css --watch

# Run
go run ./cmd/server --debug --debug-port=6060

# Access
open http://localhost:6060
```
