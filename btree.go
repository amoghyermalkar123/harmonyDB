package harmonydb

import (
	"errors"
	"fmt"
	"time"

	"harmonydb/metrics"
)

type BTree struct {
	*fileStore
	// the very first time, a `root` node is always a leaf node
	// the recursive split logic, takes care of eventually making the `root` node
	// become an internal node
	root *Node
}

func NewBTree() *BTree {
	return NewBTreeWithPath("harmony.db")
}

func NewBTreeWithPath(dbPath string) *BTree {
	f, err := newFileStore(dbPath)
	if err != nil {
		panic(fmt.Errorf("file store: %w", err))
	}

	b := &BTree{
		fileStore: f,
		root:      nil,
	}

	// root page always starts out as leaf
	b.setRootPage(&Node{isLeaf: true})

	return b
}

// in-memory store
type store struct {
	pages []*Node
}

func (b *BTree) add(pg *Node) {
	pg.setFileOffsetForNode(b.nextFreeOffset)
	b.cache[int64(pg.fileOffset)] = pg
	b.nextFreeOffset += pageSize

	// Update metrics
	metrics.BTreeTotalPages.Inc()
	metrics.BTreeCacheSizeBytes.Add(float64(pageSize))
}

func (b *BTree) fetch(fo uint64) *Node {
	return b.cache[int64(fo)]
}

func (b *BTree) getRootPage() *Node {
	return b.root
}

func (b *BTree) setRootPage(pg *Node) {
	b.add(pg)
	b.root = pg
}

func (b *BTree) put(key, val []byte) error {
	startTime := time.Now()
	defer func() {
		metrics.BTreeOperationDuration.WithLabelValues("put").Observe(time.Since(startTime).Seconds())
		metrics.BTreeOperationsTotal.WithLabelValues("put").Inc()
	}()

	pg := b.getRootPage()

	if pg.isLeaf {
		return b.insertLeaf(nil, pg, key, val)
	}

	return b.insertInternal(nil, pg, key, val)
}

func (b *BTree) Get(key []byte) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		metrics.BTreeOperationDuration.WithLabelValues("get").Observe(time.Since(startTime).Seconds())
		metrics.BTreeOperationsTotal.WithLabelValues("get").Inc()
	}()

	return b.get(b.getRootPage(), key)
}

func (b *BTree) get(next *Node, key []byte) ([]byte, error) {
	// search starts from the root page
	// we find the offset for a key in a node,
	// this continues until we find the leaf node
	// which has this exact key
	if next.isLeaf {
		cell := next.findLeafCell(key)
		if cell == nil {
			return nil, errors.New("key not found")
		}

		return cell.val, nil
	}

	// `next` is an internal node, we need to follow the pointer
	// and retrieve the child page, and recursively call `get`
	// until we find the leaf cell
	ofs := next.findChildPage(key)
	nextPg := b.fetch(next.internalCell[ofs].fileOffset)

	return b.get(nextPg, key)
}

func (b *BTree) insertLeaf(parent, curr *Node, key, value []byte) error {
	// Check if key already exists and update it
	existingCell := curr.findLeafCell(key)
	if existingCell != nil {
		// Update existing value
		existingCell.val = value
		existingCell.valSize = uint32(len(value))
		curr.markDirty()
		return nil
	}

	// Key doesn't exist, insert new cell
	ofs := curr.findInsPointForKey(key)
	curr.insertLeafCell(ofs, key, value)

	curr.markDirty()

	if !curr.isFull() {
		return nil
	}

	newpg := &Node{}
	// add split data from curr node to newly allocated node
	sep := curr.split(newpg)
	// once data is copied to new node, then make it addressable
	// and add this page to our page store
	b.add(newpg)

	// Record split metric
	metrics.BTreeSplitsTotal.WithLabelValues("leaf").Inc()

	// let's check if the parent is nil
	if parent == nil {
		// we dont have a root page

		// if so, this means the current leaf page acted as the root node
		// now that we've split this leaf node, we need to add a new root ndoe
		// which will now act as the internal node
		parentPg := &Node{}
		parentPg.appendInternalCell(curr.fileOffset, curr.leafCell[curr.offsets[0]].key)
		parentPg.appendInternalCell(newpg.fileOffset, sep)

		b.setRootPage(parentPg)
	} else {
		// now we add the seperator key to the current internal node
		// to make it reachable via our BTree
		of := parent.findInsPointForKey(sep)
		if of == uint16(len(parent.offsets)) {
			parent.appendInternalCell(newpg.fileOffset, sep)
		} else {
			parent.insertInternalCell(of, newpg.fileOffset, sep)
		}
	}

	curr.markDirty()

	return nil
}

func (b *BTree) insertInternal(parent, curr *Node, key, value []byte) error {
	// 1. find insertion point
	// 2. fetch the child page based on the calculated fileOffset
	// 3. if the child page is a leaf page, insert data
	// 	  else its internal, recursively call insertInternal until leaf is found
	// 4. check if the internal node is full, if not return nil
	// 5. if yes, split, add sep key to node

	// TODO: use a different searcher here
	offset := curr.findChildPage(key)
	fileOffset := curr.internalCell[offset].fileOffset
	childpg := b.cache[int64(fileOffset)]

	if childpg.isLeaf {
		b.insertLeaf(curr, childpg, key, value)
	} else {
		b.insertInternal(curr, childpg, key, value)
	}

	// we always work the curr node
	if !curr.isFull() {
		return nil
	}

	newpg := &Node{}
	sep := curr.split(newpg)
	b.add(newpg)

	// Record split metric
	metrics.BTreeSplitsTotal.WithLabelValues("internal").Inc()

	of := newpg.findInsPointForKey(sep)
	newpg.insertInternalCell(of, newpg.fileOffset, sep)

	return nil
}

// GetDebugState returns the current tree structure for visualization
func (b *BTree) GetDebugState() *DebugBTreeState {
	return &DebugBTreeState{
		TotalPages: len(b.cache),
		TreeHeight: b.calculateHeight(),
		TotalKeys:  b.countKeys(),
		RootNode:   b.nodeToViz(b.root),
		Timestamp:  time.Now(),
	}
}

// nodeToViz recursively converts a node to visualization format
func (b *BTree) nodeToViz(n *Node) *DebugNodeViz {
	if n == nil {
		return nil
	}

	viz := &DebugNodeViz{
		FileOffset: n.fileOffset,
		IsLeaf:     n.isLeaf,
		IsDirty:    n.isDirty,
		KeyCount:   len(n.offsets),
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
		viz.Children = make([]*DebugNodeViz, len(n.offsets))
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

// Debug state types for visualization
type DebugBTreeState struct {
	TotalPages int           `json:"total_pages"`
	TreeHeight int           `json:"tree_height"`
	TotalKeys  int           `json:"total_keys"`
	RootNode   *DebugNodeViz `json:"root"`
	Timestamp  time.Time     `json:"timestamp"`
}

type DebugNodeViz struct {
	FileOffset  uint64          `json:"file_offset"`
	IsLeaf      bool            `json:"is_leaf"`
	IsDirty     bool            `json:"is_dirty"`
	KeyCount    int             `json:"key_count"`
	Keys        []string        `json:"keys"`
	Children    []*DebugNodeViz `json:"children,omitempty"`
	Values      []string        `json:"values,omitempty"`
	Utilization float64         `json:"utilization"`
}
