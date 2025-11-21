# Debug Visualization Implementation Plan

## Technology Stack

- **UI Framework**: templui (Go templ components)
- **Backend**: Go net/http
- **Real-time**: HTMX with SSE (Server-Sent Events)
- **Styling**: Tailwind CSS v4.1+
- **Templating**: templ v0.3.960+

## Prerequisites

```bash
# Install templui CLI
go install github.com/templui/templui/cmd/templui@latest

# Initialize in project
cd harmonydb
templui init

# Install required components
templui add tabs table card badge progress accordion code tooltip button
```

## File Structure

```
harmonydb/
  debug/
    server.go              # HTTP server, SSE handlers
    handlers.go            # Route handlers
    state.go               # Debug state types
    templates/
      layout.templ         # Base layout with templui setup
      dashboard.templ      # Main dashboard
      raft.templ           # Raft visualization page
      btree.templ          # B+Tree visualization page
      components/
        node_card.templ    # Raft node card component
        log_table.templ    # Log entries table
        tree_node.templ    # B+Tree node accordion
        stats_card.templ   # Statistics display
    assets/
      css/
        input.css          # Tailwind input
      js/
        htmx.min.js        # HTMX for dynamic updates
        sse.js             # SSE client utilities
```

## Implementation Phases

### Phase 1: Infrastructure Setup

**Tasks:**
1. Initialize templui in project
2. Create debug server with basic routing
3. Set up templ layout with Tailwind
4. Add HTMX for dynamic content updates

**Files to create:**

`debug/server.go`:
```go
package debug

import (
    "net/http"
    "time"
)

type Server struct {
    raft   RaftStateProvider
    btree  BTreeStateProvider
    port   int
}

func NewServer(raft RaftStateProvider, btree BTreeStateProvider, port int) *Server {
    return &Server{raft: raft, btree: btree, port: port}
}

func (s *Server) Start() error {
    mux := http.NewServeMux()

    // Pages
    mux.HandleFunc("/", s.handleDashboard)
    mux.HandleFunc("/raft", s.handleRaft)
    mux.HandleFunc("/btree", s.handleBTree)

    // API endpoints
    mux.HandleFunc("/api/raft/state", s.handleRaftState)
    mux.HandleFunc("/api/btree/state", s.handleBTreeState)

    // SSE endpoints for real-time updates
    mux.HandleFunc("/sse/raft", s.handleRaftSSE)
    mux.HandleFunc("/sse/btree", s.handleBTreeSSE)

    // Static assets
    mux.Handle("/assets/", http.StripPrefix("/assets/",
        http.FileServer(http.Dir("debug/assets"))))

    return http.ListenAndServe(fmt.Sprintf(":%d", s.port), mux)
}
```

### Phase 2: Raft Visualization

**templui components used:**
- `tabs` - Switch between cluster view and log comparison
- `card` - Display each node's state
- `badge` - Show node role (Leader/Follower/Candidate)
- `table` - Display log entries
- `progress` - Show replication progress per follower
- `tooltip` - Additional info on hover
- `code` - Display command details

**Template: `debug/templates/raft.templ`**

