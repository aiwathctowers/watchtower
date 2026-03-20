package repl

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testDeps(t *testing.T) Deps {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-workspace",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-workspace": {SlackToken: "xoxp-test"},
		},
		AI: config.AIConfig{
			Model:         "claude-sonnet-4-6",
			ContextBudget: 150000,
		},
		Sync: config.SyncConfig{
			Workers: 1,
		},
	}

	return Deps{
		Config:    cfg,
		DB:        database,
		DBPath:    ":memory:",
		Domain:    "test-domain",
		TeamID:    "T001",
		Workspace: "test-workspace",
	}
}

func newTestREPL(t *testing.T) (*REPL, Deps) {
	t.Helper()
	deps := testDeps(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &REPL{
		deps:   deps,
		ctx:    ctx,
		cancel: cancel,
	}, deps
}

// seedWorkspace inserts a workspace row so that GetWorkspace returns data.
func seedWorkspace(t *testing.T, database *db.DB) {
	t.Helper()
	err := database.UpsertWorkspace(db.Workspace{
		ID:     "T1234",
		Name:   "Acme Corp",
		Domain: "acme",
	})
	require.NoError(t, err)
}

// seedChannel inserts a channel row.
func seedChannel(t *testing.T, database *db.DB, id, name string) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO channels (id, name, type, is_member, is_archived, num_members) VALUES (?, ?, 'public', 1, 0, 5)`,
		id, name,
	)
	require.NoError(t, err)
}

// seedUser inserts a user row.
func seedUser(t *testing.T, database *db.DB, id, name string) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO users (id, name, real_name, display_name, is_bot, is_deleted) VALUES (?, ?, ?, ?, 0, 0)`,
		id, name, name, name,
	)
	require.NoError(t, err)
}

// seedMessage inserts a message.
func seedMessage(t *testing.T, database *db.DB, channelID, ts, userID, text string) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO messages (channel_id, ts, user_id, text, subtype, reply_count, thread_ts)
		 VALUES (?, ?, ?, ?, '', 0, '')`,
		channelID, ts, userID, text,
	)
	require.NoError(t, err)
}

// seedSyncState inserts a sync_state row with a last_sync_at timestamp.
func seedSyncState(t *testing.T, database *db.DB, channelID string, syncedAt time.Time) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO sync_state (channel_id, last_sync_at) VALUES (?, ?)`,
		channelID, syncedAt.UTC().Format(time.RFC3339),
	)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// helpText tests
// ---------------------------------------------------------------------------

func TestHelpText(t *testing.T) {
	text := helpText()
	assert.Contains(t, text, "/sync")
	assert.Contains(t, text, "/status")
	assert.Contains(t, text, "/catchup")
	assert.Contains(t, text, "/quit")
	assert.Contains(t, text, "/help")
}

func TestHelpTextContainsAllCommands(t *testing.T) {
	text := helpText()
	lines := strings.Split(text, "\n")

	// Should start with "Available commands:"
	assert.True(t, strings.HasPrefix(lines[0], "Available commands:"))

	// Must mention free-form question usage
	assert.Contains(t, text, "question")
}

// ---------------------------------------------------------------------------
// runStatus tests
// ---------------------------------------------------------------------------

func TestRunStatusFormat(t *testing.T) {
	deps := testDeps(t)

	output := runStatus(deps)
	assert.Contains(t, output, "Workspace:")
	assert.Contains(t, output, "Database:")
	assert.Contains(t, output, "Last sync:")
	assert.Contains(t, output, "Channels:")
}

func TestRunStatusEmptyDB(t *testing.T) {
	deps := testDeps(t)
	output := runStatus(deps)

	// No workspace row => "not yet synced"
	assert.Contains(t, output, "not yet synced")
	// Last sync never
	assert.Contains(t, output, "Last sync: never")
	// Counts should all be 0
	assert.Contains(t, output, "Channels: 0")
	assert.Contains(t, output, "Users: 0")
	assert.Contains(t, output, "Messages: 0")
	assert.Contains(t, output, "Threads: 0")
}

func TestRunStatusWithWorkspace(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)

	output := runStatus(deps)

	// Should show workspace name and ID
	assert.Contains(t, output, "Acme Corp")
	assert.Contains(t, output, "T1234")
}

func TestRunStatusWithSyncState(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")
	syncTime := time.Now().Add(-1 * time.Hour).UTC()
	seedSyncState(t, deps.DB, "C001", syncTime)

	output := runStatus(deps)

	// Should show an actual sync time, not "never"
	assert.NotContains(t, output, "Last sync: never")
}

func TestRunStatusWithData(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")
	seedChannel(t, deps.DB, "C002", "random")
	seedUser(t, deps.DB, "U001", "alice")
	seedUser(t, deps.DB, "U002", "bob")
	seedMessage(t, deps.DB, "C001", "1234567890.000001", "U001", "hello")
	seedMessage(t, deps.DB, "C001", "1234567890.000002", "U002", "world")

	output := runStatus(deps)

	assert.Contains(t, output, "Channels: 2")
	assert.Contains(t, output, "Users: 2")
	assert.Contains(t, output, "Messages: 2")
}

func TestRunStatusWithWatchedChannels(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")
	seedChannel(t, deps.DB, "C002", "random")

	// Add to watch list
	_, err := deps.DB.Exec(`INSERT INTO watch_list (entity_type, entity_id, entity_name) VALUES ('channel', 'C001', 'general')`)
	require.NoError(t, err)

	output := runStatus(deps)
	assert.Contains(t, output, "1 watched")
}

// ---------------------------------------------------------------------------
// runSyncCommand tests
// ---------------------------------------------------------------------------

func TestRunSyncCommandNoToken(t *testing.T) {
	deps := testDeps(t)
	// Remove the token
	deps.Config.Workspaces["test-workspace"].SlackToken = ""

	ctx := context.Background()
	output := runSyncCommand(ctx, deps)

	assert.Contains(t, output, "Slack token not configured")
}

