// Package db provides database operations and schema management for watchtower's SQLite database.
package db

import (
	"database/sql"
	"fmt"
	"log"
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
		if _, err := tx.Exec("PRAGMA user_version = 35"); err != nil {
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

	if version < 17 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v17 tx: %w", err)
		}
		defer tx.Rollback()

		// Feedback table for thumbs up/down on AI-generated content.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'action_item', 'decision')),
			entity_id   TEXT NOT NULL,
			rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
			comment     TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v17 create feedback: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
			return fmt.Errorf("migration v17 feedback entity index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
			return fmt.Errorf("migration v17 feedback rating index: %w", err)
		}

		// Editable AI prompt templates.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS prompts (
			id         TEXT PRIMARY KEY,
			template   TEXT NOT NULL,
			version    INTEGER NOT NULL DEFAULT 1,
			language   TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v17 create prompts: %w", err)
		}

		// Prompt version history.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS prompt_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			prompt_id  TEXT NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
			version    INTEGER NOT NULL,
			template   TEXT NOT NULL,
			reason     TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v17 create prompt_history: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_prompt_history_prompt ON prompt_history(prompt_id)`); err != nil {
			return fmt.Errorf("migration v17 prompt_history prompt index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_prompt_history_version ON prompt_history(prompt_id, version)`); err != nil {
			return fmt.Errorf("migration v17 prompt_history version index: %w", err)
		}

		// Add prompt_version column to existing tables.
		for _, col := range []string{
			`ALTER TABLE digests ADD COLUMN prompt_version INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE action_items ADD COLUMN prompt_version INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE user_analyses ADD COLUMN prompt_version INTEGER NOT NULL DEFAULT 0`,
		} {
			if _, err := tx.Exec(col); err != nil {
				return fmt.Errorf("migration v17 alter: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 17"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v17: %w", err)
		}
		version = 17
	}

	if version < 18 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v18: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS decision_importance_corrections (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			digest_id            INTEGER NOT NULL,
			decision_idx         INTEGER NOT NULL,
			decision_text        TEXT NOT NULL DEFAULT '',
			original_importance  TEXT NOT NULL CHECK(original_importance IN ('high', 'medium', 'low')),
			new_importance       TEXT NOT NULL CHECK(new_importance IN ('high', 'medium', 'low')),
			created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v18 create decision_importance_corrections: %w", err)
		}
		if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_dic_dedup ON decision_importance_corrections(digest_id, decision_idx)`); err != nil {
			return fmt.Errorf("migration v18 dedup index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_dic_created ON decision_importance_corrections(created_at)`); err != nil {
			return fmt.Errorf("migration v18 created index: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 18"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v18: %w", err)
		}
		version = 18
	}

	if version < 19 {
		// Rename action_items → tracks, action_item_history → track_history.
		if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("migration v19 disable FK: %w", err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v19 tx: %w", err)
		}
		defer tx.Rollback()

		// Rename tables.
		if _, err := tx.Exec(`ALTER TABLE action_items RENAME TO tracks`); err != nil {
			return fmt.Errorf("migration v19 rename action_items: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE action_item_history RENAME TO track_history`); err != nil {
			return fmt.Errorf("migration v19 rename action_item_history: %w", err)
		}

		// Rename column action_item_id → track_id in track_history.
		if _, err := tx.Exec(`ALTER TABLE track_history RENAME COLUMN action_item_id TO track_id`); err != nil {
			return fmt.Errorf("migration v19 rename column: %w", err)
		}

		// Recreate indexes with new names.
		for _, stmt := range []string{
			`DROP INDEX IF EXISTS idx_action_items_dedup`,
			`DROP INDEX IF EXISTS idx_action_items_assignee`,
			`DROP INDEX IF EXISTS idx_action_items_status`,
			`DROP INDEX IF EXISTS idx_action_items_period`,
			`DROP INDEX IF EXISTS idx_action_item_history_item`,
			`CREATE UNIQUE INDEX idx_tracks_dedup ON tracks(channel_id, assignee_user_id, source_message_ts, text)`,
			`CREATE INDEX idx_tracks_assignee ON tracks(assignee_user_id)`,
			`CREATE INDEX idx_tracks_status ON tracks(status)`,
			`CREATE INDEX idx_tracks_period ON tracks(period_from, period_to)`,
			`CREATE INDEX idx_track_history_track ON track_history(track_id)`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v19 index %q: %w", stmt[:40], err)
			}
		}

		// Recreate feedback table with updated CHECK constraint ('action_item' → 'track', add 'user_analysis').
		if _, err := tx.Exec(`CREATE TABLE feedback_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis')),
			entity_id   TEXT NOT NULL,
			rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
			comment     TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v19 create feedback_new: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO feedback_new (id, entity_type, entity_id, rating, comment, created_at)
			SELECT id, CASE WHEN entity_type = 'action_item' THEN 'track' ELSE entity_type END,
			entity_id, rating, comment, created_at FROM feedback`); err != nil {
			return fmt.Errorf("migration v19 copy feedback: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
			return fmt.Errorf("migration v19 drop feedback: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
			return fmt.Errorf("migration v19 rename feedback: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
			return fmt.Errorf("migration v19 feedback entity index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
			return fmt.Errorf("migration v19 feedback rating index: %w", err)
		}

		// Fix JSON column defaults for existing rows ('' → '[]').
		for _, col := range []string{"participants", "source_refs", "tags", "decision_options", "related_digest_ids", "sub_items"} {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE tracks SET %s = '[]' WHERE %s = ''`, col, col)); err != nil {
				return fmt.Errorf("migration v19 fix JSON default %s: %w", col, err)
			}
		}

		// Update prompt IDs.  Reset template text so arity matches the new pipeline.
		if _, err := tx.Exec(`UPDATE prompts SET id = 'tracks.extract', template = '' WHERE id = 'actionitems.extract'`); err != nil {
			return fmt.Errorf("migration v19 update prompt extract: %w", err)
		}
		if _, err := tx.Exec(`UPDATE prompts SET id = 'tracks.update', template = '' WHERE id = 'actionitems.update'`); err != nil {
			return fmt.Errorf("migration v19 update prompt update: %w", err)
		}
		if _, err := tx.Exec(`UPDATE prompt_history SET prompt_id = 'tracks.extract' WHERE prompt_id = 'actionitems.extract'`); err != nil {
			return fmt.Errorf("migration v19 update prompt_history extract: %w", err)
		}
		if _, err := tx.Exec(`UPDATE prompt_history SET prompt_id = 'tracks.update' WHERE prompt_id = 'actionitems.update'`); err != nil {
			return fmt.Errorf("migration v19 update prompt_history update: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 19"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v19: %w", err)
		}
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return fmt.Errorf("migration v19 re-enable FK: %w", err)
		}
		// Verify referential integrity after table renames.
		func() {
			rows, fkErr := db.Query("PRAGMA foreign_key_check")
			if fkErr != nil {
				return
			}
			defer rows.Close()
			for rows.Next() {
				var table, parent string
				var rowid, fkidx int64
				if err := rows.Scan(&table, &rowid, &parent, &fkidx); err != nil {
					log.Printf("warning: failed to scan FK check row: %v", err)
					continue
				}
				log.Printf("warning: foreign key violation after migration v19: table=%s rowid=%d parent=%s fkidx=%d", table, rowid, parent, fkidx)
			}
		}()
		version = 19
	}

	if version < 20 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v20 tx: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS user_profile (
			id                    INTEGER PRIMARY KEY,
			slack_user_id         TEXT NOT NULL UNIQUE,
			role                  TEXT NOT NULL DEFAULT '',
			team                  TEXT NOT NULL DEFAULT '',
			responsibilities      TEXT NOT NULL DEFAULT '[]',
			reports               TEXT NOT NULL DEFAULT '[]',
			peers                 TEXT NOT NULL DEFAULT '[]',
			manager               TEXT NOT NULL DEFAULT '',
			starred_channels      TEXT NOT NULL DEFAULT '[]',
			starred_people        TEXT NOT NULL DEFAULT '[]',
			pain_points           TEXT NOT NULL DEFAULT '[]',
			track_focus           TEXT NOT NULL DEFAULT '[]',
			onboarding_done       INTEGER NOT NULL DEFAULT 0,
			custom_prompt_context TEXT NOT NULL DEFAULT '',
			created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v20 create user_profile: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 20"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v20: %w", err)
		}
		version = 20
	}

	if version < 21 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v21 tx: %w", err)
		}
		defer tx.Rollback()

		// Check if ownership column already exists (fresh install from schema.sql).
		hasOwnership := hasColumn(tx, "tracks", "ownership")

		if !hasOwnership {
			for _, stmt := range []string{
				`ALTER TABLE tracks ADD COLUMN ownership TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine', 'delegated', 'watching'))`,
				`ALTER TABLE tracks ADD COLUMN ball_on TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE tracks ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT ''`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return fmt.Errorf("migration v21 alter tracks: %w", err)
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 21"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v21: %w", err)
		}
		version = 21
	}

	if version < 22 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v22 tx: %w", err)
		}
		defer tx.Rollback()

		hasChains := hasColumn(tx, "chains", "id")

		if !hasChains {
			for _, stmt := range []string{
				`CREATE TABLE IF NOT EXISTS chains (
					id          INTEGER PRIMARY KEY AUTOINCREMENT,
					parent_id   INTEGER REFERENCES chains(id) ON DELETE SET NULL,
					title       TEXT NOT NULL,
					slug        TEXT NOT NULL,
					status      TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'resolved', 'stale')),
					summary     TEXT NOT NULL DEFAULT '',
					channel_ids TEXT NOT NULL DEFAULT '[]',
					first_seen  REAL NOT NULL,
					last_seen   REAL NOT NULL,
					item_count  INTEGER NOT NULL DEFAULT 0,
					read_at     TEXT,
					created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
					updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
				)`,
				`CREATE INDEX IF NOT EXISTS idx_chains_status ON chains(status)`,
				`CREATE INDEX IF NOT EXISTS idx_chains_last_seen ON chains(last_seen DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_chains_parent ON chains(parent_id)`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_chains_slug ON chains(slug)`,
				`CREATE TABLE IF NOT EXISTS chain_refs (
					id            INTEGER PRIMARY KEY AUTOINCREMENT,
					chain_id      INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
					ref_type      TEXT NOT NULL CHECK(ref_type IN ('decision', 'track', 'digest')),
					digest_id     INTEGER NOT NULL DEFAULT 0,
					decision_idx  INTEGER NOT NULL DEFAULT 0,
					track_id      INTEGER NOT NULL DEFAULT 0,
					channel_id    TEXT NOT NULL DEFAULT '',
					timestamp     REAL NOT NULL,
					created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
					UNIQUE(chain_id, ref_type, digest_id, decision_idx, track_id)
				)`,
				`CREATE INDEX IF NOT EXISTS idx_chain_refs_chain ON chain_refs(chain_id)`,
				`CREATE INDEX IF NOT EXISTS idx_chain_refs_digest ON chain_refs(digest_id)`,
				`CREATE INDEX IF NOT EXISTS idx_chain_refs_track ON chain_refs(track_id)`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return fmt.Errorf("migration v22 create chains: %w", err)
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 22"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v22: %w", err)
		}
		version = 22
	}

	if version < 23 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v23 tx: %w", err)
		}
		defer tx.Rollback()

		hasInteractions := hasColumn(tx, "user_interactions", "user_a")

		if !hasInteractions {
			for _, stmt := range []string{
				`CREATE TABLE IF NOT EXISTS user_interactions (
					user_a              TEXT NOT NULL,
					user_b              TEXT NOT NULL,
					period_from         REAL NOT NULL,
					period_to           REAL NOT NULL,
					messages_to         INTEGER NOT NULL DEFAULT 0,
					messages_from       INTEGER NOT NULL DEFAULT 0,
					shared_channels     INTEGER NOT NULL DEFAULT 0,
					thread_replies_to   INTEGER NOT NULL DEFAULT 0,
					thread_replies_from INTEGER NOT NULL DEFAULT 0,
					shared_channel_ids  TEXT NOT NULL DEFAULT '[]',
					PRIMARY KEY (user_a, user_b, period_from, period_to)
				)`,
				`CREATE INDEX IF NOT EXISTS idx_user_interactions_a ON user_interactions(user_a, period_from, period_to)`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return fmt.Errorf("migration v23 create user_interactions: %w", err)
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 23"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v23: %w", err)
		}
		version = 23
	}

	// --- Migration v24: Add parent_id + read_at to chains, digest ref_type to chain_refs ---
	if version < 24 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v24: %w", err)
		}
		defer tx.Rollback()

		// Add parent_id to chains if not present.
		if !hasColumn(tx, "chains", "parent_id") {
			if _, err := tx.Exec(`ALTER TABLE chains ADD COLUMN parent_id INTEGER REFERENCES chains(id) ON DELETE SET NULL`); err != nil {
				return fmt.Errorf("migration v24 add parent_id: %w", err)
			}
		}
		// Add read_at to chains if not present.
		if !hasColumn(tx, "chains", "read_at") {
			if _, err := tx.Exec(`ALTER TABLE chains ADD COLUMN read_at TEXT`); err != nil {
				return fmt.Errorf("migration v24 add read_at: %w", err)
			}
		}
		// Index on parent_id.
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chains_parent ON chains(parent_id)`); err != nil {
			return fmt.Errorf("migration v24 create idx_chains_parent: %w", err)
		}

		// Widen chain_refs.ref_type CHECK to include 'digest'.
		// SQLite doesn't support ALTER CHECK, so we recreate the table.
		for _, stmt := range []string{
			`CREATE TABLE IF NOT EXISTS chain_refs_new (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				chain_id      INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
				ref_type      TEXT NOT NULL CHECK(ref_type IN ('decision', 'track', 'digest')),
				digest_id     INTEGER NOT NULL DEFAULT 0,
				decision_idx  INTEGER NOT NULL DEFAULT 0,
				track_id      INTEGER NOT NULL DEFAULT 0,
				channel_id    TEXT NOT NULL DEFAULT '',
				timestamp     REAL NOT NULL,
				created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
				UNIQUE(chain_id, ref_type, digest_id, decision_idx, track_id)
			)`,
			`INSERT INTO chain_refs_new SELECT * FROM chain_refs`,
			`DROP TABLE chain_refs`,
			`ALTER TABLE chain_refs_new RENAME TO chain_refs`,
			`CREATE INDEX IF NOT EXISTS idx_chain_refs_chain ON chain_refs(chain_id)`,
			`CREATE INDEX IF NOT EXISTS idx_chain_refs_digest ON chain_refs(digest_id)`,
			`CREATE INDEX IF NOT EXISTS idx_chain_refs_track ON chain_refs(track_id)`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v24 recreate chain_refs: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 24"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v24: %w", err)
		}
		version = 24
	}

	// v25: Add new interaction columns for DMs, mentions, reactions, scoring
	if version < 25 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v25: %w", err)
		}
		defer tx.Rollback()

		columns := map[string]string{
			"dm_messages_to":    "INTEGER NOT NULL DEFAULT 0",
			"dm_messages_from":  "INTEGER NOT NULL DEFAULT 0",
			"mentions_to":       "INTEGER NOT NULL DEFAULT 0",
			"mentions_from":     "INTEGER NOT NULL DEFAULT 0",
			"reactions_to":      "INTEGER NOT NULL DEFAULT 0",
			"reactions_from":    "INTEGER NOT NULL DEFAULT 0",
			"interaction_score": "REAL NOT NULL DEFAULT 0",
			"connection_type":   "TEXT NOT NULL DEFAULT ''",
		}
		for col, typ := range columns {
			if !hasColumn(tx, "user_interactions", col) {
				if _, err := tx.Exec("ALTER TABLE user_interactions ADD COLUMN " + col + " " + typ); err != nil {
					return fmt.Errorf("migration v25 add %s: %w", col, err)
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 25"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v25: %w", err)
		}
		version = 25
	}

	if version < 26 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v26: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "channels", "last_read") {
			if _, err := tx.Exec(`ALTER TABLE channels ADD COLUMN last_read TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v26 add last_read: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 26"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v26: %w", err)
		}
		version = 26
	}

	// v27: Add communication_guides and guide_summaries tables
	if version < 27 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v27: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS communication_guides (
			id                      INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id                 TEXT NOT NULL,
			period_from             REAL NOT NULL,
			period_to               REAL NOT NULL,
			message_count           INTEGER NOT NULL DEFAULT 0,
			channels_active         INTEGER NOT NULL DEFAULT 0,
			threads_initiated       INTEGER NOT NULL DEFAULT 0,
			threads_replied         INTEGER NOT NULL DEFAULT 0,
			avg_message_length      REAL NOT NULL DEFAULT 0,
			active_hours_json       TEXT NOT NULL DEFAULT '{}',
			volume_change_pct       REAL NOT NULL DEFAULT 0,
			summary                 TEXT NOT NULL DEFAULT '',
			communication_preferences TEXT NOT NULL DEFAULT '',
			availability_patterns   TEXT NOT NULL DEFAULT '',
			decision_process        TEXT NOT NULL DEFAULT '',
			situational_tactics     TEXT NOT NULL DEFAULT '[]',
			effective_approaches    TEXT NOT NULL DEFAULT '[]',
			recommendations         TEXT NOT NULL DEFAULT '[]',
			relationship_context    TEXT NOT NULL DEFAULT '',
			model                   TEXT NOT NULL DEFAULT '',
			input_tokens            INTEGER NOT NULL DEFAULT 0,
			output_tokens           INTEGER NOT NULL DEFAULT 0,
			cost_usd                REAL NOT NULL DEFAULT 0,
			prompt_version          INTEGER NOT NULL DEFAULT 0,
			created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(user_id, period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("migration v27 create communication_guides: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_communication_guides_user ON communication_guides(user_id)`); err != nil {
			return fmt.Errorf("migration v27 create index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_communication_guides_period ON communication_guides(period_from, period_to)`); err != nil {
			return fmt.Errorf("migration v27 create index: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS guide_summaries (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			period_from   REAL NOT NULL,
			period_to     REAL NOT NULL,
			summary       TEXT NOT NULL DEFAULT '',
			tips          TEXT NOT NULL DEFAULT '[]',
			model         TEXT NOT NULL DEFAULT '',
			input_tokens  INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd      REAL NOT NULL DEFAULT 0,
			prompt_version INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("migration v27 create guide_summaries: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 27"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v27: %w", err)
		}
		version = 27
	}

	if version < 28 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v28: %w", err)
		}
		defer tx.Rollback()

		// Add people_signals column to digests table (MAP phase output)
		if !hasColumn(tx, "digests", "people_signals") {
			if _, err := tx.Exec(`ALTER TABLE digests ADD COLUMN people_signals TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migration v28 add people_signals: %w", err)
			}
		}

		// Create unified people_cards table (REDUCE phase output)
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS people_cards (
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
			accomplishments     TEXT NOT NULL DEFAULT '[]',
			how_to_communicate  TEXT NOT NULL DEFAULT '',
			decision_style      TEXT NOT NULL DEFAULT '',
			tactics             TEXT NOT NULL DEFAULT '[]',
			relationship_context TEXT NOT NULL DEFAULT '',
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			prompt_version      INTEGER NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(user_id, period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("migration v28 create people_cards: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_people_cards_user ON people_cards(user_id)`); err != nil {
			return fmt.Errorf("migration v28 create index: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_people_cards_period ON people_cards(period_from, period_to)`); err != nil {
			return fmt.Errorf("migration v28 create index: %w", err)
		}

		// Create people_card_summaries table
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS people_card_summaries (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			period_from   REAL NOT NULL,
			period_to     REAL NOT NULL,
			summary       TEXT NOT NULL DEFAULT '',
			attention     TEXT NOT NULL DEFAULT '[]',
			tips          TEXT NOT NULL DEFAULT '[]',
			model         TEXT NOT NULL DEFAULT '',
			input_tokens  INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd      REAL NOT NULL DEFAULT 0,
			prompt_version INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(period_from, period_to)
		)`); err != nil {
			return fmt.Errorf("migration v28 create people_card_summaries: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 28"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v28: %w", err)
		}
		version = 28
	}

	if version < 29 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v29: %w", err)
		}
		defer tx.Rollback()

		// Add situations column to digests table (replaces people_signals in v2)
		if !hasColumn(tx, "digests", "situations") {
			if _, err := tx.Exec(`ALTER TABLE digests ADD COLUMN situations TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migration v29 add situations: %w", err)
			}
		}

		// Create digest_participants table
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS digest_participants (
			digest_id      INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
			user_id        TEXT NOT NULL,
			situation_idx  INTEGER NOT NULL DEFAULT 0,
			role           TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (digest_id, user_id, situation_idx)
		)`); err != nil {
			return fmt.Errorf("migration v29 create digest_participants: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_digest_participants_user ON digest_participants(user_id)`); err != nil {
			return fmt.Errorf("migration v29 create index: %w", err)
		}

		// Rename how_to_communicate → communication_guide in people_cards
		if hasColumn(tx, "people_cards", "how_to_communicate") && !hasColumn(tx, "people_cards", "communication_guide") {
			if _, err := tx.Exec(`ALTER TABLE people_cards RENAME COLUMN how_to_communicate TO communication_guide`); err != nil {
				return fmt.Errorf("migration v29 rename how_to_communicate: %w", err)
			}
		}

		// Add status column to people_cards
		if !hasColumn(tx, "people_cards", "status") {
			if _, err := tx.Exec(`ALTER TABLE people_cards ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
				return fmt.Errorf("migration v29 add status: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 29"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v29: %w", err)
		}
		version = 29
	}

	if version < 30 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v30: %w", err)
		}
		defer tx.Rollback()

		// Deduplicate chains: for each slug, keep the chain with the highest item_count
		// and reassign all chain_refs from duplicates to the keeper.
		if _, err := tx.Exec(`
			UPDATE chain_refs
			SET chain_id = (
				SELECT c2.id FROM chains c2
				WHERE c2.slug = (SELECT slug FROM chains WHERE id = chain_refs.chain_id)
				ORDER BY c2.item_count DESC, c2.id ASC
				LIMIT 1
			)
			WHERE chain_id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY slug ORDER BY item_count DESC, id ASC) as rn
					FROM chains
				) WHERE rn = 1
			)
		`); err != nil {
			return fmt.Errorf("migration v30 reassign chain_refs: %w", err)
		}

		if _, err := tx.Exec(`
			DELETE FROM chains WHERE id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY slug ORDER BY item_count DESC, id ASC) as rn
					FROM chains
				) WHERE rn = 1
			)
		`); err != nil {
			return fmt.Errorf("migration v30 delete duplicate chains: %w", err)
		}

		// Update item_count for remaining chains.
		if _, err := tx.Exec(`
			UPDATE chains SET item_count = (
				SELECT COUNT(*) FROM chain_refs WHERE chain_refs.chain_id = chains.id
			)
		`); err != nil {
			return fmt.Errorf("migration v30 update item_count: %w", err)
		}

		// Add unique index on slug to prevent future duplicates.
		if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_chains_slug ON chains(slug)`); err != nil {
			return fmt.Errorf("migration v30 create unique index: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 30"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v30: %w", err)
		}
		version = 30
	}

	if version < 31 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v31: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "digests", "running_summary") {
			if _, err := tx.Exec(`ALTER TABLE digests ADD COLUMN running_summary TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v31 add running_summary: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 31"); err != nil {
			return fmt.Errorf("setting schema version v31: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v31: %w", err)
		}
		version = 31
	}

	if version < 32 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v32: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS pipeline_runs (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				pipeline         TEXT NOT NULL,
				source           TEXT NOT NULL DEFAULT 'cli',
				status           TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'done', 'error')),
				error_msg        TEXT NOT NULL DEFAULT '',
				items_found      INTEGER NOT NULL DEFAULT 0,
				input_tokens     INTEGER NOT NULL DEFAULT 0,
				output_tokens    INTEGER NOT NULL DEFAULT 0,
				cost_usd         REAL NOT NULL DEFAULT 0,
				total_api_tokens INTEGER NOT NULL DEFAULT 0,
				period_from      REAL,
				period_to        REAL,
				started_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
				finished_at      TEXT,
				duration_seconds REAL NOT NULL DEFAULT 0
			)`); err != nil {
			return fmt.Errorf("migration v32 create pipeline_runs: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline ON pipeline_runs(pipeline)`); err != nil {
			return fmt.Errorf("migration v32 index pipeline_runs pipeline: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_pipeline_runs_started ON pipeline_runs(started_at DESC)`); err != nil {
			return fmt.Errorf("migration v32 index pipeline_runs started: %w", err)
		}

		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS pipeline_steps (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				run_id           INTEGER NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
				step             INTEGER NOT NULL,
				total            INTEGER NOT NULL,
				status           TEXT NOT NULL DEFAULT '',
				channel_id       TEXT NOT NULL DEFAULT '',
				channel_name     TEXT NOT NULL DEFAULT '',
				input_tokens     INTEGER NOT NULL DEFAULT 0,
				output_tokens    INTEGER NOT NULL DEFAULT 0,
				cost_usd         REAL NOT NULL DEFAULT 0,
				total_api_tokens INTEGER NOT NULL DEFAULT 0,
				message_count    INTEGER NOT NULL DEFAULT 0,
				period_from      REAL,
				period_to        REAL,
				duration_seconds REAL NOT NULL DEFAULT 0,
				created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			)`); err != nil {
			return fmt.Errorf("migration v32 create pipeline_steps: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_pipeline_steps_run ON pipeline_steps(run_id)`); err != nil {
			return fmt.Errorf("migration v32 index pipeline_steps run: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 32"); err != nil {
			return fmt.Errorf("setting schema version v32: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v32: %w", err)
		}
		version = 32
	}

	if version < 33 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("starting migration v33: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS briefings (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				workspace_id     TEXT NOT NULL DEFAULT '',
				user_id          TEXT NOT NULL,
				date             TEXT NOT NULL,
				role             TEXT NOT NULL DEFAULT '',
				attention        TEXT NOT NULL DEFAULT '[]',
				your_day         TEXT NOT NULL DEFAULT '[]',
				what_happened    TEXT NOT NULL DEFAULT '[]',
				team_pulse       TEXT NOT NULL DEFAULT '[]',
				coaching         TEXT NOT NULL DEFAULT '[]',
				model            TEXT NOT NULL DEFAULT '',
				input_tokens     INTEGER NOT NULL DEFAULT 0,
				output_tokens    INTEGER NOT NULL DEFAULT 0,
				cost_usd         REAL NOT NULL DEFAULT 0,
				prompt_version   INTEGER NOT NULL DEFAULT 0,
				read_at          TEXT,
				created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
				UNIQUE(user_id, date)
			)`); err != nil {
			return fmt.Errorf("migration v33 create briefings: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_briefings_user_date ON briefings(user_id, date DESC)`); err != nil {
			return fmt.Errorf("migration v33 index briefings user_date: %w", err)
		}

		// Expand feedback entity_type CHECK to include 'briefing'.
		// SQLite cannot ALTER CHECK constraints, so we recreate the table.
		if !hasColumn(tx, "feedback", "id") {
			// Table doesn't exist yet (fresh install via schema.sql handles it).
		} else {
			// Recreate feedback table with expanded CHECK.
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing')),
				entity_id   TEXT NOT NULL,
				rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
				comment     TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			)`); err != nil {
				return fmt.Errorf("migration v33 create feedback_new: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO feedback_new SELECT * FROM feedback`); err != nil {
				return fmt.Errorf("migration v33 copy feedback: %w", err)
			}
			if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
				return fmt.Errorf("migration v33 drop feedback: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
				return fmt.Errorf("migration v33 rename feedback: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
				return fmt.Errorf("migration v33 index feedback entity: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
				return fmt.Errorf("migration v33 index feedback rating: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 33"); err != nil {
			return fmt.Errorf("setting schema version v33: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v33: %w", err)
		}
		version = 33
	}

	if version < 34 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v34: %w", err)
		}
		defer tx.Rollback()

		// Expand feedback entity_type CHECK to include 'chain'.
		if !hasColumn(tx, "feedback", "id") {
			// Table doesn't exist yet (fresh install via schema.sql handles it).
		} else {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing', 'chain')),
				entity_id   TEXT NOT NULL,
				rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
				comment     TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			)`); err != nil {
				return fmt.Errorf("migration v34 create feedback_new: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO feedback_new SELECT * FROM feedback`); err != nil {
				return fmt.Errorf("migration v34 copy feedback: %w", err)
			}
			if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
				return fmt.Errorf("migration v34 drop feedback: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
				return fmt.Errorf("migration v34 rename feedback: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
				return fmt.Errorf("migration v34 index feedback_entity: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
				return fmt.Errorf("migration v34 index feedback_rating: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 34"); err != nil {
			return fmt.Errorf("setting schema version v34: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v34: %w", err)
		}
		version = 34
	}

	if version < 35 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v35: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "tracks", "fingerprint") {
			if _, err := tx.Exec(`ALTER TABLE tracks ADD COLUMN fingerprint TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migration v35 add fingerprint column: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 35"); err != nil {
			return fmt.Errorf("setting schema version v35: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v35: %w", err)
		}
		version = 35
	}

	_ = version // silence unused variable if this is the last migration
	return nil
}

// hasColumn checks whether a table has a specific column via PRAGMA table_info.
// table must be a valid identifier (alphanumeric + underscore only).
func hasColumn(querier interface {
	Query(string, ...any) (*sql.Rows, error)
}, table, column string) bool {
	// Validate table name to prevent SQL injection — PRAGMA doesn't support parameterized table names.
	for _, r := range table {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	rows, err := querier.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err == nil {
			if name == column {
				return true
			}
		}
	}
	return false
}

// UserVersion returns the current schema version.
func (db *DB) UserVersion() (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}
