package cmd

import (
	"testing"
	"time"

	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatchupCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "catchup" {
			found = true
			break
		}
	}
	assert.True(t, found, "catchup command should be registered")
}

func TestCatchupCommandRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := catchupCmd.RunE(catchupCmd, nil)
	assert.Error(t, err)
}

func TestCatchupCommandFlags(t *testing.T) {
	f := catchupCmd.Flags()

	assert.NotNil(t, f.Lookup("since"))
	assert.NotNil(t, f.Lookup("watched-only"))
	assert.NotNil(t, f.Lookup("channel"))
}

func TestDetermineSinceTimeExplicitDuration(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	before := time.Now().Add(-2 * time.Hour)
	result, err := database.DetermineSinceTime(2*time.Hour)
	require.NoError(t, err)

	// Result should be approximately 2 hours ago
	assert.WithinDuration(t, before, result, 5*time.Second)
}

func TestDetermineSinceTimeFromCheckpoint(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	checkpoint := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	require.NoError(t, database.UpdateCheckpoint(checkpoint))

	result, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.Equal(t, checkpoint, result)
}

func TestDetermineSinceTimeDefault24h(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	before := time.Now().Add(-24 * time.Hour)
	result, err := database.DetermineSinceTime(0)
	require.NoError(t, err)

	// Should be approximately 24 hours ago
	assert.WithinDuration(t, before, result, 5*time.Second)
}

func TestDetermineSinceTimeExplicitOverridesCheckpoint(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// Set a checkpoint from a week ago
	checkpoint := time.Now().Add(-7 * 24 * time.Hour)
	require.NoError(t, database.UpdateCheckpoint(checkpoint))

	// But request only last 1 hour
	before := time.Now().Add(-1 * time.Hour)
	result, err := database.DetermineSinceTime(1*time.Hour)
	require.NoError(t, err)

	// Explicit duration should win
	assert.WithinDuration(t, before, result, 5*time.Second)
}
