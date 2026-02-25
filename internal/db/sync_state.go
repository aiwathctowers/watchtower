package db

import (
	"database/sql"
	"fmt"
)

// GetSyncState returns the sync state for a channel.
// Returns nil if no sync state exists for the channel.
func (db *DB) GetSyncState(channelID string) (*SyncState, error) {
	var s SyncState
	err := db.QueryRow(`
		SELECT channel_id, last_synced_ts, oldest_synced_ts, is_initial_sync_complete,
			cursor, messages_synced, last_sync_at, error
		FROM sync_state WHERE channel_id = ?`, channelID,
	).Scan(
		&s.ChannelID, &s.LastSyncedTS, &s.OldestSyncedTS, &s.IsInitialSyncComplete,
		&s.Cursor, &s.MessagesSynced, &s.LastSyncAt, &s.Error,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting sync state for %s: %w", channelID, err)
	}
	return &s, nil
}

// UpdateSyncState inserts or updates the sync state for a channel.
func (db *DB) UpdateSyncState(channelID string, state SyncState) error {
	_, err := db.Exec(`
		INSERT INTO sync_state (channel_id, last_synced_ts, oldest_synced_ts, is_initial_sync_complete, cursor, messages_synced, last_sync_at, error)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), ?)
		ON CONFLICT(channel_id) DO UPDATE SET
			last_synced_ts = excluded.last_synced_ts,
			oldest_synced_ts = excluded.oldest_synced_ts,
			is_initial_sync_complete = excluded.is_initial_sync_complete,
			cursor = excluded.cursor,
			messages_synced = excluded.messages_synced,
			last_sync_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
			error = excluded.error`,
		channelID, state.LastSyncedTS, state.OldestSyncedTS,
		state.IsInitialSyncComplete, state.Cursor, state.MessagesSynced, state.Error,
	)
	if err != nil {
		return fmt.Errorf("updating sync state for %s: %w", channelID, err)
	}
	return nil
}

