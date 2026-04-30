package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestDB is a test helper that opens an in-memory DB and registers cleanup.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenMemory(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Should be able to query
	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected tables to be created")
}

func TestOpenCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sub", "dir", "watchtower.db")

	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = os.Stat(filepath.Dir(dbPath))
	assert.NoError(t, err, "directory should have been created")
}

func TestPragmas(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Check WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	// In-memory databases may use "memory" journal mode instead of WAL
	assert.Contains(t, []string{"wal", "memory"}, journalMode)

	// Check busy_timeout
	var busyTimeout int
	err = db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout)
	require.NoError(t, err)
	assert.Equal(t, 5000, busyTimeout)

	// Check foreign_keys
	var fk int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	require.NoError(t, err)
	assert.Equal(t, 1, fk)

	// Check synchronous
	var sync int
	err = db.QueryRow("PRAGMA synchronous").Scan(&sync)
	require.NoError(t, err)
	assert.Equal(t, 1, sync) // NORMAL = 1
}

func TestMigrationSetsUserVersion(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	v, err := db.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 73, v)
}

func TestMigrationIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	// Open and close to create the DB
	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Insert some data
	_, err = db1.Exec("INSERT INTO workspace (id, name, domain) VALUES ('T1', 'test', 'test')")
	require.NoError(t, err)
	db1.Close()

	// Open again - migration should be idempotent, data should persist
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	var name string
	err = db2.QueryRow("SELECT name FROM workspace WHERE id = 'T1'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "test", name)
}

func TestAllTablesExist(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	expectedTables := []string{
		"workspace", "users", "channels", "messages",
		"reactions", "files", "sync_state", "watch_list", "user_checkpoints",
		"digests", "decision_reads", "user_analyses", "period_summaries",
		"custom_emojis", "tracks", "decision_importance_corrections",
		"feedback", "prompts", "prompt_history", "user_profile",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		require.NoError(t, err, "table %q should exist", table)
		assert.Equal(t, table, name)
	}
}

func TestFTS5TableExists(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='messages_fts'",
	).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "messages_fts", name)
}

func TestMessageInsertTriggerPopulatesFTS(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert a message
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1234567890.123456', 'U1', 'hello world deployment')",
	)
	require.NoError(t, err)

	// FTS should find it
	var text string
	err = db.QueryRow(
		"SELECT text FROM messages_fts WHERE messages_fts MATCH 'deployment'",
	).Scan(&text)
	require.NoError(t, err)
	assert.Contains(t, text, "deployment")
}

