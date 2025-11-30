package harmonydb

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileStore(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)
	require.NotNil(t, fs)
	require.NotNil(t, fs.file)
	require.NotNil(t, fs.cache)

	assert.FileExists(t, storePath)

	info, err := os.Stat(storePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestNewFileStoreExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "existing.db")

	fs1, err := newFileStore(storePath)
	require.NoError(t, err)
	require.NotNil(t, fs1)

	fs2, err := newFileStore(storePath)
	require.NoError(t, err)
	require.NotNil(t, fs2)

	assert.FileExists(t, storePath)
}

func TestFileStoreSetAndGetRoot(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 1024

	fs.setRoot(node)
	assert.Equal(t, uint64(1024), fs.rootOffset)

	fs.getRoot(node)
	assert.Equal(t, uint64(1024), fs.rootOffset)
}

func TestFileStoreUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key1"), []byte("value1"))

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.NotNil(t, cachedNode)
	assert.Equal(t, node, cachedNode)

	fileInfo, err := os.Stat(storePath)
	require.NoError(t, err)
	assert.Greater(t, fileInfo.Size(), int64(0))
}

func TestFileStoreUpdateMultipleNodes(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node1 := &Node{isLeaf: true}
	node1.fileOffset = 4096
	node1.insertLeafCell(0, []byte("key1"), []byte("value1"))

	node2 := &Node{isLeaf: true}
	node2.fileOffset = 8192
	node2.insertLeafCell(0, []byte("key2"), []byte("value2"))

	err = fs.update(node1)
	require.NoError(t, err)

	err = fs.update(node2)
	require.NoError(t, err)

	assert.Equal(t, 2, fs.cache.len())
	assert.NotNil(t, fs.cache.fetch(4096))
	assert.NotNil(t, fs.cache.fetch(8192))
}

func TestFileStoreUpdatePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 0
	node.insertLeafCell(0, []byte("test_key"), []byte("test_value"))

	err = fs.update(node)
	require.NoError(t, err)

	fileInfo, err := os.Stat(storePath)
	require.NoError(t, err)
	assert.Greater(t, fileInfo.Size(), int64(0))

	data := make([]byte, fileInfo.Size())
	n, err := fs.file.ReadAt(data, int64(node.fileOffset))
	if err != nil && n > 0 {
		t.Logf("Partial read: %d bytes, error: %v", n, err)
	} else {
		require.NoError(t, err)
	}
	assert.Greater(t, n, 0)
	hasData := false
	for _, b := range data[:n] {
		if b != 0 {
			hasData = true
			break
		}
	}
	assert.True(t, hasData, "Expected data to be written to file")
}

func TestFileStoreCacheBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key"), []byte("value"))

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.Nil(t, cachedNode)

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode = fs.cache.fetch(node.fileOffset)
	assert.NotNil(t, cachedNode)
	assert.Equal(t, node, cachedNode)
}

func TestFileStoreCacheUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key1"), []byte("value1"))

	err = fs.update(node)
	require.NoError(t, err)

	node.insertLeafCell(1, []byte("key2"), []byte("value2"))
	node.markDirty()

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.Equal(t, 2, len(cachedNode.offsets))
}

func TestFileStoreFlushPagesCleanNodes(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key"), []byte("value"))
	node.markClean()

	fs.cache.add(node)

	err = fs.flushPages()
	require.NoError(t, err)
}

func TestFileStoreFlushPagesDirtyNodes(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key"), []byte("value"))
	node.markDirty()

	fs.cache.add(node)

	err = fs.flushPages()
	require.NoError(t, err)

	assert.False(t, node.isDirty)
}

func TestFileStoreFlushPagesMultipleDirtyNodes(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node1 := &Node{isLeaf: true}
	node1.fileOffset = 4096
	node1.insertLeafCell(0, []byte("key1"), []byte("value1"))
	node1.markDirty()

	node2 := &Node{isLeaf: true}
	node2.fileOffset = 8192
	node2.insertLeafCell(0, []byte("key2"), []byte("value2"))
	node2.markDirty()

	node3 := &Node{isLeaf: true}
	node3.fileOffset = 12288
	node3.insertLeafCell(0, []byte("key3"), []byte("value3"))
	node3.markClean()

	fs.cache.add(node1)
	fs.cache.add(node2)
	fs.cache.add(node3)

	err = fs.flushPages()
	require.NoError(t, err)

	assert.False(t, node1.isDirty)
	assert.False(t, node2.isDirty)
	assert.False(t, node3.isDirty)
}

func TestFileStoreSave(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	fs.nextFreeOffset = 16384

	err = fs.save()
	require.NoError(t, err)

	data := make([]byte, 8)
	_, err = fs.file.ReadAt(data, 0)
	require.NoError(t, err)

	assert.NotEqual(t, make([]byte, 8), data)
}

