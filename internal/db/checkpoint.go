package db

import (
	"database/sql"
	"fmt"
	"time"
)

// GetCheckpoint returns the user's last catchup checkpoint time.
// Returns nil if no checkpoint has been set.
func (db *DB) GetCheckpoint() (*UserCheckpoint, error) {
	var cp UserCheckpoint
	err := db.QueryRow(
		`SELECT id, last_checked_at FROM user_checkpoints WHERE id = 1`,
	).Scan(&cp.ID, &cp.LastCheckedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting checkpoint: %w", err)
	}
	return &cp, nil
}

// UpdateCheckpoint sets the user's catchup checkpoint to the given time.
// Uses INSERT OR REPLACE to handle both initial set and updates.
func (db *DB) UpdateCheckpoint(t time.Time) error {
	_, err := db.Exec(`
		INSERT INTO user_checkpoints (id, last_checked_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET last_checked_at = excluded.last_checked_at`,
		t.UTC().Format("2006-01-02T15:04:05Z"),
	)
	if err != nil {
		return fmt.Errorf("updating checkpoint: %w", err)
	}
	return nil
}
