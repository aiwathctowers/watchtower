package db

import (
	"database/sql"
	"fmt"
)

// MessageOpts provides options for querying messages.
type MessageOpts struct {
	ChannelID string
	UserID    string
	Limit     int
}

// UpsertMessage inserts or updates a message.
func (db *DB) UpsertMessage(msg Message) error {
	_, err := db.Exec(`
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
			raw_json = excluded.raw_json`,
		msg.ChannelID, msg.TS, msg.UserID, msg.Text, msg.ThreadTS,
		msg.ReplyCount, msg.IsEdited, msg.IsDeleted, msg.Subtype,
		msg.Permalink, msg.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("upserting message %s/%s: %w", msg.ChannelID, msg.TS, err)
	}
	return nil
}

// GetMessages returns messages matching the given options.
func (db *DB) GetMessages(opts MessageOpts) ([]Message, error) {
	query := `SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json FROM messages`
	var conditions []string
	var args []interface{}

	if opts.ChannelID != "" {
		conditions = append(conditions, "channel_id = ?")
		args = append(args, opts.ChannelID)
	}
	if opts.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, opts.UserID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY ts_unix DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetMessagesByTimeRange returns messages in a channel within a Unix timestamp range.
func (db *DB) GetMessagesByTimeRange(channelID string, from, to float64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND ts_unix >= ? AND ts_unix <= ?
		ORDER BY ts_unix ASC`,
		channelID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages by time range: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetMessagesByChannel returns the most recent messages in a channel.
func (db *DB) GetMessagesByChannel(channelID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ?
		ORDER BY ts_unix DESC
		LIMIT ?`,
		channelID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages by channel: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetThreadReplies returns all messages in a thread, including the parent.
func (db *DB) GetThreadReplies(channelID, threadTS string) ([]Message, error) {
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND (ts = ? OR thread_ts = ?)
		ORDER BY ts_unix ASC`,
		channelID, threadTS, threadTS,
	)
	if err != nil {
		return nil, fmt.Errorf("querying thread replies: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(
			&msg.ChannelID, &msg.TS, &msg.UserID, &msg.Text, &msg.ThreadTS,
			&msg.ReplyCount, &msg.IsEdited, &msg.IsDeleted, &msg.Subtype,
			&msg.Permalink, &msg.TSUnix, &msg.RawJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}
