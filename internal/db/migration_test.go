package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHasColumn verifies the hasColumn helper function.
func TestHasColumn(t *testing.T) {
	db := openTestDB(t)

	// Existing column
	assert.True(t, hasColumn(db.DB, "workspace", "name"))
	assert.True(t, hasColumn(db.DB, "workspace", "id"))

	// Non-existent column
	assert.False(t, hasColumn(db.DB, "workspace", "nonexistent_column"))

	// Non-existent table (PRAGMA table_info returns 0 rows)
	assert.False(t, hasColumn(db.DB, "nonexistent_table", "id"))

	// Invalid table name (SQL injection chars) — should return false
	assert.False(t, hasColumn(db.DB, "workspace; DROP TABLE users", "name"))
	assert.False(t, hasColumn(db.DB, "work space", "name"))
	assert.False(t, hasColumn(db.DB, "table-name", "name"))
}

// TestHasColumn_WithTransaction verifies hasColumn works with a transaction.
func TestHasColumn_WithTransaction(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	assert.True(t, hasColumn(tx, "tracks", "ownership"))
	assert.True(t, hasColumn(tx, "tracks", "ball_on"))
	assert.False(t, hasColumn(tx, "tracks", "nonexistent"))
}

// setUserVersion sets the PRAGMA user_version for a database.
// PRAGMA doesn't support parameterized queries.
func setUserVersion(t *testing.T, db *DB, version int) {
	t.Helper()
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version))
	require.NoError(t, err)
}

// TestMigrationFromV20_CreatesChains tests v21-v23 migrations creating chains, chain_refs,
// and user_interactions tables when they don't exist.
func TestMigrationFromV20_CreatesChains(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Drop tables that would be created by v22 and v23 migrations
	_, err = db1.Exec("DROP TABLE IF EXISTS chain_refs")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS chains")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS user_interactions")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_chains_status")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_chains_last_seen")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_chain_refs_chain")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_chain_refs_digest")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_chain_refs_track")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_user_interactions_a")
	require.NoError(t, err)

	setUserVersion(t, db1, 20)
	db1.Close()

	// Reopen — migrations v21, v22, v23 should run
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Verify chains table created by v22
	id, err := db2.CreateChain(Chain{
		Title: "Test Chain", Slug: "test-chain", Status: "active",
		ChannelIDs: `["C1"]`, FirstSeen: 1000, LastSeen: 2000, ItemCount: 1,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify chain_refs work
	err = db2.InsertChainRef(ChainRef{
		ChainID: int(id), RefType: "decision",
		DigestID: 1, DecisionIdx: 0, ChannelID: "C1", Timestamp: 1500,
	})
	require.NoError(t, err)

	// Verify user_interactions created by v23
	_, err = db2.Exec(`INSERT INTO user_interactions (user_a, user_b, period_from, period_to, shared_channels)
		VALUES ('U1', 'U2', 1000, 2000, 3)`)
	require.NoError(t, err)

	var count int
	err = db2.QueryRow("SELECT shared_channels FROM user_interactions WHERE user_a = 'U1'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// TestMigrationFromV21_ChainsAndInteractions tests v22-v23 migrations.
func TestMigrationFromV21_ChainsAndInteractions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Drop only the tables that v22 and v23 create
	_, err = db1.Exec("DROP TABLE IF EXISTS chain_refs")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS chains")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS user_interactions")
	require.NoError(t, err)

	setUserVersion(t, db1, 21)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Verify all tables exist and are usable
	for _, tbl := range []string{"chains", "chain_refs", "user_interactions"} {
		var n string
		err := db2.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&n)
		require.NoError(t, err, "table %q should exist", tbl)
	}
}

// TestMigrationFromV22_UserInteractions tests v23 migration only.
func TestMigrationFromV22_UserInteractions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	_, err = db1.Exec("DROP TABLE IF EXISTS user_interactions")
	require.NoError(t, err)
	_, err = db1.Exec("DROP INDEX IF EXISTS idx_user_interactions_a")
	require.NoError(t, err)

	setUserVersion(t, db1, 22)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Insert and query to verify table structure
	err = db2.UpsertUserInteractions([]UserInteraction{
		{
			UserA: "U1", UserB: "U2",
			PeriodFrom: 1000, PeriodTo: 2000,
			MessagesTo: 5, MessagesFrom: 3, SharedChannels: 2,
			SharedChannelIDs: `["C1","C2"]`,
		},
	})
	require.NoError(t, err)

	interactions, err := db2.GetUserInteractions("U1", 1000, 2000)
	require.NoError(t, err)
	require.Len(t, interactions, 1)
	assert.Equal(t, "U2", interactions[0].UserB)
	assert.Equal(t, 5, interactions[0].MessagesTo)
}

