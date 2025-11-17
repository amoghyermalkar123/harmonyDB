package harmonydb

import (
	"errors"
	"fmt"
)

type BTree struct {
	*fileStore
	// the very first time, a `root` node is always a leaf node
	// the recursive split logic, takes care of eventually making the `root` node
	// become an internal node
	root *Node
}

func NewBTree() *BTree {
	f, err := newFileStore("harmony.db")
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

	of := newpg.findInsPointForKey(sep)
	newpg.insertInternalCell(of, newpg.fileOffset, sep)

	return nil
}