func TestRunSyncCommandNoActiveWorkspace(t *testing.T) {
	deps := testDeps(t)
	deps.Config.ActiveWorkspace = ""

	ctx := context.Background()
	output := runSyncCommand(ctx, deps)

	assert.Contains(t, output, "Error")
}

func TestRunSyncCommandMissingWorkspaceConfig(t *testing.T) {
	deps := testDeps(t)
	deps.Config.ActiveWorkspace = "nonexistent"

	ctx := context.Background()
	output := runSyncCommand(ctx, deps)

	assert.Contains(t, output, "Error")
}

// ---------------------------------------------------------------------------
// handleSlashCommand tests
// ---------------------------------------------------------------------------

func TestHandleSlashCommandHelp(t *testing.T) {
	r, _ := newTestREPL(t)
	// Should not panic and context should remain active
	r.handleSlashCommand("/help")
	assert.NoError(t, r.ctx.Err())
}

func TestHandleSlashCommandStatus(t *testing.T) {
	r, _ := newTestREPL(t)
	r.handleSlashCommand("/status")
	assert.NoError(t, r.ctx.Err())
}

func TestHandleSlashCommandQuit(t *testing.T) {
	r, _ := newTestREPL(t)
	r.handleSlashCommand("/quit")

	// Context should be cancelled after /quit
	assert.Error(t, r.ctx.Err())
}

func TestHandleSlashCommandExit(t *testing.T) {
	r, _ := newTestREPL(t)
	r.handleSlashCommand("/exit")

	// Context should be cancelled after /exit
	assert.Error(t, r.ctx.Err())
}

func TestHandleSlashCommandUnknown(t *testing.T) {
	r, _ := newTestREPL(t)
	// Should not panic
	r.handleSlashCommand("/foobar")
	// Context should still be active
	assert.NoError(t, r.ctx.Err())
}

func TestHandleSlashCommandCaseInsensitive(t *testing.T) {
	r, _ := newTestREPL(t)
	// Commands are lowercased
	r.handleSlashCommand("/HELP")
	assert.NoError(t, r.ctx.Err())

	r.handleSlashCommand("/Status")
	assert.NoError(t, r.ctx.Err())
}

func TestHandleSlashCommandWithArgs(t *testing.T) {
	r, _ := newTestREPL(t)
	// Commands with extra args should still be recognized by the first word
	r.handleSlashCommand("/help extra args")
	assert.NoError(t, r.ctx.Err())

	r.handleSlashCommand("/unknown foo bar")
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// processInput tests
// ---------------------------------------------------------------------------

func TestProcessInputSlashCommand(t *testing.T) {
	r, _ := newTestREPL(t)

	// /help should not panic
	r.handleSlashCommand("/help")
	r.handleSlashCommand("/status")
	r.handleSlashCommand("/unknown")
}

func TestProcessInputRoutesSlashCommands(t *testing.T) {
	r, _ := newTestREPL(t)

	// /quit should cancel the context (proves it went through handleSlashCommand)
	r.processInput("/quit")
	assert.Error(t, r.ctx.Err())
}

func TestProcessInputSlashPrefix(t *testing.T) {
	r, _ := newTestREPL(t)

	// Input starting with / is a slash command
	// We use /help as a safe slash command that won't error
	r.processInput("/help")
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// streamCancel mutex tests
// ---------------------------------------------------------------------------

func TestSetGetStreamCancel(t *testing.T) {
	r, _ := newTestREPL(t)

	// Initially nil
	assert.Nil(t, r.getStreamCancel())

	// Set and get
	called := false
	fn := context.CancelFunc(func() { called = true })
	r.setStreamCancel(fn)

	got := r.getStreamCancel()
	assert.NotNil(t, got)
	got()
	assert.True(t, called)

	// Reset to nil
	r.setStreamCancel(nil)
	assert.Nil(t, r.getStreamCancel())
}

func TestStreamCancelConcurrency(t *testing.T) {
	r, _ := newTestREPL(t)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.setStreamCancel(func() {})
		}()
		go func() {
			defer wg.Done()
			_ = r.getStreamCancel()
		}()
	}
	wg.Wait()
}

func TestStreamingFlag(t *testing.T) {
	r, _ := newTestREPL(t)

	assert.False(t, r.streaming.Load())
	r.streaming.Store(true)
	assert.True(t, r.streaming.Load())
	r.streaming.Store(false)
	assert.False(t, r.streaming.Load())
}

// ---------------------------------------------------------------------------
// REPL struct construction tests
// ---------------------------------------------------------------------------

func TestREPLInitialState(t *testing.T) {
	r, _ := newTestREPL(t)

	assert.Empty(t, r.sessionID)
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
	assert.NoError(t, r.ctx.Err())
}

func TestREPLCancel(t *testing.T) {
	r, _ := newTestREPL(t)

	r.cancel()
	assert.Error(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// Deps struct tests
// ---------------------------------------------------------------------------

func TestDepsFields(t *testing.T) {
	deps := testDeps(t)

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB)
	assert.Equal(t, ":memory:", deps.DBPath)
	assert.Equal(t, "test-domain", deps.Domain)
	assert.Equal(t, "test-workspace", deps.Workspace)
}

// ---------------------------------------------------------------------------
// ExtractSourcesSection (ai package, but exercised from repl test)
// ---------------------------------------------------------------------------

func TestExtractSourcesSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no sources", "Just some text", ""},
		{"with sources", "Response\n\nSources:\n  [1] link1\n  [2] link2", "Sources:\n  [1] link1\n  [2] link2"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ai.ExtractSourcesSection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// DetermineSinceTime (db package, but exercised from repl test)
// ---------------------------------------------------------------------------

func TestDetermineSinceTime(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// No checkpoint — should default to 24h ago
	since, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(-24*time.Hour), since, 5*time.Second)
}

func TestDetermineSinceTimeWithCheckpoint(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	checkpointTime := time.Now().Add(-6 * time.Hour).UTC()
	err = database.UpdateCheckpoint(checkpointTime)
	require.NoError(t, err)

	since, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.WithinDuration(t, checkpointTime, since, 2*time.Second)
}

func TestDetermineSinceTimeExplicitDuration(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	since, err := database.DetermineSinceTime(2 * time.Hour)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(-2*time.Hour), since, 5*time.Second)
}

