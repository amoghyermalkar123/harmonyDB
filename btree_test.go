package harmonydb

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestLogger initializes a test logger for btree tests
func setupTestLogger(t *testing.T) {
	// Initialize logger using the same system as the main application
	// This will respect HARMONYDB_DEBUG environment variables
	if err := InitLogger(8080, true); err != nil {
		t.Fatalf("Failed to initialize test logger: %v", err)
	}
}

func TestBasicBTree(t *testing.T) {
	setupTestLogger(t)
	bt := NewBTree()

	err := bt.put([]byte("amogh"), []byte("yermalkar"))
	assert.NoError(t, err)

	val, err := bt.Get([]byte("amogh"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("yermalkar"), val)
}

func TestBTreeGetNonExistent(t *testing.T) {
	setupTestLogger(t)
	bt := NewBTree()

	_, err := bt.Get([]byte("missing"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestBTreeMultiplePutGet(t *testing.T) {
	setupTestLogger(t)
	bt := NewBTree()

	testData := map[string]string{
		"apple":  "red",
		"banana": "yellow",
		"cherry": "red",
		"date":   "brown",
		"grape":  "purple",
	}

	for key, val := range testData {
		err := bt.put([]byte(key), []byte(val))
		require.NoError(t, err)
	}

	for key, expectedVal := range testData {
		val, err := bt.Get([]byte(key))
		require.NoError(t, err)
		assert.Equal(t, []byte(expectedVal), val, "key: %s", key)
	}
}

func TestBTreeRootTransition(t *testing.T) {
	setupTestLogger(t)
	t.Run("first insert creates leaf root", func(t *testing.T) {
		bt := NewBTree()
		err := bt.put([]byte("first"), []byte("value"))
		require.NoError(t, err)

		root := bt.getRootPage()
		require.NotNil(t, root)
		assert.True(t, root.isLeaf)
	})

	t.Run("root remains leaf until full", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 3; i++ {
			err := bt.put([]byte(fmt.Sprintf("key%d", i)), []byte("val"))
			require.NoError(t, err)
		}

		root := bt.getRootPage()
		require.NotNil(t, root)
	})
}

func TestBTreeSequentialInsertions(t *testing.T) {
	setupTestLogger(t)
	t.Run("ascending order", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			err := bt.put(key, val)
			require.NoError(t, err)
		}

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			expectedVal := []byte(fmt.Sprintf("val%03d", i))
			val, err := bt.Get(key)
			require.NoError(t, err)
			assert.Equal(t, expectedVal, val)
		}
	})

	t.Run("descending order", func(t *testing.T) {
		bt := NewBTree()

		for i := 20; i > 0; i-- {
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			err := bt.put(key, val)
			require.NoError(t, err)
		}

		for i := 20; i > 0; i-- {
			key := []byte(fmt.Sprintf("key%03d", i))
			expectedVal := []byte(fmt.Sprintf("val%03d", i))
			val, err := bt.Get(key)
			if err != nil {
				t.Logf("Failed to get key %s: %v", string(key), err)
				root := bt.getRootPage()
				if !root.isLeaf {
					t.Logf("Root is internal with %d cells:", len(root.internalCell))
					for j, cell := range root.internalCell {
						t.Logf("  Cell %d: key=%s, offset=%d", j, string(cell.key), cell.fileOffset)
					}
				}
			}
			require.NoError(t, err)
			assert.Equal(t, expectedVal, val)
		}
	})

	t.Run("random order", func(t *testing.T) {
		bt := NewBTree()

		keys := make([]int, 20)
		for i := 0; i < 20; i++ {
			keys[i] = i
		}

		rand.Shuffle(len(keys), func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})

		for _, i := range keys {
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			err := bt.put(key, val)
			require.NoError(t, err)
		}

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			expectedVal := []byte(fmt.Sprintf("val%03d", i))
			val, err := bt.Get(key)
			require.NoError(t, err)
			assert.Equal(t, expectedVal, val)
		}
	})
}

