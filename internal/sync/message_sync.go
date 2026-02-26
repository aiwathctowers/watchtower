package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	goslack "github.com/slack-go/slack"

	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
)

// syncMessages builds a priority-ordered channel queue and uses the worker pool
// to sync message history for each channel in parallel.
func (o *Orchestrator) syncMessages(ctx context.Context, opts SyncOptions) error {
	channels, err := o.buildChannelQueue(opts)
	if err != nil {
		return fmt.Errorf("building channel queue: %w", err)
	}

	if len(channels) == 0 {
		o.logger.Println("no channels to sync")
		return nil
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = o.config.Sync.Workers
	}
	if workers <= 0 {
		workers = 1
	}

	o.progress.SetMessageChannels(len(channels))

	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()

	pool := NewWorkerPool(workers, poolCancel)
	pool.Start(poolCtx, func(ctx context.Context, task SyncTask) error {
		o.progress.SetCurrentChannel(task.ChannelID)
		if err := o.syncChannel(ctx, task.ChannelID, opts.Full); err != nil {
			if isNonFatalError(err) {
				o.logger.Printf("skipping channel %s: %v", task.ChannelID, err)
				o.progress.IncMessageChannel()
				return nil
			}
			return fmt.Errorf("syncing channel %s: %w", task.ChannelID, err)
		}
		o.progress.IncMessageChannel()
		return nil
	})

	for _, task := range channels {
		if !pool.Submit(poolCtx, task) {
			break
		}
	}
	pool.Close()

	errs := pool.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("message sync had %d errors; first: %w", len(errs), errs[0])
	}

	return nil
}

// buildChannelQueue fetches channels from the DB, applies any --channels filter,
// assigns priorities based on watch list and membership, and returns a sorted task list.
func (o *Orchestrator) buildChannelQueue(opts SyncOptions) ([]SyncTask, error) {
	allChannels, err := o.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		return nil, fmt.Errorf("fetching channels: %w", err)
	}

	// Build a filter set if --channels was specified
	var filterSet map[string]bool
	if len(opts.Channels) > 0 {
		filterSet = make(map[string]bool, len(opts.Channels))
		for _, ch := range opts.Channels {
			filterSet[strings.ToLower(ch)] = true
		}
	}

	// Get watch list for priority assignment
	watchList, err := o.db.GetWatchList()
	if err != nil {
		return nil, fmt.Errorf("fetching watch list: %w", err)
	}
	watchMap := make(map[string]string, len(watchList))
	for _, w := range watchList {
		if w.EntityType == "channel" {
			watchMap[w.EntityID] = w.Priority
		}
	}

	var tasks []SyncTask
	for _, ch := range allChannels {
		// Apply --channels filter (match by name or ID, case-insensitive)
		if filterSet != nil {
			if !filterSet[strings.ToLower(ch.Name)] && !filterSet[strings.ToLower(ch.ID)] {
				continue
			}
		}

		// Skip archived channels unless explicitly requested
		if ch.IsArchived && filterSet == nil {
			continue
		}

		priority := assignChannelPriority(ch, watchMap)
		tasks = append(tasks, SyncTask{
			ChannelID: ch.ID,
			Priority:  priority,
		})
	}

	SortTasksByPriority(tasks)
	return tasks, nil
}

// assignChannelPriority determines the sync priority for a channel
// based on watch list status and membership.
func assignChannelPriority(ch db.Channel, watchMap map[string]string) TaskPriority {
	if priority, ok := watchMap[ch.ID]; ok {
		switch priority {
		case "high":
			return PriorityWatchHigh
		case "normal":
			return PriorityWatchNormal
		case "low":
			return PriorityWatchLow
		}
	}
	if ch.IsMember {
		return PriorityMember
	}
	return PriorityRest
}