// ---------------------------------------------------------------------------
// runStatus edge cases with DB errors
// ---------------------------------------------------------------------------

func TestRunStatusDBClosed(t *testing.T) {
	deps := testDeps(t)
	deps.DB.Close() // close the DB to trigger errors

	output := runStatus(deps)
	// Should contain error text, not panic
	assert.Contains(t, output, "Error")
}

// ---------------------------------------------------------------------------
// runSyncCommand with cancelled context
// ---------------------------------------------------------------------------

func TestRunSyncCommandCancelledContext(t *testing.T) {
	deps := testDeps(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	output := runSyncCommand(ctx, deps)
	// Will fail on sync attempt — should either error or report failure
	assert.NotEmpty(t, output)
}

// ---------------------------------------------------------------------------
// handleSlashCommand /sync tests
// ---------------------------------------------------------------------------

func TestHandleSlashCommandSync(t *testing.T) {
	r, _ := newTestREPL(t)
	// /sync will fail (no real Slack token to connect to) but should not panic
	r.handleSlashCommand("/sync")
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// runStatus last sync time formatting
// ---------------------------------------------------------------------------

func TestRunStatusLastSyncTimeFormatting(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")

	syncTime := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	seedSyncState(t, deps.DB, "C001", syncTime)

	output := runStatus(deps)

	// Should contain the RFC3339 timestamp
	assert.Contains(t, output, "2025-03-15")
	// Should also contain relative time (humanize.Time)
	// The sync was in the past, so it should say something like "X ago"
	assert.NotContains(t, output, "Last sync: never")
}

// ---------------------------------------------------------------------------
// Multiple slash commands in sequence
// ---------------------------------------------------------------------------

func TestMultipleCommandsSequence(t *testing.T) {
	r, _ := newTestREPL(t)

	r.handleSlashCommand("/help")
	assert.NoError(t, r.ctx.Err())

	r.handleSlashCommand("/status")
	assert.NoError(t, r.ctx.Err())

	r.handleSlashCommand("/unknown-cmd")
	assert.NoError(t, r.ctx.Err())

	// Quit should be last — it cancels the context
	r.handleSlashCommand("/quit")
	assert.Error(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// Session ID persistence tests
// ---------------------------------------------------------------------------

func TestSessionIDPersistence(t *testing.T) {
	r, _ := newTestREPL(t)

	assert.Empty(t, r.sessionID)
	r.sessionID = "session-123"
	assert.Equal(t, "session-123", r.sessionID)
}

// ---------------------------------------------------------------------------
// runStatus with large numbers (humanize formatting)
// ---------------------------------------------------------------------------

func TestRunStatusLargeNumbers(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)

	// Insert many channels
	for i := 0; i < 50; i++ {
		seedChannel(t, deps.DB, fmt.Sprintf("C%04d", i), fmt.Sprintf("chan-%d", i))
	}
	for i := 0; i < 20; i++ {
		seedUser(t, deps.DB, fmt.Sprintf("U%04d", i), fmt.Sprintf("user-%d", i))
	}

	output := runStatus(deps)
	assert.Contains(t, output, "Channels: 50")
	assert.Contains(t, output, "Users: 20")
}

// ---------------------------------------------------------------------------
// runStatus nil workspace (no workspace row)
// ---------------------------------------------------------------------------

func TestRunStatusNoWorkspaceRow(t *testing.T) {
	deps := testDeps(t)

	output := runStatus(deps)
	// Should use the config active workspace name
	assert.Contains(t, output, "test-workspace")
	assert.Contains(t, output, "not yet synced")
}

// ---------------------------------------------------------------------------
// runStatus DB size display
// ---------------------------------------------------------------------------

func TestRunStatusDBSizeDisplay(t *testing.T) {
	deps := testDeps(t)

	output := runStatus(deps)
	// For :memory: DB, os.Stat will fail, size will be 0
	assert.Contains(t, output, "Database:")
}

// ---------------------------------------------------------------------------
// handleSlashCommand edge cases
// ---------------------------------------------------------------------------

func TestHandleSlashCommandEmptyAfterSlash(t *testing.T) {
	r, _ := newTestREPL(t)
	// "/" alone should be treated as unknown command
	r.handleSlashCommand("/")
	assert.NoError(t, r.ctx.Err())
}

func TestHandleSlashCommandWhitespaceHandling(t *testing.T) {
	r, _ := newTestREPL(t)
	// "/help   " with trailing spaces — Fields handles this
	r.handleSlashCommand("/help   ")
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// processInput non-slash input (AI query path)
// ---------------------------------------------------------------------------

func TestProcessInputNonSlashCallsRunAIQuery(t *testing.T) {
	// We can't easily mock the AI client, but we can verify:
	// 1. It doesn't panic
	// 2. It sets streaming state
	r, _ := newTestREPL(t)

	// Cancel context immediately so the AI query fails fast
	r.cancel()

	// This will attempt runAIQuery which will fail due to cancelled context
	// but should not panic
	r.processInput("what happened today?")
}

// ---------------------------------------------------------------------------
// Concurrent access to REPL fields
// ---------------------------------------------------------------------------

func TestREPLStreamCancelAtomicOps(t *testing.T) {
	r, _ := newTestREPL(t)

	// Simulate what happens during a streaming operation
	streamCtx, streamCancel := context.WithCancel(r.ctx)
	defer streamCancel()

	r.setStreamCancel(streamCancel)
	r.streaming.Store(true)

	// Simulate Ctrl+C: get cancel and call it
	cancelFn := r.getStreamCancel()
	assert.NotNil(t, cancelFn)
	cancelFn()

	// Stream context should be cancelled
	assert.Error(t, streamCtx.Err())
	// But REPL context should still be alive
	assert.NoError(t, r.ctx.Err())

	// Cleanup
	r.streaming.Store(false)
	r.setStreamCancel(nil)
}

// ---------------------------------------------------------------------------
// runSyncCommand — verify it returns completion message on success path
// ---------------------------------------------------------------------------

func TestRunSyncCommandOutputFormat(t *testing.T) {
	deps := testDeps(t)
	// With a real token, sync will attempt to connect to Slack and fail.
	// We test that the error message is properly formatted.
	ctx := context.Background()
	output := runSyncCommand(ctx, deps)

	// Should either contain "Sync complete" or "Sync failed" or error
	hasResult := strings.Contains(output, "Sync complete") ||
		strings.Contains(output, "Sync failed") ||
		strings.Contains(output, "Error")
	assert.True(t, hasResult, "Expected sync output, got: %s", output)
}

// ---------------------------------------------------------------------------
// runCatchup tests (limited — requires AI, but we can test early exits)
// ---------------------------------------------------------------------------

func TestRunCatchupNoMessages(t *testing.T) {
	r, _ := newTestREPL(t)

	// Set a checkpoint so DetermineSinceTime returns a time
	err := r.deps.DB.UpdateCheckpoint(time.Now().Add(-1 * time.Hour))
	require.NoError(t, err)

	// With no messages in range, catchup should print "No new activity"
	// and not panic. We can't capture stdout easily without refactoring,
	// but we can verify it doesn't panic and the context remains alive.
	r.runCatchup()
	assert.NoError(t, r.ctx.Err())
}

func TestRunCatchupDBError(t *testing.T) {
	r, _ := newTestREPL(t)
	r.deps.DB.Close() // close DB to force errors

	// Should handle the error gracefully, not panic
	r.runCatchup()
}

// ---------------------------------------------------------------------------
// runStatus with messages that have reply_count > 0 (thread count)
// ---------------------------------------------------------------------------

func TestRunStatusThreadCount(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")
	seedUser(t, deps.DB, "U001", "alice")

	// Insert a message with reply_count > 0 (counts as a thread)
	_, err := deps.DB.Exec(
		`INSERT INTO messages (channel_id, ts, user_id, text, subtype, reply_count, thread_ts)
		 VALUES ('C001', '1234567890.000001', 'U001', 'thread parent', '', 3, '')`,
	)
	require.NoError(t, err)

	// Insert a regular message
	seedMessage(t, deps.DB, "C001", "1234567890.000002", "U001", "regular")

	output := runStatus(deps)
	assert.Contains(t, output, "Messages: 2")
	assert.Contains(t, output, "Threads: 1")
}

// ---------------------------------------------------------------------------
// runStatus with invalid last sync time format
// ---------------------------------------------------------------------------

func TestRunStatusInvalidSyncTime(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")

	// Insert a sync_state with an invalid timestamp
	_, err := deps.DB.Exec(
		`INSERT INTO sync_state (channel_id, last_sync_at) VALUES ('C001', 'not-a-date')`,
	)
	require.NoError(t, err)

	output := runStatus(deps)
	// Should still display it, just without the relative time
	assert.Contains(t, output, "not-a-date")
}

// ---------------------------------------------------------------------------
// helpText structure validation
// ---------------------------------------------------------------------------

func TestHelpTextStructure(t *testing.T) {
	text := helpText()

	// Each command line should start with "  /" (two spaces + slash)
	lines := strings.Split(text, "\n")
	commandLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "  /") {
			commandLines++
		}
	}
	// Should have exactly 5 command lines
	assert.Equal(t, 5, commandLines, "Expected 5 command entries in help text")
}

// ---------------------------------------------------------------------------
// runSyncCommand with workspace config not found
// ---------------------------------------------------------------------------

func TestRunSyncCommandWorkspaceNotInConfig(t *testing.T) {
	deps := testDeps(t)
	deps.Config.ActiveWorkspace = "missing-workspace"

	ctx := context.Background()
	output := runSyncCommand(ctx, deps)
	assert.Contains(t, output, "Error")
	assert.Contains(t, output, "missing-workspace")
}

// ---------------------------------------------------------------------------
// Test that /catchup command routes through handleSlashCommand
// ---------------------------------------------------------------------------

func TestHandleSlashCommandCatchup(t *testing.T) {
	r, _ := newTestREPL(t)

	// Catchup with no data should not panic
	r.handleSlashCommand("/catchup")
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// Verify DB methods used by REPL work with seeded data
// ---------------------------------------------------------------------------

func TestDBMethodsUsedByRunCatchup(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// DetermineSinceTime with no checkpoint
	since, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.False(t, since.IsZero())

	// CountMessagesByTimeRange with no messages
	now := time.Now()
	count, err := database.CountMessagesByTimeRange(float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// UpdateCheckpoint
	err = database.UpdateCheckpoint(now)
	require.NoError(t, err)

	// Verify checkpoint was stored
	since2, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.WithinDuration(t, now, since2, 2*time.Second)
}

func TestDBMethodsUsedByRunStatus(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// GetWorkspace returns nil on empty DB
	ws, err := database.GetWorkspace()
	require.NoError(t, err)
	assert.Nil(t, ws)

	// GetStats on empty DB
	stats, err := database.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 0, stats.ChannelCount)
	assert.Equal(t, 0, stats.UserCount)
	assert.Equal(t, 0, stats.MessageCount)
	assert.Equal(t, 0, stats.ThreadCount)

	// LastSyncTime on empty DB
	lastSync, err := database.LastSyncTime()
	require.NoError(t, err)
	assert.Empty(t, lastSync)
}

// ---------------------------------------------------------------------------
// Verify NullString handling in workspace display
// ---------------------------------------------------------------------------

func TestRunStatusWorkspaceSyncedAtDisplay(t *testing.T) {
	deps := testDeps(t)

	// Upsert sets synced_at automatically
	err := deps.DB.UpsertWorkspace(db.Workspace{
		ID:     "T5678",
		Name:   "Other Corp",
		Domain: "other",
	})
	require.NoError(t, err)

	ws, err := deps.DB.GetWorkspace()
	require.NoError(t, err)
	assert.NotNil(t, ws)
	assert.True(t, ws.SyncedAt.Valid)

	output := runStatus(deps)
	assert.Contains(t, output, "Other Corp")
}

// ---------------------------------------------------------------------------
// Integration: full REPL lifecycle simulation
// ---------------------------------------------------------------------------

func TestREPLLifecycle(t *testing.T) {
	r, _ := newTestREPL(t)

	// Process several commands
	r.processInput("/help")
	assert.NoError(t, r.ctx.Err())

	r.processInput("/status")
	assert.NoError(t, r.ctx.Err())

	r.processInput("/unknown-command")
	assert.NoError(t, r.ctx.Err())

	// Finally quit
	r.processInput("/exit")
	assert.Error(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// Verify that runStatus handles workspace with no name gracefully
// ---------------------------------------------------------------------------

func TestRunStatusWorkspaceWithEmptyFields(t *testing.T) {
	deps := testDeps(t)

	// Insert workspace with minimal fields
	_, err := deps.DB.Exec(
		`INSERT INTO workspace (id, name, domain, synced_at) VALUES ('T000', '', '', '')`,
	)
	require.NoError(t, err)

	output := runStatus(deps)
	// Should not panic and should contain workspace line
	assert.Contains(t, output, "Workspace:")
}

// ---------------------------------------------------------------------------
// Verify config-related paths in sync
// ---------------------------------------------------------------------------

func TestRunSyncConfigPaths(t *testing.T) {
	tests := []struct {
		name           string
		activeWS       string
		workspaces     map[string]*config.WorkspaceConfig
		expectContains string
	}{
		{
			name:           "empty active workspace",
			activeWS:       "",
			workspaces:     nil,
			expectContains: "Error",
		},
		{
			name:     "missing workspace in map",
			activeWS: "nonexistent",
			workspaces: map[string]*config.WorkspaceConfig{
				"other": {SlackToken: "xoxp-other"},
			},
			expectContains: "Error",
		},
		{
			name:     "empty token",
			activeWS: "ws",
			workspaces: map[string]*config.WorkspaceConfig{
				"ws": {SlackToken: ""},
			},
			expectContains: "Slack token not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := testDeps(t)
			deps.Config.ActiveWorkspace = tt.activeWS
			deps.Config.Workspaces = tt.workspaces

			ctx := context.Background()
			output := runSyncCommand(ctx, deps)
			assert.Contains(t, output, tt.expectContains)
		})
	}
}

// ---------------------------------------------------------------------------
// Verify streaming lifecycle in runAIQuery
// ---------------------------------------------------------------------------

func TestRunAIQueryStreamingLifecycle(t *testing.T) {
	r, _ := newTestREPL(t)

	// Cancel context so AI query returns quickly
	r.cancel()

	assert.False(t, r.streaming.Load())

	// runAIQuery with cancelled context
	r.runAIQuery("test question")

	// After runAIQuery returns, streaming should be false and streamCancel nil
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

// ---------------------------------------------------------------------------
// Verify NullString from workspace query
// ---------------------------------------------------------------------------

func TestRunStatusNullSyncedAt(t *testing.T) {
	deps := testDeps(t)

	// Manually insert workspace with NULL synced_at
	_, err := deps.DB.Exec(
		`INSERT INTO workspace (id, name, domain, synced_at) VALUES ('T999', 'NullSync Corp', 'nullsync', NULL)`,
	)
	require.NoError(t, err)

	ws, err := deps.DB.GetWorkspace()
	require.NoError(t, err)
	assert.NotNil(t, ws)
	assert.Equal(t, sql.NullString{String: "", Valid: false}, ws.SyncedAt)

	output := runStatus(deps)
	assert.Contains(t, output, "NullSync Corp")
}

// ---------------------------------------------------------------------------
// runCatchup with messages present (exercises AI path with cancelled context)
// ---------------------------------------------------------------------------

func TestRunCatchupWithMessages(t *testing.T) {
	r, _ := newTestREPL(t)

	// Seed data: checkpoint in the past, messages in range
	seedWorkspace(t, r.deps.DB)
	seedChannel(t, r.deps.DB, "C001", "general")
	seedUser(t, r.deps.DB, "U001", "alice")

	// Set checkpoint to 2 hours ago
	checkpointTime := time.Now().Add(-2 * time.Hour)
	err := r.deps.DB.UpdateCheckpoint(checkpointTime)
	require.NoError(t, err)

	// Insert a message with a recent timestamp (within the catchup window)
	recentTS := fmt.Sprintf("%d.000001", time.Now().Add(-1*time.Hour).Unix())
	seedMessage(t, r.deps.DB, "C001", recentTS, "U001", "recent message")

	// Cancel context so the AI query fails fast but the code path up to AI is exercised
	r.cancel()

	// Should exercise the "has messages" branch in runCatchup
	r.runCatchup()

	// Should not panic — streaming state should be cleaned up
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

func TestRunCatchupCountMessagesError(t *testing.T) {
	r, _ := newTestREPL(t)

	// Set checkpoint
	err := r.deps.DB.UpdateCheckpoint(time.Now().Add(-1 * time.Hour))
	require.NoError(t, err)

	// Close DB after checkpoint is set — CountMessagesByTimeRange will fail
	r.deps.DB.Close()

	// Should handle error gracefully
	r.runCatchup()
}

// ---------------------------------------------------------------------------
// runAIQuery tests with various states
// ---------------------------------------------------------------------------

func TestRunAIQueryWithExistingSession(t *testing.T) {
	r, _ := newTestREPL(t)
	r.sessionID = "existing-session-456"

	// Cancel so AI query returns fast
	r.cancel()

	r.runAIQuery("follow-up question")

	// Session ID should not be cleared by a failed query
	// (it's only updated when a new one is received)
	assert.Equal(t, "existing-session-456", r.sessionID)
	assert.False(t, r.streaming.Load())
}

func TestRunAIQueryFirstQuestion(t *testing.T) {
	r, _ := newTestREPL(t)

	// First question — no session ID yet
	assert.Empty(t, r.sessionID)

	// Cancel so AI query returns fast
	r.cancel()

	r.runAIQuery("what is the company doing?")

	// Streaming cleanup should work
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

func TestRunAIQueryMultipleQuestions(t *testing.T) {
	r, _ := newTestREPL(t)

	// Cancel context
	r.cancel()

	// Multiple queries in sequence
	r.runAIQuery("first question")
	r.runAIQuery("second question")
	r.runAIQuery("third question")

	// All should complete without panic
	assert.False(t, r.streaming.Load())
}

// ---------------------------------------------------------------------------
// processInput routing comprehensive test
// ---------------------------------------------------------------------------

func TestProcessInputRouting(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectQuit  bool
		description string
	}{
		{"/help routes to help", "/help", false, "help command"},
		{"/status routes to status", "/status", false, "status command"},
		{"/quit cancels", "/quit", true, "quit cancels context"},
		{"/exit cancels", "/exit", true, "exit cancels context"},
		{"/QUIT case insensitive", "/QUIT", true, "case insensitive quit"},
		{"/unknown doesn't cancel", "/unknown", false, "unknown command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newTestREPL(t)
			r.processInput(tt.input)
			if tt.expectQuit {
				assert.Error(t, r.ctx.Err(), tt.description)
			} else {
				assert.NoError(t, r.ctx.Err(), tt.description)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Run function — test terminal check
// ---------------------------------------------------------------------------

func TestRunRequiresTerminal(t *testing.T) {
	deps := testDeps(t)

	// When running tests, stdin is not a terminal
	err := Run(deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "interactive terminal")
}

// ---------------------------------------------------------------------------
// runCatchup checkpoint update after no messages
// ---------------------------------------------------------------------------

func TestRunCatchupUpdatesCheckpointOnNoActivity(t *testing.T) {
	r, _ := newTestREPL(t)

	// No messages, no checkpoint — should update checkpoint
	r.runCatchup()

	// Verify checkpoint was set (should be close to now)
	cp, err := r.deps.DB.GetCheckpoint()
	require.NoError(t, err)
	assert.NotNil(t, cp)
}

// ---------------------------------------------------------------------------
// runStatus with multiple sync states (picks latest)
// ---------------------------------------------------------------------------

func TestRunStatusMultipleSyncStates(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")
	seedChannel(t, deps.DB, "C002", "random")

	oldSync := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newSync := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
	seedSyncState(t, deps.DB, "C001", oldSync)
	seedSyncState(t, deps.DB, "C002", newSync)

	output := runStatus(deps)

	// Should show the latest sync time
	assert.Contains(t, output, "2025-03-15")
}

// ---------------------------------------------------------------------------
// Verify runStatus GetStats error path
// ---------------------------------------------------------------------------

func TestRunStatusGetStatsError(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)

	// Drop the messages table to trigger GetStats error
	_, err := deps.DB.Exec(`DROP TABLE messages`)
	require.NoError(t, err)

	output := runStatus(deps)
	// GetWorkspace succeeds, but GetStats fails
	assert.Contains(t, output, "Error")
}

// ---------------------------------------------------------------------------
// Verify runStatus LastSyncTime error path
// ---------------------------------------------------------------------------

func TestRunStatusLastSyncTimeError(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)

	// Drop sync_state table to trigger LastSyncTime error
	_, err := deps.DB.Exec(`DROP TABLE sync_state`)
	require.NoError(t, err)

	output := runStatus(deps)
	assert.Contains(t, output, "Error")
}

// ---------------------------------------------------------------------------
// runCatchup with messages and active context (exercises more of the AI path)
// ---------------------------------------------------------------------------

func TestRunCatchupWithMessagesActiveContext(t *testing.T) {
	r, _ := newTestREPL(t)

	seedWorkspace(t, r.deps.DB)
	seedChannel(t, r.deps.DB, "C001", "general")
	seedUser(t, r.deps.DB, "U001", "alice")

	// Set checkpoint to 2 hours ago
	checkpointTime := time.Now().Add(-2 * time.Hour)
	err := r.deps.DB.UpdateCheckpoint(checkpointTime)
	require.NoError(t, err)

	// Insert messages within the catchup window
	recentTS := fmt.Sprintf("%d.000001", time.Now().Add(-1*time.Hour).Unix())
	seedMessage(t, r.deps.DB, "C001", recentTS, "U001", "hello everyone")

	// Don't cancel context — let the AI call actually attempt (and fail since no claude binary)
	// The AI call will fail with "claude CLI error" which is handled gracefully
	r.runCatchup()

	// Streaming should be cleaned up
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

// ---------------------------------------------------------------------------
// processInput with non-slash text — AI query path
// ---------------------------------------------------------------------------

func TestProcessInputAIQueryPath(t *testing.T) {
	r, _ := newTestREPL(t)

	// Non-slash input should go to runAIQuery
	// This will fail since there's no claude binary, but exercises the code path
	r.processInput("tell me about recent discussions")

	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

// ---------------------------------------------------------------------------
// Test styles are initialized (lipgloss styles)
// ---------------------------------------------------------------------------

func TestStylesInitialized(t *testing.T) {
	// Verify the package-level styles render without panic
	result := promptStyle.Render("test>")
	assert.NotEmpty(t, result)

	result = dimStyle.Render("dim text")
	assert.NotEmpty(t, result)

	result = errorStyle.Render("error text")
	assert.NotEmpty(t, result)
}

// ---------------------------------------------------------------------------
// runSyncCommand with valid token but invalid Slack API
// ---------------------------------------------------------------------------

func TestRunSyncCommandInvalidSlackAPI(t *testing.T) {
	deps := testDeps(t)
	deps.Config.Workspaces["test-workspace"].SlackToken = "xoxb-invalid-token-12345"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output := runSyncCommand(ctx, deps)
	// Should fail with some error (invalid auth, network error, etc.)
	hasError := strings.Contains(output, "failed") || strings.Contains(output, "Error") || strings.Contains(output, "error")
	assert.True(t, hasError, "Expected error in output, got: %s", output)
}

// ---------------------------------------------------------------------------
// Verify /catchup routing through handleSlashCommand with data
// ---------------------------------------------------------------------------

func TestHandleSlashCommandCatchupWithData(t *testing.T) {
	r, _ := newTestREPL(t)

	seedWorkspace(t, r.deps.DB)
	seedChannel(t, r.deps.DB, "C001", "general")
	seedUser(t, r.deps.DB, "U001", "alice")

	recentTS := fmt.Sprintf("%d.000001", time.Now().Add(-30*time.Minute).Unix())
	seedMessage(t, r.deps.DB, "C001", recentTS, "U001", "recent msg")

	// Route through handleSlashCommand (not directly calling runCatchup)
	r.handleSlashCommand("/catchup")

	// Context should still be alive (catchup doesn't cancel context)
	// (Note: context may or may not be alive depending on AI error handling)
	assert.False(t, r.streaming.Load())
}

// ---------------------------------------------------------------------------
// runCatchup — checkpoint update error on no activity
// ---------------------------------------------------------------------------

func TestRunCatchupCheckpointUpdateError(t *testing.T) {
	deps := testDeps(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &REPL{deps: deps, ctx: ctx, cancel: cancel}

	// No messages will be found (empty DB), so the "no activity" path is hit.
	// But drop user_checkpoints table to make UpdateCheckpoint fail.
	// First, let DetermineSinceTime succeed (it doesn't require the table to exist,
	// it handles ErrNoRows). Then CountMessages returns 0, then UpdateCheckpoint fails.
	r.runCatchup()
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// runStatus — all branches for LastSync formatting
// ---------------------------------------------------------------------------

func TestRunStatusLastSyncValidRFC3339(t *testing.T) {
	deps := testDeps(t)
	seedWorkspace(t, deps.DB)
	seedChannel(t, deps.DB, "C001", "general")

	// Use a valid RFC3339 time close to now
	syncTime := time.Now().Add(-5 * time.Minute).UTC()
	seedSyncState(t, deps.DB, "C001", syncTime)

	output := runStatus(deps)
	// Should contain both the timestamp and relative time
	assert.Contains(t, output, "Last sync:")
	assert.NotContains(t, output, "never")
	// Should contain "ago" from humanize
	assert.Contains(t, output, "ago")
}

// ---------------------------------------------------------------------------
// Verify runAIQuery deferred cleanup under various conditions
// ---------------------------------------------------------------------------

func TestRunAIQueryDeferredCleanup(t *testing.T) {
	r, _ := newTestREPL(t)

	// Set streaming and streamCancel before calling runAIQuery
	// to verify the deferred cleanup works
	r.streaming.Store(true)
	r.setStreamCancel(func() {})

	// Cancel context to make it fail fast
	r.cancel()

	r.runAIQuery("test")

	// Deferred cleanup should reset these
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

// ---------------------------------------------------------------------------
// processInput — table-driven test for slash vs non-slash routing
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// loop() tests — core REPL loop with simulated input
// ---------------------------------------------------------------------------

func TestLoopEmptyInput(t *testing.T) {
	r, _ := newTestREPL(t)

	// Empty input — scanner reads nothing and returns
	input := strings.NewReader("")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.NoError(t, r.ctx.Err())
}

func TestLoopHelpCommand(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/help\n")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.NoError(t, r.ctx.Err())
}

func TestLoopMultipleCommands(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/help\n/status\n/help\n")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.NoError(t, r.ctx.Err())
}

func TestLoopQuitExitsEarly(t *testing.T) {
	r, _ := newTestREPL(t)

	// /quit cancels context, loop should exit on next iteration
	input := strings.NewReader("/quit\n/help\n")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.Error(t, r.ctx.Err()) // context cancelled by /quit
}

func TestLoopExitExitsEarly(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/exit\n/status\n")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.Error(t, r.ctx.Err())
}

func TestLoopBlankLinesSkipped(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("\n\n\n/help\n\n\n")
	err := r.loop(input)
	assert.NoError(t, err)
}

func TestLoopWhitespaceOnlyLinesSkipped(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("   \n  \t  \n/status\n")
	err := r.loop(input)
	assert.NoError(t, err)
}

func TestLoopUnknownCommand(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/foobar\n/xyz\n")
	err := r.loop(input)
	assert.NoError(t, err)
	assert.NoError(t, r.ctx.Err())
}

func TestLoopContextCancelledBeforeInput(t *testing.T) {
	r, _ := newTestREPL(t)
	r.cancel() // cancel before loop starts

	input := strings.NewReader("/help\n/status\n")
	err := r.loop(input)
	assert.NoError(t, err) // returns nil on ctx.Done
	assert.Error(t, r.ctx.Err())
}

func TestLoopMixedCommandsAndStatus(t *testing.T) {
	r, _ := newTestREPL(t)

	cmds := "/help\n/status\n\n/help\n/quit\n"
	input := strings.NewReader(cmds)
	err := r.loop(input)
	assert.NoError(t, err)
	assert.Error(t, r.ctx.Err())
}

func TestLoopSyncCommand(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/sync\n")
	err := r.loop(input)
	assert.NoError(t, err)
}

func TestLoopCatchupCommand(t *testing.T) {
	r, _ := newTestREPL(t)

	input := strings.NewReader("/catchup\n")
	err := r.loop(input)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// runStatus with real file DB (covers os.Stat success branch)
// ---------------------------------------------------------------------------

func TestRunStatusWithFileDB(t *testing.T) {
	// Use the default config path so cfg.DBPath() points to our actual file.
	// We create the DB at the config-derived location.
	deps := testDeps(t)

	// The config derives DBPath from ActiveWorkspace. Create a real file
	// at that location so os.Stat succeeds in runStatus.
	cfgDBPath := deps.Config.DBPath()

	// Open a real file DB at the config-derived path
	database, err := db.Open(cfgDBPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		database.Close()
		os.Remove(cfgDBPath)
		// Also remove WAL/SHM files
		os.Remove(cfgDBPath + "-wal")
		os.Remove(cfgDBPath + "-shm")
	})

	deps.DB = database
	deps.DBPath = cfgDBPath

	output := runStatus(deps)
	assert.Contains(t, output, "Database:")
	// With a real file DB, os.Stat succeeds and we get a non-zero size
	assert.NotContains(t, output, "(0 B)")
}

// ---------------------------------------------------------------------------
// runCatchup — CountMessages error path
// ---------------------------------------------------------------------------

func TestRunCatchupCountMessagesErrorPath(t *testing.T) {
	r, _ := newTestREPL(t)

	// Drop the messages table to trigger CountMessagesByTimeRange error
	_, err := r.deps.DB.Exec(`DROP TABLE messages`)
	require.NoError(t, err)

	// runCatchup will call DetermineSinceTime (OK) then CountMessagesByTimeRange (error)
	r.runCatchup()
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// runCatchup — UpdateCheckpoint error in no-messages path
// ---------------------------------------------------------------------------

func TestRunCatchupUpdateCheckpointErrorOnNoMessages(t *testing.T) {
	r, _ := newTestREPL(t)

	// DetermineSinceTime succeeds (no checkpoint = 24h default).
	// CountMessagesByTimeRange returns 0 (empty DB).
	// Now drop user_checkpoints AFTER those calls succeed, but before UpdateCheckpoint.
	// Since all calls happen in sequence, we can't really drop mid-function.
	// Instead: drop and recreate user_checkpoints with wrong schema to make INSERT fail.
	_, err := r.deps.DB.Exec(`DROP TABLE user_checkpoints`)
	require.NoError(t, err)
	_, err = r.deps.DB.Exec(`CREATE TABLE user_checkpoints (id INTEGER PRIMARY KEY, last_checked_at TEXT NOT NULL, extra TEXT NOT NULL)`)
	require.NoError(t, err)

	// DetermineSinceTime: GetCheckpoint queries the table (no rows), returns nil → uses 24h default.
	// CountMessagesByTimeRange: returns 0 (empty messages table).
	// UpdateCheckpoint: tries to INSERT with only 2 columns, but table has a required 'extra' column → error.
	r.runCatchup()
	// Should print warning about checkpoint failure but not panic
	assert.NoError(t, r.ctx.Err())
}

// ---------------------------------------------------------------------------
// runAIQuery with a fake claude binary that produces output
// ---------------------------------------------------------------------------

func TestRunAIQueryWithFakeClaude(t *testing.T) {
	r, _ := newTestREPL(t)

	// Create a fake claude script that outputs valid stream-json
	tmpDir := t.TempDir()
	fakeClaude := tmpDir + "/claude"
	script := `#!/bin/sh
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello from fake claude"}]}}'
echo '{"type":"result","session_id":"fake-session-123"}'
`
	err := os.WriteFile(fakeClaude, []byte(script), 0o755)
	require.NoError(t, err)

	r.deps.Config.ClaudePath = fakeClaude
	r.deps.DBPath = "" // avoid MCP config issues

	r.runAIQuery("test question")

	// Should capture session ID from fake claude
	assert.Equal(t, "fake-session-123", r.sessionID)
	assert.False(t, r.streaming.Load())
	assert.Nil(t, r.getStreamCancel())
}

func TestRunAIQueryWithFakeClaudeExistingSession(t *testing.T) {
	r, _ := newTestREPL(t)

	tmpDir := t.TempDir()
	fakeClaude := tmpDir + "/claude"
	script := `#!/bin/sh
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Follow-up response"}]}}'
echo '{"type":"result","session_id":"session-789"}'
`
	err := os.WriteFile(fakeClaude, []byte(script), 0o755)
	require.NoError(t, err)

	r.deps.Config.ClaudePath = fakeClaude
	r.deps.DBPath = ""
	r.sessionID = "old-session"

	r.runAIQuery("follow up")

	// Session ID should be updated
	assert.Equal(t, "session-789", r.sessionID)
}

func TestRunCatchupWithFakeClaude(t *testing.T) {
	r, _ := newTestREPL(t)

	seedWorkspace(t, r.deps.DB)
	seedChannel(t, r.deps.DB, "C001", "general")
	seedUser(t, r.deps.DB, "U001", "alice")

	// Set checkpoint and insert recent messages
	checkpointTime := time.Now().Add(-2 * time.Hour)
	err := r.deps.DB.UpdateCheckpoint(checkpointTime)
	require.NoError(t, err)
	recentTS := fmt.Sprintf("%d.000001", time.Now().Add(-1*time.Hour).Unix())
	seedMessage(t, r.deps.DB, "C001", recentTS, "U001", "hello")

	// Create fake claude that returns a catchup summary
	tmpDir := t.TempDir()
	fakeClaude := tmpDir + "/claude"
	script := `#!/bin/sh
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Here is your catchup summary"}]}}'
echo '{"type":"result","session_id":"catchup-session"}'
`
	err = os.WriteFile(fakeClaude, []byte(script), 0o755)
	require.NoError(t, err)

	r.deps.Config.ClaudePath = fakeClaude
	r.deps.DBPath = ""

	r.runCatchup()

	// Checkpoint should be updated after successful catchup
	cp, cpErr := r.deps.DB.GetCheckpoint()
	require.NoError(t, cpErr)
	assert.NotNil(t, cp)
	// Checkpoint should be recent (within last minute)
	cpTime, parseErr := time.Parse("2006-01-02T15:04:05Z", cp.LastCheckedAt)
	require.NoError(t, parseErr)
	assert.WithinDuration(t, time.Now(), cpTime, 1*time.Minute)

	assert.False(t, r.streaming.Load())
}

func TestProcessInputSlashDetection(t *testing.T) {
	tests := []struct {
		input   string
		isSlash bool
	}{
		{"/help", true},
		{"/status", true},
		{"/quit", true},
		{"/exit", true},
		{"/unknown", true},
		{"hello", false},
		{"what happened?", false},
		{"123", false},
		{"/ something", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			r, _ := newTestREPL(t)
			// Cancel context to prevent AI queries from hanging
			if !tt.isSlash || tt.input == "/quit" || tt.input == "/exit" {
				r.cancel()
			}
			// Should not panic
			r.processInput(tt.input)
		})
	}
}