func TestFTSStemming(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'we are deploying the new version')",
	)
	require.NoError(t, err)

	// Porter stemmer: "deployed" should match "deploying"
	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'deployed'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestFTSDeletedMessageNotIndexed(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert a deleted message
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text, is_deleted) VALUES ('C1', '1000000000.000001', 'U1', 'secret text', 1)",
	)
	require.NoError(t, err)

	// FTS should not find it
	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'secret'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFTSEmptyTextNotIndexed(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert a message with empty text
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', '')",
	)
	require.NoError(t, err)

	// FTS should have no rows
	var count int
	err = db.QueryRow("SELECT count(*) FROM messages_fts").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFTSUpdateTrigger(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'original text')",
	)
	require.NoError(t, err)

	// Update the text
	_, err = db.Exec(
		"UPDATE messages SET text = 'updated content' WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	)
	require.NoError(t, err)

	// Old text should not be found
	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'original'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// New text should be found
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'updated'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestFTSDeleteTrigger(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'removable message')",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		"DELETE FROM messages WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	)
	require.NoError(t, err)

	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'removable'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFTSSoftDeleteUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'soft delete me')",
	)
	require.NoError(t, err)

	// Soft-delete by setting is_deleted
	_, err = db.Exec(
		"UPDATE messages SET is_deleted = 1 WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	)
	require.NoError(t, err)

	// FTS should no longer find it
	var count int
	err = db.QueryRow(
		"SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'soft'",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestTSUnixGeneratedColumn(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1234567890.123456', 'U1', 'test')",
	)
	require.NoError(t, err)

	var tsUnix float64
	err = db.QueryRow(
		"SELECT ts_unix FROM messages WHERE channel_id = 'C1' AND ts = '1234567890.123456'",
	).Scan(&tsUnix)
	require.NoError(t, err)
	assert.Equal(t, float64(1234567890), tsUnix)
}

func TestWatchListConstraints(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Valid insert
	_, err = db.Exec(
		"INSERT INTO watch_list (entity_type, entity_id, entity_name, priority) VALUES ('channel', 'C1', 'general', 'high')",
	)
	require.NoError(t, err)

	// Invalid entity_type should fail
	_, err = db.Exec(
		"INSERT INTO watch_list (entity_type, entity_id, entity_name, priority) VALUES ('invalid', 'X1', 'foo', 'normal')",
	)
	assert.Error(t, err)

	// Invalid priority should fail
	_, err = db.Exec(
		"INSERT INTO watch_list (entity_type, entity_id, entity_name, priority) VALUES ('user', 'U1', 'alice', 'urgent')",
	)
	assert.Error(t, err)
}

func TestUserCheckpointSingleton(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(
		"INSERT INTO user_checkpoints (id, last_checked_at) VALUES (1, '2025-01-01T00:00:00Z')",
	)
	require.NoError(t, err)

	// Trying to insert with id != 1 should fail
	_, err = db.Exec(
		"INSERT INTO user_checkpoints (id, last_checked_at) VALUES (2, '2025-01-01T00:00:00Z')",
	)
	assert.Error(t, err)
}

func TestChannelTypeConstraint(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Valid types
	for _, typ := range []string{"public", "private", "dm", "group_dm"} {
		_, err = db.Exec(
			"INSERT INTO channels (id, name, type) VALUES (?, ?, ?)",
			"C_"+typ, "test-"+typ, typ,
		)
		require.NoError(t, err, "type %q should be valid", typ)
	}

	// Invalid type
	_, err = db.Exec(
		"INSERT INTO channels (id, name, type) VALUES ('C_bad', 'bad', 'invalid')",
	)
	assert.Error(t, err)
}

func TestMessageUpsert(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'v1')",
	)
	require.NoError(t, err)

	// Upsert via INSERT OR REPLACE
	_, err = db.Exec(
		"INSERT OR REPLACE INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'v2')",
	)
	require.NoError(t, err)

	var text string
	err = db.QueryRow(
		"SELECT text FROM messages WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	).Scan(&text)
	require.NoError(t, err)
	assert.Equal(t, "v2", text)
}

func TestCloseDatabase(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)

	err = db.Close()
	require.NoError(t, err)

	// After close, queries should fail
	_, err = db.Exec("SELECT 1")
	assert.Error(t, err)
}

func TestUnicodeMessage(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	text := "Hello 世界! 🚀 デプロイメント"
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', ?)",
		text,
	)
	require.NoError(t, err)

	var got string
	err = db.QueryRow(
		"SELECT text FROM messages WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	).Scan(&got)
	require.NoError(t, err)
	assert.Equal(t, text, got)
}

