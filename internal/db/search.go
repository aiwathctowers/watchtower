package db

import (
	"fmt"
	"strings"
)

// SearchOpts provides filtering options for full-text search.
type SearchOpts struct {
	ChannelIDs []string
	UserIDs    []string
	FromUnix   float64
	ToUnix     float64
	Limit      int
}

// SearchMessages performs a full-text search on messages using FTS5.
// The query string is passed to FTS5 MATCH. Results can be filtered by
// channel, user, and time range. Returns messages joined with the messages
// table for full data.
func (db *DB) SearchMessages(query string, opts SearchOpts) ([]Message, error) {
	if query == "" {
		return nil, nil
	}

	sqlQuery := `
		SELECT m.channel_id, m.ts, m.user_id, m.text, m.thread_ts, m.reply_count,
			m.is_edited, m.is_deleted, m.subtype, m.permalink, m.ts_unix, m.raw_json
		FROM messages_fts fts
		JOIN messages m ON m.channel_id = fts.channel_id AND m.ts = fts.ts`

	var conditions []string
	var args []interface{}

	// Sanitize FTS5 query: quote each term to prevent FTS5 syntax injection
	sanitizedQuery := sanitizeFTS5Query(query)
	conditions = append(conditions, "messages_fts MATCH ?")
	args = append(args, sanitizedQuery)

	if len(opts.ChannelIDs) > 0 {
		placeholders := make([]string, len(opts.ChannelIDs))
		for i, id := range opts.ChannelIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, "m.channel_id IN ("+strings.Join(placeholders, ",")+")")
	}

	if len(opts.UserIDs) > 0 {
		placeholders := make([]string, len(opts.UserIDs))
		for i, id := range opts.UserIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, "m.user_id IN ("+strings.Join(placeholders, ",")+")")
	}

	if opts.FromUnix > 0 {
		conditions = append(conditions, "m.ts_unix >= ?")
		args = append(args, opts.FromUnix)
	}

	if opts.ToUnix > 0 {
		conditions = append(conditions, "m.ts_unix <= ?")
		args = append(args, opts.ToUnix)
	}

	sqlQuery += " WHERE " + strings.Join(conditions, " AND ")
	sqlQuery += " ORDER BY m.ts_unix DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	sqlQuery += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("searching messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// sanitizeFTS5Query escapes user input for safe use in FTS5 MATCH queries.
// Each word is wrapped in double quotes to treat it as a literal token,
// preventing FTS5 operator injection (AND, OR, NOT, NEAR, etc.).
func sanitizeFTS5Query(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return query
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		// Escape any embedded double quotes
		w = strings.ReplaceAll(w, `"`, `""`)
		quoted[i] = `"` + w + `"`
	}
	return strings.Join(quoted, " ")
}