func TestFileStoreSaveMultipleTimes(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	offsets := []uint64{4096, 8192, 16384, 32768}

	for _, offset := range offsets {
		fs.nextFreeOffset = offset
		err = fs.save()
		require.NoError(t, err)

		data := make([]byte, 8)
		_, err = fs.file.ReadAt(data, 0)
		require.NoError(t, err)
		assert.NotEqual(t, make([]byte, 8), data)
	}
}

func TestFileStoreConcurrentUpdates(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			node := &Node{isLeaf: true}
			node.fileOffset = uint64(4096 * (idx + 1))
			node.insertLeafCell(0, []byte("key"), []byte("value"))

			err := fs.update(node)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, numGoroutines, fs.cache.len())
}

func TestFileStoreConcurrentFlush(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		node := &Node{isLeaf: true}
		node.fileOffset = uint64(4096 * (i + 1))
		node.insertLeafCell(0, []byte("key"), []byte("value"))
		node.markDirty()
		fs.cache.add(node)
	}

	var wg sync.WaitGroup
	numGoroutines := 3

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fs.flushPages()
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	for _, node := range fs.cache.all() {
		assert.False(t, node.isDirty)
	}
}

func TestFileStoreAutoFlush(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("key"), []byte("value"))
	node.markDirty()

	fs.cache.add(node)

	time.Sleep(1500 * time.Millisecond)

	assert.False(t, node.isDirty)
}

func TestFileStoreUpdateWithEmptyNode(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.NotNil(t, cachedNode)
	assert.Equal(t, 0, len(cachedNode.offsets))
}

func TestFileStoreUpdateLargeNode(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096

	for i := 0; i < 50; i++ {
		key := []byte{byte(i)}
		value := make([]byte, 100)
		for j := range value {
			value[j] = byte(i)
		}
		node.insertLeafCell(uint16(i), key, value)
	}

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.Equal(t, 50, len(cachedNode.offsets))
}

func TestFileStoreMultipleOffsets(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	offsets := []uint64{4096, 8192, 16384, 32768, 65536}

	for _, offset := range offsets {
		node := &Node{isLeaf: true}
		node.fileOffset = offset
		node.insertLeafCell(0, []byte("key"), []byte("value"))

		err = fs.update(node)
		require.NoError(t, err)
	}

	assert.Equal(t, len(offsets), fs.cache.len())

	for _, offset := range offsets {
		node := fs.cache.fetch(offset)
		assert.NotNil(t, node)
	}
}

func TestFileStoreSaveZeroOffset(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	fs.nextFreeOffset = 0

	err = fs.save()
	require.NoError(t, err)
}

func TestFileStoreFlushEmptyCache(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	err = fs.flushPages()
	require.NoError(t, err)
}

func TestFileStoreInternalNode(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "test.db")

	fs, err := newFileStore(storePath)
	require.NoError(t, err)

	node := &Node{isLeaf: false}
	node.fileOffset = 4096

	childNode := &Node{isLeaf: true}
	childNode.fileOffset = 8192
	node.appendInternalCell(childNode.fileOffset, []byte("separator"))

	err = fs.update(node)
	require.NoError(t, err)

	cachedNode := fs.cache.fetch(node.fileOffset)
	assert.NotNil(t, cachedNode)
	assert.False(t, cachedNode.isLeaf)
}

func BenchmarkFileStoreUpdate(b *testing.B) {
	tmpDir := b.TempDir()
	storePath := filepath.Join(tmpDir, "bench.db")

	fs, err := newFileStore(storePath)
	require.NoError(b, err)

	node := &Node{isLeaf: true}
	node.fileOffset = 4096
	node.insertLeafCell(0, []byte("benchmark_key"), []byte("benchmark_value"))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := fs.update(node)
		require.NoError(b, err)
	}
}

func BenchmarkFileStoreFlush(b *testing.B) {
	tmpDir := b.TempDir()
	storePath := filepath.Join(tmpDir, "bench.db")

	fs, err := newFileStore(storePath)
	require.NoError(b, err)

	for i := 0; i < 10; i++ {
		node := &Node{isLeaf: true}
		node.fileOffset = uint64(4096 * (i + 1))
		node.insertLeafCell(0, []byte("key"), []byte("value"))
		node.markDirty()
		fs.cache.add(node)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, node := range fs.cache.all() {
			node.markDirty()
		}
		err := fs.flushPages()
		require.NoError(b, err)
	}
}

func BenchmarkFileStoreConcurrentUpdates(b *testing.B) {
	tmpDir := b.TempDir()
	storePath := filepath.Join(tmpDir, "bench.db")

	fs, err := newFileStore(storePath)
	require.NoError(b, err)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				node := &Node{isLeaf: true}
				node.fileOffset = uint64(4096 * (idx + 1))
				node.insertLeafCell(0, []byte("key"), []byte("value"))
				fs.update(node)
			}(j)
		}
		wg.Wait()
	}
}
