package db

import "fmt"

// Stats holds aggregate statistics about the database contents.
type Stats struct {
	ChannelCount int
	WatchedCount int
	UserCount    int
	MessageCount int
	ThreadCount  int
}

// GetStats returns aggregate counts for the database in a single query.
func (db *DB) GetStats() (*Stats, error) {
	var s Stats
	err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM channels),
		(SELECT COUNT(*) FROM watch_list),
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM messages),
		(SELECT COUNT(*) FROM messages WHERE reply_count > 0)`).
		Scan(&s.ChannelCount, &s.WatchedCount, &s.UserCount, &s.MessageCount, &s.ThreadCount)
	if err != nil {
		return nil, fmt.Errorf("querying stats: %w", err)
	}
	return &s, nil
}

// LastSyncTime returns the most recent last_sync_at across all channels,
// or empty string if no sync has occurred.
func (db *DB) LastSyncTime() (string, error) {
	var lastSync *string
	err := db.QueryRow(`SELECT MAX(last_sync_at) FROM sync_state`).Scan(&lastSync)
	if err != nil {
		return "", fmt.Errorf("getting last sync time: %w", err)
	}
	if lastSync == nil {
		return "", nil
	}
	return *lastSync, nil
}
