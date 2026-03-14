package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestStatusCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "status" {
			found = true
			break
		}
	}
	assert.True(t, found, "status command should be registered")
}

func TestStatusCommandRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := statusCmd.RunE(statusCmd, nil)
	assert.Error(t, err)
}

func TestStatusCommandOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a config file pointing to a DB in our temp dir
	dbPath := filepath.Join(tmpDir, "watchtower.db")
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: "xoxp-test-token"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	// Manually open the DB and populate it with some data
	database, err := db.Open(dbPath)
	require.NoError(t, err)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{
		ID:     "T001",
		Name:   "test-ws",
		Domain: "test-ws",
	}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C002", Name: "random", Type: "public"}))
	require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1000000000.000001", UserID: "U001", Text: "hello"}))
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1000000000.000002", UserID: "U002", Text: "thread", ReplyCount: 2}))

	database.Close()

	// Override the config path and DBPath by patching the config file to
	// point to the right location. We need to make DBPath() return our tmpDir
	// path. The simplest approach: override flagConfig and patch the config
	// so DBPath resolves to our temp DB.
	//
	// Since DBPath() uses the home directory convention, we'll just override
	// the entire runStatus function's config loading by pointing to our config
	// and overriding the workspace name to match.
	//
	// Actually, the cleanest test: call runStatus directly, but we need to
	// control the config. We'll need a config that makes DBPath() return our
	// temp path. Since DBPath uses ~/.local/share/watchtower/<workspace>/watchtower.db,
	// we can't easily override that. Instead, let's test the output by running
	// the function with the DB already at the expected path.
	//
	// Simplest approach: create the DB at the path DBPath() would return.
	// Or better: let the function open the already-populated DB.
	// The DB is at dbPath but config.DBPath() returns a different path.
	// So let's create a symlink or just test with a fresh approach.

	// Let's do a simpler integration test where we set HOME to tmpDir
	// so that DBPath() returns tmpDir/.local/share/watchtower/test-ws/watchtower.db
	homeDir := tmpDir
	wsDBDir := filepath.Join(homeDir, ".local", "share", "watchtower", "test-ws")
	require.NoError(t, os.MkdirAll(wsDBDir, 0o755))
	wsDBPath := filepath.Join(wsDBDir, "watchtower.db")

	// Re-create DB at the expected path
	database2, err := db.Open(wsDBPath)
	require.NoError(t, err)
	require.NoError(t, database2.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database2.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database2.UpsertUser(db.User{ID: "U002", Name: "bob"}))
	require.NoError(t, database2.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database2.UpsertChannel(db.Channel{ID: "C002", Name: "random", Type: "public"}))
	require.NoError(t, database2.AddWatch("channel", "C001", "general", "high"))
	require.NoError(t, database2.UpsertMessage(db.Message{ChannelID: "C001", TS: "1000000000.000001", UserID: "U001", Text: "hello"}))
	require.NoError(t, database2.UpsertMessage(db.Message{ChannelID: "C001", TS: "1000000000.000002", UserID: "U002", Text: "thread", ReplyCount: 2}))
	database2.Close()

	// Override HOME so DBPath() resolves to our temp dir
	t.Setenv("HOME", homeDir)

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err = statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Workspace: test-ws (T001)")
	assert.Contains(t, output, "Channels: 2 (1 watched)")
	assert.Contains(t, output, "Users: 2")
	assert.Contains(t, output, "Messages: 2")
	assert.Contains(t, output, "Threads: 1")
	assert.Contains(t, output, "Last sync: never")
}

func TestStatusCommandNoSync(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `active_workspace: fresh-ws
workspaces:
  fresh-ws:
    slack_token: "xoxp-test"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	// Create empty DB
	homeDir := tmpDir
	wsDBDir := filepath.Join(homeDir, ".local", "share", "watchtower", "fresh-ws")
	require.NoError(t, os.MkdirAll(wsDBDir, 0o755))
	wsDBPath := filepath.Join(wsDBDir, "watchtower.db")

	database, err := db.Open(wsDBPath)
	require.NoError(t, err)
	database.Close()

	t.Setenv("HOME", homeDir)

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err = statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "fresh-ws (not yet synced)")
	assert.Contains(t, output, "Last sync: never")
	assert.Contains(t, output, "Channels: 0")
	assert.Contains(t, output, "Users: 0")
	assert.Contains(t, output, "Messages: 0")
}

func TestDbFileSize(t *testing.T) {
	assert.Equal(t, int64(0), dbFileSize("/nonexistent/file"))

	tmpFile := filepath.Join(t.TempDir(), "test.db")
	require.NoError(t, os.WriteFile(tmpFile, []byte("test data"), 0o600))
	assert.Greater(t, dbFileSize(tmpFile), int64(0))
}