// syncChannel handles per-channel message sync logic:
// - Determines the oldest timestamp to fetch from (sync_state or initial_history_days cutoff)
// - Resumes pagination from cursor if a previous sync was interrupted
// - Paginates through conversations.history (200 messages per page)
// - Upserts messages in transactions per page
// - Updates sync_state after each page
// - Marks initial sync complete when done
func (o *Orchestrator) syncChannel(ctx context.Context, channelID string, full bool) error {
	state, err := o.db.GetSyncState(channelID)
	if err != nil {
		return fmt.Errorf("getting sync state: %w", err)
	}

	oldest := o.computeOldest(state, full)
	cursor := ""
	if !full && state != nil && state.Cursor != "" {
		cursor = state.Cursor
	}

	isInitial := state == nil || !state.IsInitialSyncComplete || full
	messagesSynced := 0
	if state != nil {
		messagesSynced = state.MessagesSynced
	}
	var latestTS string
	// Preserve previous LastSyncedTS during pagination so an interrupted sync
	// doesn't advance the high-water mark past unfetched messages.
	previousLastSyncedTS := ""
	if state != nil {
		previousLastSyncedTS = state.LastSyncedTS
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := o.slackClient.GetConversationHistory(ctx, watchtowerslack.HistoryOptions{
			ChannelID: channelID,
			Cursor:    cursor,
			Oldest:    oldest,
			Limit:     200,
		})
		if err != nil {
			// Save error in sync state so we can report it
			o.saveSyncError(channelID, state, err)
			return err
		}

		if len(resp.Messages) == 0 {
			break
		}

		// Track the latest timestamp we've seen (first message in response is newest)
		if latestTS == "" || resp.Messages[0].Timestamp > latestTS {
			latestTS = resp.Messages[0].Timestamp
		}

		// Upsert messages in a transaction
		count, err := o.upsertMessagePage(channelID, resp.Messages)
		if err != nil {
			return fmt.Errorf("upserting messages: %w", err)
		}
		messagesSynced += count
		o.progress.AddMessages(count)

		done := !resp.HasMore

		// Save cursor and message count for resumability, but only advance
		// LastSyncedTS once all pages have been fetched. This prevents an
		// interrupted sync from permanently skipping unfetched messages.
		savedTS := previousLastSyncedTS
		if done {
			savedTS = latestTS
		}
		syncState := db.SyncState{
			LastSyncedTS:          savedTS,
			OldestSyncedTS:        oldest,
			IsInitialSyncComplete: !isInitial || done,
			Cursor:                resp.NextCursor,
			MessagesSynced:        messagesSynced,
		}
		if err := o.db.UpdateSyncState(channelID, syncState); err != nil {
			return fmt.Errorf("updating sync state: %w", err)
		}

		if done {
			break
		}
		cursor = resp.NextCursor
	}

	return nil
}

// computeOldest determines the oldest timestamp to fetch from.
// For incremental sync: use last_synced_ts from sync_state.
// For initial sync or --full: compute cutoff from initial_history_days.
func (o *Orchestrator) computeOldest(state *db.SyncState, full bool) string {
	if !full && state != nil && state.IsInitialSyncComplete && state.LastSyncedTS != "" {
		return state.LastSyncedTS
	}

	days := o.config.Sync.InitialHistoryDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	return strconv.FormatFloat(float64(cutoff.Unix()), 'f', 6, 64)
}

// upsertMessagePage inserts/updates a page of messages in a single transaction.
func (o *Orchestrator) upsertMessagePage(channelID string, messages []goslack.Message) (int, error) {
	tx, err := o.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO messages (channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, raw_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, ts) DO UPDATE SET
			user_id = excluded.user_id,
			text = excluded.text,
			thread_ts = excluded.thread_ts,
			reply_count = excluded.reply_count,
			is_edited = excluded.is_edited,
			is_deleted = excluded.is_deleted,
			subtype = excluded.subtype,
			permalink = excluded.permalink,
			raw_json = excluded.raw_json`)
	if err != nil {
		return 0, fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	count := 0
	for _, msg := range messages {
		rawJSON, _ := json.Marshal(msg)
		threadTS := sql.NullString{}
		if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
			threadTS = sql.NullString{String: msg.ThreadTimestamp, Valid: true}
		}
		isEdited := msg.Edited != nil

		_, err := stmt.Exec(
			channelID,
			msg.Timestamp,
			msg.User,
			msg.Text,
			threadTS,
			msg.ReplyCount,
			isEdited,
			false, // is_deleted
			msg.SubType,
			"", // permalink generated later
			string(rawJSON),
		)
		if err != nil {
			return count, fmt.Errorf("upserting message %s: %w", msg.Timestamp, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing transaction: %w", err)
	}
	return count, nil
}

// saveSyncError persists a sync error in the sync_state table.
func (o *Orchestrator) saveSyncError(channelID string, state *db.SyncState, syncErr error) {
	s := db.SyncState{
		Error: syncErr.Error(),
	}
	if state != nil {
		s.LastSyncedTS = state.LastSyncedTS
		s.OldestSyncedTS = state.OldestSyncedTS
		s.IsInitialSyncComplete = state.IsInitialSyncComplete
		s.Cursor = state.Cursor
		s.MessagesSynced = state.MessagesSynced
	}
	if err := o.db.UpdateSyncState(channelID, s); err != nil {
		o.logger.Printf("failed to save sync error for %s: %v", channelID, err)
	}
}
