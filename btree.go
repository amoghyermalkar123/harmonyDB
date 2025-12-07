package harmonydb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"go.uber.org/zap"
)

type BTree struct {
	*fileStore
	// the very first time, a `root` node is always a leaf node
	// the recursive split logic, takes care of eventually making the `root` node
	// become an internal node
	root   *Node
	logger *zap.Logger
	meta   *os.File
}

type metaPage struct {
	LastApplied int64
}

func (b *BTree) readMeta() int64 {
	if _, err := b.meta.Seek(0, io.SeekStart); err != nil {
		panic(fmt.Errorf("readMeta: seek: %w", err))
	}

	var meta metaPage
	if err := binary.Read(b.meta, binary.LittleEndian, &meta); err != nil {
		if err == io.EOF {
			return 0
		}

		panic(fmt.Errorf("readMeta: read: %w", err))
	}

	return meta.LastApplied
}

func (b *BTree) updateMeta(lastApplied int64) {
	if _, err := b.meta.Seek(0, io.SeekStart); err != nil {
		panic(fmt.Errorf("updateMeta: seek: %w", err))
	}

	if err := binary.Write(b.meta, binary.LittleEndian, &metaPage{LastApplied: lastApplied}); err != nil {
		panic(fmt.Errorf("updateMeta: write: %w", err))
	}

	if err := b.meta.Sync(); err != nil {
		panic(fmt.Errorf("updateMeta: sync: %w", err))
	}
}

func NewBTree() *BTree {
	return NewBTreeWithPath("harmony.db")
}

func NewBTreeWithPath(dbPath string) *BTree {
	f, err := newFileStore(dbPath)
	if err != nil {
		panic(fmt.Errorf("file store: %w", err))
	}

	metaDataPage, err := os.OpenFile(fmt.Sprintf("%s.meta", dbPath), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		panic(fmt.Errorf("file store: meta: %w", err))
	}

	b := &BTree{
		fileStore: f,
		root:      nil,
		logger:    GetStructuredLogger("btree"),
		meta:      metaDataPage,
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
	b.pageCache.add(pg)
	b.nextFreeOffset += pageSize
}

func (b *BTree) fetch(fo uint64) *Node {
	if n := b.pageCache.fetch(fo); n != nil {
		return n
	}

	paged := make([]byte, 4096)

	_, err := b.file.ReadAt(paged, int64(fo))
	if err != nil {
		panic(fmt.Errorf("fetch: read: %w", err))
	}

	node := &Node{}
	if err := node.decode(paged); err != nil {
		panic(fmt.Errorf("fetch: decode: %w", err))
	}

	node.fileOffset = fo

	b.pageCache.add(node)

	return node
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
	ofs := next.findChildPage(key)
	childOffset := next.internalCell[ofs].fileOffset

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

	offset := curr.findChildPage(key)
	fileOffset := curr.internalCell[offset].fileOffset
	childpg := b.pageCache.fetch(fileOffset)

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
	if of == uint16(len(parent.offsets)) {
		parent.appendInternalCell(newpg.fileOffset, sep)
	} else {
		parent.insertInternalCell(of, newpg.fileOffset, sep)
	}

	return nil
}
