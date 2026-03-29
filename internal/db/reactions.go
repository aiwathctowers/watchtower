package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// UpsertReactionBatch inserts reactions in bulk within the given transaction.
// Duplicate (channel_id, message_ts, user_id, emoji) rows are silently ignored.
func (db *DB) UpsertReactionBatch(tx *sql.Tx, reactions []Reaction) error {
	if len(reactions) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO reactions (channel_id, message_ts, user_id, emoji) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing reaction insert: %w", err)
	}
	defer stmt.Close()
	for _, r := range reactions {
		if _, err := stmt.Exec(r.ChannelID, r.MessageTS, r.UserID, r.Emoji); err != nil {
			return fmt.Errorf("inserting reaction %s on %s: %w", r.Emoji, r.MessageTS, err)
		}
	}
	return nil
}

// GetReactionsForMessages returns aggregated reaction counts for a set of messages
// in a single channel. The result maps message_ts → slice of ReactionSummary (sorted
// by count descending, limited to top 5 per message).
func (db *DB) GetReactionsForMessages(channelID string, messageTSs []string) (map[string][]ReactionSummary, error) {
	if len(messageTSs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(messageTSs))
	args := make([]interface{}, 0, len(messageTSs)+1)
	args = append(args, channelID)
	for i, ts := range messageTSs {
		placeholders[i] = "?"
		args = append(args, ts)
	}

	query := fmt.Sprintf(`
		SELECT message_ts, emoji, COUNT(*) as cnt
		FROM reactions
		WHERE channel_id = ? AND message_ts IN (%s)
		GROUP BY message_ts, emoji
		ORDER BY message_ts, cnt DESC`,
		strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying reactions: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]ReactionSummary)
	for rows.Next() {
		var ts, emoji string
		var cnt int
		if err := rows.Scan(&ts, &emoji, &cnt); err != nil {
			return nil, fmt.Errorf("scanning reaction: %w", err)
		}
		if len(result[ts]) < 5 {
			result[ts] = append(result[ts], ReactionSummary{Emoji: emoji, Count: cnt})
		}
	}
	return result, rows.Err()
}

// FormatReactions formats a slice of ReactionSummary into a compact string
// like "[+1:3 fire:2]". Returns empty string if no reactions.
func FormatReactions(reactions []ReactionSummary) string {
	if len(reactions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(" [")
	for i, r := range reactions {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%s:%d", r.Emoji, r.Count)
	}
	sb.WriteByte(']')
	return sb.String()
}
