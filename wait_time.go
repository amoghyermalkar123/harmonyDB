package harmonydb

import "sync"

// For linearizing read path
type WaitTime interface {
	Wait(deadline int64) <-chan struct{}
	Trigger(deadline int64)
}

func newWaitTime() WaitTime {
	return &waitTime{
		lastTriggered: 0,
		m:             make(map[int64]chan struct{}),
	}
}

// whenever a read path is trying to read a message
// that was already linearized and is safe to read, use this
// channel to immediately signal the same
var closec chan struct{}

// pre-close at start so the channel is ready for use
func init() { closec = make(chan struct{}); close(closec) }

type waitTime struct {
	lastTriggered int64
	m             map[int64]chan struct{}
	sync.Mutex
}

func (w *waitTime) Wait(deadline int64) <-chan struct{} {
	w.Lock()
	defer w.Unlock()

	// fast path
	if deadline <= w.lastTriggered {
		return closec
	}

	// slow path, wait for the read to be linearized and safe for client unblock
	if ch := w.m[deadline]; ch != nil {
		return ch
	}

	ch := make(chan struct{})
	w.m[deadline] = ch
	return ch
}

// Trigger wakes up all the read waiters who were
// waiting for a deadline == `deadline` or before
// AND it triggers the current `deadline` as well
func (w *waitTime) Trigger(triggerNewDeadline int64) {
	w.Lock()
	defer w.Unlock()

	w.lastTriggered = triggerNewDeadline

	for waitingDeadline, waitingRead := range w.m {
		if waitingDeadline <= triggerNewDeadline {
			close(waitingRead)
			delete(w.m, waitingDeadline)
		}
	}
}