```go
package templates

import (
    "harmonydb/debug/templates/components"
    "github.com/templui/templui/components/tabs"
    "github.com/templui/templui/components/card"
    "github.com/templui/templui/components/badge"
    "github.com/templui/templui/components/table"
    "github.com/templui/templui/components/progress"
)

templ RaftPage(state *ClusterState) {
    @Layout("Raft Cluster") {
        <div hx-ext="sse" sse-connect="/sse/raft" sse-swap="message">
            @tabs.Tabs(tabs.Props{ID: "raft-tabs"}) {
                @tabs.List() {
                    @tabs.Trigger(tabs.TriggerProps{Value: "cluster", IsActive: true}) {
                        Cluster Overview
                    }
                    @tabs.Trigger(tabs.TriggerProps{Value: "logs"}) {
                        Log Comparison
                    }
                    @tabs.Trigger(tabs.TriggerProps{Value: "leader"}) {
                        Leader State
                    }
                }

                @tabs.Content(tabs.ContentProps{Value: "cluster", IsActive: true}) {
                    @ClusterOverview(state)
                }

                @tabs.Content(tabs.ContentProps{Value: "logs"}) {
                    @LogComparison(state)
                }

                @tabs.Content(tabs.ContentProps{Value: "leader"}) {
                    @LeaderState(state)
                }
            }
        </div>
    }
}

templ ClusterOverview(state *ClusterState) {
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4 p-4">
        for _, node := range state.Nodes {
            @NodeCard(node)
        }
    </div>
}

templ NodeCard(node *NodeState) {
    @card.Card() {
        @card.Header() {
            @card.Title() {
                Node { fmt.Sprintf("%d", node.NodeID) }
            }
            @card.Description() {
                @RoleBadge(node.Role)
            }
        }
        @card.Content() {
            <div class="space-y-2">
                <p>Term: { fmt.Sprintf("%d", node.Term) }</p>
                <p>Commit Index: { fmt.Sprintf("%d", node.CommitIndex) }</p>
                <p>Last Applied: { fmt.Sprintf("%d", node.LastApplied) }</p>
                <p>Log Entries: { fmt.Sprintf("%d", len(node.LogEntries)) }</p>
            </div>
        }
    }
}

templ RoleBadge(role string) {
    if role == "leader" {
        @badge.Badge(badge.Props{Variant: badge.VariantPrimary}) {
            Leader
        }
    } else if role == "candidate" {
        @badge.Badge(badge.Props{Variant: badge.VariantOutline}) {
            Candidate
        }
    } else {
        @badge.Badge(badge.Props{Variant: badge.VariantSecondary}) {
            Follower
        }
    }
}

templ LogComparison(state *ClusterState) {
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4 p-4">
        for _, node := range state.Nodes {
            @card.Card() {
                @card.Header() {
                    @card.Title() {
                        Node { fmt.Sprintf("%d", node.NodeID) }
                    }
                }
                @card.Content() {
                    @LogTable(node.LogEntries, node.CommitIndex)
                }
            }
        }
    </div>
}

templ LogTable(entries []LogEntry, commitIndex int64) {
    @table.Table() {
        @table.Header() {
            @table.Row() {
                @table.Head() { ID }
                @table.Head() { Term }
                @table.Head() { Key }
                @table.Head() { Status }
            }
        }
        @table.Body() {
            for _, entry := range entries {
                @table.Row() {
                    @table.Cell() { { fmt.Sprintf("%d", entry.ID) } }
                    @table.Cell() { { fmt.Sprintf("%d", entry.Term) } }
                    @table.Cell() { { entry.Key } }
                    @table.Cell() {
                        if entry.Committed {
                            @badge.Badge(badge.Props{Variant: badge.VariantPrimary}) {
                                Committed
                            }
                        } else {
                            @badge.Badge(badge.Props{Variant: badge.VariantOutline}) {
                                Pending
                            }
                        }
                    }
                }
            }
        }
    }
}

templ LeaderState(state *ClusterState) {
    for _, node := range state.Nodes {
        if node.Role == "leader" {
            @card.Card() {
                @card.Header() {
                    @card.Title() {
                        Leader Replication State
                    }
                }
                @card.Content() {
                    <div class="space-y-4">
                        for peerID, nextIdx := range node.NextIndex {
                            <div class="space-y-1">
                                <div class="flex justify-between">
                                    <span>Node { fmt.Sprintf("%d", peerID) }</span>
                                    <span>Next: { fmt.Sprintf("%d", nextIdx) } / Match: { fmt.Sprintf("%d", node.MatchIndex[peerID]) }</span>
                                </div>
                                @progress.Progress(progress.Props{
                                    Value: int(float64(node.MatchIndex[peerID]) / float64(len(node.LogEntries)) * 100),
                                    Variant: progress.VariantSuccess,
                                })
                            </div>
                        }
                    </div>
                }
            }
        }
    }
}
```

### Phase 3: B+Tree Visualization

**templui components used:**
- `accordion` - Collapsible tree nodes
- `card` - Node detail panels
- `badge` - Node type (Leaf/Internal)
- `progress` - Node utilization
- `table` - Key-value display
- `code` - Raw node data

**Template: `debug/templates/btree.templ`**

