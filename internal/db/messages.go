package db

import (
	"database/sql"
	"fmt"
	"strings"
)

const (
	DefaultMessageLimit         = 100
	DefaultTimeRangeLimit       = 500
	DefaultChannelActivityLimit = 10
	DefaultUserActivityLimit    = 5
	DefaultSearchLimit          = 50
)

// MessageOpts provides options for querying messages.
type MessageOpts struct {
	ChannelID  string
	UserID     string
	FromUnix   float64
	ToUnix     float64
	Limit      int
	ExcludeDMs bool // exclude dm and group_dm channels
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

// UpsertMessageBatch inserts or updates a batch of messages within an existing
// transaction, using a prepared statement for efficiency.
func (db *DB) UpsertMessageBatch(tx *sql.Tx, msgs []Message) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("UpsertMessageBatch: nil transaction")
	}
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
	for _, msg := range msgs {
		_, err := stmt.Exec(
			msg.ChannelID, msg.TS, msg.UserID, msg.Text, msg.ThreadTS,
			msg.ReplyCount, msg.IsEdited, msg.IsDeleted, msg.Subtype,
			msg.Permalink, msg.RawJSON,
		)
		if err != nil {
			return 0, fmt.Errorf("upserting message %s/%s: %w", msg.ChannelID, msg.TS, err)
		}
		count++
	}
	return count, nil
}

// GetMessages returns messages matching the given options.
func (db *DB) GetMessages(opts MessageOpts) ([]Message, error) {
	fromTable := "messages"
	if opts.ExcludeDMs {
		fromTable = "messages m JOIN channels c ON c.id = m.channel_id"
	}
	query := `SELECT ` + msgColumns(opts.ExcludeDMs) + ` FROM ` + fromTable
	var conditions []string
	var args []any

	col := func(name string) string {
		if opts.ExcludeDMs {
			return "m." + name
		}
		return name
	}

	if opts.ExcludeDMs {
		conditions = append(conditions, "c.type NOT IN ('dm', 'group_dm')")
	}
	if opts.ChannelID != "" {
		conditions = append(conditions, col("channel_id")+" = ?")
		args = append(args, opts.ChannelID)
	}
	if opts.UserID != "" {
		conditions = append(conditions, col("user_id")+" = ?")
		args = append(args, opts.UserID)
	}
	if opts.FromUnix > 0 {
		conditions = append(conditions, col("ts_unix")+" >= ?")
		args = append(args, opts.FromUnix)
	}
	if opts.ToUnix > 0 {
		conditions = append(conditions, col("ts_unix")+" <= ?")
		args = append(args, opts.ToUnix)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY " + col("ts_unix") + " DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultMessageLimit
	}
	query += " LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

const msgSelectCols = `channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json`

func msgColumns(prefixed bool) string {
	if !prefixed {
		return msgSelectCols
	}
	return `m.channel_id, m.ts, m.user_id, m.text, m.thread_ts, m.reply_count, m.is_edited, m.is_deleted, m.subtype, m.permalink, m.ts_unix, m.raw_json`
}

// GetMessagesByTimeRange returns messages in a channel within a Unix timestamp range.
// Results are limited to DefaultTimeRangeLimit rows (newest first) to bound memory usage.
func (db *DB) GetMessagesByTimeRange(channelID string, from, to float64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND ts_unix >= ? AND ts_unix <= ?
		ORDER BY ts_unix DESC
		LIMIT ?`,
		channelID, from, to, DefaultTimeRangeLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages by time range: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// CountMessagesByTimeRange returns the number of messages in a time range.
func (db *DB) CountMessagesByTimeRange(from, to float64) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE ts_unix >= ? AND ts_unix <= ?`, from, to).Scan(&count)
	return count, err
}

// ChannelMessageCount holds a channel ID/name pair with a message count.
type ChannelMessageCount struct {
	ChannelID string
	Name      string
	Count     int
}

// UserMessageCount holds a user ID with a message count.
type UserMessageCount struct {
	UserID string
	Count  int
}