// TestMigrationIdempotent_V21HasColumn tests that v21 migration is idempotent
// when ownership column already exists (hasColumn returns true, skips ALTER).
func TestMigrationIdempotent_V21HasColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Insert test data in tracks
	_, err = db1.UpsertTrack(Track{
		ChannelID: "C1", AssigneeUserID: "U1", Text: "test track",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
		Ownership: "mine", Participants: "[]", SourceRefs: "[]",
		Tags: "[]", SubItems: "[]", RelatedDigestIDs: "[]", DecisionOptions: "[]",
	})
	require.NoError(t, err)

	// Downgrade to v20 — v21 migration uses hasColumn, will detect ownership exists
	setUserVersion(t, db1, 20)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Data should survive
	tracks, err := db2.GetTracks(TrackFilter{Status: "inbox"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "test track", tracks[0].Text)
	assert.Equal(t, "mine", tracks[0].Ownership)
}

// TestMigrationIdempotent_V22HasColumn tests that v22 migration is idempotent
// when chains table already exists (hasColumn returns true, skips CREATE).
func TestMigrationIdempotent_V22HasColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Insert test data in chains
	chainID, err := db1.CreateChain(Chain{
		Title: "Existing Chain", Slug: "existing", Status: "active",
		ChannelIDs: `["C1"]`, FirstSeen: 1000, LastSeen: 2000,
	})
	require.NoError(t, err)

	// Downgrade to v21 — v22 migration uses hasColumn on chains.id
	setUserVersion(t, db1, 21)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Chain data should survive
	chain, err := db2.GetChainByID(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, "Existing Chain", chain.Title)
}

// TestMigrationIdempotent_V23HasColumn tests that v23 migration is idempotent
// when user_interactions table already exists.
func TestMigrationIdempotent_V23HasColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Insert data in user_interactions
	err = db1.UpsertUserInteractions([]UserInteraction{
		{UserA: "U1", UserB: "U2", PeriodFrom: 1000, PeriodTo: 2000, SharedChannels: 5, SharedChannelIDs: "[]"},
	})
	require.NoError(t, err)

	// Downgrade to v22 — v23 uses hasColumn to detect existing table
	setUserVersion(t, db1, 22)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	v, err := db2.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Data should survive
	interactions, err := db2.GetUserInteractions("U1", 1000, 2000)
	require.NoError(t, err)
	require.Len(t, interactions, 1)
	assert.Equal(t, 5, interactions[0].SharedChannels)
}

// TestUserVersion verifies UserVersion returns the current schema version.
func TestUserVersion(t *testing.T) {
	db := openTestDB(t)

	v, err := db.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)
}

// TestUserVersion_CustomValue verifies UserVersion after manual set.
func TestUserVersion_CustomValue(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec("PRAGMA user_version = 42")
	require.NoError(t, err)

	v, err := db.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 42, v)
}

// TestChannelNameByID tests the ChannelNameByID helper.
func TestChannelNameByID(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	name, err := db.ChannelNameByID("C1")
	require.NoError(t, err)
	assert.Equal(t, "general", name)

	// Non-existent channel
	name, err = db.ChannelNameByID("C_NONEXISTENT")
	assert.Error(t, err)
	assert.Equal(t, "", name)
}

// TestOpenFileBased tests opening a file-based database that persists across opens.
func TestOpenFileBased(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	_, err = db1.Exec("INSERT INTO workspace (id, name, domain) VALUES ('T1', 'test', 'test.slack.com')")
	require.NoError(t, err)
	db1.Close()

	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	var name string
	err = db2.QueryRow("SELECT name FROM workspace WHERE id = 'T1'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "test", name)
}

// TestSetPragmas_FileBased verifies pragmas on file-based databases.
func TestSetPragmas_FileBased(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "pragma_test.db")

	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)

	var fk int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	require.NoError(t, err)
	assert.Equal(t, 1, fk)
}

// TestHasColumnWithQuerier verifies hasColumn works with *sql.DB and *sql.Tx.
func TestHasColumnWithQuerier(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)

	_, err = sqlDB.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)

	assert.True(t, hasColumn(sqlDB, "test_table", "id"))
	assert.True(t, hasColumn(sqlDB, "test_table", "name"))
	assert.False(t, hasColumn(sqlDB, "test_table", "nonexistent"))

	tx, err := sqlDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	assert.True(t, hasColumn(tx, "test_table", "id"))
	assert.False(t, hasColumn(tx, "test_table", "missing"))
}

