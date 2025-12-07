package harmonydb

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const pageSize = 4096

type NodeType uint8

const (
	_ NodeType = iota
	LeafNode
	InternalNode
)

const (
	maxLeafNodeCells     = 9
	maxInternalNodeCells = 290
)

type internalCell struct {
	key        []byte
	fileOffset uint64
}

type leafCell struct {
	keySize uint32
	valSize uint32
	key     []byte
	val     []byte
}

//	// 1. A fixed-sized header containing the type of the node (leaf node or internal node) and the number of keys.
//
// 2. A list of pointers to the child nodes. (Used by internal nodes).
// 3. A list of oﬀsets pointing to each key-value pair.
// 4. Packed KV pairs.
// Main structure
// | type | nkeys | pointers | offsets | key-values
// | 2B | 2B | nkeys * 8B | nkeys * 2B | ...
// KV Structure
// | klen | vlen | key | val |
// | 2B | 2B | ... | ... |
type Node struct {
	isDirty    bool
	isLeaf     bool
	fileOffset uint64

	// offsets maintains an ordered list of pointers to the cells
	offsets []uint16

	// if the node is internal, this array is populated, this is un-ordered but
	// since `offsets` is ordered, this array acts as append-only
	internalCell []*internalCell
	// if the node is leaf, this array is populated, this is un-ordered but
	// since `offsets` is ordered, this array acts as append-only
	leafCell []*leafCell
}

func (n *Node) split(newpg *Node) []byte {
	newpg.isLeaf = n.isLeaf

	splitpt := len(n.offsets) / 2

	if n.isLeaf {
		for i := splitpt; i < len(n.offsets); i++ {
			// takes care of setting proper offsets as well as the leaf cells.
			// since we are starting anew for `newpg` offset should start from 0
			newpg.appendLeafCell(n.leafCell[n.offsets[i]].key, n.leafCell[n.offsets[i]].val)
		}

		// rectify offsets for old node till split point
		// since we moved the other half of the cells to the
		// new page.
		// TODO: a possible optimization here would
		// be to re-arrange the cells in the old node to reduce
		// memory usage and garbage. The leaf cells in the old
		// node still have copied over KVs to the new page, they
		// they wont be referenced as they are not in the offsets
		// but they still take up space.
		n.offsets = n.offsets[:splitpt]

		separatorKey := newpg.leafCell[newpg.offsets[0]].key

		// we return the first key of this leaf cell, this is used by the caller
		// that works on internal node. The caller shall add this key to the internal
		// node as a seperator key so that `newpg` is searchable in the btree
		return separatorKey
	}

	for i := splitpt; i < len(n.offsets); i++ {
		// takes care of setting proper offsets as well as the leaf cells.
		// since we are starting anew for `newpg` offset should start from 0
		newpg.appendInternalCell(n.internalCell[n.offsets[i]].fileOffset, n.internalCell[n.offsets[i]].key)
	}

	n.offsets = n.offsets[:splitpt]

	separatorKey := newpg.internalCell[newpg.offsets[0]].key

	return separatorKey
}

func (n *Node) isFull() bool {
	if n.isLeaf {
		return len(n.offsets) >= maxLeafNodeCells
	}

	return len(n.offsets) > maxInternalNodeCells
}

func (n *Node) setFileOffsetForNode(fo uint64) {
	n.fileOffset = fo
}

func (n *Node) appendInternalCell(fileOffset uint64, key []byte) {
	n.offsets = append(n.offsets, uint16(len(n.offsets)))
	n.internalCell = append(n.internalCell, &internalCell{
		key:        key,
		fileOffset: fileOffset,
	})
}

func (n *Node) insertInternalCell(offset uint16, fileOffset uint64, key []byte) {
	n.offsets = append(n.offsets[:offset+1], n.offsets[offset:]...)
	n.offsets[offset] = uint16(len(n.internalCell))

	n.internalCell = append(n.internalCell, &internalCell{
		key:        key,
		fileOffset: fileOffset,
	})
}

func (n *Node) appendLeafCell(key []byte, value []byte) {
	n.offsets = append(n.offsets, uint16(len(n.offsets)))

	n.leafCell = append(n.leafCell, &leafCell{
		keySize: uint32(len(key)),
		valSize: uint32(len(value)),
		key:     key,
		val:     value,
	})
}

func (n *Node) insertLeafCell(offset uint16, key []byte, value []byte) {
	if offset == uint16(len(n.leafCell)) {
		n.offsets = append(n.offsets, offset)
	} else {
		n.offsets = append(n.offsets[:offset+1], n.offsets[offset:]...)
		n.offsets[offset] = uint16(len(n.leafCell))
	}

	n.leafCell = append(n.leafCell, &leafCell{
		keySize: uint32(len(key)),
		valSize: uint32(len(value)),
		key:     key,
		val:     value,
	})
}

func (n *Node) cellKey(offset uint16) []byte {
	if n.isLeaf {
		return n.leafCell[offset].key
	}
	return n.internalCell[offset].key
}

func (n *Node) findInsPointForKey(key []byte) uint16 {
	low := 0

	high := len(n.offsets) - 1

	for low <= high {
		mid := low + (high-low)/2
		k := n.cellKey(n.offsets[mid])
		if bytes.Compare(key, k) == -1 {
			high = mid - 1
		} else if bytes.Compare(key, k) == 1 {
			low = mid + 1
		} else {
			return uint16(mid)
		}
	}

	return uint16(low)
}

func (n *Node) findChildPage(key []byte) uint16 {
	var found uint16 = 0

	for _, ofs := range n.offsets {
		if bytes.Compare(key, n.internalCell[ofs].key) >= 0 {
			found = ofs
		} else {
			break
		}
	}

	return found
}