// GetChannelActivityCounts returns message counts per non-archived channel in a time range,
// ordered by count descending, limited to the top N channels.
func (db *DB) GetChannelActivityCounts(from, to float64, limit int) ([]ChannelMessageCount, error) {
	if limit <= 0 {
		limit = DefaultChannelActivityLimit
	}
	rows, err := db.Query(`
		SELECT m.channel_id, c.name, COUNT(*) as cnt
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ? AND c.is_archived = 0
		GROUP BY m.channel_id
		ORDER BY cnt DESC
		LIMIT ?`,
		from, to, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying channel activity counts: %w", err)
	}
	defer rows.Close()

	var results []ChannelMessageCount
	for rows.Next() {
		var r ChannelMessageCount
		if err := rows.Scan(&r.ChannelID, &r.Name, &r.Count); err != nil {
			return nil, fmt.Errorf("scanning channel activity count: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetUserActivityCounts returns message counts per user in a time range,
// ordered by count descending, limited to the top N users.
func (db *DB) GetUserActivityCounts(from, to float64, limit int) ([]UserMessageCount, error) {
	if limit <= 0 {
		limit = DefaultUserActivityLimit
	}
	rows, err := db.Query(`
		SELECT user_id, COUNT(*) as cnt
		FROM messages
		WHERE ts_unix >= ? AND ts_unix <= ? AND user_id != ''
		GROUP BY user_id
		ORDER BY cnt DESC
		LIMIT ?`,
		from, to, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying user activity counts: %w", err)
	}
	defer rows.Close()

	var results []UserMessageCount
	for rows.Next() {
		var r UserMessageCount
		if err := rows.Scan(&r.UserID, &r.Count); err != nil {
			return nil, fmt.Errorf("scanning user activity count: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetMessagesByChannel returns the most recent messages in a channel.
func (db *DB) GetMessagesByChannel(channelID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = DefaultMessageLimit
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

// GetThreadReplies returns messages in a thread (including the parent), limited to 200 replies.
func (db *DB) GetThreadReplies(channelID, threadTS string) ([]Message, error) {
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND (ts = ? OR thread_ts = ?)
		ORDER BY ts_unix ASC
		LIMIT 200`,
		channelID, threadTS, threadTS,
	)
	if err != nil {
		return nil, fmt.Errorf("querying thread replies: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetAllThreadParents returns messages with reply_count > 0 across all channels
// that likely need thread reply syncing, limited to at most `limit` rows.
// NOTE: For large databases, consider adding an index on (channel_id, thread_ts)
// to optimize this query's JOIN + subquery.
func (db *DB) GetAllThreadParents(limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := db.Query(`
		SELECT m.channel_id, m.ts, m.user_id, m.text, m.thread_ts, m.reply_count,
			m.is_edited, m.is_deleted, m.subtype, m.permalink, m.ts_unix, m.raw_json
		FROM messages m
		LEFT JOIN (
			SELECT channel_id, thread_ts, COUNT(*) as cnt
			FROM messages
			WHERE thread_ts IS NOT NULL
			GROUP BY channel_id, thread_ts
		) r ON r.channel_id = m.channel_id AND r.thread_ts = m.ts
		WHERE m.reply_count > 0 AND COALESCE(r.cnt, 0) < m.reply_count
		ORDER BY m.ts_unix DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying all thread parents: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// GetMessageNear finds the message in a channel closest to the given Unix
// timestamp, within a tolerance of +/- 60 seconds. Returns nil if no match.
func (db *DB) GetMessageNear(channelID string, tsUnix float64) (*Message, error) {
	const tolerance = 60.0
	row := db.QueryRow(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND ts_unix >= ? AND ts_unix <= ?
		ORDER BY ABS(ts_unix - ?) ASC
		LIMIT 1`,
		channelID, tsUnix-tolerance, tsUnix+tolerance, tsUnix,
	)

	var msg Message
	err := row.Scan(
		&msg.ChannelID, &msg.TS, &msg.UserID, &msg.Text, &msg.ThreadTS,
		&msg.ReplyCount, &msg.IsEdited, &msg.IsDeleted, &msg.Subtype,
		&msg.Permalink, &msg.TSUnix, &msg.RawJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting message near ts %f in %s: %w", tsUnix, channelID, err)
	}
	return &msg, nil
}

// GetThreadRepliesAfterTS returns thread replies in a channel where
// thread_ts matches the given parent TS and the message TS is strictly
// greater than afterTS. Results are ordered oldest-first, capped at 200.
func (db *DB) GetThreadRepliesAfterTS(channelID, threadTS, afterTS string) ([]Message, error) {
	rows, err := db.Query(`
		SELECT channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix, raw_json
		FROM messages
		WHERE channel_id = ? AND thread_ts = ? AND ts > ?
		ORDER BY ts_unix ASC
		LIMIT 200`,
		channelID, threadTS, afterTS,
	)
	if err != nil {
		return nil, fmt.Errorf("querying thread replies after ts: %w", err)
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
