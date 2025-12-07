package harmonydb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"time"
)

type cache struct {
	nodes map[int64]*Node
	sync.Mutex
}

func (c *cache) add(pg *Node) {
	c.Lock()
	defer c.Unlock()

	c.nodes[int64(pg.fileOffset)] = pg
}

func (c *cache) fetch(fo uint64) *Node {
	c.Lock()
	defer c.Unlock()

	return c.nodes[int64(fo)]
}

func (c *cache) remove(fo uint64) {
	c.Lock()
	defer c.Unlock()

	delete(c.nodes, int64(fo))
}

func (c *cache) len() int {
	c.Lock()
	defer c.Unlock()

	return len(c.nodes)
}

func (c *cache) all() map[int64]*Node {
	c.Lock()
	defer c.Unlock()

	result := make(map[int64]*Node, len(c.nodes))
	for k, v := range c.nodes {
		result[k] = v
	}
	return result
}

type fileStore struct {
	file *os.File
	lock sync.Mutex
	// TODO: replace pageCache with buffer pool manager + lru
	pageCache      *cache
	nextFreeOffset uint64
	rootOffset     uint64
}

func newFileStore(path string) (*fileStore, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	f := &fileStore{
		file:      file,
		pageCache: &cache{nodes: make(map[int64]*Node)},
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
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, page := range f.pageCache.nodes {
		if !page.isDirty {
			continue
		}

		if err := f.update(page); err != nil {
			return fmt.Errorf("update page: %w", err)
		}

		page.markClean()
	}

	f.save()

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

	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	f.pageCache.Lock()
	defer f.pageCache.Unlock()

	f.pageCache.nodes[int64(node.fileOffset)] = node

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