// TestMigrationFromV1_FullPath creates a genuine v1 database (only the core tables,
// no v2+ columns or tables) and migrates it all the way to v23, exercising
// every migration branch in sequence.
func TestMigrationFromV1_FullPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	// Manually create a v1 database without using Open()
	sqlDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// V1 schema: workspace (without search_last_date and current_user_id),
	// users, channels, messages, reactions, files, sync_state, watch_list, user_checkpoints.
	// Also the FTS table and triggers.
	v1Schema := `
CREATE TABLE workspace (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL,
    domain   TEXT NOT NULL DEFAULT '',
    synced_at TEXT
);
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    real_name     TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    is_bot        INTEGER NOT NULL DEFAULT 0,
    is_deleted    INTEGER NOT NULL DEFAULT 0,
    profile_json  TEXT NOT NULL DEFAULT '{}',
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_users_name ON users(name);
CREATE INDEX idx_users_is_bot ON users(is_bot);
CREATE TABLE channels (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL CHECK(type IN ('public', 'private', 'dm', 'group_dm')),
    topic        TEXT NOT NULL DEFAULT '',
    purpose      TEXT NOT NULL DEFAULT '',
    is_archived  INTEGER NOT NULL DEFAULT 0,
    is_member    INTEGER NOT NULL DEFAULT 0,
    dm_user_id   TEXT,
    num_members  INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_channels_name ON channels(name);
CREATE INDEX idx_channels_type ON channels(type);
CREATE TABLE messages (
    channel_id TEXT NOT NULL,
    ts         TEXT NOT NULL,
    user_id    TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL DEFAULT '',
    thread_ts  TEXT,
    reply_count INTEGER NOT NULL DEFAULT 0,
    is_edited  INTEGER NOT NULL DEFAULT 0,
    is_deleted INTEGER NOT NULL DEFAULT 0,
    subtype    TEXT NOT NULL DEFAULT '',
    permalink  TEXT NOT NULL DEFAULT '',
    ts_unix    REAL GENERATED ALWAYS AS (CASE WHEN INSTR(ts, '.') > 0 THEN CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL) ELSE CAST(ts AS REAL) END) STORED,
    raw_json   TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (channel_id, ts)
);
CREATE INDEX idx_messages_user_id ON messages(user_id);
CREATE INDEX idx_messages_thread ON messages(channel_id, thread_ts);
CREATE INDEX idx_messages_ts_unix ON messages(ts_unix);
CREATE INDEX idx_messages_channel_ts_unix ON messages(channel_id, ts_unix);
CREATE VIRTUAL TABLE messages_fts USING fts5(
    text, channel_id UNINDEXED, ts UNINDEXED, user_id UNINDEXED,
    tokenize='porter unicode61'
);
CREATE TRIGGER messages_ai AFTER INSERT ON messages
WHEN NEW.text != '' AND NEW.is_deleted = 0
BEGIN
    DELETE FROM messages_fts WHERE channel_id = NEW.channel_id AND ts = NEW.ts;
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    VALUES (NEW.text, NEW.channel_id, NEW.ts, NEW.user_id);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages
BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
END;
CREATE TRIGGER messages_au AFTER UPDATE OF text, is_deleted ON messages
WHEN OLD.text != NEW.text OR OLD.is_deleted != NEW.is_deleted
BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    SELECT NEW.text, NEW.channel_id, NEW.ts, NEW.user_id
    WHERE NEW.text != '' AND NEW.is_deleted = 0;
END;
CREATE TABLE reactions (
    channel_id TEXT NOT NULL,
    message_ts TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    emoji      TEXT NOT NULL,
    PRIMARY KEY (channel_id, message_ts, user_id, emoji)
);
CREATE TABLE files (
    id                 TEXT PRIMARY KEY,
    message_channel_id TEXT NOT NULL DEFAULT '',
    message_ts         TEXT NOT NULL DEFAULT '',
    name               TEXT NOT NULL DEFAULT '',
    mimetype           TEXT NOT NULL DEFAULT '',
    size               INTEGER NOT NULL DEFAULT 0,
    permalink          TEXT NOT NULL DEFAULT ''
);
CREATE TABLE sync_state (
    channel_id              TEXT PRIMARY KEY,
    last_synced_ts          TEXT NOT NULL DEFAULT '',
    oldest_synced_ts        TEXT NOT NULL DEFAULT '',
    is_initial_sync_complete INTEGER NOT NULL DEFAULT 0,
    cursor                  TEXT NOT NULL DEFAULT '',
    messages_synced         INTEGER NOT NULL DEFAULT 0,
    last_sync_at            TEXT,
    error                   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE watch_list (
    entity_type TEXT NOT NULL CHECK(entity_type IN ('channel', 'user')),
    entity_id   TEXT NOT NULL,
    entity_name TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('high', 'normal', 'low')),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (entity_type, entity_id)
);
CREATE TABLE user_checkpoints (
    id              INTEGER PRIMARY KEY CHECK(id = 1),
    last_checked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
PRAGMA user_version = 1;
`
	_, err = sqlDB.Exec(v1Schema)
	require.NoError(t, err)

	// Insert test data using v1 schema
	_, err = sqlDB.Exec("INSERT INTO workspace (id, name, domain) VALUES ('T1', 'test-ws', 'test.slack.com')")
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO users (id, name) VALUES ('U1', 'alice')")
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')")
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1500000000.000001', 'U1', 'hello from v1')")
	require.NoError(t, err)
	sqlDB.Close()

	// Open with watchtower's Open() — this triggers all migrations from v2 to v23
	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Verify schema version is now 23
	v, err := db.UserVersion()
	require.NoError(t, err)
	assert.Equal(t, 35, v)

	// Verify data survived all migrations
	var wsName string
	err = db.QueryRow("SELECT name FROM workspace WHERE id = 'T1'").Scan(&wsName)
	require.NoError(t, err)
	assert.Equal(t, "test-ws", wsName)

	var msgText string
	err = db.QueryRow("SELECT text FROM messages WHERE channel_id = 'C1' AND ts = '1500000000.000001'").Scan(&msgText)
	require.NoError(t, err)
	assert.Equal(t, "hello from v1", msgText)

	// V2 migration: workspace should now have search_last_date column
	var searchLastDate string
	err = db.QueryRow("SELECT search_last_date FROM workspace WHERE id = 'T1'").Scan(&searchLastDate)
	require.NoError(t, err)
	assert.Equal(t, "", searchLastDate)

	// V3 migration: digests table should exist
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel", PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "test", Model: "haiku",
	})
	require.NoError(t, err)

	// V5 migration: user_analyses table should exist
	_, err = db.UpsertUserAnalysis(UserAnalysis{
		UserID: "U1", PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "test analysis", Model: "haiku",
	})
	require.NoError(t, err)

	// V6 migration: custom_emojis table should exist
	require.NoError(t, db.UpsertCustomEmoji(CustomEmoji{Name: "test", URL: "https://test.png"}))

	// V10 migration: workspace should have current_user_id, tracks table should exist
	var currentUserID string
	err = db.QueryRow("SELECT current_user_id FROM workspace WHERE id = 'T1'").Scan(&currentUserID)
	require.NoError(t, err)

	// V17 migration: feedback, prompts, prompt_history tables should exist
	_, err = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "1", Rating: 1})
	require.NoError(t, err)
	require.NoError(t, db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "test", Version: 1}))

	// V18 migration: decision_importance_corrections should exist
	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 1, DecisionIdx: 0,
		DecisionText: "test", OriginalImportance: "low", NewImportance: "high",
	})
	require.NoError(t, err)

	// V19 migration: action_items renamed to tracks (v13+ built the table as action_items,
	// v19 renamed it). Since we started from v1, v10 created action_items, v13 rebuilt it,
	// v19 renamed it to tracks.
	trackID, err := db.UpsertTrack(Track{
		ChannelID: "C1", AssigneeUserID: "U1", Text: "test track",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
		Participants: "[]", SourceRefs: "[]", Tags: "[]", SubItems: "[]",
		RelatedDigestIDs: "[]", DecisionOptions: "[]",
	})
	require.NoError(t, err)
	assert.Greater(t, trackID, int64(0))

	// V20 migration: user_profile table should exist
	require.NoError(t, db.UpsertUserProfile(UserProfile{SlackUserID: "U1", Role: "engineer"}))

	// V21 migration: tracks should have ownership column
	track, err := db.GetTrackByID(int(trackID))
	require.NoError(t, err)
	assert.Equal(t, "mine", track.Ownership)

	// V22 migration: chains should exist
	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `["C1"]`, FirstSeen: 1000, LastSeen: 2000,
	})
	require.NoError(t, err)
	assert.Greater(t, chainID, int64(0))

	// V23 migration: user_interactions should exist
	err = db.UpsertUserInteractions([]UserInteraction{
		{UserA: "U1", UserB: "U2", PeriodFrom: 1000, PeriodTo: 2000, SharedChannels: 3, SharedChannelIDs: "[]"},
	})
	require.NoError(t, err)

	// Verify all tables exist
	for _, tbl := range []string{
		"workspace", "users", "channels", "messages", "reactions", "files",
		"sync_state", "watch_list", "user_checkpoints",
		"digests", "decision_reads", "user_analyses", "period_summaries",
		"custom_emojis", "tracks", "track_history",
		"feedback", "prompts", "prompt_history",
		"decision_importance_corrections", "user_profile",
		"chains", "chain_refs", "user_interactions",
	} {
		var n string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&n)
		require.NoError(t, err, "table %q should exist after full migration from v1", tbl)
	}
}