func TestMigrationV19ActionItemsToTracks(t *testing.T) {
	// Simulate a v18 database, then open it to trigger migration v19.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	// Open fresh DB (creates all tables up to v19), then revert to v18 state.
	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Drop v43 tracks table and create v18-era action_items + action_item_history from scratch.
	_, err = db1.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS tracks")
	require.NoError(t, err)
	_, err = db1.Exec(`CREATE TABLE action_items (
		id                  INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_id          TEXT NOT NULL DEFAULT '',
		assignee_user_id    TEXT NOT NULL DEFAULT '',
		source_message_ts   TEXT NOT NULL DEFAULT '',
		text                TEXT NOT NULL,
		status              TEXT NOT NULL DEFAULT 'inbox',
		priority            TEXT NOT NULL DEFAULT 'medium',
		period_from         REAL NOT NULL DEFAULT 0,
		period_to           REAL NOT NULL DEFAULT 0,
		participants        TEXT NOT NULL DEFAULT '',
		created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`)
	require.NoError(t, err)
	_, err = db1.Exec(`CREATE TABLE action_item_history (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		action_item_id  INTEGER NOT NULL,
		event           TEXT NOT NULL,
		field           TEXT NOT NULL DEFAULT '',
		old_value       TEXT NOT NULL DEFAULT '',
		new_value       TEXT NOT NULL DEFAULT '',
		created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`)
	require.NoError(t, err)
	_, err = db1.Exec("CREATE UNIQUE INDEX idx_action_items_dedup ON action_items(channel_id, assignee_user_id, source_message_ts, text)")
	require.NoError(t, err)
	_, err = db1.Exec("CREATE INDEX idx_action_items_assignee ON action_items(assignee_user_id)")
	require.NoError(t, err)
	_, err = db1.Exec("CREATE INDEX idx_action_items_status ON action_items(status)")
	require.NoError(t, err)
	_, err = db1.Exec("CREATE INDEX idx_action_items_period ON action_items(period_from, period_to)")
	require.NoError(t, err)
	_, err = db1.Exec("CREATE INDEX idx_action_item_history_item ON action_item_history(action_item_id)")
	require.NoError(t, err)

	// Recreate feedback with old CHECK constraint (action_item instead of track, no user_analysis).
	_, err = db1.Exec(`CREATE TABLE feedback_old (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'action_item', 'decision')),
		entity_id   TEXT NOT NULL,
		rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
		comment     TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`)
	require.NoError(t, err)
	_, err = db1.Exec(`INSERT INTO feedback_old SELECT * FROM feedback`)
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE feedback")
	require.NoError(t, err)
	_, err = db1.Exec("ALTER TABLE feedback_old RENAME TO feedback")
	require.NoError(t, err)

	// Insert test data using v18 names.
	_, err = db1.Exec("INSERT INTO workspace (id, name, domain) VALUES ('T1', 'test', 'test.slack.com')")
	require.NoError(t, err)
	_, err = db1.Exec(`INSERT INTO action_items (channel_id, assignee_user_id, text, status, priority, period_from, period_to, participants)
		VALUES ('C1', 'U1', 'review PR', 'inbox', 'high', 1000, 2000, '')`)
	require.NoError(t, err)
	_, err = db1.Exec(`INSERT INTO feedback (entity_type, entity_id, rating) VALUES ('action_item', '1', 1)`)
	require.NoError(t, err)
	_, err = db1.Exec(`INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
		VALUES (1, 'created', '', '', '')`)
	require.NoError(t, err)
	_, err = db1.Exec(`INSERT INTO prompts (id, template, version) VALUES ('actionitems.extract', 'old template', 1)`)
	require.NoError(t, err)

	// Downgrade user_version to 18 so migration v19 runs on next open.
	_, err = db1.Exec("PRAGMA user_version = 18")
	require.NoError(t, err)
	_, err = db1.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	db1.Close()

	// Reopen — migrations v19..v45 should run.
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 73, v)

	// v45 drops old tracks and recreates with hybrid v2 schema.
	// Verify new tracks table exists with v2 columns.
	_, err = db2.Exec(`INSERT INTO tracks (text, priority) VALUES ('test track', 'high')`)
	require.NoError(t, err, "new tracks table should accept v2 inserts")

	// Verify track_history no longer exists (dropped by v43).
	var cnt int
	err = db2.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='track_history'").Scan(&cnt)
	require.NoError(t, err)
	assert.Equal(t, 0, cnt, "track_history should not exist after v43")

	// Verify feedback entity_type transformed: action_item → track.
	var entityType string
	err = db2.QueryRow("SELECT entity_type FROM feedback WHERE entity_id = '1'").Scan(&entityType)
	require.NoError(t, err)
	assert.Equal(t, "track", entityType)
}

