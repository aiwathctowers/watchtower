package sync

import (
	"context"
	"database/sql"
	"encoding/json"
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

	workers := opts.Workers
	if workers <= 0 {
		workers = o.config.Sync.Workers
	}
	if workers <= 0 {
		workers = 1
	}

	pool := NewWorkerPool(workers)
	pool.Start(ctx, func(ctx context.Context, task SyncTask) error {
		if err := o.syncThread(ctx, task.ChannelID, task.ThreadTS); err != nil {
			if isNonFatalError(err) {
				o.logger.Printf("skipping thread %s/%s: %v", task.ChannelID, task.ThreadTS, err)
				return nil
			}
			return fmt.Errorf("syncing thread %s/%s: %w", task.ChannelID, task.ThreadTS, err)
		}
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
func (o *Orchestrator) syncThread(ctx context.Context, channelID, threadTS string) error {
	replies, err := o.slackClient.GetConversationReplies(ctx, channelID, threadTS)
	if err != nil {
		return err
	}

	if len(replies) == 0 {
		return nil
	}

	tx, err := o.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
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
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, msg := range replies {
		rawJSON, _ := json.Marshal(msg)
		threadTSVal := sql.NullString{}
		if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
			threadTSVal = sql.NullString{String: msg.ThreadTimestamp, Valid: true}
		}
		isEdited := msg.Edited != nil

		_, err := stmt.Exec(
			channelID,
			msg.Timestamp,
			msg.User,
			msg.Text,
			threadTSVal,
			msg.ReplyCount,
			isEdited,
			false, // is_deleted
			msg.SubType,
			"", // permalink generated later
			string(rawJSON),
		)
		if err != nil {
			return fmt.Errorf("upserting thread reply %s: %w", msg.Timestamp, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}