// TestGetAllSyncStates tests the GetAllSyncStates function.
func TestGetAllSyncStates(t *testing.T) {
	db := openTestDB(t)

	// Empty initially
	states, err := db.GetAllSyncStates()
	require.NoError(t, err)
	assert.Empty(t, states)

	// Add some sync states
	require.NoError(t, db.UpdateSyncState("C1", SyncState{
		LastSyncedTS: "1000.001", MessagesSynced: 10,
		IsInitialSyncComplete: true,
	}))
	require.NoError(t, db.UpdateSyncState("C2", SyncState{
		LastSyncedTS: "2000.001", MessagesSynced: 20,
	}))

	states, err = db.GetAllSyncStates()
	require.NoError(t, err)
	assert.Len(t, states, 2)

	s1 := states["C1"]
	require.NotNil(t, s1)
	assert.Equal(t, "1000.001", s1.LastSyncedTS)
	assert.Equal(t, 10, s1.MessagesSynced)
	assert.True(t, s1.IsInitialSyncComplete)

	s2 := states["C2"]
	require.NotNil(t, s2)
	assert.Equal(t, "2000.001", s2.LastSyncedTS)
	assert.Equal(t, 20, s2.MessagesSynced)
}

// TestGetAllSyncStates_WithError tests sync state with error field.
func TestGetAllSyncStates_WithError(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpdateSyncState("C1", SyncState{
		LastSyncedTS: "1000.001",
		Error:        "channel_not_found",
	}))

	states, err := db.GetAllSyncStates()
	require.NoError(t, err)
	require.Len(t, states, 1)

	s := states["C1"]
	assert.Equal(t, "channel_not_found", s.Error)
	// When error is set, last_sync_at should not be updated
	assert.False(t, s.LastSyncAt.Valid)
}

