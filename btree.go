package harmonydb

import (
	"errors"
	"fmt"

	"go.uber.org/zap"
)

type BTree struct {
	*fileStore
	// the very first time, a `root` node is always a leaf node
	// the recursive split logic, takes care of eventually making the `root` node
	// become an internal node
	root   *Node
	logger *zap.Logger
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
		logger:    GetStructuredLogger("btree"),
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
	b.cache.add(pg)
	b.nextFreeOffset += pageSize
}

func (b *BTree) fetch(fo uint64) *Node {
	return b.cache.fetch(fo)
}

func (b *BTree) getRootPage() *Node {
	return b.root
}

func (b *BTree) setRootPage(pg *Node) {
	b.add(pg)
	b.root = pg
}

func (b *BTree) put(key, val []byte) error {
	pg := b.getRootPage()

	if pg.isLeaf {
		return b.insertLeaf(nil, pg, key, val)
	}

	return b.insertInternal(nil, pg, key, val)
}

func (b *BTree) Get(key []byte) ([]byte, error) {
	return b.get(b.getRootPage(), key)
}

func (b *BTree) get(next *Node, key []byte) ([]byte, error) {
	// search starts from the root page
	// we find the offset for a key in a node,
	// this continues until we find the leaf node
	// which has this exact key
	if next.isLeaf {
		b.logger.Debug("Searching in leaf node",
			zap.String("operation", "search"),
			zap.String("key", string(key)),
			zap.Uint64("page_offset", next.fileOffset),
			zap.Int("cell_count", len(next.offsets)))

		cell := next.findLeafCell(key)
		if cell == nil {
			b.logger.Debug("Key not found in leaf node",
				zap.String("operation", "search"),
				zap.String("key", string(key)),
				zap.Uint64("page_offset", next.fileOffset))
			return nil, errors.New("key not found")
		}

		b.logger.Debug("Key found in leaf node",
			zap.String("operation", "search"),
			zap.String("key", string(key)),
			zap.Uint64("page_offset", next.fileOffset),
			zap.Int("value_size", len(cell.val)))

		return cell.val, nil
	}

	// `next` is an internal node, we need to follow the pointer
	// and retrieve the child page, and recursively call `get`
	ofs := next.findChildPage(key)
	childOffset := next.internalCell[ofs].fileOffset

	b.logger.Debug("Traversing internal node",
		zap.String("operation", "search"),
		zap.String("key", string(key)),
		zap.Uint64("current_page_offset", next.fileOffset),
		zap.Uint64("child_page_offset", childOffset),
		zap.Uint16("child_index", ofs))

	nextPg := b.fetch(childOffset)

	return b.get(nextPg, key)
}

func (b *BTree) insertLeaf(parent, curr *Node, key, value []byte) error {
	// Check if key already exists and update it
	existingCell := curr.findLeafCell(key)
	if existingCell != nil {
		// TODO: this doesn't work for some reason
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
		b.logger.Debug("Leaf insertion completed without split",
			zap.String("operation", "split"),
			zap.String("key", string(key)),
			zap.Uint64("page_offset", curr.fileOffset),
			zap.Int("cell_count", len(curr.offsets)))
		return nil
	}

	b.logger.Debug("Leaf node is full, initiating split",
		zap.String("operation", "split"),
		zap.String("key", string(key)),
		zap.Uint64("current_page_offset", curr.fileOffset),
		zap.Int("current_cell_count", len(curr.offsets)))

	newpg := &Node{}
	// add split data from curr node to newly allocated node
	sep := curr.split(newpg)
	// once data is copied to new node, then make it addressable
	// and add this page to our page store
	b.add(newpg)

	b.logger.Debug("Leaf split completed",
		zap.String("operation", "split"),
		zap.String("separator_key", string(sep)),
		zap.Uint64("original_page_offset", curr.fileOffset),
		zap.Uint64("new_page_offset", newpg.fileOffset),
		zap.Int("original_cell_count", len(curr.offsets)),
		zap.Int("new_cell_count", len(newpg.offsets)))

	// let's check if the parent is nil
	if parent == nil {
		// we dont have a root page

		// if so, this means the current leaf page acted as the root node
		// now that we've split this leaf node, we need to add a new root ndoe
		// which will now act as the internal node
		b.logger.Debug("Creating new root node after leaf split",
			zap.String("operation", "split"),
			zap.String("separator_key", string(sep)),
			zap.Uint64("left_child_offset", curr.fileOffset),
			zap.Uint64("right_child_offset", newpg.fileOffset))

		parentPg := &Node{}
		parentPg.appendInternalCell(curr.fileOffset, curr.leafCell[curr.offsets[0]].key)
		parentPg.appendInternalCell(newpg.fileOffset, sep)

		b.setRootPage(parentPg)

		b.logger.Debug("New root node created",
			zap.String("operation", "split"),
			zap.Uint64("new_root_offset", parentPg.fileOffset))
	} else {
		// now we add the seperator key to the current internal node
		// to make it reachable via our BTree
		of := parent.findInsPointForKey(sep)

		b.logger.Debug("Adding separator to parent internal node",
			zap.String("operation", "split"),
			zap.String("separator_key", string(sep)),
			zap.Uint64("parent_offset", parent.fileOffset),
			zap.Uint64("new_page_offset", newpg.fileOffset),
			zap.Uint16("insertion_point", of))

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

	offset := curr.findChildPage(key)
	fileOffset := curr.internalCell[offset].fileOffset
	childpg := b.cache.fetch(fileOffset)

	b.logger.Debug("Inserting into internal node",
		zap.String("operation", "split"),
		zap.String("key", string(key)),
		zap.Uint64("internal_node_offset", curr.fileOffset),
		zap.Uint64("child_page_offset", fileOffset),
		zap.Bool("child_is_leaf", childpg.isLeaf))

	if childpg.isLeaf {
		b.insertLeaf(curr, childpg, key, value)
	} else {
		b.insertInternal(curr, childpg, key, value)
	}

	// we always work the curr node
	if !curr.isFull() {
		b.logger.Debug("Internal node insertion completed without split",
			zap.String("operation", "split"),
			zap.Uint64("internal_node_offset", curr.fileOffset),
			zap.Int("cell_count", len(curr.offsets)))
		return nil
	}

	b.logger.Debug("Internal node is full, initiating split",
		zap.String("operation", "split"),
		zap.Uint64("current_internal_offset", curr.fileOffset),
		zap.Int("current_cell_count", len(curr.offsets)))

	newpg := &Node{}
	sep := curr.split(newpg)
	b.add(newpg)

	b.logger.Debug("Internal split completed",
		zap.String("operation", "split"),
		zap.String("separator_key", string(sep)),
		zap.Uint64("original_internal_offset", curr.fileOffset),
		zap.Uint64("new_internal_offset", newpg.fileOffset),
		zap.Int("original_cell_count", len(curr.offsets)),
		zap.Int("new_cell_count", len(newpg.offsets)))

	of := newpg.findInsPointForKey(sep)
	if of == uint16(len(parent.offsets)) {
		parent.appendInternalCell(newpg.fileOffset, sep)
	} else {
		parent.insertInternalCell(of, newpg.fileOffset, sep)
	}

	return nil
}
