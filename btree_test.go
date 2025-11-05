package harmonydb

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicBTree(t *testing.T) {
	bt := NewBTree()

	err := bt.Put([]byte("amogh"), []byte("yermalkar"))
	assert.NoError(t, err)

	val, err := bt.Get([]byte("amogh"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("yermalkar"), val)
}

func TestBTreeGetNonExistent(t *testing.T) {
	bt := NewBTree()

	_, err := bt.Get([]byte("missing"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// func TestBTreePutOverwrite(t *testing.T) {
// 	bt := NewBTree()

// 	err := bt.Put([]byte("key"), []byte("value1"))
// 	require.NoError(t, err)

// 	err = bt.Put([]byte("key"), []byte("value2"))
// 	require.NoError(t, err)

// 	val, err := bt.Get([]byte("key"))
// 	require.NoError(t, err)
// 	assert.Equal(t, []byte("value2"), val)
// }

func TestBTreeMultiplePutGet(t *testing.T) {
	bt := NewBTree()

	testData := map[string]string{
		"apple":  "red",
		"banana": "yellow",
		"cherry": "red",
		"date":   "brown",
		"grape":  "purple",
	}

	for key, val := range testData {
		err := bt.Put([]byte(key), []byte(val))
		require.NoError(t, err)
	}

	for key, expectedVal := range testData {
		val, err := bt.Get([]byte(key))
		require.NoError(t, err)
		assert.Equal(t, []byte(expectedVal), val, "key: %s", key)
	}
}

func TestBTreeRootTransition(t *testing.T) {
	t.Run("first insert creates leaf root", func(t *testing.T) {
		bt := NewBTree()
		err := bt.Put([]byte("first"), []byte("value"))
		require.NoError(t, err)

		root := bt.getRootPage()
		require.NotNil(t, root)
		assert.True(t, root.isLeaf)
	})

	t.Run("root remains leaf until full", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 3; i++ {
			err := bt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("val"))
			require.NoError(t, err)
		}

		root := bt.getRootPage()
		require.NotNil(t, root)
	})
}

func TestBTreeSequentialInsertions(t *testing.T) {
	t.Run("ascending order", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 20; i++ {
			key := []byte(fmt.Sprintf("key%03d", i))
			val := []byte(fmt.Sprintf("val%03d", i))
			err := bt.Put(key, val)
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
			err := bt.Put(key, val)
			require.NoError(t, err)
		}

		for i := 20; i > 0; i-- {
			key := []byte(fmt.Sprintf("key%03d", i))
			expectedVal := []byte(fmt.Sprintf("val%03d", i))
			val, err := bt.Get(key)
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
			err := bt.Put(key, val)
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
	t.Run("empty key and value", func(t *testing.T) {
		bt := NewBTree()

		err := bt.Put([]byte(""), []byte(""))
		require.NoError(t, err)

		val, err := bt.Get([]byte(""))
		require.NoError(t, err)
		assert.Equal(t, []byte(""), val)
	})

	t.Run("single byte key and value", func(t *testing.T) {
		bt := NewBTree()

		err := bt.Put([]byte("k"), []byte("v"))
		require.NoError(t, err)

		val, err := bt.Get([]byte("k"))
		require.NoError(t, err)
		assert.Equal(t, []byte("v"), val)
	})

	t.Run("large key", func(t *testing.T) {
		bt := NewBTree()

		largeKey := bytes.Repeat([]byte("k"), 150)
		err := bt.Put(largeKey, []byte("value"))
		require.NoError(t, err)

		val, err := bt.Get(largeKey)
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})

	t.Run("large value", func(t *testing.T) {
		bt := NewBTree()

		largeValue := bytes.Repeat([]byte("v"), 1500)
		err := bt.Put([]byte("key"), largeValue)
		require.NoError(t, err)

		val, err := bt.Get([]byte("key"))
		require.NoError(t, err)
		assert.Equal(t, largeValue, val)
	})

	t.Run("binary keys", func(t *testing.T) {
		bt := NewBTree()

		binaryKey := []byte{0x00, 0x01, 0xFF, 0xAB, 0xCD}
		err := bt.Put(binaryKey, []byte("binary_value"))
		require.NoError(t, err)

		val, err := bt.Get(binaryKey)
		require.NoError(t, err)
		assert.Equal(t, []byte("binary_value"), val)
	})
}

func TestBTreeStoreOperations(t *testing.T) {
	t.Run("page storage grows", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 10; i++ {
			err := bt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("val"))
			require.NoError(t, err)
		}

		assert.Greater(t, len(bt.pages), 0)
	})

	t.Run("file offset uniqueness", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 5; i++ {
			err := bt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("val"))
			require.NoError(t, err)
		}

		offsets := make(map[uint64]bool)
		for _, page := range bt.pages {
			assert.False(t, offsets[page.fileOffset], "duplicate file offset found")
			offsets[page.fileOffset] = true
		}
	})
}

func _TestBTreeIntegration(t *testing.T) {
	t.Run("100 sequential puts then random gets", func(t *testing.T) {
		bt := NewBTree()

		for i := 0; i < 100; i++ {
			key := []byte(fmt.Sprintf("key%04d", i))
			val := []byte(fmt.Sprintf("val%04d", i))
			err := bt.Put(key, val)
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

			err := bt.Put(key, val)
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

			err := bt.Put([]byte(key), []byte(val))
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
	t.Run("tree with single element", func(t *testing.T) {
		bt := NewBTree()

		err := bt.Put([]byte("only"), []byte("one"))
		require.NoError(t, err)

		val, err := bt.Get([]byte("only"))
		require.NoError(t, err)
		assert.Equal(t, []byte("one"), val)

		_, err = bt.Get([]byte("other"))
		assert.Error(t, err)
	})
}

func BenchmarkBTreePut(b *testing.B) {
	bt := NewBTree()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("val%d", i))
		bt.Put(key, val)
	}
}

func BenchmarkBTreeGet(b *testing.B) {
	bt := NewBTree()

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("val%d", i))
		bt.Put(key, val)
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
				bt.Put(key, val)
			}
		})
	}
}
