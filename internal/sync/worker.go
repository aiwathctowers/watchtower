package sync

import (
	"context"
	"sort"
	"sync"
)

// TaskType distinguishes between channel message sync and thread reply sync.
type TaskType int

const (
	TaskChannel TaskType = iota
	TaskThread
)

// TaskPriority determines the processing order of sync tasks.
type TaskPriority int

const (
	PriorityWatchHigh   TaskPriority = 0
	PriorityWatchNormal TaskPriority = 1
	PriorityMember      TaskPriority = 2
	PriorityRest        TaskPriority = 3
)

// SyncTask represents a unit of work for the worker pool.
type SyncTask struct {
	Type      TaskType
	ChannelID string
	ThreadTS  string
	Priority  TaskPriority
}

// WorkerPool manages a fixed number of goroutines that process SyncTasks.
type WorkerPool struct {
	workers int
	tasks   chan SyncTask
	wg      sync.WaitGroup

	mu   sync.Mutex
	errs []error
}

// NewWorkerPool creates a worker pool with the given concurrency level.
func NewWorkerPool(workers int) *WorkerPool {
	if workers < 1 {
		workers = 1
	}
	return &WorkerPool{
		workers: workers,
		tasks:   make(chan SyncTask, workers*2),
	}
}

// Start launches worker goroutines that call handler for each task.
// Workers run until the task channel is closed or the context is canceled.
func (wp *WorkerPool) Start(ctx context.Context, handler func(ctx context.Context, task SyncTask) error) {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-wp.tasks:
					if !ok {
						return
					}
					if err := handler(ctx, task); err != nil {
						wp.mu.Lock()
						wp.errs = append(wp.errs, err)
						wp.mu.Unlock()
					}
				}
			}
		}()
	}
}

// Submit sends a task to the worker pool. It blocks if all workers are busy
// and the task buffer is full. Returns false if the context is canceled.
func (wp *WorkerPool) Submit(ctx context.Context, task SyncTask) bool {
	// Check context first to ensure cancelled contexts are respected
	// even when the channel buffer has space.
	select {
	case <-ctx.Done():
		return false
	default:
	}
	select {
	case <-ctx.Done():
		return false
	case wp.tasks <- task:
		return true
	}
}

// Close signals that no more tasks will be submitted.
// Must be called after all Submit calls are done.
func (wp *WorkerPool) Close() {
	close(wp.tasks)
}

// Wait waits for all workers to finish and returns any collected errors.
func (wp *WorkerPool) Wait() []error {
	wp.wg.Wait()
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.errs
}

// SortTasksByPriority sorts tasks so that higher-priority tasks come first.
func SortTasksByPriority(tasks []SyncTask) {
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Priority < tasks[j].Priority
	})
}
