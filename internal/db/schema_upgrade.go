package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// RunSchemaUpgrade is the one-shot transition from the legacy migration
// engine (PRAGMA user_version + manual switch in migrate()) to goose.
//
// It opens dbPath directly (no schema-version checks during Open) and:
//
//   - returns nil immediately if the DB has already been transitioned
//     (goose_db_version table exists) — fully idempotent
//   - returns nil if PRAGMA user_version == 0 (fresh DB, goose handles it)
//   - otherwise creates goose_db_version and marks the baseline as applied
//
// PRAGMA user_version is preserved on the legacy value: the Swift Desktop
// app uses it as a "is the schema usable" guard (requires >= 3) and
// resetting it would lock the Desktop out of an otherwise-fine database.
//
// Caller is responsible for invoking this once per startup before any
// db.Open() call when config.DB.SchemaFormat < CurrentSchemaFormat.
func RunSchemaUpgrade(dbPath string) error {
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("opening db for schema upgrade: %w", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	var hasGoose int
	if err := raw.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='goose_db_version')`,
	).Scan(&hasGoose); err != nil {
		return fmt.Errorf("checking goose_db_version: %w", err)
	}
	if hasGoose == 1 {
		return nil
	}

	var userVersion int
	if err := raw.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("reading user_version: %w", err)
	}
	if userVersion == 0 {
		return nil
	}

	tx, err := raw.Begin()
	if err != nil {
		return fmt.Errorf("beginning transition tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE goose_db_version (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id  INTEGER NOT NULL,
		is_applied  INTEGER NOT NULL,
		tstamp      TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("creating goose_db_version: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1)`); err != nil {
		return fmt.Errorf("seeding goose_db_version baseline: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (1, 1)`); err != nil {
		return fmt.Errorf("marking baseline applied: %w", err)
	}
	// Note: PRAGMA user_version is NOT reset — Swift Desktop uses it as a
	// schema-version sanity check (requires >= 3) and zeroing it locks
	// the desktop out. Goose tracks state in goose_db_version instead.
	return tx.Commit()
}