```go
package templates

import (
    "github.com/templui/templui/components/accordion"
    "github.com/templui/templui/components/card"
    "github.com/templui/templui/components/badge"
    "github.com/templui/templui/components/progress"
    "github.com/templui/templui/components/table"
)

templ BTreePage(state *BTreeState) {
    @Layout("B+Tree") {
        <div hx-ext="sse" sse-connect="/sse/btree" sse-swap="message">
            <div class="grid grid-cols-4 gap-4 p-4">
                // Statistics sidebar
                <div class="col-span-1">
                    @BTreeStats(state)
                </div>

                // Tree view
                <div class="col-span-3">
                    @card.Card() {
                        @card.Header() {
                            @card.Title() { Tree Structure }
                        }
                        @card.Content() {
                            @TreeNodeAccordion(state.RootNode, 0)
                        }
                    }
                </div>
            </div>
        </div>
    }
}

templ BTreeStats(state *BTreeState) {
    @card.Card() {
        @card.Header() {
            @card.Title() { Statistics }
        }
        @card.Content() {
            <div class="space-y-3">
                <div>
                    <p class="text-sm text-gray-500">Total Pages</p>
                    <p class="text-2xl font-bold">{ fmt.Sprintf("%d", state.TotalPages) }</p>
                </div>
                <div>
                    <p class="text-sm text-gray-500">Tree Height</p>
                    <p class="text-2xl font-bold">{ fmt.Sprintf("%d", state.TreeHeight) }</p>
                </div>
                <div>
                    <p class="text-sm text-gray-500">Total Keys</p>
                    <p class="text-2xl font-bold">{ fmt.Sprintf("%d", state.TotalKeys) }</p>
                </div>
            </div>
        }
    }
}

templ TreeNodeAccordion(node *NodeViz, depth int) {
    if node == nil {
        return
    }

    @accordion.Accordion() {
        @accordion.Item() {
            @accordion.Trigger() {
                <div class="flex items-center gap-2">
                    if node.IsLeaf {
                        @badge.Badge(badge.Props{Variant: badge.VariantPrimary}) {
                            Leaf
                        }
                    } else {
                        @badge.Badge(badge.Props{Variant: badge.VariantSecondary}) {
                            Internal
                        }
                    }
                    <span>Offset: { fmt.Sprintf("%d", node.FileOffset) }</span>
                    <span>Keys: { fmt.Sprintf("%d", node.KeyCount) }</span>
                    @progress.Progress(progress.Props{
                        Value: int(node.Utilization),
                        Size: progress.SizeSmall,
                    })
                </div>
            }
            @accordion.Content() {
                <div class="pl-4 space-y-2">
                    // Node details
                    @NodeDetails(node)

                    // Children (for internal nodes)
                    if !node.IsLeaf && len(node.Children) > 0 {
                        <div class="mt-2 border-l-2 border-gray-200 pl-4">
                            for _, child := range node.Children {
                                @TreeNodeAccordion(child, depth+1)
                            }
                        </div>
                    }
                </div>
            }
        }
    }
}

templ NodeDetails(node *NodeViz) {
    if node.IsLeaf {
        @table.Table() {
            @table.Header() {
                @table.Row() {
                    @table.Head() { Key }
                    @table.Head() { Value }
                }
            }
            @table.Body() {
                for i, key := range node.Keys {
                    @table.Row() {
                        @table.Cell() { { key } }
                        @table.Cell() { { node.Values[i] } }
                    }
                }
            }
        }
    } else {
        <div class="space-y-1">
            <p class="font-medium">Separator Keys:</p>
            <div class="flex flex-wrap gap-1">
                for _, key := range node.Keys {
                    @badge.Badge(badge.Props{Variant: badge.VariantOutline}) {
                        { key }
                    }
                }
            </div>
        </div>
    }
}
```

### Phase 4: SSE Handlers for Real-time Updates

`debug/handlers.go`:
```go
package debug

import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

func (s *Server) handleRaftSSE(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            state := s.raft.GetDebugState()

            // Render the updated component
            var buf bytes.Buffer
            templates.ClusterOverview(state).Render(r.Context(), &buf)

            fmt.Fprintf(w, "data: %s\n\n", buf.String())
            flusher.Flush()
        }
    }
}

func (s *Server) handleBTreeSSE(w http.ResponseWriter, r *http.Request) {
    // Similar implementation for B+Tree updates
}
```

## Task Breakdown

### Week 1: Setup & Infrastructure
- [ ] Install templui CLI and dependencies
- [ ] Initialize templui in project (`templui init`)
- [ ] Add required components
- [ ] Create base layout template with Tailwind
- [ ] Set up debug HTTP server
- [ ] Implement basic routing

### Week 2: Raft Visualization
- [ ] Add `GetDebugState()` to raftNode
- [ ] Create Raft page template with tabs
- [ ] Implement ClusterOverview component
- [ ] Implement LogComparison component
- [ ] Implement LeaderState with progress bars
- [ ] Add SSE endpoint for real-time updates

### Week 3: B+Tree Visualization
- [ ] Add `GetDebugState()` to BTree
- [ ] Create B+Tree page template
- [ ] Implement recursive TreeNodeAccordion
- [ ] Implement NodeDetails component
- [ ] Implement Statistics card
- [ ] Add SSE endpoint for real-time updates

### Week 4: Integration & Polish
- [ ] Create unified dashboard
- [ ] Add command-line flags (--debug, --debug-port)
- [ ] Test with multi-node cluster
- [ ] Performance optimization
- [ ] Documentation

## Commands to Run

```bash
# Initialize templui
templui init

# Add components
templui add tabs table card badge progress accordion code tooltip button

# Generate templ files
templ generate

# Build CSS
npx tailwindcss -i debug/assets/css/input.css -o debug/assets/css/output.css

# Run with debug mode
go run ./cmd/server --debug --debug-port=6060
```

## Dependencies to Add

```bash
go get github.com/a-h/templ
```

`go.mod` additions:
```
require (
    github.com/a-h/templ v0.3.960
)
```
