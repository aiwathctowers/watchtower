package db

import (
	"database/sql"
	"fmt"
)

// ToggleMuteForLLM toggles the is_muted_for_llm flag for a channel.
// Returns the new value of the flag.
func (db *DB) ToggleMuteForLLM(channelID string) (bool, error) {
	_, err := db.Exec(`
		INSERT INTO channel_settings (channel_id, is_muted_for_llm, updated_at)
		VALUES (?, 1, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(channel_id) DO UPDATE SET
			is_muted_for_llm = 1 - channel_settings.is_muted_for_llm,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		channelID)
	if err != nil {
		return false, fmt.Errorf("toggling mute for %s: %w", channelID, err)
	}
	var val bool
	err = db.QueryRow(`SELECT is_muted_for_llm FROM channel_settings WHERE channel_id = ?`, channelID).Scan(&val)
	if err != nil {
		return false, fmt.Errorf("reading mute state for %s: %w", channelID, err)
	}
	return val, nil
}

// ToggleFavorite toggles the is_favorite flag for a channel.
// Returns the new value of the flag.
func (db *DB) ToggleFavorite(channelID string) (bool, error) {
	_, err := db.Exec(`
		INSERT INTO channel_settings (channel_id, is_favorite, updated_at)
		VALUES (?, 1, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(channel_id) DO UPDATE SET
			is_favorite = 1 - channel_settings.is_favorite,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		channelID)
	if err != nil {
		return false, fmt.Errorf("toggling favorite for %s: %w", channelID, err)
	}
	var val bool
	err = db.QueryRow(`SELECT is_favorite FROM channel_settings WHERE channel_id = ?`, channelID).Scan(&val)
	if err != nil {
		return false, fmt.Errorf("reading favorite state for %s: %w", channelID, err)
	}
	return val, nil
}

// SetMuteForLLM sets the is_muted_for_llm flag explicitly.
func (db *DB) SetMuteForLLM(channelID string, muted bool) error {
	val := 0
	if muted {
		val = 1
	}
	_, err := db.Exec(`
		INSERT INTO channel_settings (channel_id, is_muted_for_llm, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(channel_id) DO UPDATE SET
			is_muted_for_llm = excluded.is_muted_for_llm,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		channelID, val)
	if err != nil {
		return fmt.Errorf("setting mute for %s: %w", channelID, err)
	}
	return nil
}

// SetFavorite sets the is_favorite flag explicitly.
func (db *DB) SetFavorite(channelID string, favorite bool) error {
	val := 0
	if favorite {
		val = 1
	}
	_, err := db.Exec(`
		INSERT INTO channel_settings (channel_id, is_favorite, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(channel_id) DO UPDATE SET
			is_favorite = excluded.is_favorite,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		channelID, val)
	if err != nil {
		return fmt.Errorf("setting favorite for %s: %w", channelID, err)
	}
	return nil
}

// GetChannelSettings returns settings for a channel. Returns nil if no settings exist.
func (db *DB) GetChannelSettings(channelID string) (*ChannelSettings, error) {
	var cs ChannelSettings
	err := db.QueryRow(`SELECT channel_id, is_muted_for_llm, is_favorite, updated_at
		FROM channel_settings WHERE channel_id = ?`, channelID).
		Scan(&cs.ChannelID, &cs.IsMutedForLLM, &cs.IsFavorite, &cs.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting channel settings for %s: %w", channelID, err)
	}
	return &cs, nil
}

// GetAllChannelSettings returns settings for all channels that have custom settings.
func (db *DB) GetAllChannelSettings() ([]ChannelSettings, error) {
	rows, err := db.Query(`SELECT channel_id, is_muted_for_llm, is_favorite, updated_at FROM channel_settings`)
	if err != nil {
		return nil, fmt.Errorf("querying channel settings: %w", err)
	}
	defer rows.Close()

	var settings []ChannelSettings
	for rows.Next() {
		var cs ChannelSettings
		if err := rows.Scan(&cs.ChannelID, &cs.IsMutedForLLM, &cs.IsFavorite, &cs.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel settings: %w", err)
		}
		settings = append(settings, cs)
	}
	return settings, rows.Err()
}

// GetMutedChannelIDs returns the list of channel IDs that are muted for LLM processing.
func (db *DB) GetMutedChannelIDs() ([]string, error) {
	rows, err := db.Query(`SELECT channel_id FROM channel_settings WHERE is_muted_for_llm = 1`)
	if err != nil {
		return nil, fmt.Errorf("querying muted channels: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning muted channel id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
