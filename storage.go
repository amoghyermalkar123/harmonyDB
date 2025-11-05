package harmonydb

import (
	"os"
	"sync"
)

type fileStore struct {
	file *os.File
	lock sync.Mutex
}

func newFileStore(path string) (*fileStore, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	return &fileStore{
		file: file,
	}, nil
}
