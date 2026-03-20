package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "db" {
			found = true
			break
		}
	}
	assert.True(t, found, "db command should be registered")
}

func TestDbMigrateSubcommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range dbCmd.Commands() {
		if cmd.Name() == "migrate" {
			found = true
			break
		}
	}
	assert.True(t, found, "db migrate subcommand should be registered")
}

func TestDbMigrate(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := dbMigrateCmd.RunE(dbMigrateCmd, nil)
	require.NoError(t, err)
}

func TestDbMigrate_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := dbMigrateCmd.RunE(dbMigrateCmd, nil)
	assert.Error(t, err)
}
