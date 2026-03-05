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

// GetAllSyncStates returns sync states for all channels as a map keyed by channel_id.
func (db *DB) GetAllSyncStates() (map[string]*SyncState, error) {
	rows, err := db.Query(`
		SELECT channel_id, last_synced_ts, oldest_synced_ts, is_initial_sync_complete,
			cursor, messages_synced, last_sync_at, error
		FROM sync_state`)
	if err != nil {
		return nil, fmt.Errorf("querying all sync states: %w", err)
	}
	defer rows.Close()

	states := make(map[string]*SyncState)
	for rows.Next() {
		var s SyncState
		if err := rows.Scan(
			&s.ChannelID, &s.LastSyncedTS, &s.OldestSyncedTS, &s.IsInitialSyncComplete,
			&s.Cursor, &s.MessagesSynced, &s.LastSyncAt, &s.Error,
		); err != nil {
			return nil, fmt.Errorf("scanning sync state: %w", err)
		}
		states[s.ChannelID] = &s
	}
	return states, rows.Err()
}

// UpdateSyncState inserts or updates the sync state for a channel.
// Only updates last_sync_at when the sync succeeded (state.Error is empty).
func (db *DB) UpdateSyncState(channelID string, state SyncState) error {
	_, err := db.Exec(`
		INSERT INTO sync_state (channel_id, last_synced_ts, oldest_synced_ts, is_initial_sync_complete, cursor, messages_synced, last_sync_at, error)
		VALUES (?, ?, ?, ?, ?, ?,
			CASE WHEN ? = '' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE NULL END,
			?)
		ON CONFLICT(channel_id) DO UPDATE SET
			last_synced_ts = excluded.last_synced_ts,
			oldest_synced_ts = excluded.oldest_synced_ts,
			is_initial_sync_complete = excluded.is_initial_sync_complete,
			cursor = excluded.cursor,
			messages_synced = excluded.messages_synced,
			last_sync_at = CASE WHEN excluded.error = '' THEN strftime('%Y-%m-%dT%H:%M:%SZ', 'now') ELSE sync_state.last_sync_at END,
			error = excluded.error`,
		channelID, state.LastSyncedTS, state.OldestSyncedTS,
		state.IsInitialSyncComplete, state.Cursor, state.MessagesSynced,
		state.Error, state.Error,
	)
	if err != nil {
		return fmt.Errorf("updating sync state for %s: %w", channelID, err)
	}
	return nil
}
