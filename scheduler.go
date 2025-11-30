package harmonydb

import (
	"context"
	"sync"
)

// establishes total order of operations performed on the database
// by establishing a FIFO scheduler that runs tasks in the order they were added.
type fifoScheduler struct {
	queue  []job
	ctx    context.Context
	cancel context.CancelFunc
	cond   *sync.Cond
	mu     sync.Mutex
}

// an applier that usually applies consensus committed entries
// to the database
type job struct {
	name string
	do   func(context.Context)
}

func newJob(name string, do func(context.Context)) job {
	return job{
		name: name,
		do:   do,
	}
}

func NewFifoScheduler() *fifoScheduler {
	f := &fifoScheduler{
		queue: make([]job, 0),
	}
	f.cond = sync.NewCond(&f.mu)
	f.ctx, f.cancel = context.WithCancel(context.Background())
	go f.run()

	return f
}

func (s *fifoScheduler) AddTask(task job) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queue = append(s.queue, task)
	// Wake up the scheduler
	s.cond.Signal()
}

// nextTask MUST be called with lock held
func (s *fifoScheduler) nextTask() job {
	if len(s.queue) == 0 {
		return job{}
	}

	task := s.queue[0]
	s.queue = s.queue[1:]
	return task
}

// FIFO scheduler runs tasks in the order they were added.
func (f *fifoScheduler) run() {
	for {
		f.mu.Lock()

		// Wait for tasks to be available
		for len(f.queue) == 0 {
			f.cond.Wait()
		}

		task := f.nextTask()
		f.mu.Unlock()

		// Execute task outside of lock
		if task.do != nil {
			task.do(f.ctx)
		}
	}
}

// Stop stops the scheduler and cancels all pending jobs.
func (f *fifoScheduler) stop() {
	f.mu.Lock()
	f.cancel()
	f.cancel = nil
	f.mu.Unlock()
}