func TestNullableFields(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Message with NULL thread_ts
	_, err = db.Exec(
		"INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000000.000001', 'U1', 'test')",
	)
	require.NoError(t, err)

	var threadTS sql.NullString
	err = db.QueryRow(
		"SELECT thread_ts FROM messages WHERE channel_id = 'C1' AND ts = '1000000000.000001'",
	).Scan(&threadTS)
	require.NoError(t, err)
	assert.False(t, threadTS.Valid)

	// Channel with NULL dm_user_id
	_, err = db.Exec(
		"INSERT INTO channels (id, name, type) VALUES ('C2', 'test', 'public')",
	)
	require.NoError(t, err)

	var dmUserID sql.NullString
	err = db.QueryRow("SELECT dm_user_id FROM channels WHERE id = 'C2'").Scan(&dmUserID)
	require.NoError(t, err)
	assert.False(t, dmUserID.Valid)
}

// tableColumns returns a set of column names for a table.
func tableColumns(t *testing.T, database *DB, table string) map[string]bool {
	t.Helper()
	rows, err := database.Query("PRAGMA table_info(" + table + ")")
	require.NoError(t, err)
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk))
		cols[name] = true
	}
	require.NoError(t, rows.Err())
	return cols
}

// requireCol asserts that a column exists in the column set returned by tableColumns.
func requireCol(t *testing.T, cols map[string]bool, col string) {
	t.Helper()
	if !cols[col] {
		t.Errorf("expected column %q to exist", col)
	}
}

// tableExists checks whether a table exists in the database.
func tableExists(t *testing.T, database *DB, table string) bool {
	t.Helper()
	var cnt int
	err := database.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&cnt)
	require.NoError(t, err)
	return cnt > 0
}

func TestMigration_v67_InboxPulse(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cols := tableColumns(t, database, "inbox_items")
	requireCol(t, cols, "item_class")
	requireCol(t, cols, "pinned")
	requireCol(t, cols, "archived_at")
	requireCol(t, cols, "archive_reason")

	if !tableExists(t, database, "inbox_learned_rules") {
		t.Error("inbox_learned_rules missing")
	}
	if !tableExists(t, database, "inbox_feedback") {
		t.Error("inbox_feedback missing")
	}

	var ver int
	if err := database.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver < 67 {
		t.Errorf("want user_version >=67, got %d", ver)
	}
}

