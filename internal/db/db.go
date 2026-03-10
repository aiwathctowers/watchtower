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
		if _, err := tx.Exec("PRAGMA user_version = 16"); err != nil {
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
		version = 4
	}

	if version < 5 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v5 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS user_analyses (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id             TEXT NOT NULL,
			period_from         REAL NOT NULL,
			period_to           REAL NOT NULL,
			message_count       INTEGER NOT NULL DEFAULT 0,
			channels_active     INTEGER NOT NULL DEFAULT 0,
			threads_initiated   INTEGER NOT NULL DEFAULT 0,
			threads_replied     INTEGER NOT NULL DEFAULT 0,
			avg_message_length  REAL NOT NULL DEFAULT 0,
			active_hours_json   TEXT NOT NULL DEFAULT '{}',
			volume_change_pct   REAL NOT NULL DEFAULT 0,
			summary             TEXT NOT NULL DEFAULT '',
			communication_style TEXT NOT NULL DEFAULT '',
			decision_role       TEXT NOT NULL DEFAULT '',
			red_flags           TEXT NOT NULL DEFAULT '[]',
			highlights          TEXT NOT NULL DEFAULT '[]',
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(user_id, period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("creating user_analyses table: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_user_analyses_user ON user_analyses(user_id)`); err != nil {
			return fmt.Errorf("creating user_analyses user index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_user_analyses_period ON user_analyses(period_from, period_to)`); err != nil {
			return fmt.Errorf("creating user_analyses period index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 5"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v5: %w", err)
		}
		version = 5
	}

	if version < 6 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v6 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS custom_emojis (
			name       TEXT PRIMARY KEY,
			url        TEXT NOT NULL,
			alias_for  TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("creating custom_emojis table: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 6"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v6: %w", err)
		}
		version = 6
	}

	if version < 7 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v7 tx: %w", err)
		}
		defer tx.Rollback()
		for _, col := range []string{
			`ALTER TABLE user_analyses ADD COLUMN style_details TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE user_analyses ADD COLUMN recommendations TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE user_analyses ADD COLUMN concerns TEXT NOT NULL DEFAULT '[]'`,
		} {
			if _, err := tx.Exec(col); err != nil {
				return fmt.Errorf("migration v7 alter: %w", err)
			}
		}
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS period_summaries (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			period_from   REAL NOT NULL,
			period_to     REAL NOT NULL,
			summary       TEXT NOT NULL DEFAULT '',
			attention     TEXT NOT NULL DEFAULT '[]',
			model         TEXT NOT NULL DEFAULT '',
			input_tokens  INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd      REAL NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("creating period_summaries table: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_period_summaries_period ON period_summaries(period_from, period_to)`); err != nil {
			return fmt.Errorf("creating period_summaries index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 7"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v7: %w", err)
		}
		version = 7
	}

	if version < 8 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v8 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`ALTER TABLE user_analyses ADD COLUMN accomplishments TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("migration v8 alter: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 8"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v8: %w", err)
		}
		version = 8
	}

	if version < 9 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v9 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`ALTER TABLE digests ADD COLUMN read_at TEXT`); err != nil {
			return fmt.Errorf("migration v9 alter digests: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS decision_reads (
			digest_id    INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
			decision_idx INTEGER NOT NULL,
			read_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			PRIMARY KEY (digest_id, decision_idx)
		)`); err != nil {
			return fmt.Errorf("creating decision_reads table: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 9"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v9: %w", err)
		}
		version = 9
	}

	if version < 10 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v10 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`ALTER TABLE workspace ADD COLUMN current_user_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration v10 alter workspace: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS action_items (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id          TEXT NOT NULL,
			assignee_user_id    TEXT NOT NULL,
			assignee_raw        TEXT NOT NULL DEFAULT '',
			text                TEXT NOT NULL,
			context             TEXT NOT NULL DEFAULT '',
			source_message_ts   TEXT NOT NULL DEFAULT '',
			source_channel_name TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','done','dismissed','snoozed')),
			priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			due_date            REAL,
			period_from         REAL NOT NULL,
			period_to           REAL NOT NULL,
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			completed_at        TEXT
		)`); err != nil {
			return fmt.Errorf("creating action_items table: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_action_items_assignee ON action_items(assignee_user_id)`); err != nil {
			return fmt.Errorf("creating action_items assignee index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_action_items_status ON action_items(status)`); err != nil {
			return fmt.Errorf("creating action_items status index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_action_items_period ON action_items(period_from, period_to)`); err != nil {
			return fmt.Errorf("creating action_items period index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 10"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v10: %w", err)
		}
		version = 10
	}

	if version < 11 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v11 tx: %w", err)
		}
		defer tx.Rollback()
		// Deduplicate existing rows before adding unique index
		if _, err := tx.Exec(`DELETE FROM action_items WHERE id NOT IN (
			SELECT MIN(id) FROM action_items GROUP BY channel_id, assignee_user_id, source_message_ts, text
		)`); err != nil {
			return fmt.Errorf("migration v11 dedup: %w", err)
		}
		if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_action_items_dedup
			ON action_items(channel_id, assignee_user_id, source_message_ts, text)`); err != nil {
			return fmt.Errorf("migration v11 unique index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 11"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v11: %w", err)
		}
		version = 11
	}

	if version < 12 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v12 tx: %w", err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS action_item_history (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			action_item_id  INTEGER NOT NULL REFERENCES action_items(id) ON DELETE CASCADE,
			event           TEXT NOT NULL,
			field           TEXT NOT NULL DEFAULT '',
			old_value       TEXT NOT NULL DEFAULT '',
			new_value       TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v12 create history table: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_action_item_history_item ON action_item_history(action_item_id)`); err != nil {
			return fmt.Errorf("migration v12 history index: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 12"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v12: %w", err)
		}
		version = 12
	}

	if version < 13 {
		// Disable FK checks: dropping action_items would cascade-delete action_item_history.
		if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("migration v13 disable FK: %w", err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v13 tx: %w", err)
		}
		defer tx.Rollback()

		// Recreate action_items with new CHECK constraint and new columns.
		if _, err := tx.Exec(`CREATE TABLE action_items_new (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id          TEXT NOT NULL,
			assignee_user_id    TEXT NOT NULL,
			assignee_raw        TEXT NOT NULL DEFAULT '',
			text                TEXT NOT NULL,
			context             TEXT NOT NULL DEFAULT '',
			source_message_ts   TEXT NOT NULL DEFAULT '',
			source_channel_name TEXT NOT NULL DEFAULT '',
			status              TEXT NOT NULL DEFAULT 'inbox' CHECK(status IN ('inbox','active','done','dismissed','snoozed')),
			priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			due_date            REAL,
			period_from         REAL NOT NULL,
			period_to           REAL NOT NULL,
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			completed_at        TEXT,
			has_updates         INTEGER NOT NULL DEFAULT 0,
			last_checked_ts     TEXT NOT NULL DEFAULT '',
			snooze_until        REAL,
			pre_snooze_status   TEXT NOT NULL DEFAULT ''
		)`); err != nil {
			return fmt.Errorf("migration v13 create new table: %w", err)
		}

		// Copy data, mapping 'open' → 'inbox'.
		if _, err := tx.Exec(`INSERT INTO action_items_new
			(id, channel_id, assignee_user_id, assignee_raw, text, context,
			 source_message_ts, source_channel_name,
			 status, priority, due_date,
			 period_from, period_to, model, input_tokens, output_tokens, cost_usd,
			 created_at, completed_at,
			 has_updates, last_checked_ts, snooze_until, pre_snooze_status)
			SELECT
			 id, channel_id, assignee_user_id, assignee_raw, text, context,
			 source_message_ts, source_channel_name,
			 CASE WHEN status = 'open' THEN 'inbox' ELSE status END,
			 priority, due_date,
			 period_from, period_to, model, input_tokens, output_tokens, cost_usd,
			 created_at, completed_at,
			 0, '', NULL, ''
			FROM action_items`); err != nil {
			return fmt.Errorf("migration v13 copy data: %w", err)
		}

		if _, err := tx.Exec(`DROP TABLE action_items`); err != nil {
			return fmt.Errorf("migration v13 drop old table: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE action_items_new RENAME TO action_items`); err != nil {
			return fmt.Errorf("migration v13 rename table: %w", err)
		}

		// Recreate all indexes.
		if _, err := tx.Exec(`CREATE UNIQUE INDEX idx_action_items_dedup ON action_items(channel_id, assignee_user_id, source_message_ts, text)`); err != nil {
			return fmt.Errorf("migration v13 dedup index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX idx_action_items_assignee ON action_items(assignee_user_id)`); err != nil {
			return fmt.Errorf("migration v13 assignee index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX idx_action_items_status ON action_items(status)`); err != nil {
			return fmt.Errorf("migration v13 status index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX idx_action_items_period ON action_items(period_from, period_to)`); err != nil {
			return fmt.Errorf("migration v13 period index: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 13"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v13: %w", err)
		}
		// Re-enable FK checks.
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return fmt.Errorf("migration v13 re-enable FK: %w", err)
		}
		version = 13
	}

	if version < 14 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v14 tx: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`ALTER TABLE action_items ADD COLUMN participants TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration v14 add participants: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE action_items ADD COLUMN source_refs TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration v14 add source_refs: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 14"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v14: %w", err)
		}
		version = 14
	}

	if version < 15 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v15 tx: %w", err)
		}
		defer tx.Rollback()

		for _, col := range []string{
			`ALTER TABLE action_items ADD COLUMN requester_name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN requester_user_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN category TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN blocking TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN tags TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN decision_summary TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN decision_options TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE action_items ADD COLUMN related_digest_ids TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := tx.Exec(col); err != nil {
				return fmt.Errorf("migration v15 alter: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 15"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v15: %w", err)
		}
		version = 15
	}

	if version < 16 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v16 tx: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`ALTER TABLE action_items ADD COLUMN sub_items TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration v16 add sub_items: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 16"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v16: %w", err)
		}
		version = 16
	}

	_ = version // silence unused variable if this is the last migration
	return nil
}

// UserVersion returns the current schema version.
func (db *DB) UserVersion() (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}
