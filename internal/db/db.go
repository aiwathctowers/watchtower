package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps a *sql.DB connection to the watchtower SQLite database.
type DB struct {
	*sql.DB
}

// Open creates directories if needed, opens the SQLite database, sets pragmas,
// and runs migrations. Pass ":memory:" for an in-memory database.
func Open(dbPath string) (*DB, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// For in-memory databases, limit to 1 connection so all operations
	// share the same database instance (each connection to :memory: gets
	// its own independent database).
	if dbPath == ":memory:" {
		sqlDB.SetMaxOpenConns(1)
	}

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
		if _, err := db.Exec(schemaSQL); err != nil {
			return fmt.Errorf("applying schema v1: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("setting user_version: %w", err)
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
