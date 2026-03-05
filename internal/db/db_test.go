package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, 4, v)
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
		"digests",
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
