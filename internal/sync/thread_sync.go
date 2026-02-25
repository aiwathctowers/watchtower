package sync

import (
	"context"
	"fmt"
)

// syncThreads fetches replies for threads that have reply_count > 0 but
// don't yet have all their replies synced locally. It skips entirely when
// config.sync.sync_threads is false.
func (o *Orchestrator) syncThreads(ctx context.Context, opts SyncOptions) error {
	if !o.config.Sync.SyncThreads {
		o.logger.Println("thread sync disabled, skipping")
		return nil
	}

	threadParents, err := o.db.GetAllThreadParents()
	if err != nil {
		return fmt.Errorf("querying thread parents: %w", err)
	}

	if len(threadParents) == 0 {
		o.logger.Println("no threads to sync")
		return nil
	}

	o.logger.Printf("found %d threads to sync", len(threadParents))
	o.progress.SetThreadsTotal(len(threadParents))

	workers := opts.Workers
	if workers <= 0 {
		workers = o.config.Sync.Workers
	}
	if workers <= 0 {
		workers = 1
	}

	pool := NewWorkerPool(workers)
	pool.Start(ctx, func(ctx context.Context, task SyncTask) error {
		replyCount, err := o.syncThread(ctx, task.ChannelID, task.ThreadTS)
		if err != nil {
			if isNonFatalError(err) {
				o.logger.Printf("skipping thread %s/%s: %v", task.ChannelID, task.ThreadTS, err)
				o.progress.IncThread(0)
				return nil
			}
			return fmt.Errorf("syncing thread %s/%s: %w", task.ChannelID, task.ThreadTS, err)
		}
		o.progress.IncThread(replyCount)
		return nil
	})

	// Build tasks from thread parents. Use PriorityRest since we don't
	// differentiate thread priority; the channel priority already handled
	// ordering during message sync.
	for _, parent := range threadParents {
		if !pool.Submit(ctx, SyncTask{
			Type:      TaskThread,
			ChannelID: parent.ChannelID,
			ThreadTS:  parent.TS,
			Priority:  PriorityRest,
		}) {
			break
		}
	}
	pool.Close()

	errs := pool.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("thread sync had %d errors; first: %w", len(errs), errs[0])
	}

	return nil
}

// syncThread fetches all replies for a single thread and upserts them.
// Returns the number of replies synced and any error.
func (o *Orchestrator) syncThread(ctx context.Context, channelID, threadTS string) (int, error) {
	replies, err := o.slackClient.GetConversationReplies(ctx, channelID, threadTS)
	if err != nil {
		return 0, err
	}

	if len(replies) == 0 {
		return 0, nil
	}

	return o.upsertMessagePage(channelID, replies)
}
