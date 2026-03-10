package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	goslack "github.com/slack-go/slack"

	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
)

// slackPageSize is the number of messages to request per Slack API call.
const slackPageSize = 200

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

	workers := o.resolveWorkerCount(opts.Workers)
	o.logger.Printf("starting message sync: %d channels, %d workers", len(channels), workers)

	o.progress.SetMessageChannels(len(channels))

	poolCtx, poolCancel := context.WithCancel(ctx)
	defer poolCancel()

	pool := NewWorkerPool(workers, poolCancel)
	pool.Start(poolCtx, func(ctx context.Context, task SyncTask) error {
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

	// Get sync states to skip channels with no messages in the history window
	syncStates, err := o.db.GetAllSyncStates()
	if err != nil {
		return nil, fmt.Errorf("fetching sync states: %w", err)
	}

	// Log channel breakdown for debugging
	var cntTotal, cntArchived, cntNonMember, cntDM, cntGroupDM, cntPublic, cntPrivate int
	for _, ch := range allChannels {
		cntTotal++
		if ch.IsArchived {
			cntArchived++
		}
		if !ch.IsMember {
			cntNonMember++
		}
		switch ch.Type {
		case "dm":
			cntDM++
		case "group_dm":
			cntGroupDM++
		case "public":
			cntPublic++
		case "private":
			cntPrivate++
		}
	}
	o.logger.Printf("channels in workspace: %d total (%d public, %d private, %d dm, %d group_dm, %d archived, %d non-member)",
		cntTotal, cntPublic, cntPrivate, cntDM, cntGroupDM, cntArchived, cntNonMember)

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

		// Skip channels where we're not a member (can't read history)
		// unless explicitly requested via --channels
		if !ch.IsMember && filterSet == nil {
			continue
		}

		// Skip DMs and group DMs if --skip-dms is set
		if (ch.Type == "dm" || ch.Type == "group_dm") && opts.SkipDMs && filterSet == nil {
			continue
		}

		// For incremental sync: only sync channels found active by discovery
		// or on the watch list. Skips API calls for inactive channels.
		// Channels with no sync state (never synced) are always included.
		if !opts.Full && filterSet == nil && len(o.discoveredChannelIDs) > 0 {
			if st := syncStates[ch.ID]; st != nil && st.IsInitialSyncComplete {
				_, isDiscovered := o.discoveredChannelIDs[ch.ID]
				_, isWatched := watchMap[ch.ID]
				if !isDiscovered && !isWatched {
					continue
				}
			}
		}

		priority := assignChannelPriority(ch, watchMap)
		tasks = append(tasks, SyncTask{
			ChannelID: ch.ID,
			Priority:  priority,
		})
	}

	// Build channel name map for logging
	o.channelNames = make(map[string]string, len(allChannels))
	for _, ch := range allChannels {
		o.channelNames[ch.ID] = ch.Name
	}

	SortTasksByPriority(tasks)
	o.logger.Printf("channels to sync: %d (after filters)", len(tasks))

	// Show skipped channel breakdown in progress display
	skipped := cntTotal - len(tasks)
	if skipped > 0 {
		parts := []string{}
		if cntDM+cntGroupDM > 0 && opts.SkipDMs {
			parts = append(parts, fmt.Sprintf("%d DMs", cntDM+cntGroupDM))
		}
		if cntArchived > 0 {
			parts = append(parts, fmt.Sprintf("%d archived", cntArchived))
		}
		if cntNonMember > 0 {
			parts = append(parts, fmt.Sprintf("%d non-member", cntNonMember))
		}
		if len(parts) > 0 {
			o.progress.SetChannelsSkippedInfo(fmt.Sprintf("skipped: %s", strings.Join(parts, ", ")))
		}
	}

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
		o.logger.Printf("channel %s: resuming from cursor", o.channelName(channelID))
	}

	isInitial := state == nil || !state.IsInitialSyncComplete || full
	if isInitial {
		o.logger.Printf("channel %s: initial sync (oldest=%s)", o.channelName(channelID), oldest)
	} else {
		o.logger.Printf("channel %s: incremental sync (since=%s)", o.channelName(channelID), oldest)
	}
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
			Limit:     slackPageSize,
		})
		if err != nil {
			// Save error in sync state so we can report it.
			// Pass oldest so interrupted initial syncs can resume with the same window.
			o.saveSyncError(channelID, state, oldest, err)
			return err
		}

		if len(resp.Messages) == 0 {
			o.logger.Printf("channel %s: no messages in window", o.channelName(channelID))
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
		o.logger.Printf("channel %s: fetched %d messages (total: %d)", o.channelName(channelID), count, messagesSynced)

		// Inline thread sync: immediately fetch replies for any thread parents
		// in this page so threads are available right after message sync.
		if o.config.Sync.SyncThreads {
			for _, msg := range resp.Messages {
				if msg.ReplyCount > 0 {
					replyCount, err := o.syncThread(ctx, channelID, msg.Timestamp)
					if err != nil {
						if isNonFatalError(err) {
							o.logger.Printf("skipping thread %s/%s: %v", channelID, msg.Timestamp, err)
							continue
						}
						return fmt.Errorf("syncing inline thread %s/%s: %w", channelID, msg.Timestamp, err)
					}
					if replyCount > 0 {
						messagesSynced += replyCount
						o.progress.AddMessages(replyCount)
						o.logger.Printf("channel %s: thread %s: %d replies", o.channelName(channelID), msg.Timestamp, replyCount)
					}
				}
			}
		}

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
			o.logger.Printf("channel %s: done (%d messages)", o.channelName(channelID), messagesSynced)
			break
		}
		cursor = resp.NextCursor
	}

	return nil
}

