package sync

import (
	"context"
	"fmt"
	"strings"

	"watchtower/internal/db"
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

	// Apply --channels filter to thread parents
	if len(opts.Channels) > 0 {
		threadParents, err = filterThreadParentsByChannel(threadParents, opts.Channels, o.db)
		if err != nil {
			return fmt.Errorf("filtering thread parents by channel: %w", err)
		}
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

	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()

	pool := NewWorkerPool(workers, poolCancel)
	pool.Start(poolCtx, func(ctx context.Context, task SyncTask) error {
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
		if !pool.Submit(poolCtx, SyncTask{
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

// filterThreadParentsByChannel keeps only thread parents whose channel matches the filter.
// Matches by channel name or ID, case-insensitive (consistent with buildChannelQueue).
func filterThreadParentsByChannel(parents []db.Message, channels []string, database *db.DB) ([]db.Message, error) {
	filterSet := make(map[string]bool, len(channels))
	for _, ch := range channels {
		filterSet[strings.ToLower(ch)] = true
	}

	// Build name-to-ID mapping from DB so we can match by name
	allChannels, err := database.GetChannels(db.ChannelFilter{})
	if err != nil {
		return nil, fmt.Errorf("fetching channels for thread filter: %w", err)
	}
	allowedIDs := make(map[string]bool, len(channels))
	for _, ch := range allChannels {
		if filterSet[strings.ToLower(ch.Name)] || filterSet[strings.ToLower(ch.ID)] {
			allowedIDs[ch.ID] = true
		}
	}

	var filtered []db.Message
	for _, p := range parents {
		if allowedIDs[p.ChannelID] {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
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