// TestMigrationPreservesDataAcrossVersions tests data integrity when migrating
// from v20 (with existing tracks, digests, etc.) to v23.
func TestMigrationPreservesDataAcrossVersions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watchtower.db")

	db1, err := Open(dbPath)
	require.NoError(t, err)

	// Insert data across multiple tables
	require.NoError(t, db1.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, db1.UpsertUser(User{ID: "U1", Name: "alice"}))
	require.NoError(t, db1.UpsertMessage(Message{ChannelID: "C1", TS: "1000.001", UserID: "U1", Text: "hello", RawJSON: "{}"}))

	_, err = db1.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel", PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "test digest", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db1.UpsertTrack(Track{
		ChannelID: "C1", AssigneeUserID: "U1", Text: "review PR",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
		Participants: "[]", SourceRefs: "[]", Tags: "[]", SubItems: "[]",
		RelatedDigestIDs: "[]", DecisionOptions: "[]",
	})
	require.NoError(t, err)

	// Drop v22/v23 tables and downgrade
	_, err = db1.Exec("DROP TABLE IF EXISTS chain_refs")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS chains")
	require.NoError(t, err)
	_, err = db1.Exec("DROP TABLE IF EXISTS user_interactions")
	require.NoError(t, err)
	setUserVersion(t, db1, 20)
	db1.Close()

	// Reopen — v21-v23 migrations run
	db2, err := Open(dbPath)
	require.NoError(t, err)
	defer db2.Close()

	// Verify existing data
	ch, err := db2.GetChannelByID("C1")
	require.NoError(t, err)
	assert.Equal(t, "general", ch.Name)

	msgs, err := db2.GetMessages(MessageOpts{ChannelID: "C1", Limit: 100})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Text)

	tracks, err := db2.GetTracks(TrackFilter{Status: "inbox"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "review PR", tracks[0].Text)
}
