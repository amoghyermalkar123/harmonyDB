package scheduler

import (
	"context"
	"sync"
)

// establishes total order of operations performed on the database
// by establishing a FIFO scheduler that runs tasks in the order they were added.
type FifoScheduler struct {
	queue  []Job
	ctx    context.Context
	cancel context.CancelFunc
	cond   *sync.Cond
	mu     sync.Mutex
}

// an applier that usually applies consensus committed entries
// to the database
type Job struct {
	name string
	do   func(context.Context)
}

func NewJob(name string, do func(context.Context)) Job {
	return Job{
		name: name,
		do:   do,
	}
}

func NewFifoScheduler() *FifoScheduler {
	f := &FifoScheduler{
		queue: make([]Job, 0),
	}
	f.cond = sync.NewCond(&f.mu)
	f.ctx, f.cancel = context.WithCancel(context.Background())
	go f.run()

	return f
}

func (s *FifoScheduler) AddTask(task Job) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queue = append(s.queue, task)
	// Wake up the scheduler
	s.cond.Signal()
}

// nextTask MUST be called with lock held
func (s *FifoScheduler) nextTask() Job {
	if len(s.queue) == 0 {
		return Job{}
	}

	task := s.queue[0]
	s.queue = s.queue[1:]
	return task
}

// FIFO scheduler runs tasks in the order they were added.
func (f *FifoScheduler) run() {
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
func (f *FifoScheduler) stop() {
	f.mu.Lock()
	f.cancel()
	f.cancel = nil
	f.mu.Unlock()
}