func TestBTreeKeyValueVariations(t *testing.T) {
	setupTestLogger(t)
	t.Run("empty key and value", func(t *testing.T) {
		bt := NewBTree()

		err := bt.put([]byte(""), []byte(""))
		require.NoError(t, err)

		val, err := bt.Get([]byte(""))
		require.NoError(t, err)
		assert.Equal(t, []byte(""), val)
	})

	t.Run("single byte key and value", func(t *testing.T) {
		bt := NewBTree()

		err := bt.put([]byte("k"), []byte("v"))
		require.NoError(t, err)

		val, err := bt.Get([]byte("k"))
		require.NoError(t, err)
		assert.Equal(t, []byte("v"), val)
	})

	t.Run("large key", func(t *testing.T) {
		bt := NewBTree()

		largeKey := bytes.Repeat([]byte("k"), 150)
		err := bt.put(largeKey, []byte("value"))
		require.NoError(t, err)

		val, err := bt.Get(largeKey)
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})

	t.Run("large value", func(t *testing.T) {
		bt := NewBTree()

		largeValue := bytes.Repeat([]byte("v"), 1500)
		err := bt.put([]byte("key"), largeValue)
		require.NoError(t, err)

		val, err := bt.Get([]byte("key"))
		require.NoError(t, err)
		assert.Equal(t, largeValue, val)
	})

	t.Run("binary keys", func(t *testing.T) {
		bt := NewBTree()

		binaryKey := []byte{0x00, 0x01, 0xFF, 0xAB, 0xCD}
		err := bt.put(binaryKey, []byte("binary_value"))
		require.NoError(t, err)

		val, err := bt.Get(binaryKey)
		require.NoError(t, err)
		assert.Equal(t, []byte("binary_value"), val)
	})
}

func TestBTreeStoreOperations(t *testing.T) {
	setupTestLogger(t)
	t.Run("page storage grows", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 10; i++ {
			err := bt.put([]byte(fmt.Sprintf("key%d", i)), []byte("val"))
			require.NoError(t, err)
		}

		assert.Greater(t, bt.pageCache.len(), 0)
	})

	t.Run("file offset uniqueness", func(t *testing.T) {
		bt := NewBTree()

		for i := range 5 {
			err := bt.put(fmt.Appendf(nil, "key%d", i), []byte("val"))
			require.NoError(t, err)
		}

		offsets := make(map[uint64]bool)
		for _, page := range bt.pageCache.all() {
			assert.False(t, offsets[page.fileOffset], "duplicate file offset found")
			offsets[page.fileOffset] = true
		}
	})
}

func _TestBTreeIntegration(t *testing.T) {
	t.Run("100 sequential puts then random gets", func(t *testing.T) {
		bt := NewBTree()

		for i := range 100 {
			key := []byte(fmt.Sprintf("key%04d", i))
			val := []byte(fmt.Sprintf("val%04d", i))
			err := bt.put(key, val)
			require.NoError(t, err)
		}

		indices := make([]int, 100)
		for i := 0; i < 100; i++ {
			indices[i] = i
		}
		rand.Shuffle(len(indices), func(i, j int) {
			indices[i], indices[j] = indices[j], indices[i]
		})

		for _, i := range indices {
			key := []byte(fmt.Sprintf("key%04d", i))
			expectedVal := []byte(fmt.Sprintf("val%04d", i))
			val, err := bt.Get(key)
			require.NoError(t, err)
			assert.Equal(t, expectedVal, val, "key: %s", key)
		}
	})

	t.Run("interleaved put and get", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 50; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			val := []byte(fmt.Sprintf("val%d", i))

			err := bt.put(key, val)
			require.NoError(t, err)

			retrievedVal, err := bt.Get(key)
			require.NoError(t, err)
			assert.Equal(t, val, retrievedVal)
		}
	})

	t.Run("verify all data after many operations", func(t *testing.T) {
		bt := NewBTree()
		testData := make(map[string]string)

		for i := 0; i < 50; i++ {
			key := fmt.Sprintf("key%d", i)
			val := fmt.Sprintf("val%d", i)
			testData[key] = val

			err := bt.put([]byte(key), []byte(val))
			require.NoError(t, err)
		}

		for key, expectedVal := range testData {
			val, err := bt.Get([]byte(key))
			require.NoError(t, err)
			assert.Equal(t, []byte(expectedVal), val, "key: %s", key)
		}
	})
}

