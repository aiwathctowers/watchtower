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

	// Sanitize FTS5 query: strip operators and special characters
	sanitizedQuery := sanitizeFTS5Query(query)
	if sanitizedQuery == "" {
		return nil, nil
	}
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
// FTS5 operators (AND, OR, NOT, NEAR) and special characters are stripped,
// while normal words are left unquoted so the porter stemmer can work.
func sanitizeFTS5Query(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return query
	}
	// FTS5 reserved operators (case-insensitive)
	operators := map[string]bool{
		"AND": true, "OR": true, "NOT": true, "NEAR": true,
	}
	var safe []string
	for _, w := range words {
		// Skip FTS5 operators
		if operators[strings.ToUpper(w)] {
			continue
		}
		// Strip FTS5 special characters: * ^ : "
		w = strings.Map(func(r rune) rune {
			switch r {
			case '*', '^', ':', '"', '(', ')', '+':
				return -1
			default:
				return r
			}
		}, w)
		if w != "" {
			safe = append(safe, w)
		}
	}
	if len(safe) == 0 {
		return ""
	}
	return strings.Join(safe, " ")
}
