package sync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkerPool(t *testing.T) {
	wp := NewWorkerPool(5)
	assert.Equal(t, 5, wp.workers)
	assert.NotNil(t, wp.tasks)
}

func TestNewWorkerPoolMinimumOne(t *testing.T) {
	wp := NewWorkerPool(0)
	assert.Equal(t, 1, wp.workers)

	wp = NewWorkerPool(-3)
	assert.Equal(t, 1, wp.workers)
}

func TestWorkerPoolProcessesTasks(t *testing.T) {
	wp := NewWorkerPool(2)

	var processed atomic.Int32
	wp.Start(context.Background(), func(ctx context.Context, task SyncTask) error {
		processed.Add(1)
		return nil
	})

	for i := 0; i < 10; i++ {
		wp.Submit(context.Background(), SyncTask{
			Type:      TaskChannel,
			ChannelID: "C001",
			Priority:  PriorityMember,
		})
	}
	wp.Close()

	errs := wp.Wait()
	assert.Empty(t, errs)
	assert.Equal(t, int32(10), processed.Load())
}

func TestWorkerPoolCollectsErrors(t *testing.T) {
	wp := NewWorkerPool(2)

	wp.Start(context.Background(), func(ctx context.Context, task SyncTask) error {
		if task.ChannelID == "fail" {
			return errors.New("sync failed")
		}
		return nil
	})

	wp.Submit(context.Background(), SyncTask{ChannelID: "ok"})
	wp.Submit(context.Background(), SyncTask{ChannelID: "fail"})
	wp.Submit(context.Background(), SyncTask{ChannelID: "ok"})
	wp.Submit(context.Background(), SyncTask{ChannelID: "fail"})
	wp.Close()

	errs := wp.Wait()
	assert.Len(t, errs, 2)
	for _, err := range errs {
		assert.EqualError(t, err, "sync failed")
	}
}

func TestWorkerPoolContextCancellation(t *testing.T) {
	wp := NewWorkerPool(2)
	ctx, cancel := context.WithCancel(context.Background())

	var processed atomic.Int32
	wp.Start(ctx, func(ctx context.Context, task SyncTask) error {
		processed.Add(1)
		// Simulate slow work
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	})

	// Submit a few tasks then cancel
	wp.Submit(ctx, SyncTask{ChannelID: "C001"})
	wp.Submit(ctx, SyncTask{ChannelID: "C002"})
	cancel()

	wp.Close()
	wp.Wait()

	// Workers should stop promptly after cancel
	// We just verify it doesn't hang
}

func TestWorkerPoolSubmitReturnsFalseOnCancel(t *testing.T) {
	wp := NewWorkerPool(1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before submit

	ok := wp.Submit(ctx, SyncTask{ChannelID: "C001"})
	assert.False(t, ok)
}

func TestWorkerPoolConcurrency(t *testing.T) {
	wp := NewWorkerPool(3)

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	wp.Start(context.Background(), func(ctx context.Context, task SyncTask) error {
		cur := concurrent.Add(1)
		// Track max concurrent workers
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		concurrent.Add(-1)
		return nil
	})

	for i := 0; i < 9; i++ {
		wp.Submit(context.Background(), SyncTask{ChannelID: "C001"})
	}
	wp.Close()
	wp.Wait()

	// With 3 workers and enough tasks, we should see > 1 concurrent
	assert.Greater(t, maxConcurrent.Load(), int32(1))
}

func TestWorkerPoolNoTasksReturnsCleanly(t *testing.T) {
	wp := NewWorkerPool(2)
	wp.Start(context.Background(), func(ctx context.Context, task SyncTask) error {
		return nil
	})
	wp.Close()
	errs := wp.Wait()
	assert.Empty(t, errs)
}

func TestSyncTaskTypes(t *testing.T) {
	channelTask := SyncTask{
		Type:      TaskChannel,
		ChannelID: "C001",
		Priority:  PriorityWatchHigh,
	}
	assert.Equal(t, TaskChannel, channelTask.Type)
	assert.Empty(t, channelTask.ThreadTS)

	threadTask := SyncTask{
		Type:      TaskThread,
		ChannelID: "C001",
		ThreadTS:  "1234567890.123456",
		Priority:  PriorityMember,
	}
	assert.Equal(t, TaskThread, threadTask.Type)
	assert.Equal(t, "1234567890.123456", threadTask.ThreadTS)
}

func TestSortTasksByPriority(t *testing.T) {
	tasks := []SyncTask{
		{ChannelID: "rest", Priority: PriorityRest},
		{ChannelID: "high", Priority: PriorityWatchHigh},
		{ChannelID: "member", Priority: PriorityMember},
		{ChannelID: "normal", Priority: PriorityWatchNormal},
		{ChannelID: "high2", Priority: PriorityWatchHigh},
	}

	SortTasksByPriority(tasks)

	assert.Equal(t, PriorityWatchHigh, tasks[0].Priority)
	assert.Equal(t, PriorityWatchHigh, tasks[1].Priority)
	assert.Equal(t, PriorityWatchNormal, tasks[2].Priority)
	assert.Equal(t, PriorityMember, tasks[3].Priority)
	assert.Equal(t, PriorityRest, tasks[4].Priority)
}

func TestSortTasksByPriorityEmpty(t *testing.T) {
	var tasks []SyncTask
	SortTasksByPriority(tasks) // should not panic
	assert.Empty(t, tasks)
}

func TestSortTasksByPrioritySingle(t *testing.T) {
	tasks := []SyncTask{{ChannelID: "C001", Priority: PriorityMember}}
	SortTasksByPriority(tasks)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "C001", tasks[0].ChannelID)
}

func TestWorkerPoolTaskOrder(t *testing.T) {
	// Use 1 worker to guarantee serial processing
	wp := NewWorkerPool(1)

	var order []string
	var mu = &sync.Mutex{}
	wp.Start(context.Background(), func(ctx context.Context, task SyncTask) error {
		mu.Lock()
		order = append(order, task.ChannelID)
		mu.Unlock()
		return nil
	})

	// Pre-sort tasks by priority then submit in order
	tasks := []SyncTask{
		{ChannelID: "rest", Priority: PriorityRest},
		{ChannelID: "high", Priority: PriorityWatchHigh},
		{ChannelID: "member", Priority: PriorityMember},
		{ChannelID: "normal", Priority: PriorityWatchNormal},
	}
	SortTasksByPriority(tasks)

	for _, task := range tasks {
		wp.Submit(context.Background(), task)
	}
	wp.Close()
	wp.Wait()

	require.Len(t, order, 4)
	assert.Equal(t, "high", order[0])
	assert.Equal(t, "normal", order[1])
	assert.Equal(t, "member", order[2])
	assert.Equal(t, "rest", order[3])
}

func TestWorkerPoolHandlerReceivesContext(t *testing.T) {
	wp := NewWorkerPool(1)
	ctx := context.WithValue(context.Background(), contextKey("test"), "value")

	var received string
	wp.Start(ctx, func(ctx context.Context, task SyncTask) error {
		received = ctx.Value(contextKey("test")).(string)
		return nil
	})

	wp.Submit(ctx, SyncTask{ChannelID: "C001"})
	wp.Close()
	wp.Wait()

	assert.Equal(t, "value", received)
}

type contextKey string