func (n *Node) findLeafCell(key []byte) *leafCell {
	for _, cell := range n.leafCell {
		if bytes.Equal(cell.key, key) {
			return cell
		}
	}

	return nil
}

func (n *Node) encode() ([]byte, error) {
	if n.isLeaf {
		return n.encodeLeaf()
	}

	return n.encodeInternal()
}

func (n *Node) markDirty() {
	n.isDirty = true
}

func (n *Node) markClean() {
	n.isDirty = false
}

func (n *Node) encodeLeaf() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.LittleEndian, LeafNode); err != nil {
		return nil, err
	}

	// Write the number of offsets first
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(n.offsets))); err != nil {
		return nil, err
	}

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, ofs); err != nil {
			return nil, err
		}
	}

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, n.leafCell[ofs].keySize); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.leafCell[ofs].valSize); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.leafCell[ofs].key); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.leafCell[ofs].val); err != nil {
			return nil, err
		}
	}

	// Pad to pageSize
	currentSize := buf.Len()
	if currentSize+2 > pageSize {
		return nil, fmt.Errorf("leaf node data size %d exceeds page size %d", currentSize, pageSize)
	}

	freeSize := uint16(pageSize - currentSize - 2)
	if err := binary.Write(buf, binary.LittleEndian, freeSize); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.LittleEndian, make([]byte, freeSize)); err != nil {
		return nil, err
	}

	if buf.Len() != pageSize {
		panic(fmt.Sprintf("leaf page size is not %d bytes, got %d\n", pageSize, buf.Len()))
	}

	return buf.Bytes(), nil
}

func (n *Node) encodeInternal() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.LittleEndian, InternalNode); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.LittleEndian, n.fileOffset); err != nil {
		return nil, err
	}

	cellCount := uint32(len(n.offsets))
	if err := binary.Write(buf, binary.LittleEndian, cellCount); err != nil {
		return nil, err
	}

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, ofs); err != nil {
			return nil, err
		}
	}

	for _, ofs := range n.offsets {
		// Write key length first so we can decode it
		keyLen := uint32(len(n.internalCell[ofs].key))
		if err := binary.Write(buf, binary.LittleEndian, keyLen); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.internalCell[ofs].key); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.internalCell[ofs].fileOffset); err != nil {
			return nil, err
		}
	}

	// write freesize to ensure 4096 page
	currentSize := buf.Len()
	if currentSize+2 > pageSize {
		return nil, fmt.Errorf("encodeInternal: internal node data size %d exceeds page size %d", currentSize, pageSize)
	}

	freeSize := uint16(pageSize - currentSize - 2)

	if err := binary.Write(buf, binary.LittleEndian, freeSize); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.LittleEndian, make([]byte, freeSize)); err != nil {
		return nil, err
	}

	if buf.Len() != pageSize {
		panic(fmt.Sprintf("page size is not %d bytes, got %d\n", pageSize, buf.Len()))
	}

	return buf.Bytes(), nil
}

// the `data` should be a raw page read at the specific offset
func (n *Node) decode(data []byte) error {
	buf := bytes.NewBuffer(data)

	// read the type of node
	var nodeType NodeType
	if err := binary.Read(buf, binary.LittleEndian, &nodeType); err != nil {
		return err
	}

	// decode the page to in-mem
	if nodeType == LeafNode {
		return n.decodeLeaf(buf)
	}

	return n.decodeInternal(buf)
}

func (n *Node) decodeLeaf(buf *bytes.Buffer) error {
	n.isLeaf = true

	var offsetCount uint16
	if err := binary.Read(buf, binary.LittleEndian, &offsetCount); err != nil {
		return err
	}

	n.offsets = make([]uint16, offsetCount)
	for i := 0; i < int(offsetCount); i++ {
		if err := binary.Read(buf, binary.LittleEndian, &n.offsets[i]); err != nil {
			return err
		}
	}

	n.leafCell = make([]*leafCell, 0, offsetCount)
	for i := 0; i < int(offsetCount); i++ {
		cell := &leafCell{}

		if err := binary.Read(buf, binary.LittleEndian, &cell.keySize); err != nil {
			return err
		}

		if err := binary.Read(buf, binary.LittleEndian, &cell.valSize); err != nil {
			return err
		}

		cell.key = make([]byte, cell.keySize)
		if err := binary.Read(buf, binary.LittleEndian, &cell.key); err != nil {
			return err
		}

		cell.val = make([]byte, cell.valSize)
		if err := binary.Read(buf, binary.LittleEndian, &cell.val); err != nil {
			return err
		}

		n.leafCell = append(n.leafCell, cell)
	}

	return nil
}

func (n *Node) decodeInternal(buf *bytes.Buffer) error {
	n.isLeaf = false

	if err := binary.Read(buf, binary.LittleEndian, &n.fileOffset); err != nil {
		return err
	}

	var cellCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &cellCount); err != nil {
		return err
	}

	n.offsets = make([]uint16, cellCount)
	for i := 0; i < int(cellCount); i++ {
		if err := binary.Read(buf, binary.LittleEndian, &n.offsets[i]); err != nil {
			return err
		}
	}

	n.internalCell = make([]*internalCell, 0, cellCount)
	for i := 0; i < int(cellCount); i++ {
		cell := &internalCell{}

		var keyLen uint32
		if err := binary.Read(buf, binary.LittleEndian, &keyLen); err != nil {
			return err
		}

		cell.key = make([]byte, keyLen)
		if err := binary.Read(buf, binary.LittleEndian, &cell.key); err != nil {
			return err
		}

		if err := binary.Read(buf, binary.LittleEndian, &cell.fileOffset); err != nil {
			return err
		}

		n.internalCell = append(n.internalCell, cell)
	}

	return nil
}
