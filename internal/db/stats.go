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

// GetStats returns aggregate counts for the database.
func (db *DB) GetStats() (*Stats, error) {
	var s Stats

	err := db.QueryRow(`SELECT COUNT(*) FROM channels`).Scan(&s.ChannelCount)
	if err != nil {
		return nil, fmt.Errorf("counting channels: %w", err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM watch_list`).Scan(&s.WatchedCount)
	if err != nil {
		return nil, fmt.Errorf("counting watch list: %w", err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&s.UserCount)
	if err != nil {
		return nil, fmt.Errorf("counting users: %w", err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&s.MessageCount)
	if err != nil {
		return nil, fmt.Errorf("counting messages: %w", err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM messages WHERE reply_count > 0`).Scan(&s.ThreadCount)
	if err != nil {
		return nil, fmt.Errorf("counting threads: %w", err)
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
