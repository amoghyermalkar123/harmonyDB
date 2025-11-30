package harmonydb

import (
	"sync"
)

// Wait is an interface that provides the ability to wait and trigger events that
// are associated with IDs.
//
// For linearizing write path
type Wait interface {
	// Register returns a chan that waits on the given ID.
	// The chan will be triggered when Trigger is called with
	// the same ID.
	Register(id uint64) <-chan any
	// Trigger triggers the waiting chans with the given ID.
	Trigger(id uint64, x any)
}

type waiter struct {
	mu    sync.Mutex
	chans map[uint64]chan any
}

func newWaiter() Wait {
	return &waiter{
		chans: make(map[uint64]chan any),
	}
}

func (w *waiter) Trigger(id uint64, x any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if ch, ok := w.chans[id]; ok {
		close(ch)
		delete(w.chans, id)
	}
}

func (w *waiter) Register(id uint64) <-chan any {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.chans[id]; !ok {
		w.chans[id] = make(chan any)
	}

	return w.chans[id]
}
