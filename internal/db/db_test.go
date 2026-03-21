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
	assert.Equal(t, 29, v)
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
		"custom_emojis", "tracks", "track_history", "decision_importance_corrections",
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

	// Rename tables back to v18 names and restore v18 indexes.
	_, err = db1.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	_, err = db1.Exec("ALTER TABLE tracks RENAME TO action_items")
	require.NoError(t, err)
	_, err = db1.Exec("ALTER TABLE track_history RENAME TO action_item_history")
	require.NoError(t, err)
	_, err = db1.Exec("ALTER TABLE action_item_history RENAME COLUMN track_id TO action_item_id")
	require.NoError(t, err)
	// Drop v19 indexes so migration can recreate them.
	for _, idx := range []string{
		"idx_tracks_dedup", "idx_tracks_assignee", "idx_tracks_status",
		"idx_tracks_period", "idx_track_history_track",
		"idx_feedback_entity", "idx_feedback_rating",
	} {
		_, err = db1.Exec("DROP INDEX IF EXISTS " + idx)
		require.NoError(t, err)
	}
	// Create v18-era indexes that migration expects to drop.
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

	// Reopen — migration v19 should run.
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 29, v)

	// Verify table rename: action_items → tracks.
	var text string
	err = db2.QueryRow("SELECT text FROM tracks WHERE id = 1").Scan(&text)
	require.NoError(t, err)
	assert.Equal(t, "review PR", text)

	// Verify column rename: action_item_id → track_id.
	var event string
	err = db2.QueryRow("SELECT event FROM track_history WHERE track_id = 1").Scan(&event)
	require.NoError(t, err)
	assert.Equal(t, "created", event)

	// Verify feedback entity_type transformed: action_item → track.
	var entityType string
	err = db2.QueryRow("SELECT entity_type FROM feedback WHERE entity_id = '1'").Scan(&entityType)
	require.NoError(t, err)
	assert.Equal(t, "track", entityType)

	// Verify user_analysis entity type is now allowed by updated CHECK constraint.
	_, err = db2.Exec(`INSERT INTO feedback (entity_type, entity_id, rating) VALUES ('user_analysis', '1', 1)`)
	require.NoError(t, err, "user_analysis entity_type should be allowed by CHECK constraint")

	// Verify prompt ID renamed.
	var promptID string
	err = db2.QueryRow("SELECT id FROM prompts WHERE id = 'tracks.extract'").Scan(&promptID)
	require.NoError(t, err)
	assert.Equal(t, "tracks.extract", promptID)

	// Verify customized prompt template was reset (arity mismatch protection).
	var tmpl string
	err = db2.QueryRow("SELECT template FROM prompts WHERE id = 'tracks.extract'").Scan(&tmpl)
	require.NoError(t, err)
	assert.Equal(t, "", tmpl, "customized prompt should be reset to force fallback to built-in default")

	// Verify JSON column defaults fixed: '' → '[]'.
	var participants string
	err = db2.QueryRow("SELECT participants FROM tracks WHERE id = 1").Scan(&participants)
	require.NoError(t, err)
	assert.Equal(t, "[]", participants, "empty JSON columns should be migrated to '[]'")
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
