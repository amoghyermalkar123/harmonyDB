package harmonydb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"time"

	"harmonydb/metrics"
)

type fileStore struct {
	file           *os.File
	lock           sync.Mutex
	cache          map[int64]*Node
	nextFreeOffset uint64
	rootOffset     uint64
}

func newFileStore(path string) (*fileStore, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	f := &fileStore{
		file:  file,
		cache: make(map[int64]*Node),
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ticker.C:
				if err := f.flushPages(); err != nil {
					panic(fmt.Errorf("flushPages(): %w", err))
				}
			}
		}
	}()

	return f, nil
}

func (f *fileStore) setRoot(node *Node) {
	f.rootOffset = node.fileOffset
}

func (f *fileStore) getRoot(node *Node) {
	f.rootOffset = node.fileOffset
}

func (f *fileStore) flushPages() error {
	startTime := time.Now()
	f.lock.Lock()
	defer f.lock.Unlock()

	pagesFlushed := 0
	for _, page := range f.cache {
		if !page.isDirty {
			continue
		}

		if err := f.update(page); err != nil {
			return fmt.Errorf("update page: %w", err)
		}

		page.markClean()
		pagesFlushed++
	}

	f.save()

	// Record metrics
	if pagesFlushed > 0 {
		metrics.StorageFlushDuration.Observe(time.Since(startTime).Seconds())
		metrics.StorageFlushPagesTotal.Add(float64(pagesFlushed))
	}

	// Update dirty pages gauge
	dirtyCount := 0
	for _, page := range f.cache {
		if page.isDirty {
			dirtyCount++
		}
	}
	metrics.BTreeDirtyPages.Set(float64(dirtyCount))

	return nil
}

func (f *fileStore) update(node *Node) error {
	buf, err := node.encode()
	if err != nil {
		return fmt.Errorf("encode node: %w", err)
	}

	if _, err := f.file.WriteAt(buf, int64(node.fileOffset)); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Track bytes written
	metrics.StorageWriteBytesTotal.Add(float64(len(buf)))

	f.cache[int64(node.fileOffset)] = node

	return nil
}

func (f *fileStore) save() error {
	writer := bytes.NewBuffer(make([]byte, 0, 8))

	if err := binary.Write(writer, binary.LittleEndian, f.nextFreeOffset); err != nil {
		return err
	}

	if _, err := f.file.WriteAt(writer.Bytes(), 0); err != nil {
		return err
	}

	return nil
}
