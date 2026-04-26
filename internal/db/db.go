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
		if _, err := tx.Exec("PRAGMA user_version = 69"); err != nil {
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
			if hasColumn(tx, "tracks", col) {
				if _, err := tx.Exec(fmt.Sprintf(`UPDATE tracks SET %s = '[]' WHERE %s = ''`, col, col)); err != nil {
					return fmt.Errorf("migration v19 fix JSON default %s: %w", col, err)
				}
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

		// Add parent_id + read_at to chains, widen chain_refs CHECK.
		// Guard: v43 drops chains/chain_refs, so skip if tables don't exist.
		if hasColumn(tx, "chains", "id") {
			if !hasColumn(tx, "chains", "parent_id") {
				if _, err := tx.Exec(`ALTER TABLE chains ADD COLUMN parent_id INTEGER REFERENCES chains(id) ON DELETE SET NULL`); err != nil {
					return fmt.Errorf("migration v24 add parent_id: %w", err)
				}
			}
			if !hasColumn(tx, "chains", "read_at") {
				if _, err := tx.Exec(`ALTER TABLE chains ADD COLUMN read_at TEXT`); err != nil {
					return fmt.Errorf("migration v24 add read_at: %w", err)
				}
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chains_parent ON chains(parent_id)`); err != nil {
				return fmt.Errorf("migration v24 create idx_chains_parent: %w", err)
			}

			// Widen chain_refs.ref_type CHECK to include 'digest'.
			if hasColumn(tx, "chain_refs", "id") {
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
					`INSERT INTO chain_refs_new SELECT id, chain_id, ref_type, digest_id, decision_idx, track_id, channel_id, timestamp, created_at FROM chain_refs`,
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

		// Deduplicate chains (guard: v43 drops chains, so skip if missing).
		if hasColumn(tx, "chains", "id") {
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

			if _, err := tx.Exec(`
				UPDATE chains SET item_count = (
					SELECT COUNT(*) FROM chain_refs WHERE chain_refs.chain_id = chains.id
				)
			`); err != nil {
				return fmt.Errorf("migration v30 update item_count: %w", err)
			}

			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_chains_slug ON chains(slug)`); err != nil {
				return fmt.Errorf("migration v30 create unique index: %w", err)
			}
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

	if version < 36 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v36: %w", err)
		}
		defer tx.Rollback()

		// Tracks are no longer auto-created as "inbox"; promote all inbox tracks to "active".
		// Guard: v43 drops tracks table and recreates without status column.
		if hasColumn(tx, "tracks", "status") {
			if _, err := tx.Exec(`UPDATE tracks SET status = 'active' WHERE status = 'inbox'`); err != nil {
				return fmt.Errorf("migration v36 inbox→active: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 36"); err != nil {
			return fmt.Errorf("setting schema version v36: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v36: %w", err)
		}
		version = 36
	}

	if version < 37 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v37: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "tracks", "user_intent") {
			if _, err := tx.Exec(`ALTER TABLE tracks ADD COLUMN user_intent TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v37 add user_intent: %w", err)
			}
		}
		if !hasColumn(tx, "tracks", "source_chain_id") {
			if _, err := tx.Exec(`ALTER TABLE tracks ADD COLUMN source_chain_id INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("migration v37 add source_chain_id: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 37"); err != nil {
			return fmt.Errorf("setting schema version v37: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v37: %w", err)
		}
		version = 37
	}

	if version < 38 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v38: %w", err)
		}
		defer tx.Rollback()

		// Guard: v43 drops chains table, so skip if it doesn't exist.
		if hasColumn(tx, "chains", "id") {
			for _, col := range []struct{ table, column, ddl string }{
				{"chains", "participants", `ALTER TABLE chains ADD COLUMN participants TEXT NOT NULL DEFAULT '[]'`},
				{"chains", "timeline", `ALTER TABLE chains ADD COLUMN timeline TEXT NOT NULL DEFAULT '[]'`},
				{"chains", "current_status", `ALTER TABLE chains ADD COLUMN current_status TEXT NOT NULL DEFAULT ''`},
				{"chains", "key_messages", `ALTER TABLE chains ADD COLUMN key_messages TEXT NOT NULL DEFAULT '[]'`},
				{"chains", "related_track_ids", `ALTER TABLE chains ADD COLUMN related_track_ids TEXT NOT NULL DEFAULT '[]'`},
			} {
				if !hasColumn(tx, col.table, col.column) {
					if _, err := tx.Exec(col.ddl); err != nil {
						return fmt.Errorf("migration v38 add %s.%s: %w", col.table, col.column, err)
					}
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 38"); err != nil {
			return fmt.Errorf("setting schema version v38: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v38: %w", err)
		}
		version = 38
	}

	if version < 39 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v39: %w", err)
		}
		defer tx.Rollback()

		// Create digest_topics table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS digest_topics (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			digest_id     INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
			idx           INTEGER NOT NULL DEFAULT 0,
			title         TEXT NOT NULL,
			summary       TEXT NOT NULL DEFAULT '',
			decisions     TEXT NOT NULL DEFAULT '[]',
			action_items  TEXT NOT NULL DEFAULT '[]',
			situations    TEXT NOT NULL DEFAULT '[]',
			key_messages  TEXT NOT NULL DEFAULT '[]',
			UNIQUE(digest_id, idx)
		)`); err != nil {
			return fmt.Errorf("migration v39 create digest_topics: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_digest_topics_digest ON digest_topics(digest_id)`); err != nil {
			return fmt.Errorf("migration v39 create digest_topics index: %w", err)
		}

		// Add topic_id columns to existing tables.
		// Guard: some tables (chain_refs) may not exist if v43 dropped them.
		for _, col := range []struct{ table, column, ddl string }{
			{"chain_refs", "topic_id", `ALTER TABLE chain_refs ADD COLUMN topic_id INTEGER NOT NULL DEFAULT 0`},
			{"decision_reads", "topic_id", `ALTER TABLE decision_reads ADD COLUMN topic_id INTEGER NOT NULL DEFAULT 0`},
			{"decision_importance_corrections", "topic_id", `ALTER TABLE decision_importance_corrections ADD COLUMN topic_id INTEGER NOT NULL DEFAULT 0`},
			{"digest_participants", "topic_id", `ALTER TABLE digest_participants ADD COLUMN topic_id INTEGER NOT NULL DEFAULT 0`},
		} {
			if hasColumn(tx, col.table, "id") && !hasColumn(tx, col.table, col.column) {
				if _, err := tx.Exec(col.ddl); err != nil {
					return fmt.Errorf("migration v39 add %s.%s: %w", col.table, col.column, err)
				}
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 39"); err != nil {
			return fmt.Errorf("setting schema version v39: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v39: %w", err)
		}
		version = 39
	}

	if version < 40 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v40: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS channel_settings (
			channel_id       TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
			is_muted_for_llm INTEGER NOT NULL DEFAULT 0,
			is_favorite      INTEGER NOT NULL DEFAULT 0,
			updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v40 create channel_settings: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 40"); err != nil {
			return fmt.Errorf("setting schema version v40: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v40: %w", err)
		}
		version = 40
	}

	if version < 41 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v41: %w", err)
		}
		defer tx.Rollback()

		// Fix UNIQUE constraint on chain_refs to include topic_id.
		// Guard: v43 drops chains/chain_refs, so skip if missing.
		if hasColumn(tx, "chain_refs", "id") {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS chain_refs_new (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				chain_id      INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
				ref_type      TEXT NOT NULL CHECK(ref_type IN ('decision', 'track', 'digest', 'topic')),
				digest_id     INTEGER NOT NULL DEFAULT 0,
				decision_idx  INTEGER NOT NULL DEFAULT 0,
				track_id      INTEGER NOT NULL DEFAULT 0,
				topic_id      INTEGER NOT NULL DEFAULT 0,
				channel_id    TEXT NOT NULL DEFAULT '',
				timestamp     REAL NOT NULL,
				created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
				UNIQUE(chain_id, ref_type, digest_id, decision_idx, track_id, topic_id)
			)`); err != nil {
				return fmt.Errorf("migration v41 create chain_refs_new: %w", err)
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO chain_refs_new (id, chain_id, ref_type, digest_id, decision_idx, track_id, topic_id, channel_id, timestamp, created_at)
				SELECT id, chain_id, ref_type, digest_id, decision_idx, track_id, topic_id, channel_id, timestamp, created_at FROM chain_refs`); err != nil {
				return fmt.Errorf("migration v41 copy chain_refs: %w", err)
			}
			if _, err := tx.Exec(`DROP TABLE chain_refs`); err != nil {
				return fmt.Errorf("migration v41 drop old chain_refs: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE chain_refs_new RENAME TO chain_refs`); err != nil {
				return fmt.Errorf("migration v41 rename chain_refs: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chain_refs_chain ON chain_refs(chain_id)`); err != nil {
				return fmt.Errorf("migration v41 chain_refs chain index: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chain_refs_digest ON chain_refs(digest_id)`); err != nil {
				return fmt.Errorf("migration v41 chain_refs digest index: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chain_refs_track ON chain_refs(track_id)`); err != nil {
				return fmt.Errorf("migration v41 chain_refs track index: %w", err)
			}
		}

		// Delete ghost chains: 0 refs and created more than 1 hour ago.
		if hasColumn(tx, "chains", "id") {
			if _, err := tx.Exec(`DELETE FROM chains WHERE id IN (
				SELECT c.id FROM chains c
				LEFT JOIN chain_refs r ON r.chain_id = c.id
				WHERE r.id IS NULL
				AND c.created_at < strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-1 hour')
			)`); err != nil {
				return fmt.Errorf("migration v41 cleanup ghost chains: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 41"); err != nil {
			return fmt.Errorf("setting schema version v41: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v41: %w", err)
		}
		version = 41
	}

	if version < 42 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v42: %w", err)
		}
		defer tx.Rollback()

		// Guard: v43 drops chains/chain_refs, so skip if missing.
		if hasColumn(tx, "chains", "id") {
			cols := []struct{ column, def string }{
				{"priority", `TEXT NOT NULL DEFAULT 'medium'`},
				{"tags", `TEXT NOT NULL DEFAULT '[]'`},
			}
			for _, col := range cols {
				if !hasColumn(tx, "chains", col.column) {
					if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE chains ADD COLUMN %s %s", col.column, col.def)); err != nil {
						return fmt.Errorf("migration v42 add chains.%s: %w", col.column, err)
					}
				}
			}
		}

		if hasColumn(tx, "chain_refs", "id") {
			if _, err := tx.Exec(`DELETE FROM chain_refs WHERE ref_type = 'track' AND track_id > 0
				AND track_id NOT IN (SELECT id FROM tracks)`); err != nil {
				return fmt.Errorf("migration v42 cleanup orphan track refs: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 42"); err != nil {
			return fmt.Errorf("setting schema version v42: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v42: %w", err)
		}
		version = 42
	}

	if version < 43 {
		// Tracks v3: replace chains + old tracks with auto-generated informational tracks.
		if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("migration v43 disable FK: %w", err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v43: %w", err)
		}
		defer tx.Rollback()

		// Drop old tables.
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS chain_refs`,
			`DROP TABLE IF EXISTS chains`,
			`DROP TABLE IF EXISTS track_history`,
			`DROP TABLE IF EXISTS tracks`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v43 drop: %w", err)
			}
		}

		// Create new tracks table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS tracks (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			title           TEXT NOT NULL,
			narrative       TEXT NOT NULL DEFAULT '',
			current_status  TEXT NOT NULL DEFAULT '',
			participants    TEXT NOT NULL DEFAULT '[]',
			timeline        TEXT NOT NULL DEFAULT '[]',
			key_messages    TEXT NOT NULL DEFAULT '[]',
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			tags            TEXT NOT NULL DEFAULT '[]',
			channel_ids     TEXT NOT NULL DEFAULT '[]',
			source_refs     TEXT NOT NULL DEFAULT '[]',
			read_at         TEXT,
			has_updates     INTEGER NOT NULL DEFAULT 0,
			model           TEXT NOT NULL DEFAULT '',
			input_tokens    INTEGER NOT NULL DEFAULT 0,
			output_tokens   INTEGER NOT NULL DEFAULT 0,
			cost_usd        REAL NOT NULL DEFAULT 0,
			prompt_version  INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v43 create tracks: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX idx_tracks_priority ON tracks(priority)`,
			`CREATE INDEX idx_tracks_has_updates ON tracks(has_updates)`,
			`CREATE INDEX idx_tracks_updated ON tracks(updated_at DESC)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v43 index: %w", err)
			}
		}

		// Clean up stale prompt/feedback data.
		if _, err := tx.Exec(`DELETE FROM prompts WHERE id = 'chains.enrich'`); err != nil {
			return fmt.Errorf("migration v43 delete chains.enrich prompt: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM prompts WHERE id = 'tracks.update'`); err != nil {
			return fmt.Errorf("migration v43 delete tracks.update prompt: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM feedback WHERE entity_type = 'chain'`); err != nil {
			return fmt.Errorf("migration v43 delete chain feedback: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 43"); err != nil {
			return fmt.Errorf("setting schema version v43: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v43: %w", err)
		}
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return fmt.Errorf("migration v43 re-enable FK: %w", err)
		}
		version = 43
	}

	if version < 44 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v44: %w", err)
		}
		defer tx.Rollback()

		// Create tasks table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS tasks (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			text            TEXT NOT NULL,
			intent          TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ownership       TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
			ball_on         TEXT NOT NULL DEFAULT '',
			due_date        TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			blocking        TEXT NOT NULL DEFAULT '',
			tags            TEXT NOT NULL DEFAULT '[]',
			sub_items       TEXT NOT NULL DEFAULT '[]',
			source_type     TEXT NOT NULL DEFAULT 'manual' CHECK(source_type IN ('track','digest','briefing','manual','chat')),
			source_id       TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v44 create tasks: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_due_date ON tasks(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source_type, source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v44 index: %w", err)
			}
		}

		// Expand feedback entity_type CHECK to include 'task'.
		if hasColumn(tx, "feedback", "id") {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing', 'task')),
				entity_id   TEXT NOT NULL,
				rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
				comment     TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
			)`); err != nil {
				return fmt.Errorf("migration v44 create feedback_new: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO feedback_new SELECT * FROM feedback`); err != nil {
				return fmt.Errorf("migration v44 copy feedback: %w", err)
			}
			if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
				return fmt.Errorf("migration v44 drop feedback: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
				return fmt.Errorf("migration v44 rename feedback: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
				return fmt.Errorf("migration v44 index feedback entity: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
				return fmt.Errorf("migration v44 index feedback rating: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 44"); err != nil {
			return fmt.Errorf("setting schema version v44: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v44: %w", err)
		}
		version = 44
	}

	if version < 45 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v45: %w", err)
		}
		defer tx.Rollback()

		// Drop old tracks table (v3 narrative-based) and recreate as hybrid v2 action-item tracks.
		if _, err := tx.Exec(`DROP TABLE IF EXISTS tracks`); err != nil {
			return fmt.Errorf("migration v45 drop tracks: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS tracks (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			assignee_user_id    TEXT NOT NULL DEFAULT '',
			text                TEXT NOT NULL,
			context             TEXT NOT NULL DEFAULT '',
			category            TEXT NOT NULL DEFAULT 'task',
			ownership           TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
			ball_on             TEXT NOT NULL DEFAULT '',
			owner_user_id       TEXT NOT NULL DEFAULT '',
			requester_name      TEXT NOT NULL DEFAULT '',
			requester_user_id   TEXT NOT NULL DEFAULT '',
			blocking            TEXT NOT NULL DEFAULT '',
			decision_summary    TEXT NOT NULL DEFAULT '',
			decision_options    TEXT NOT NULL DEFAULT '[]',
			sub_items           TEXT NOT NULL DEFAULT '[]',
			participants        TEXT NOT NULL DEFAULT '[]',
			source_refs         TEXT NOT NULL DEFAULT '[]',
			tags                TEXT NOT NULL DEFAULT '[]',
			channel_ids         TEXT NOT NULL DEFAULT '[]',
			related_digest_ids  TEXT NOT NULL DEFAULT '[]',
			priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			due_date            REAL,
			fingerprint         TEXT NOT NULL DEFAULT '[]',
			read_at             TEXT,
			has_updates         INTEGER NOT NULL DEFAULT 0,
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			prompt_version      INTEGER NOT NULL DEFAULT 0,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v45 create tracks: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_tracks_priority ON tracks(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_tracks_has_updates ON tracks(has_updates)`,
			`CREATE INDEX IF NOT EXISTS idx_tracks_updated ON tracks(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_tracks_ownership ON tracks(ownership)`,
			`CREATE INDEX IF NOT EXISTS idx_tracks_assignee ON tracks(assignee_user_id)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v45 index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 45"); err != nil {
			return fmt.Errorf("setting schema version v45: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v45: %w", err)
		}
		version = 45
	}

	if version < 46 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v46: %w", err)
		}
		defer tx.Rollback()

		// Add inbox_last_processed_ts to workspace.
		if !hasColumn(tx, "workspace", "inbox_last_processed_ts") {
			if _, err := tx.Exec(`ALTER TABLE workspace ADD COLUMN inbox_last_processed_ts REAL NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("migration v46 add workspace column: %w", err)
			}
		}

		// Create inbox_items table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS inbox_items (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id      TEXT NOT NULL,
			message_ts      TEXT NOT NULL,
			thread_ts       TEXT NOT NULL DEFAULT '',
			sender_user_id  TEXT NOT NULL,
			trigger_type    TEXT NOT NULL CHECK(trigger_type IN ('mention','dm')),
			snippet         TEXT NOT NULL DEFAULT '',
			permalink       TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ai_reason       TEXT NOT NULL DEFAULT '',
			resolved_reason TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			task_id         INTEGER,
			read_at         TEXT,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(channel_id, message_ts)
		)`); err != nil {
			return fmt.Errorf("migration v46 create inbox_items: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v46 index: %w", err)
			}
		}

		// Recreate feedback with expanded CHECK to include 'inbox'.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing', 'task', 'inbox')),
			entity_id   TEXT NOT NULL,
			rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
			comment     TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v46 create feedback_new: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO feedback_new SELECT * FROM feedback`); err != nil {
			return fmt.Errorf("migration v46 copy feedback: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
			return fmt.Errorf("migration v46 drop feedback: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
			return fmt.Errorf("migration v46 rename feedback: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`,
			`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v46 feedback index: %w", err)
			}
		}

		// Recreate tasks with expanded source_type CHECK to include 'inbox'.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS tasks_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			text            TEXT NOT NULL,
			intent          TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ownership       TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
			ball_on         TEXT NOT NULL DEFAULT '',
			due_date        TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			blocking        TEXT NOT NULL DEFAULT '',
			tags            TEXT NOT NULL DEFAULT '[]',
			sub_items       TEXT NOT NULL DEFAULT '[]',
			source_type     TEXT NOT NULL DEFAULT 'manual' CHECK(source_type IN ('track','digest','briefing','manual','chat','inbox')),
			source_id       TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v46 create tasks_new: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO tasks_new (id, text, intent, status, priority, ownership,
			ball_on, due_date, snooze_until, blocking, tags, sub_items,
			source_type, source_id, created_at, updated_at)
			SELECT id, text, intent, status, priority, ownership,
			ball_on, due_date, snooze_until, blocking, tags, sub_items,
			source_type, source_id, created_at, updated_at FROM tasks`); err != nil {
			return fmt.Errorf("migration v46 copy tasks: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE tasks`); err != nil {
			return fmt.Errorf("migration v46 drop tasks: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE tasks_new RENAME TO tasks`); err != nil {
			return fmt.Errorf("migration v46 rename tasks: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_due_date ON tasks(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source_type, source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v46 tasks index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 46"); err != nil {
			return fmt.Errorf("setting schema version v46: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v46: %w", err)
		}
		version = 46
	}

	if version < 47 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v47 tx: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "pipeline_runs", "model") {
			if _, err := tx.Exec(`ALTER TABLE pipeline_runs ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v47 add model to pipeline_runs: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 47"); err != nil {
			return fmt.Errorf("setting schema version v47: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v47: %w", err)
		}
		version = 47
	}

	if version < 48 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v48: %w", err)
		}
		defer tx.Rollback()

		// Guard: if inbox_items.task_id is already absent (forward-migrated schema,
		// e.g. bootstrap ran latest Schema then user_version was downgraded for an
		// idempotency test), skip the table recreate — structure is already newer.
		if !hasColumn(tx, "inbox_items", "task_id") {
			if _, err := tx.Exec("PRAGMA user_version = 48"); err != nil {
				return fmt.Errorf("v48: set version: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("committing migration v48: %w", err)
			}
			version = 48
			goto afterV48
		}

		// Recreate inbox_items with expanded trigger_type CHECK and new columns.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS inbox_items_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id      TEXT NOT NULL,
			message_ts      TEXT NOT NULL,
			thread_ts       TEXT NOT NULL DEFAULT '',
			sender_user_id  TEXT NOT NULL,
			trigger_type    TEXT NOT NULL CHECK(trigger_type IN ('mention','dm','thread_reply','reaction')),
			snippet         TEXT NOT NULL DEFAULT '',
			context         TEXT NOT NULL DEFAULT '',
			raw_text        TEXT NOT NULL DEFAULT '',
			permalink       TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ai_reason       TEXT NOT NULL DEFAULT '',
			resolved_reason TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			task_id         INTEGER,
			read_at         TEXT,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(channel_id, message_ts)
		)`); err != nil {
			return fmt.Errorf("v48: create inbox_items_new: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO inbox_items_new (id, channel_id, message_ts, thread_ts, sender_user_id,
			trigger_type, snippet, permalink, status, priority, ai_reason, resolved_reason,
			snooze_until, task_id, read_at, created_at, updated_at)
			SELECT id, channel_id, message_ts, thread_ts, sender_user_id,
			trigger_type, snippet, permalink, status, priority, ai_reason, resolved_reason,
			snooze_until, task_id, read_at, created_at, updated_at
			FROM inbox_items`); err != nil {
			return fmt.Errorf("v48: copy inbox_items: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE inbox_items`); err != nil {
			return fmt.Errorf("v48: drop inbox_items: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE inbox_items_new RENAME TO inbox_items`); err != nil {
			return fmt.Errorf("v48: rename inbox_items: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("v48: inbox index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 48"); err != nil {
			return fmt.Errorf("v48: set version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v48: %w", err)
		}
		version = 48
	}
afterV48:

	if version < 49 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v49: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "users", "is_stub") {
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN is_stub INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("migration v49 add is_stub: %w", err)
			}
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_users_is_stub ON users(is_stub)`); err != nil {
			return fmt.Errorf("migration v49 index is_stub: %w", err)
		}
		// Mark existing users with empty real_name and profile as stubs for backfill.
		if _, err := tx.Exec(`UPDATE users SET is_stub = 1 WHERE real_name = '' AND profile_json = '{}'`); err != nil {
			return fmt.Errorf("migration v49 mark stubs: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 49"); err != nil {
			return fmt.Errorf("setting schema version v49: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v49: %w", err)
		}
		version = 49
	}

	if version < 50 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v50: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "inbox_items", "waiting_user_ids") {
			if _, err := tx.Exec(`ALTER TABLE inbox_items ADD COLUMN waiting_user_ids TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v50 add waiting_user_ids: %w", err)
			}
		}

		// Backfill: set waiting_user_ids from sender_user_id for existing items.
		if _, err := tx.Exec(`UPDATE inbox_items SET waiting_user_ids = '["' || sender_user_id || '"]'
			WHERE waiting_user_ids = '' AND sender_user_id != ''`); err != nil {
			return fmt.Errorf("migration v50 backfill waiting_user_ids: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 50"); err != nil {
			return fmt.Errorf("setting schema version v50: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v50: %w", err)
		}
		version = 50
	}

	if version < 51 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v51 tx: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "users", "is_bot_override") {
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN is_bot_override INTEGER DEFAULT NULL`); err != nil {
				return fmt.Errorf("migration v51 add is_bot_override: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 51"); err != nil {
			return fmt.Errorf("setting schema version v51: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v51: %w", err)
		}
		version = 51
	}

	if version < 52 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v52: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "users", "is_muted_for_llm") {
			if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN is_muted_for_llm INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("migration v52 add is_muted_for_llm to users: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 52"); err != nil {
			return fmt.Errorf("setting schema version v52: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v52: %w", err)
		}
		version = 52
	}

	if version < 53 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v53: %w", err)
		}
		defer tx.Rollback()

		// Convert due_date from YYYY-MM-DD to YYYY-MM-DDTHH:MM format.
		if _, err := tx.Exec(`UPDATE tasks SET due_date = due_date || 'T00:00'
			WHERE due_date != '' AND due_date NOT LIKE '%T%'`); err != nil {
			return fmt.Errorf("migration v53 convert due_date: %w", err)
		}
		// Convert snooze_until from YYYY-MM-DD to YYYY-MM-DDTHH:MM format.
		if _, err := tx.Exec(`UPDATE tasks SET snooze_until = snooze_until || 'T00:00'
			WHERE snooze_until != '' AND snooze_until NOT LIKE '%T%'`); err != nil {
			return fmt.Errorf("migration v53 convert snooze_until: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 53"); err != nil {
			return fmt.Errorf("setting schema version v53: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v53: %w", err)
		}
		version = 53
	}

	if version < 54 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v54: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "tracks", "dismissed_at") {
			if _, err := tx.Exec(`ALTER TABLE tracks ADD COLUMN dismissed_at TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v54 add dismissed_at: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 54"); err != nil {
			return fmt.Errorf("setting schema version v54: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v54: %w", err)
		}
		version = 54
	}

	if version < 55 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v55: %w", err)
		}
		defer tx.Rollback()

		// Drop old calendar_events table (had REAL timestamps, single-table design).
		if _, err := tx.Exec(`DROP TABLE IF EXISTS calendar_events`); err != nil {
			return fmt.Errorf("migration v55 drop old calendar_events: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS calendar_calendars (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			is_primary  INTEGER NOT NULL DEFAULT 0,
			is_selected INTEGER NOT NULL DEFAULT 1,
			color       TEXT NOT NULL DEFAULT '',
			synced_at   TEXT NOT NULL DEFAULT ''
		)`); err != nil {
			return fmt.Errorf("migration v55 create calendar_calendars: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS calendar_events (
			id              TEXT PRIMARY KEY,
			calendar_id     TEXT NOT NULL REFERENCES calendar_calendars(id),
			title           TEXT NOT NULL DEFAULT '',
			description     TEXT NOT NULL DEFAULT '',
			location        TEXT NOT NULL DEFAULT '',
			start_time      TEXT NOT NULL,
			end_time        TEXT NOT NULL,
			organizer_email TEXT NOT NULL DEFAULT '',
			attendees       TEXT NOT NULL DEFAULT '[]',
			is_recurring    INTEGER NOT NULL DEFAULT 0,
			is_all_day      INTEGER NOT NULL DEFAULT 0,
			event_status    TEXT NOT NULL DEFAULT 'confirmed',
			event_type      TEXT NOT NULL DEFAULT '',
			html_link       TEXT NOT NULL DEFAULT '',
			raw_json        TEXT NOT NULL DEFAULT '{}',
			synced_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at      TEXT NOT NULL DEFAULT ''
		)`); err != nil {
			return fmt.Errorf("migration v55 create calendar_events: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_calendar_events_calendar ON calendar_events(calendar_id)`); err != nil {
			return fmt.Errorf("migration v55 create idx_calendar_events_calendar: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_calendar_events_start ON calendar_events(start_time)`); err != nil {
			return fmt.Errorf("migration v55 create idx_calendar_events_start: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_calendar_events_end ON calendar_events(end_time)`); err != nil {
			return fmt.Errorf("migration v55 create idx_calendar_events_end: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 55"); err != nil {
			return fmt.Errorf("setting schema version v55: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v55: %w", err)
		}
		version = 55
	}

	if version < 56 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v56: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS calendar_attendee_map (
			email         TEXT PRIMARY KEY,
			slack_user_id TEXT NOT NULL DEFAULT '',
			resolved_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`); err != nil {
			return fmt.Errorf("migration v56 create calendar_attendee_map: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 56"); err != nil {
			return fmt.Errorf("setting schema version v56: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v56: %w", err)
		}
		version = 56
	}

	if version < 57 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v57: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS meeting_prep_cache (
			event_id      TEXT PRIMARY KEY,
			result_json   TEXT NOT NULL DEFAULT '',
			user_notes    TEXT NOT NULL DEFAULT '',
			generated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`); err != nil {
			return fmt.Errorf("migration v57 create meeting_prep_cache: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_meeting_prep_cache_generated ON meeting_prep_cache(generated_at)`); err != nil {
			return fmt.Errorf("migration v57 create idx_meeting_prep_cache_generated: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 57"); err != nil {
			return fmt.Errorf("setting schema version v57: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v57: %w", err)
		}
		version = 57
	}

	if version < 58 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v58: %w", err)
		}
		defer tx.Rollback()

		jiraTables := []string{
			`CREATE TABLE IF NOT EXISTS jira_boards (
				id INTEGER PRIMARY KEY, name TEXT NOT NULL, project_key TEXT NOT NULL DEFAULT '',
				board_type TEXT NOT NULL DEFAULT '', is_selected INTEGER NOT NULL DEFAULT 0,
				issue_count INTEGER NOT NULL DEFAULT 0, synced_at TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS jira_issues (
				key TEXT PRIMARY KEY, id TEXT NOT NULL DEFAULT '', project_key TEXT NOT NULL,
				board_id INTEGER,
				summary TEXT NOT NULL, description_text TEXT NOT NULL DEFAULT '',
				issue_type TEXT NOT NULL DEFAULT '', issue_type_category TEXT NOT NULL DEFAULT '',
				is_bug INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL, status_category TEXT NOT NULL,
				status_category_changed_at TEXT NOT NULL DEFAULT '',
				assignee_account_id TEXT NOT NULL DEFAULT '', assignee_email TEXT NOT NULL DEFAULT '',
				assignee_display_name TEXT NOT NULL DEFAULT '', assignee_slack_id TEXT NOT NULL DEFAULT '',
				reporter_account_id TEXT NOT NULL DEFAULT '', reporter_email TEXT NOT NULL DEFAULT '',
				reporter_display_name TEXT NOT NULL DEFAULT '', reporter_slack_id TEXT NOT NULL DEFAULT '',
				priority TEXT NOT NULL DEFAULT '', story_points REAL,
				due_date TEXT NOT NULL DEFAULT '', sprint_id INTEGER, sprint_name TEXT NOT NULL DEFAULT '',
				epic_key TEXT NOT NULL DEFAULT '',
				labels TEXT NOT NULL DEFAULT '[]', components TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL, updated_at TEXT NOT NULL, resolved_at TEXT NOT NULL DEFAULT '',
				raw_json TEXT NOT NULL DEFAULT '', synced_at TEXT NOT NULL, is_deleted INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE TABLE IF NOT EXISTS jira_sprints (
				id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL, name TEXT NOT NULL,
				state TEXT NOT NULL, goal TEXT NOT NULL DEFAULT '',
				start_date TEXT NOT NULL DEFAULT '', end_date TEXT NOT NULL DEFAULT '',
				complete_date TEXT NOT NULL DEFAULT '', synced_at TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS jira_issue_links (
				id TEXT PRIMARY KEY, source_key TEXT NOT NULL, target_key TEXT NOT NULL,
				link_type TEXT NOT NULL, synced_at TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS jira_user_map (
				jira_account_id TEXT PRIMARY KEY, email TEXT NOT NULL DEFAULT '',
				slack_user_id TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
				match_method TEXT NOT NULL DEFAULT '', match_confidence REAL NOT NULL DEFAULT 0,
				resolved_at TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS jira_sync_state (
				project_key TEXT PRIMARY KEY, last_synced_at TEXT NOT NULL DEFAULT '',
				issues_synced INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
				last_error_at TEXT NOT NULL DEFAULT ''
			)`,
		}
		for _, stmt := range jiraTables {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v58 create table: %w", err)
			}
		}

		jiraIndexes := []string{
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_project ON jira_issues(project_key)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee ON jira_issues(assignee_account_id)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_status_cat ON jira_issues(status_category)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_sprint ON jira_issues(sprint_id)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_epic ON jira_issues(epic_key)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_updated ON jira_issues(updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_due ON jira_issues(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_board ON jira_issues(board_id)`,
		}
		for _, stmt := range jiraIndexes {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v58 create index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 58"); err != nil {
			return fmt.Errorf("setting schema version v58: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v58: %w", err)
		}
		version = 58
	}

	if version < 59 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v59: %w", err)
		}
		defer tx.Rollback()

		// Add board profile columns.
		boardProfileCols := []struct {
			name string
			def  string
		}{
			{"raw_columns_json", "TEXT NOT NULL DEFAULT ''"},
			{"raw_config_json", "TEXT NOT NULL DEFAULT ''"},
			{"llm_profile_json", "TEXT NOT NULL DEFAULT ''"},
			{"workflow_summary", "TEXT NOT NULL DEFAULT ''"},
			{"user_overrides_json", "TEXT NOT NULL DEFAULT ''"},
			{"config_hash", "TEXT NOT NULL DEFAULT ''"},
			{"profile_generated_at", "TEXT NOT NULL DEFAULT ''"},
		}
		for _, col := range boardProfileCols {
			if !hasColumn(tx, "jira_boards", col.name) {
				if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE jira_boards ADD COLUMN %s %s", col.name, col.def)); err != nil {
					return fmt.Errorf("migration v59 add column %s: %w", col.name, err)
				}
			}
		}

		// Create jira_slack_links table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS jira_slack_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_key TEXT NOT NULL,
			channel_id TEXT NOT NULL DEFAULT '',
			message_ts TEXT NOT NULL DEFAULT '',
			track_id INTEGER,
			digest_id INTEGER,
			link_type TEXT NOT NULL DEFAULT 'mention',
			detected_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			UNIQUE(issue_key, channel_id, message_ts)
		)`); err != nil {
			return fmt.Errorf("migration v59 create jira_slack_links: %w", err)
		}
		slackLinksIndexes := []string{
			`CREATE INDEX IF NOT EXISTS idx_jira_slack_links_issue ON jira_slack_links(issue_key)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_slack_links_channel ON jira_slack_links(channel_id, message_ts)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_slack_links_track ON jira_slack_links(track_id)`,
		}
		for _, stmt := range slackLinksIndexes {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v59 create index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 59"); err != nil {
			return fmt.Errorf("setting schema version v59: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v59: %w", err)
		}
		version = 59
	}

	if version < 60 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v60 tx: %w", err)
		}
		defer tx.Rollback()

		// New indexes for jira_slack_links and jira_issues.
		indexes := []string{
			`CREATE INDEX IF NOT EXISTS idx_jira_slack_links_digest ON jira_slack_links(digest_id)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee_slack ON jira_issues(assignee_slack_id)`,
			`CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee_status ON jira_issues(assignee_slack_id, status_category)`,
		}
		for _, stmt := range indexes {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v60 create index: %w", err)
			}
		}

		// Expand tasks.source_type CHECK to include 'jira'.
		// SQLite does not support ALTER TABLE to modify CHECK constraints, so we recreate the table.
		if _, err := tx.Exec(`CREATE TABLE tasks_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			text            TEXT NOT NULL,
			intent          TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ownership       TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
			ball_on         TEXT NOT NULL DEFAULT '',
			due_date        TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			blocking        TEXT NOT NULL DEFAULT '',
			tags            TEXT NOT NULL DEFAULT '[]',
			sub_items       TEXT NOT NULL DEFAULT '[]',
			source_type     TEXT NOT NULL DEFAULT 'manual' CHECK(source_type IN ('track','digest','briefing','manual','chat','inbox','jira')),
			source_id       TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v60 create tasks_new: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO tasks_new (id, text, intent, status, priority, ownership,
			ball_on, due_date, snooze_until, blocking, tags, sub_items,
			source_type, source_id, created_at, updated_at)
			SELECT id, text, intent, status, priority, ownership,
			ball_on, due_date, snooze_until, blocking, tags, sub_items,
			source_type, source_id, created_at, updated_at FROM tasks`); err != nil {
			return fmt.Errorf("migration v60 copy tasks: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE tasks`); err != nil {
			return fmt.Errorf("migration v60 drop tasks: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE tasks_new RENAME TO tasks`); err != nil {
			return fmt.Errorf("migration v60 rename tasks: %w", err)
		}
		// Recreate indexes on tasks.
		taskIndexes := []string{
			`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_due_date ON tasks(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source_type, source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC)`,
		}
		for _, stmt := range taskIndexes {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration v60 create task index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 60"); err != nil {
			return fmt.Errorf("setting schema version v60: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v60: %w", err)
		}
		version = 60
	}

	if version < 61 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v61: %w", err)
		}
		defer tx.Rollback()

		// Create jira_releases table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS jira_releases (
			id INTEGER NOT NULL,
			project_key TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			release_date TEXT NOT NULL DEFAULT '',
			released INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			synced_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (id),
			UNIQUE(project_key, name)
		)`); err != nil {
			return fmt.Errorf("migration v61 create jira_releases: %w", err)
		}

		// Add fix_versions column to jira_issues.
		if !hasColumn(tx, "jira_issues", "fix_versions") {
			if _, err := tx.Exec(`ALTER TABLE jira_issues ADD COLUMN fix_versions TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migration v61 add fix_versions: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 61"); err != nil {
			return fmt.Errorf("setting schema version v61: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v61: %w", err)
		}
		version = 61
	}

	if version < 62 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v62: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS jira_custom_fields (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			field_type TEXT NOT NULL,
			items_type TEXT NOT NULL DEFAULT '',
			is_useful INTEGER NOT NULL DEFAULT 0,
			usage_hint TEXT NOT NULL DEFAULT '',
			synced_at TEXT NOT NULL DEFAULT ''
		)`); err != nil {
			return fmt.Errorf("migration v62 create jira_custom_fields: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS jira_board_field_map (
			board_id INTEGER NOT NULL,
			field_id TEXT NOT NULL,
			role TEXT NOT NULL,
			PRIMARY KEY (board_id, field_id)
		)`); err != nil {
			return fmt.Errorf("migration v62 create jira_board_field_map: %w", err)
		}

		if !hasColumn(tx, "jira_issues", "custom_fields_json") {
			if _, err := tx.Exec(`ALTER TABLE jira_issues ADD COLUMN custom_fields_json TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migration v62 add custom_fields_json: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 62"); err != nil {
			return fmt.Errorf("setting schema version v62: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v62: %w", err)
		}
		version = 62
	}

	if version < 63 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v63: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS meeting_notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('question', 'note')),
			text TEXT NOT NULL DEFAULT '',
			is_checked INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			task_id INTEGER,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v63 create meeting_notes: %w", err)
		}

		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_meeting_notes_event ON meeting_notes(event_id)`); err != nil {
			return fmt.Errorf("migration v63 create idx_meeting_notes_event: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 63"); err != nil {
			return fmt.Errorf("setting schema version v63: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v63: %w", err)
		}
		version = 63
	}

	if version < 64 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v64: %w", err)
		}
		defer tx.Rollback()

		if !hasColumn(tx, "tasks", "notes") {
			if _, err := tx.Exec(`ALTER TABLE tasks ADD COLUMN notes TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("migration v64 add notes column: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 64"); err != nil {
			return fmt.Errorf("setting schema version v64: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v64: %w", err)
		}
		version = 64
	}

	if version < 65 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v65: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS calendar_auth_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			status TEXT NOT NULL DEFAULT 'ok',
			error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
			return fmt.Errorf("migration v65 create calendar_auth_state: %w", err)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO calendar_auth_state (id, status, error) VALUES (1, 'ok', '')`); err != nil {
			return fmt.Errorf("migration v65 seed calendar_auth_state: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 65"); err != nil {
			return fmt.Errorf("setting schema version v65: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v65: %w", err)
		}
		version = 65
	}

	if version < 66 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v66: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS day_plans (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id              TEXT NOT NULL,
			plan_date            TEXT NOT NULL,
			status               TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
			has_conflicts        INTEGER NOT NULL DEFAULT 0,
			conflict_summary     TEXT,
			generated_at         TEXT NOT NULL,
			last_regenerated_at  TEXT,
			regenerate_count     INTEGER NOT NULL DEFAULT 0,
			feedback_history     TEXT,
			prompt_version       TEXT,
			briefing_id          INTEGER,
			read_at              TEXT,
			created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			UNIQUE (user_id, plan_date),
			FOREIGN KEY (briefing_id) REFERENCES briefings(id) ON DELETE SET NULL
		)`); err != nil {
			return fmt.Errorf("migration v66 create day_plans: %w", err)
		}

		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_day_plans_date ON day_plans(plan_date DESC)`); err != nil {
			return fmt.Errorf("migration v66 create idx_day_plans_date: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_day_plans_user_date ON day_plans(user_id, plan_date DESC)`); err != nil {
			return fmt.Errorf("migration v66 create idx_day_plans_user_date: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS day_plan_items (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			day_plan_id  INTEGER NOT NULL,
			kind         TEXT NOT NULL CHECK (kind IN ('timeblock','backlog')),
			source_type  TEXT NOT NULL CHECK (source_type IN ('task','briefing_attention','jira','calendar','manual','focus')),
			source_id    TEXT,
			title        TEXT NOT NULL,
			description  TEXT,
			rationale    TEXT,
			start_time   TEXT,
			end_time     TEXT,
			duration_min INTEGER,
			priority     TEXT CHECK (priority IS NULL OR priority IN ('high','medium','low')),
			status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','skipped')),
			order_index  INTEGER NOT NULL DEFAULT 0,
			tags         TEXT,
			created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			FOREIGN KEY (day_plan_id) REFERENCES day_plans(id) ON DELETE CASCADE
		)`); err != nil {
			return fmt.Errorf("migration v66 create day_plan_items: %w", err)
		}

		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_day_plan_items_plan ON day_plan_items(day_plan_id)`); err != nil {
			return fmt.Errorf("migration v66 create idx_day_plan_items_plan: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_day_plan_items_source ON day_plan_items(source_type, source_id)`); err != nil {
			return fmt.Errorf("migration v66 create idx_day_plan_items_source: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 66"); err != nil {
			return fmt.Errorf("setting schema version v66: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v66: %w", err)
		}
		version = 66
	}

	if version < 67 {
		// Disable FK checks so we can drop tasks (inbox_items.task_id references nothing,
		// but targets has a self-referential FK that needs to exist before inbox_items rename).
		if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("migration v67 disable FK: %w", err)
		}
		// Re-enable FK unconditionally on exit from this block, even on early-return errors.
		defer func() { _, _ = db.Exec("PRAGMA foreign_keys = ON") }()

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v67: %w", err)
		}
		defer tx.Rollback()

		// 1. Create targets table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS targets (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			text                TEXT NOT NULL,
			intent              TEXT NOT NULL DEFAULT '',
			level               TEXT NOT NULL DEFAULT 'day'
			                    CHECK(level IN ('quarter','month','week','day','custom')),
			custom_label        TEXT NOT NULL DEFAULT '',
			period_start        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d','now')),
			period_end          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d','now')),
			parent_id           INTEGER REFERENCES targets(id) ON DELETE SET NULL,
			status              TEXT NOT NULL DEFAULT 'todo'
			                    CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('high','medium','low')),
			ownership           TEXT NOT NULL DEFAULT 'mine'
			                    CHECK(ownership IN ('mine','delegated','watching')),
			ball_on             TEXT NOT NULL DEFAULT '',
			due_date            TEXT NOT NULL DEFAULT '',
			snooze_until        TEXT NOT NULL DEFAULT '',
			blocking            TEXT NOT NULL DEFAULT '',
			tags                TEXT NOT NULL DEFAULT '[]',
			sub_items           TEXT NOT NULL DEFAULT '[]',
			notes               TEXT NOT NULL DEFAULT '[]',
			progress            REAL NOT NULL DEFAULT 0.0,
			source_type         TEXT NOT NULL DEFAULT 'manual'
			                    CHECK(source_type IN ('extract','track','digest','briefing','manual','chat','inbox','jira','slack','promoted_subitem')),
			source_id           TEXT NOT NULL DEFAULT '',
			ai_level_confidence REAL DEFAULT NULL,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`); err != nil {
			return fmt.Errorf("migration v67 create targets: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_targets_level     ON targets(level)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_parent    ON targets(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_period    ON targets(period_start, period_end)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_status    ON targets(status)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_priority  ON targets(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_due       ON targets(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_source    ON targets(source_type, source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_updated   ON targets(updated_at DESC)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v67 targets index: %w", err)
			}
		}

		// 2. Create target_links table.
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS target_links (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			source_target_id INTEGER NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
			target_target_id INTEGER REFERENCES targets(id) ON DELETE CASCADE,
			external_ref     TEXT NOT NULL DEFAULT '',
			relation         TEXT NOT NULL
			                 CHECK(relation IN ('contributes_to','blocks','related','duplicates')),
			confidence       REAL DEFAULT NULL,
			created_by       TEXT NOT NULL DEFAULT 'ai'
			                 CHECK(created_by IN ('ai','user')),
			created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			CHECK (target_target_id IS NOT NULL OR external_ref != ''),
			UNIQUE(source_target_id, target_target_id, external_ref, relation)
		)`); err != nil {
			return fmt.Errorf("migration v67 create target_links: %w", err)
		}
		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_target_links_source   ON target_links(source_target_id)`,
			`CREATE INDEX IF NOT EXISTS idx_target_links_target   ON target_links(target_target_id)`,
			`CREATE INDEX IF NOT EXISTS idx_target_links_external ON target_links(external_ref)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migration v67 target_links index: %w", err)
			}
		}

		// 3. Delete feedback rows with entity_type='task' before tightening CHECK.
		// Guard: feedback table may not exist in minimal/test DBs.
		if hasColumn(tx, "feedback", "id") {
			if _, err := tx.Exec(`DELETE FROM feedback WHERE entity_type = 'task'`); err != nil {
				return fmt.Errorf("migration v67 delete task feedback: %w", err)
			}
		}

		// 4. Rebuild feedback with updated CHECK constraint (remove 'task', add 'target').
		if hasColumn(tx, "feedback", "id") {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS feedback_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				entity_type TEXT NOT NULL CHECK(entity_type IN ('digest','track','decision','user_analysis','briefing','target','inbox')),
				entity_id   TEXT NOT NULL,
				rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
				comment     TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
			)`); err != nil {
				return fmt.Errorf("migration v67 create feedback_new: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO feedback_new SELECT * FROM feedback`); err != nil {
				return fmt.Errorf("migration v67 copy feedback: %w", err)
			}
			if _, err := tx.Exec(`DROP TABLE feedback`); err != nil {
				return fmt.Errorf("migration v67 drop feedback: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE feedback_new RENAME TO feedback`); err != nil {
				return fmt.Errorf("migration v67 rename feedback: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id)`); err != nil {
				return fmt.Errorf("migration v67 feedback entity index: %w", err)
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating)`); err != nil {
				return fmt.Errorf("migration v67 feedback rating index: %w", err)
			}
		}

		// 5. Rebuild inbox_items to rename task_id → target_id (SQLite cannot rename within ALTER).
		if hasColumn(tx, "inbox_items", "task_id") {
			if _, err := tx.Exec(`CREATE TABLE inbox_items_new (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				channel_id      TEXT NOT NULL,
				message_ts      TEXT NOT NULL,
				thread_ts       TEXT NOT NULL DEFAULT '',
				sender_user_id  TEXT NOT NULL,
				trigger_type    TEXT NOT NULL CHECK(trigger_type IN ('mention','dm','thread_reply','reaction')),
				snippet         TEXT NOT NULL DEFAULT '',
				context         TEXT NOT NULL DEFAULT '',
				raw_text        TEXT NOT NULL DEFAULT '',
				permalink       TEXT NOT NULL DEFAULT '',
				status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
				priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
				ai_reason       TEXT NOT NULL DEFAULT '',
				resolved_reason TEXT NOT NULL DEFAULT '',
				snooze_until    TEXT NOT NULL DEFAULT '',
				waiting_user_ids TEXT NOT NULL DEFAULT '',
				target_id       INTEGER,
				read_at         TEXT,
				created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
				updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
				UNIQUE(channel_id, message_ts)
			)`); err != nil {
				return fmt.Errorf("migration v67 create inbox_items_new: %w", err)
			}
			// Copy all columns; set target_id = NULL (old task_id values are orphaned).
			if _, err := tx.Exec(`INSERT INTO inbox_items_new
				(id, channel_id, message_ts, thread_ts, sender_user_id, trigger_type,
				 snippet, context, raw_text, permalink, status, priority,
				 ai_reason, resolved_reason, snooze_until, waiting_user_ids,
				 target_id, read_at, created_at, updated_at)
				SELECT id, channel_id, message_ts, thread_ts, sender_user_id, trigger_type,
				 snippet, COALESCE(context,''), COALESCE(raw_text,''), permalink, status, priority,
				 ai_reason, resolved_reason, snooze_until, COALESCE(waiting_user_ids,''),
				 NULL, read_at, created_at, updated_at
				FROM inbox_items`); err != nil {
				return fmt.Errorf("migration v67 copy inbox_items: %w", err)
			}
			// Capture any pre-existing indexes on inbox_items before the DROP so
			// they survive the rebuild (the canonical 5 are always recreated below).
			savedIndexes := func() []string {
				idxRows, idxErr := tx.Query(`SELECT sql FROM sqlite_master
					WHERE tbl_name='inbox_items' AND type='index' AND sql IS NOT NULL`)
				if idxErr != nil {
					return nil
				}
				defer idxRows.Close()
				var out []string
				for idxRows.Next() {
					var s string
					if scanErr := idxRows.Scan(&s); scanErr == nil {
						out = append(out, s)
					}
				}
				return out
			}()
			if _, err := tx.Exec(`DROP TABLE inbox_items`); err != nil {
				return fmt.Errorf("migration v67 drop inbox_items: %w", err)
			}
			if _, err := tx.Exec(`ALTER TABLE inbox_items_new RENAME TO inbox_items`); err != nil {
				return fmt.Errorf("migration v67 rename inbox_items: %w", err)
			}
			// Recreate the full canonical index set (using IF NOT EXISTS).
			for _, idx := range []string{
				`CREATE INDEX IF NOT EXISTS idx_inbox_items_status   ON inbox_items(status)`,
				`CREATE INDEX IF NOT EXISTS idx_inbox_items_priority  ON inbox_items(priority)`,
				`CREATE INDEX IF NOT EXISTS idx_inbox_items_updated   ON inbox_items(updated_at DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_inbox_items_sender    ON inbox_items(sender_user_id)`,
				`CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze    ON inbox_items(snooze_until)`,
			} {
				if _, err := tx.Exec(idx); err != nil {
					return fmt.Errorf("migration v67 inbox_items index: %w", err)
				}
			}
			// Replay any additional pre-existing indexes that aren't in the canonical set.
			for _, idxSQL := range savedIndexes {
				// Replace the old table reference with the renamed table (idempotent for
				// most cases since after RENAME the stored name updates in-place, but the
				// captured SQL still says "inbox_items" which is now the final table name).
				if _, err := tx.Exec(idxSQL); err != nil {
					// Non-fatal: the index may already exist via the canonical set above.
					log.Printf("migration v67: could not replay index (skipping): %v", err)
				}
			}
		}

		// 6. Drop tasks table (clean-slate migration — existing tasks are discarded).
		var taskCount int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&taskCount)
		if taskCount > 0 {
			var rowCount int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&rowCount)
			if rowCount > 0 {
				log.Printf("migration v67: dropping %d rows from tasks table", rowCount)
			}
		}
		if _, err := tx.Exec(`DROP TABLE IF EXISTS tasks`); err != nil {
			return fmt.Errorf("migration v67 drop tasks: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 67"); err != nil {
			return fmt.Errorf("setting schema version v67: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v67: %w", err)
		}
		// FK re-enable is handled by the defer above.
		version = 67
	}

	if version < 68 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v68: %w", err)
		}
		defer tx.Rollback()

		// Rebuild inbox_items to add new columns AND relax trigger_type CHECK.
		// After v67, inbox_items has target_id (not task_id). We preserve it here.
		// SQLite cannot ALTER CHECK constraints, so we must recreate the table.
		if _, err := tx.Exec(`CREATE TABLE inbox_items_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id      TEXT NOT NULL,
			message_ts      TEXT NOT NULL,
			thread_ts       TEXT NOT NULL DEFAULT '',
			sender_user_id  TEXT NOT NULL,
			trigger_type    TEXT NOT NULL CHECK(trigger_type IN (
				'mention','dm','thread_reply','reaction',
				'jira_assigned','jira_comment_mention','jira_comment_watching','jira_status_change','jira_priority_change',
				'calendar_invite','calendar_time_change','calendar_cancelled',
				'decision_made','briefing_ready'
			)),
			snippet         TEXT NOT NULL DEFAULT '',
			context         TEXT NOT NULL DEFAULT '',
			raw_text        TEXT NOT NULL DEFAULT '',
			permalink       TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
			priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
			ai_reason       TEXT NOT NULL DEFAULT '',
			resolved_reason TEXT NOT NULL DEFAULT '',
			snooze_until    TEXT NOT NULL DEFAULT '',
			waiting_user_ids TEXT NOT NULL DEFAULT '[]',
			target_id       INTEGER,
			read_at         TEXT,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			item_class      TEXT NOT NULL DEFAULT 'actionable' CHECK(item_class IN ('actionable','ambient')),
			pinned          INTEGER NOT NULL DEFAULT 0,
			archived_at     TEXT,
			archive_reason  TEXT DEFAULT '' CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed')),
			UNIQUE(channel_id, message_ts)
		)`); err != nil {
			return fmt.Errorf("migrate v68: create inbox_items_new: %w", err)
		}

		if _, err := tx.Exec(`INSERT INTO inbox_items_new (
			id, channel_id, message_ts, thread_ts, sender_user_id, trigger_type,
			snippet, context, raw_text, permalink, status, priority, ai_reason,
			resolved_reason, snooze_until, waiting_user_ids, target_id, read_at,
			created_at, updated_at, item_class
		)
		SELECT
			id, channel_id, message_ts, thread_ts, sender_user_id, trigger_type,
			snippet, COALESCE(context,''), COALESCE(raw_text,''), permalink, status, priority, ai_reason,
			resolved_reason, snooze_until, COALESCE(waiting_user_ids,'[]'), target_id,
			read_at,
			created_at, updated_at,
			CASE WHEN trigger_type = 'reaction' THEN 'ambient' ELSE 'actionable' END
		FROM inbox_items`); err != nil {
			return fmt.Errorf("migrate v68: copy inbox_items: %w", err)
		}

		if _, err := tx.Exec(`DROP TABLE inbox_items`); err != nil {
			return fmt.Errorf("migrate v68: drop inbox_items: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE inbox_items_new RENAME TO inbox_items`); err != nil {
			return fmt.Errorf("migrate v68: rename inbox_items: %w", err)
		}

		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_class_status ON inbox_items(item_class, status)`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1`,
			`CREATE INDEX IF NOT EXISTS idx_inbox_items_archived ON inbox_items(archived_at)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migrate v68: index: %w", err)
			}
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS inbox_learned_rules (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_type      TEXT NOT NULL CHECK(rule_type IN ('source_mute','source_boost','trigger_downgrade','trigger_boost')),
			scope_key      TEXT NOT NULL,
			weight         REAL NOT NULL,
			source         TEXT NOT NULL CHECK(source IN ('implicit','explicit_feedback','user_rule')),
			evidence_count INTEGER NOT NULL DEFAULT 0,
			last_updated   TEXT NOT NULL,
			UNIQUE(rule_type, scope_key)
		)`); err != nil {
			return fmt.Errorf("migrate v68: create inbox_learned_rules: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_learned_rules_scope ON inbox_learned_rules(rule_type, scope_key)`); err != nil {
			return fmt.Errorf("migrate v68: index inbox_learned_rules: %w", err)
		}

		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS inbox_feedback (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			inbox_item_id INTEGER NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
			rating        INTEGER NOT NULL CHECK(rating IN (-1,1)),
			reason        TEXT DEFAULT '' CHECK(reason IN ('','source_noise','wrong_priority','wrong_class','never_show')),
			created_at    TEXT NOT NULL
		)`); err != nil {
			return fmt.Errorf("migrate v68: create inbox_feedback: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_feedback_item ON inbox_feedback(inbox_item_id)`); err != nil {
			return fmt.Errorf("migrate v68: index inbox_feedback: %w", err)
		}

		if _, err := tx.Exec("PRAGMA user_version = 68"); err != nil {
			return fmt.Errorf("setting schema version v68: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v68: %w", err)
		}
		version = 68
	}

	if version < 69 {
		// v69: extend targets.source_type CHECK to allow 'promoted_subitem'
		// (used when a sub-item is promoted to a standalone child target).
		// SQLite cannot ALTER a CHECK constraint, so we rebuild the table.
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v69: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`CREATE TABLE targets_new (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			text                TEXT NOT NULL,
			intent              TEXT NOT NULL DEFAULT '',
			level               TEXT NOT NULL DEFAULT 'day'
			                    CHECK(level IN ('quarter','month','week','day','custom')),
			custom_label        TEXT NOT NULL DEFAULT '',
			period_start        TEXT NOT NULL DEFAULT '',
			period_end          TEXT NOT NULL DEFAULT '',
			parent_id           INTEGER REFERENCES targets(id) ON DELETE SET NULL,
			status              TEXT NOT NULL DEFAULT 'todo'
			                    CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
			priority            TEXT NOT NULL DEFAULT 'medium'
			                    CHECK(priority IN ('high','medium','low')),
			ownership           TEXT NOT NULL DEFAULT 'mine'
			                    CHECK(ownership IN ('mine','delegated','watching')),
			ball_on             TEXT NOT NULL DEFAULT '',
			due_date            TEXT NOT NULL DEFAULT '',
			snooze_until        TEXT NOT NULL DEFAULT '',
			blocking            TEXT NOT NULL DEFAULT '',
			tags                TEXT NOT NULL DEFAULT '[]',
			sub_items           TEXT NOT NULL DEFAULT '[]',
			notes               TEXT NOT NULL DEFAULT '[]',
			progress            REAL NOT NULL DEFAULT 0.0,
			source_type         TEXT NOT NULL DEFAULT 'manual'
			                    CHECK(source_type IN ('extract','track','digest','briefing','manual','chat','inbox','jira','slack','promoted_subitem')),
			source_id           TEXT NOT NULL DEFAULT '',
			ai_level_confidence REAL DEFAULT NULL,
			created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		)`); err != nil {
			return fmt.Errorf("migrate v69: create targets_new: %w", err)
		}

		if _, err := tx.Exec(`INSERT INTO targets_new
			(id, text, intent, level, custom_label, period_start, period_end, parent_id,
			 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
			 tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence,
			 created_at, updated_at)
			SELECT id, text, intent, level, custom_label, period_start, period_end, parent_id,
			 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
			 tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence,
			 created_at, updated_at FROM targets`); err != nil {
			return fmt.Errorf("migrate v69: copy targets: %w", err)
		}

		if _, err := tx.Exec(`DROP TABLE targets`); err != nil {
			return fmt.Errorf("migrate v69: drop targets: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE targets_new RENAME TO targets`); err != nil {
			return fmt.Errorf("migrate v69: rename targets: %w", err)
		}

		for _, idx := range []string{
			`CREATE INDEX IF NOT EXISTS idx_targets_level     ON targets(level)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_parent    ON targets(parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_period    ON targets(period_start, period_end)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_status    ON targets(status)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_priority  ON targets(priority)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_due       ON targets(due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_source    ON targets(source_type, source_id)`,
			`CREATE INDEX IF NOT EXISTS idx_targets_updated   ON targets(updated_at DESC)`,
		} {
			if _, err := tx.Exec(idx); err != nil {
				return fmt.Errorf("migrate v69: index: %w", err)
			}
		}

		if _, err := tx.Exec("PRAGMA user_version = 69"); err != nil {
			return fmt.Errorf("setting schema version v69: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v69: %w", err)
		}
		version = 69
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
