package harmonydb

import (
	"bytes"
	"encoding/binary"
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

// 1. A fixed-sized header containing the type of the node (leaf node or internal node) and the number of keys.
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

		// we return the first key of this leaf cell, this is used by the caller
		// that works on internal node. The caller shall add this key to the internal
		// node as a seperator key so that `newpg` is searchable in the btree
		return newpg.leafCell[newpg.offsets[0]].key
	}

	for i := splitpt; i < len(n.offsets); i++ {
		// takes care of setting proper offsets as well as the leaf cells.
		// since we are starting anew for `newpg` offset should start from 0
		newpg.appendInternalCell(n.internalCell[n.offsets[i]].fileOffset, n.internalCell[n.offsets[i]].key)
	}

	n.offsets = n.offsets[:splitpt]

	return newpg.internalCell[newpg.offsets[0]].key
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
		k := n.cellKey(uint16(mid))
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

	// TODO: return last cell file offset when `key` is greater than all
	for _, ofs := range n.offsets {
		if bytes.Compare(key, n.internalCell[ofs].key) > 0 {
			found = ofs
		} else {
			return found
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

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, ofs); err != nil {
			return nil, err
		}
	}

	for _, cell := range n.leafCell {
		if err := binary.Write(buf, binary.LittleEndian, cell.keySize); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, cell.valSize); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, cell.key); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, cell.val); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func (n *Node) encodeInternal() ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.LittleEndian, InternalNode); err != nil {
		return nil, err
	}

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, ofs); err != nil {
			return nil, err
		}
	}

	for _, ofs := range n.offsets {
		if err := binary.Write(buf, binary.LittleEndian, n.internalCell[ofs].key); err != nil {
			return nil, err
		}

		if err := binary.Write(buf, binary.LittleEndian, n.internalCell[ofs].fileOffset); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func (n *Node) decodeLeaf() {}

func (n *Node) decodeInternal() {}