func TestBTreeEdgeCases(t *testing.T) {
	setupTestLogger(t)
	t.Run("tree with single element", func(t *testing.T) {
		bt := NewBTree()

		err := bt.put([]byte("only"), []byte("one"))
		require.NoError(t, err)

		val, err := bt.Get([]byte("only"))
		require.NoError(t, err)
		assert.Equal(t, []byte("one"), val)

		_, err = bt.Get([]byte("other"))
		assert.Error(t, err)
	})
}

func TestBTreeRootSplitBug(t *testing.T) {
	setupTestLogger(t)
	t.Run("root splits correctly and becomes internal node", func(t *testing.T) {
		bt := NewBTree()

		// Insert enough unique keys to trigger a split (maxLeafNodeCells = 9)
		for i := 0; i < 10; i++ {
			key := []byte(fmt.Sprintf("key%02d", i))
			val := []byte(fmt.Sprintf("val%02d", i))
			err := bt.put(key, val)
			require.NoError(t, err)

			root := bt.getRootPage()
			t.Logf("After insert %d: root.isLeaf=%v, len(offsets)=%d", i, root.isLeaf, len(root.offsets))
		}

		root := bt.getRootPage()
		assert.False(t, root.isLeaf, "root should become internal node after split")
		assert.Greater(t, len(root.internalCell), 0, "internal root should have children")

		// Debug: print internal node structure
		t.Logf("Root has %d internal cells:", len(root.internalCell))
		for i, cell := range root.internalCell {
			t.Logf("  Cell %d: key=%s, fileOffset=%d", i, string(cell.key), cell.fileOffset)
		}

		// Verify all keys are still accessible after split
		for i := 0; i < 9; i++ {
			key := []byte(fmt.Sprintf("key%02d", i))
			expectedVal := []byte(fmt.Sprintf("val%02d", i))
			val, err := bt.Get(key)
			if err != nil {
				t.Logf("Failed to find key %s: %v", string(key), err)
			}
			require.NoError(t, err, "should find key after root split: %s", key)
			assert.Equal(t, expectedVal, val)
		}
	})
}

func TestBTreeInternalNodeTraversal(t *testing.T) {
	setupTestLogger(t)
	t.Run("traverse internal nodes to find leaf cells", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			err := bt.put(key, val)
			require.NoError(t, err)
		}

		root := bt.getRootPage()
		if !root.isLeaf {
			assert.Greater(t, len(root.internalCell), 0, "internal root should have child pointers")
		}

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			expectedVal := []byte(fmt.Sprintf("val%03d", i))
			val, err := bt.Get(key)
			require.NoError(t, err, "should find key through internal node: %s", key)
			assert.Equal(t, expectedVal, val)
		}
	})
}

func BenchmarkBTreePut(b *testing.B) {
	bt := NewBTree()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("val%d", i))
		bt.put(key, val)
	}
}

