package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB connection to the watchtower SQLite database.
type DB struct {
	*sql.DB
}

// Open creates directories if needed, opens the SQLite database, sets pragmas,
// and runs migrations. Pass ":memory:" for an in-memory database.
func Open(dbPath string) (*DB, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Limit to 1 connection: for :memory: databases each connection gets
	// its own independent database, and for file databases per-connection
	// pragmas (busy_timeout, foreign_keys, synchronous) would not apply
	// to new pooled connections. SQLite serializes writes anyway, so a
	// single connection avoids both issues with no performance loss.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: sqlDB}

	if err := db.setPragmas(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("setting pragmas: %w", err)
	}

	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func (db *DB) setPragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("executing %q: %w", p, err)
		}
	}
	return nil
}

func (db *DB) migrate() error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading user_version: %w", err)
	}

	if version < 1 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(Schema); err != nil {
			return fmt.Errorf("executing schema: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 4"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration: %w", err)
		}
		return nil // fresh install — schema is complete
	} else if version < 2 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v2 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`ALTER TABLE workspace ADD COLUMN search_last_date TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("adding search_last_date column: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 2"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v2: %w", err)
		}
		version = 2
	}

	if version < 3 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v3 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS digests (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id    TEXT NOT NULL DEFAULT '',
			period_from   REAL NOT NULL,
			period_to     REAL NOT NULL,
			type          TEXT NOT NULL CHECK(type IN ('channel', 'daily', 'weekly')),
			summary       TEXT NOT NULL,
			topics        TEXT NOT NULL DEFAULT '[]',
			decisions     TEXT NOT NULL DEFAULT '[]',
			action_items  TEXT NOT NULL DEFAULT '[]',
			message_count INTEGER NOT NULL DEFAULT 0,
			model         TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(channel_id, type, period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("creating digests table: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_digests_channel ON digests(channel_id)`); err != nil {
			return fmt.Errorf("creating digests channel index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_digests_type ON digests(type)`); err != nil {
			return fmt.Errorf("creating digests type index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_digests_period ON digests(period_from, period_to)`); err != nil {
			return fmt.Errorf("creating digests period index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 3"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v3: %w", err)
		}
		version = 3
	}

	if version < 4 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v4 tx: %w", err)
		}
		defer tx.Rollback()
		for _, col := range []string{
			`ALTER TABLE digests ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE digests ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE digests ADD COLUMN cost_usd REAL NOT NULL DEFAULT 0`,
		} {
			if _, err := tx.Exec(col); err != nil {
				return fmt.Errorf("migration v4 alter: %w", err)
			}
		}
		if _, err := tx.Exec("PRAGMA user_version = 4"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v4: %w", err)
		}
	}

	return nil
}

// UserVersion returns the current schema version.
func (db *DB) UserVersion() (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}
