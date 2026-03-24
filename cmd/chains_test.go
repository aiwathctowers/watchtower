package cmd

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestChainsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "chains" {
			found = true
			break
		}
	}
	assert.True(t, found, "chains command should be registered")
}

func TestChainsSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"generate": false, "archive": false}
	for _, cmd := range chainsCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "chains %s subcommand should be registered", name)
	}
}

func TestRunChains_Empty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err := chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No chains found")
}

func TestRunChains_WithChains(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.CreateChain(db.Chain{
		Title:      "API Migration",
		Slug:       "api-migration",
		Status:     "active",
		Summary:    "Migrating from REST v1 to v2",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400*7,
		LastSeen:   now,
		ItemCount:  5,
	})
	require.NoError(t, err)

	_, err = database.CreateChain(db.Chain{
		Title:      "Q4 Planning",
		Slug:       "q4-planning",
		Status:     "resolved",
		Summary:    "Q4 goals finalized",
		ChannelIDs: `["C001","C002"]`,
		FirstSeen:  now - 86400*30,
		LastSeen:   now - 86400*10,
		ItemCount:  8,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err = chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "API Migration")
	assert.Contains(t, output, "Q4 Planning")
	assert.Contains(t, output, "#general")
}

func TestRunChains_StatusFilter(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.CreateChain(db.Chain{
		Title:      "Active Chain",
		Slug:       "active-chain",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  2,
	})
	require.NoError(t, err)

	_, err = database.CreateChain(db.Chain{
		Title:      "Resolved Chain",
		Slug:       "resolved-chain",
		Status:     "resolved",
		ChannelIDs: `[]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  3,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = "active"
	defer func() { chainsFlagStatus = "" }()

	err = chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Active Chain")
	assert.NotContains(t, output, "Resolved Chain")
}

func TestRunChains_DetailByID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Detailed Chain",
		Slug:       "detailed-chain",
		Status:     "active",
		Summary:    "This is a detailed chain",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  1,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err = chainsCmd.RunE(chainsCmd, []string{fmt.Sprintf("%d", chainID)})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Detailed Chain")
	assert.Contains(t, output, "active")
	assert.Contains(t, output, "detailed-chain")
}

func TestRunChains_InvalidID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	chainsFlagStatus = ""
	err := chainsCmd.RunE(chainsCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid chain ID")
}

func TestRunChains_NonexistentID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	chainsFlagStatus = ""
	err := chainsCmd.RunE(chainsCmd, []string{"99999"})
	assert.Error(t, err)
}

func TestRunChainsArchive(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Archive Me",
		Slug:       "archive-me",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  1,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	chainsArchiveCmd.SetOut(buf)

	err = chainsArchiveCmd.RunE(chainsArchiveCmd, []string{fmt.Sprintf("%d", chainID)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "archived")
}

func TestRunChainsArchive_InvalidID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := chainsArchiveCmd.RunE(chainsArchiveCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid chain ID")
}

func TestRunChains_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	chainsFlagStatus = ""
	err := chainsCmd.RunE(chainsCmd, nil)
	assert.Error(t, err)
}

func TestChainsFlags(t *testing.T) {
	assert.NotNil(t, chainsCmd.Flags().Lookup("status"))
}