func TestBTreeFetch(t *testing.T) {
	setupTestLogger(t)

	t.Run("fetch node from cache", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_cache.db")
		defer bt.file.Close()

		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("key1"), []byte("value1"))
		bt.add(node)

		fetchedNode := bt.fetch(node.fileOffset)
		require.NotNil(t, fetchedNode)
		assert.Equal(t, node, fetchedNode, "should return the same node from cache")
		assert.True(t, fetchedNode.isLeaf)
		assert.Equal(t, 1, len(fetchedNode.offsets))
	})

	t.Run("fetch node from disk", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_disk.db")
		defer bt.file.Close()

		node := &Node{isLeaf: true}
		node.fileOffset = pageSize
		node.insertLeafCell(0, []byte("diskkey"), []byte("diskvalue"))

		err := bt.update(node)
		require.NoError(t, err)

		bt.pageCache.Lock()
		delete(bt.pageCache.nodes, int64(node.fileOffset))
		bt.pageCache.Unlock()

		fetchedNode := bt.fetch(node.fileOffset)
		require.NotNil(t, fetchedNode)
		assert.True(t, fetchedNode.isLeaf)
		assert.Equal(t, 1, len(fetchedNode.offsets))
		assert.Equal(t, []byte("diskkey"), fetchedNode.leafCell[fetchedNode.offsets[0]].key)
		assert.Equal(t, []byte("diskvalue"), fetchedNode.leafCell[fetchedNode.offsets[0]].val)
	})

	t.Run("fetch adds node to cache", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_add_cache.db")
		defer bt.file.Close()

		node := &Node{isLeaf: true}
		node.fileOffset = pageSize * 2
		node.insertLeafCell(0, []byte("cachekey"), []byte("cachevalue"))

		err := bt.update(node)
		require.NoError(t, err)

		bt.pageCache.Lock()
		delete(bt.pageCache.nodes, int64(node.fileOffset))
		bt.pageCache.Unlock()

		fetchedNode := bt.fetch(node.fileOffset)
		require.NotNil(t, fetchedNode)

		cachedNode := bt.pageCache.fetch(node.fileOffset)
		require.NotNil(t, cachedNode, "fetched node should be added to cache")
		assert.Equal(t, fetchedNode, cachedNode)
	})

	t.Run("fetch internal node from disk", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_internal.db")
		defer bt.file.Close()

		internalNode := &Node{isLeaf: false}
		internalNode.fileOffset = pageSize * 3
		internalNode.appendInternalCell(pageSize*4, []byte("separator1"))
		internalNode.appendInternalCell(pageSize*5, []byte("separator2"))

		err := bt.update(internalNode)
		require.NoError(t, err)

		bt.pageCache.Lock()
		delete(bt.pageCache.nodes, int64(internalNode.fileOffset))
		bt.pageCache.Unlock()

		fetchedNode := bt.fetch(internalNode.fileOffset)
		require.NotNil(t, fetchedNode)
		assert.False(t, fetchedNode.isLeaf)
		assert.Equal(t, 2, len(fetchedNode.offsets))
		assert.Equal(t, []byte("separator1"), fetchedNode.internalCell[fetchedNode.offsets[0]].key)
		assert.Equal(t, uint64(pageSize*4), fetchedNode.internalCell[fetchedNode.offsets[0]].fileOffset)
	})

	t.Run("fetch multiple times returns same cached instance", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_same_instance.db")
		defer bt.file.Close()

		node := &Node{isLeaf: true}
		node.insertLeafCell(0, []byte("multi"), []byte("fetch"))
		bt.add(node)

		first := bt.fetch(node.fileOffset)
		second := bt.fetch(node.fileOffset)
		third := bt.fetch(node.fileOffset)

		assert.Equal(t, first, second)
		assert.Equal(t, second, third)
		assert.Equal(t, first, third)
	})

	t.Run("fetch preserves node data integrity", func(t *testing.T) {
		bt := NewBTreeWithPath("test_fetch_integrity.db")
		defer bt.file.Close()

		originalNode := &Node{isLeaf: true}
		originalNode.fileOffset = pageSize * 6
		originalNode.insertLeafCell(0, []byte("key1"), []byte("value1"))
		originalNode.insertLeafCell(1, []byte("key2"), []byte("value2"))
		originalNode.insertLeafCell(2, []byte("key3"), []byte("value3"))

		err := bt.update(originalNode)
		require.NoError(t, err)

		bt.pageCache.Lock()
		delete(bt.pageCache.nodes, int64(originalNode.fileOffset))
		bt.pageCache.Unlock()

		fetchedNode := bt.fetch(originalNode.fileOffset)
		require.NotNil(t, fetchedNode)
		assert.Equal(t, len(originalNode.offsets), len(fetchedNode.offsets))

		for i := range originalNode.offsets {
			origIdx := originalNode.offsets[i]
			fetchIdx := fetchedNode.offsets[i]

			assert.Equal(t, originalNode.leafCell[origIdx].key, fetchedNode.leafCell[fetchIdx].key)
			assert.Equal(t, originalNode.leafCell[origIdx].val, fetchedNode.leafCell[fetchIdx].val)
		}
	})
}

func BenchmarkBTreeGet(b *testing.B) {
	bt := NewBTree()

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("val%d", i))
		bt.put(key, val)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i%1000))
		bt.Get(key)
	}
}

func BenchmarkBTreePutVaryingSizes(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("ValueSize%d", size), func(b *testing.B) {
			bt := NewBTree()
			val := bytes.Repeat([]byte("v"), size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := []byte(fmt.Sprintf("key%d", i))
				bt.put(key, val)
			}
		})
	}
}
