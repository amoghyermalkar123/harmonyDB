package harmonydb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeCreation(t *testing.T) {
	t.Run("create leaf node", func(t *testing.T) {
		node := &Node{isLeaf: true}
		assert.True(t, node.isLeaf)
		assert.Equal(t, uint64(0), node.fileOffset)
	})

	t.Run("create internal node", func(t *testing.T) {
		node := &Node{isLeaf: false}
		assert.False(t, node.isLeaf)
	})
}

func TestSetFileOffset(t *testing.T) {
	node := &Node{isLeaf: true}
	node.setFileOffsetForNode(42)
	assert.Equal(t, uint64(42), node.fileOffset)
}

func TestLeafCellOperations(t *testing.T) {
	t.Run("insert at end", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("key1"), []byte("value1"))

		cell := node.findLeafCell([]byte("key1"))
		require.NotNil(t, cell)
		assert.Equal(t, []byte("value1"), cell.val)
	})

	t.Run("insert multiple cells", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("a"), []byte("val_a"))
		node.insertLeafCell(1, []byte("c"), []byte("val_c"))
		node.insertLeafCell(1, []byte("b"), []byte("val_b"))

		assert.Len(t, node.offsets, 3)
		assert.Len(t, node.leafCell, 3)
	})

	t.Run("find existing cell", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("test"), []byte("data"))

		cell := node.findLeafCell([]byte("test"))
		require.NotNil(t, cell)
		assert.Equal(t, []byte("data"), cell.val)
		assert.Equal(t, uint32(4), cell.keySize)
		assert.Equal(t, uint32(4), cell.valSize)
	})

	t.Run("find non-existing cell", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("test"), []byte("data"))

		cell := node.findLeafCell([]byte("missing"))
		assert.Nil(t, cell)
	})

	t.Run("empty key and value", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte(""), []byte(""))

		cell := node.findLeafCell([]byte(""))
		require.NotNil(t, cell)
		assert.Equal(t, []byte(""), cell.val)
		assert.Equal(t, uint32(0), cell.keySize)
		assert.Equal(t, uint32(0), cell.valSize)
	})

	t.Run("large key and value", func(t *testing.T) {
		node := &Node{isLeaf: true}
		largeKey := bytes.Repeat([]byte("k"), 200)
		largeVal := bytes.Repeat([]byte("v"), 2000)

		node.insertLeafCell(0, largeKey, largeVal)

		cell := node.findLeafCell(largeKey)
		require.NotNil(t, cell)
		assert.Equal(t, largeVal, cell.val)
		assert.Equal(t, uint32(200), cell.keySize)
		assert.Equal(t, uint32(2000), cell.valSize)
	})
}

func TestInternalCellOperations(t *testing.T) {
	t.Run("append internal cell", func(t *testing.T) {
		node := &Node{isLeaf: false}
		node.appendInternalCell(10, []byte("separator"))

		assert.Len(t, node.internalCell, 1)
		assert.Equal(t, uint64(10), node.internalCell[0].fileOffset)
		assert.Equal(t, []byte("separator"), node.internalCell[0].key)
	})

	t.Run("insert internal cell at position", func(t *testing.T) {
		node := &Node{isLeaf: false}
		node.appendInternalCell(10, []byte("a"))
		node.appendInternalCell(30, []byte("c"))
		node.insertInternalCell(1, 20, []byte("b"))

		assert.Len(t, node.internalCell, 3)
		assert.Len(t, node.offsets, 3)
	})

	t.Run("multiple internal cells", func(t *testing.T) {
		node := &Node{isLeaf: false}
		for i := 0; i < 5; i++ {
			node.appendInternalCell(uint64(i*10), []byte{byte('a' + i)})
		}

		assert.Len(t, node.internalCell, 5)
	})
}

func TestCellKey(t *testing.T) {
	t.Run("leaf cell key", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("mykey"), []byte("myval"))

		key := node.cellKey(0)
		assert.Equal(t, []byte("mykey"), key)
	})

	t.Run("internal cell key", func(t *testing.T) {
		node := &Node{isLeaf: false}
		node.appendInternalCell(5, []byte("separator"))
		node.offsets = []uint16{0}

		key := node.cellKey(0)
		assert.Equal(t, []byte("separator"), key)
	})
}