// computeOldest determines the oldest timestamp to fetch from.
// For incremental sync: use last_synced_ts from sync_state.
// For resumed initial sync (interrupted by rate limit/error): use the original
// oldest_synced_ts so the cursor remains valid.
// For fresh initial sync or --full: compute cutoff from initial_history_days.
func (o *Orchestrator) computeOldest(state *db.SyncState, full bool) string {
	if !full && state != nil {
		// Completed initial sync → incremental from last_synced_ts
		if state.IsInitialSyncComplete && state.LastSyncedTS != "" {
			return state.LastSyncedTS
		}
		// Interrupted initial sync → resume with original oldest to keep cursor valid
		if !state.IsInitialSyncComplete && state.OldestSyncedTS != "" {
			return state.OldestSyncedTS
		}
	}

	days := o.config.Sync.InitialHistoryDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	return fmt.Sprintf("%d.000000", cutoff.Unix())
}

// upsertMessagePage inserts/updates a page of messages in a single transaction.
func (o *Orchestrator) upsertMessagePage(channelID string, messages []goslack.Message) (int, error) {
	tx, err := o.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	dbMsgs := make([]db.Message, 0, len(messages))
	for _, msg := range messages {
		rawJSON, err := json.Marshal(msg)
		if err != nil {
			o.logger.Printf("warning: failed to marshal message %s: %v", msg.Timestamp, err)
			rawJSON = []byte("{}")
		}
		threadTS := sql.NullString{}
		if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
			threadTS = sql.NullString{String: msg.ThreadTimestamp, Valid: true}
		}
		dbMsgs = append(dbMsgs, db.Message{
			ChannelID:  channelID,
			TS:         msg.Timestamp,
			UserID:     msg.User,
			Text:       msg.Text,
			ThreadTS:   threadTS,
			ReplyCount: msg.ReplyCount,
			IsEdited:   msg.Edited != nil,
			IsDeleted:  false,
			Subtype:    msg.SubType,
			Permalink:  "", // permalink generated at query time by context builder
			RawJSON:    string(rawJSON),
		})
	}

	count, err := o.db.UpsertMessageBatch(tx, dbMsgs)
	if err != nil {
		return 0, fmt.Errorf("upserting messages: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing transaction: %w", err)
	}
	return count, nil
}

// saveSyncError persists a sync error in the sync_state table.
// oldest is the timestamp window used by this sync attempt, preserved so
// a resumed sync uses the same window and keeps pagination cursors valid.
func (o *Orchestrator) saveSyncError(channelID string, state *db.SyncState, oldest string, syncErr error) {
	// Re-read current state to get the latest cursor (may have been updated during pagination).
	// If the re-read fails, skip the update to avoid overwriting newer state with stale data.
	current, err := o.db.GetSyncState(channelID)
	if err != nil {
		o.logger.Printf("failed to re-read sync state for %s, skipping error save: %v", channelID, err)
		return
	}
	if current != nil {
		state = current
	}

	s := db.SyncState{
		OldestSyncedTS: oldest,
		Error:          syncErr.Error(),
	}
	if state != nil {
		s.LastSyncedTS = state.LastSyncedTS
		if state.OldestSyncedTS != "" {
			s.OldestSyncedTS = state.OldestSyncedTS
		}
		s.IsInitialSyncComplete = state.IsInitialSyncComplete
		s.Cursor = state.Cursor
		s.MessagesSynced = state.MessagesSynced
	}
	if err := o.db.UpdateSyncState(channelID, s); err != nil {
		o.logger.Printf("failed to save sync error for %s: %v", channelID, err)
	}
}
