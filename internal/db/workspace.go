package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// UpsertWorkspace inserts or updates a workspace.
func (db *DB) UpsertWorkspace(ws Workspace) error {
	_, err := db.Exec(`
		INSERT INTO workspace (id, name, domain, synced_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			domain = excluded.domain,
			synced_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		ws.ID, ws.Name, ws.Domain,
	)
	if err != nil {
		return fmt.Errorf("upserting workspace %s: %w", ws.ID, err)
	}
	return nil
}

// GetWorkspace returns the first workspace found, or nil if none exist.
func (db *DB) GetWorkspace() (*Workspace, error) {
	var ws Workspace
	err := db.QueryRow(`
		SELECT id, name, domain, synced_at FROM workspace LIMIT 1`,
	).Scan(&ws.ID, &ws.Name, &ws.Domain, &ws.SyncedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting workspace: %w", err)
	}
	return &ws, nil
}

// GetSearchLastDate returns the search_last_date for the workspace, or "" if not set.
func (db *DB) GetSearchLastDate() (string, error) {
	var date string
	err := db.QueryRow(`SELECT search_last_date FROM workspace LIMIT 1`).Scan(&date)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("getting search_last_date: %w", err)
	}
	return date, nil
}

// SetSearchLastDate updates the search_last_date for the workspace.
func (db *DB) SetSearchLastDate(date string) error {
	res, err := db.Exec(`UPDATE workspace SET search_last_date = ? WHERE id = (SELECT id FROM workspace LIMIT 1)`, date)
	if err != nil {
		return fmt.Errorf("setting search_last_date: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("setting search_last_date: no workspace row exists")
	}
	return nil
}