func TestMigration_v67_ExistingData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Open a DB (runs all migrations to latest).
	database, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Insert rows using the new v67 columns to confirm they exist and constraints work.
	_, err = database.Exec(`INSERT INTO inbox_items
		(channel_id, message_ts, sender_user_id, trigger_type, item_class, created_at, updated_at)
		VALUES ('C1','1000.000001','U1','reaction','ambient',
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)
	require.NoError(t, err, "reaction+ambient should insert cleanly")

	_, err = database.Exec(`INSERT INTO inbox_items
		(channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C1','1000.000002','U1','mention',
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)
	require.NoError(t, err, "mention should insert with default item_class")
	database.Close()

	// Reopen — verify version, data survival, and item_class values.
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	var ver int
	require.NoError(t, db2.QueryRow("PRAGMA user_version").Scan(&ver))
	assert.GreaterOrEqual(t, ver, 67, "user_version should be >=67")

	// Both rows should survive.
	var rowCount int
	require.NoError(t, db2.QueryRow("SELECT count(*) FROM inbox_items").Scan(&rowCount))
	assert.Equal(t, 2, rowCount, "both rows should survive")

	// item_class check: explicitly set 'ambient' is preserved; default is 'actionable'.
	var classReaction, classMention string
	require.NoError(t, db2.QueryRow(
		"SELECT item_class FROM inbox_items WHERE trigger_type='reaction'").Scan(&classReaction))
	assert.Equal(t, "ambient", classReaction, "explicitly set item_class should be preserved")

	require.NoError(t, db2.QueryRow(
		"SELECT item_class FROM inbox_items WHERE trigger_type='mention'").Scan(&classMention))
	assert.Equal(t, "actionable", classMention, "default item_class should be actionable")

	// New trigger_types should be insertable after migration.
	_, err = db2.Exec(`INSERT INTO inbox_items
		(channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C2','2000.000001','U2','jira_assigned',
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)
	assert.NoError(t, err, "jira_assigned trigger_type should be insertable post-v67")

	_, err = db2.Exec(`INSERT INTO inbox_items
		(channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C2','2000.000002','U2','calendar_invite',
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)
	assert.NoError(t, err, "calendar_invite trigger_type should be insertable post-v67")

	_, err = db2.Exec(`INSERT INTO inbox_items
		(channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C2','2000.000003','U2','decision_made',
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%SZ','now'))`)
	assert.NoError(t, err, "decision_made trigger_type should be insertable post-v67")
}

// TestMigration_v67_Backfill verifies the migration backfill logic by directly
// creating a pre-v67 inbox_items table (without item_class) and running the v67 migration SQL.
func TestMigration_v67_Backfill(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "backfill.db")

	// Create a minimal pre-v67 DB by bypassing Open() — raw SQLite with just inbox_items.
	rawDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	rawDB.SetMaxOpenConns(1)
	defer rawDB.Close()

	_, err = rawDB.Exec(`CREATE TABLE inbox_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_id TEXT NOT NULL,
		message_ts TEXT NOT NULL,
		thread_ts TEXT NOT NULL DEFAULT '',
		sender_user_id TEXT NOT NULL,
		trigger_type TEXT NOT NULL CHECK(trigger_type IN ('mention','dm','thread_reply','reaction')),
		snippet TEXT NOT NULL DEFAULT '',
		context TEXT NOT NULL DEFAULT '',
		raw_text TEXT NOT NULL DEFAULT '',
		permalink TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		priority TEXT NOT NULL DEFAULT 'medium',
		ai_reason TEXT NOT NULL DEFAULT '',
		resolved_reason TEXT NOT NULL DEFAULT '',
		snooze_until TEXT NOT NULL DEFAULT '',
		waiting_user_ids TEXT NOT NULL DEFAULT '',
		task_id INTEGER,
		read_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(channel_id, message_ts)
	)`)
	require.NoError(t, err)

	// Seed pre-migration rows.
	_, err = rawDB.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C1','1.1','U1','reaction','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	require.NoError(t, err)
	_, err = rawDB.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, created_at, updated_at)
		VALUES ('C1','1.2','U1','mention','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	require.NoError(t, err)
	rawDB.Close()

	// Now open via our DB.Open() — should run v67 migration (starting from version 0 minus the schema bootstrap path, which won't apply since tables exist).
	// We set user_version=66 manually so the v67 migration block triggers.
	rawDB2, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	rawDB2.SetMaxOpenConns(1)
	_, err = rawDB2.Exec("PRAGMA user_version = 66")
	require.NoError(t, err)
	rawDB2.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	var ver int
	require.NoError(t, db2.QueryRow("PRAGMA user_version").Scan(&ver))
	assert.Equal(t, 73, ver)

	// Backfill: reaction → ambient, mention → actionable.
	var classReaction, classMention string
	require.NoError(t, db2.QueryRow(
		"SELECT item_class FROM inbox_items WHERE trigger_type='reaction'").Scan(&classReaction))
	assert.Equal(t, "ambient", classReaction, "migration should backfill reaction to ambient")

	require.NoError(t, db2.QueryRow(
		"SELECT item_class FROM inbox_items WHERE trigger_type='mention'").Scan(&classMention))
	assert.Equal(t, "actionable", classMention, "migration should backfill mention to actionable")
}

func TestMigrationV70CreatesMeetingRecaps(t *testing.T) {
	tmp := t.TempDir() + "/test.db"
	database, err := Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("reading user_version: %v", err)
	}
	if version < 70 {
		t.Errorf("expected user_version >= 70, got %d", version)
	}

	// Table exists
	var name string
	err = database.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='meeting_recaps'`).Scan(&name)
	if err != nil {
		t.Fatalf("meeting_recaps table missing: %v", err)
	}
	if name != "meeting_recaps" {
		t.Errorf("expected table 'meeting_recaps', got %q", name)
	}
}