func TestFindInsertionPoint(t *testing.T) {
	t.Run("empty node", func(t *testing.T) {
		node := &Node{isLeaf: true}
		pos := node.findInsPointForKey([]byte("any"))
		assert.Equal(t, uint16(0), pos)
	})

	t.Run("single element", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("m"), []byte("val"))

		posBefore := node.findInsPointForKey([]byte("a"))
		assert.Equal(t, uint16(0), posBefore)

		posAfter := node.findInsPointForKey([]byte("z"))
		assert.True(t, posAfter <= uint16(len(node.offsets)))
	})

	t.Run("multiple elements ordered", func(t *testing.T) {
		node := &Node{isLeaf: true}
		keys := [][]byte{[]byte("a"), []byte("e"), []byte("m"), []byte("r"), []byte("z")}

		for i, key := range keys {
			node.insertLeafCell(uint16(i), key, []byte("val"))
		}

		pos := node.findInsPointForKey([]byte("g"))
		assert.True(t, pos >= 0 && pos < uint16(len(node.offsets)))
	})
}

func TestNodeSplit(t *testing.T) {
	t.Run("split leaf node even count", func(t *testing.T) {
		node := &Node{isLeaf: true}
		for i := 0; i < 4; i++ {
			node.insertLeafCell(uint16(i), []byte{byte('a' + i)}, []byte("val"))
		}

		newNode := &Node{}
		separator := node.split(newNode)

		assert.True(t, newNode.isLeaf)
		assert.NotNil(t, separator)
		assert.Greater(t, len(newNode.offsets), 0)
		assert.Greater(t, len(node.offsets), 0)
	})

	t.Run("split leaf node odd count", func(t *testing.T) {
		node := &Node{isLeaf: true}
		for i := 0; i < 9; i++ {
			node.insertLeafCell(uint16(i), []byte{byte('a' + i)}, []byte("val"))
		}

		newNode := &Node{}
		separator := node.split(newNode)

		assert.NotNil(t, separator)
		assert.Greater(t, len(newNode.leafCell), 0)
		assert.Greater(t, len(node.leafCell), 0)
	})

	t.Run("split internal node", func(t *testing.T) {
		node := &Node{isLeaf: false}
		for i := 0; i < 4; i++ {
			node.appendInternalCell(uint64(i*10), []byte{byte('a' + i)})
		}

		newNode := &Node{}
		separator := node.split(newNode)

		assert.False(t, newNode.isLeaf)
		assert.NotNil(t, separator)
		assert.Greater(t, len(newNode.internalCell), 0)
		assert.Greater(t, len(node.internalCell), 0)
	})

	t.Run("separator key is first of new node", func(t *testing.T) {
		node := &Node{isLeaf: true}
		keys := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
		for i, key := range keys {
			node.insertLeafCell(uint16(i), key, []byte("val"))
		}

		newNode := &Node{}
		separator := node.split(newNode)

		if len(newNode.offsets) > 0 {
			firstKey := newNode.leafCell[newNode.offsets[0]].key
			assert.Equal(t, separator, firstKey)
		}
	})
}

func TestNodeEncoding(t *testing.T) {
	t.Run("encode leaf node", func(t *testing.T) {
		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("key1"), []byte("value1"))
		node.insertLeafCell(1, []byte("key2"), []byte("value2"))

		data, err := node.encodeLeaf()
		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Greater(t, len(data), 0)
	})

	t.Run("encode internal node", func(t *testing.T) {
		node := &Node{isLeaf: false}
		node.appendInternalCell(10, []byte("sep1"))
		node.appendInternalCell(20, []byte("sep2"))
		node.offsets = []uint16{0, 1}

		data, err := node.encodeInternal()
		assert.NoError(t, err)
		assert.NotNil(t, data)
		assert.Greater(t, len(data), 0)
	})

	t.Run("encode empty leaf node", func(t *testing.T) {
		node := &Node{isLeaf: true}

		data, err := node.encodeLeaf()
		assert.NoError(t, err)
		assert.NotNil(t, data)
	})
}

func TestNodeIsFull(t *testing.T) {
	t.Run("empty node is not full", func(t *testing.T) {
		node := &Node{isLeaf: true}
		assert.False(t, node.isFull())
	})

	t.Run("node with data reports fullness", func(t *testing.T) {
		node := &Node{isLeaf: true}
		for i := 0; i < 9; i++ {
			node.insertLeafCell(uint16(i), []byte("key"), []byte("value"))
		}
		full := node.isFull()
		assert.True(t, full)
	})

	t.Run("node with less data doesn't reports fullness", func(t *testing.T) {
		node := &Node{isLeaf: true}
		for i := 0; i < 8; i++ {
			node.insertLeafCell(uint16(i), []byte("key"), []byte("value"))
		}
		full := node.isFull()
		assert.False(t, full)
	})
}
