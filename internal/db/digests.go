package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// UpsertDigest inserts or replaces a digest based on the unique constraint
// (channel_id, type, period_from, period_to).
func (db *DB) UpsertDigest(d Digest) (int64, error) {
	_, err := db.Exec(`INSERT INTO digests (channel_id, type, period_from, period_to, summary, topics, decisions, action_items, message_count, model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, type, period_from, period_to) DO UPDATE SET
			summary = excluded.summary,
			topics = excluded.topics,
			decisions = excluded.decisions,
			action_items = excluded.action_items,
			message_count = excluded.message_count,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		d.ChannelID, d.Type, d.PeriodFrom, d.PeriodTo,
		d.Summary, d.Topics, d.Decisions, d.ActionItems,
		d.MessageCount, d.Model, d.InputTokens, d.OutputTokens, d.CostUSD)
	if err != nil {
		return 0, fmt.Errorf("upserting digest: %w", err)
	}
	// LastInsertId is unreliable for ON CONFLICT DO UPDATE; query the row explicitly.
	var id int64
	err = db.QueryRow(`SELECT id FROM digests WHERE channel_id = ? AND type = ? AND period_from = ? AND period_to = ?`,
		d.ChannelID, d.Type, d.PeriodFrom, d.PeriodTo).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting digest id after upsert: %w", err)
	}
	return id, nil
}

// DigestFilter specifies criteria for querying digests.
type DigestFilter struct {
	ChannelID string  // filter by channel (empty = any)
	Type      string  // filter by type (empty = any)
	FromUnix  float64 // period_from >= this (0 = no filter)
	ToUnix    float64 // period_to <= this (0 = no filter)
	Limit     int     // max results (0 = no limit)
}

// GetDigests returns digests matching the filter, newest first.
func (db *DB) GetDigests(f DigestFilter) ([]Digest, error) {
	query := `SELECT id, channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model, input_tokens, output_tokens, cost_usd, created_at FROM digests`
	var conditions []string
	var args []any

	if f.ChannelID != "" {
		conditions = append(conditions, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, f.Type)
	}
	if f.FromUnix > 0 {
		conditions = append(conditions, "period_from >= ?")
		args = append(args, f.FromUnix)
	}
	if f.ToUnix > 0 {
		conditions = append(conditions, "period_to <= ?")
		args = append(args, f.ToUnix)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY period_to DESC, period_from DESC`

	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying digests: %w", err)
	}
	defer rows.Close()

	var digests []Digest
	for rows.Next() {
		var d Digest
		if err := rows.Scan(&d.ID, &d.ChannelID, &d.PeriodFrom, &d.PeriodTo, &d.Type,
			&d.Summary, &d.Topics, &d.Decisions, &d.ActionItems,
			&d.MessageCount, &d.Model, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning digest: %w", err)
		}
		digests = append(digests, d)
	}
	return digests, rows.Err()
}

// GetLatestDigest returns the most recent digest for a channel and type,
// or nil if none exists.
func (db *DB) GetLatestDigest(channelID, digestType string) (*Digest, error) {
	var d Digest
	err := db.QueryRow(`SELECT id, channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model, input_tokens, output_tokens, cost_usd, created_at
		FROM digests WHERE channel_id = ? AND type = ?
		ORDER BY period_to DESC LIMIT 1`, channelID, digestType).
		Scan(&d.ID, &d.ChannelID, &d.PeriodFrom, &d.PeriodTo, &d.Type,
			&d.Summary, &d.Topics, &d.Decisions, &d.ActionItems,
			&d.MessageCount, &d.Model, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest digest: %w", err)
	}
	return &d, nil
}

// GetDigestByID returns a single digest by its ID.
func (db *DB) GetDigestByID(id int) (*Digest, error) {
	var d Digest
	err := db.QueryRow(`SELECT id, channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model, input_tokens, output_tokens, cost_usd, created_at
		FROM digests WHERE id = ?`, id).
		Scan(&d.ID, &d.ChannelID, &d.PeriodFrom, &d.PeriodTo, &d.Type,
			&d.Summary, &d.Topics, &d.Decisions, &d.ActionItems,
			&d.MessageCount, &d.Model, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting digest by id: %w", err)
	}
	return &d, nil
}

// DeleteDigestsOlderThan removes digests with period_to before the given Unix timestamp.
func (db *DB) DeleteDigestsOlderThan(beforeUnix float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM digests WHERE period_to < ?`, beforeUnix)
	if err != nil {
		return 0, fmt.Errorf("deleting old digests: %w", err)
	}
	return res.RowsAffected()
}

// DigestStats holds aggregate usage statistics for digests.
type DigestStats struct {
	TotalDigests  int
	TotalMessages int
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
}

// GetDigestStats returns aggregate stats for digests matching the filter.
func (db *DB) GetDigestStats(f DigestFilter) (DigestStats, error) {
	query := `SELECT COUNT(*), COALESCE(SUM(message_count),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cost_usd),0) FROM digests`
	var conditions []string
	var args []any

	if f.ChannelID != "" {
		conditions = append(conditions, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, f.Type)
	}
	if f.FromUnix > 0 {
		conditions = append(conditions, "period_from >= ?")
		args = append(args, f.FromUnix)
	}
	if f.ToUnix > 0 {
		conditions = append(conditions, "period_to <= ?")
		args = append(args, f.ToUnix)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	var s DigestStats
	err := db.QueryRow(query, args...).Scan(&s.TotalDigests, &s.TotalMessages, &s.InputTokens, &s.OutputTokens, &s.CostUSD)
	if err != nil {
		return s, fmt.Errorf("querying digest stats: %w", err)
	}
	return s, nil
}

// ChannelsWithNewMessages returns channel IDs that have messages after the given
// Unix timestamp. Used to determine which channels need new digests.
func (db *DB) ChannelsWithNewMessages(sinceUnix float64) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT channel_id FROM messages WHERE ts_unix > ? ORDER BY channel_id`, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("querying channels with new messages: %w", err)
	}
	defer rows.Close()

	var channels []string
	for rows.Next() {
		var ch string
		if err := rows.Scan(&ch); err != nil {
			return nil, fmt.Errorf("scanning channel id: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}
