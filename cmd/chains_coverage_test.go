package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestShowChainDetail_DecisionRef(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	// Insert a digest with decisions
	digestID, err := database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 86400,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Architecture discussion",
		Topics:       `[]`,
		Decisions:    `[{"text":"Use microservices","by":"alice","importance":"high"}]`,
		ActionItems:  `[]`,
		MessageCount: 15,
		Model:        "haiku",
	})
	require.NoError(t, err)

	chainID, err := database.CreateChain(db.Chain{
		Title:      "Architecture Chain",
		Slug:       "architecture-chain",
		Status:     "active",
		Summary:    "Architecture decisions over time",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Insert decision ref
	err = database.InsertChainRef(db.ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    int(digestID),
		DecisionIdx: 0,
		ChannelID:   "C001",
		Timestamp:   now,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showChainDetail(database, buf, int(chainID))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Architecture Chain")
	assert.Contains(t, output, "Timeline")
	assert.Contains(t, output, "decision")
	assert.Contains(t, output, "Use microservices")
	assert.Contains(t, output, "alice")

	database.Close()
}

func TestShowChainDetail_NoRefs(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Empty Chain",
		Slug:       "empty-chain",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  0,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showChainDetail(database, buf, int(chainID))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Empty Chain")
	assert.NotContains(t, output, "Timeline")

	database.Close()
}

func TestShowChainDetail_WithSummary(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Summarized Chain",
		Slug:       "summarized-chain",
		Status:     "resolved",
		Summary:    "This chain tracks deployment automation",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400*7,
		LastSeen:   now,
		ItemCount:  3,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showChainDetail(database, buf, int(chainID))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Summarized Chain")
	assert.Contains(t, output, "resolved")
	assert.Contains(t, output, "deployment automation")

	database.Close()
}

func TestRunChains_WithSummary(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.CreateChain(db.Chain{
		Title:      "Chain With Summary",
		Slug:       "chain-with-summary",
		Status:     "active",
		Summary:    "Tracking API versioning decisions",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  2,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err = chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Chain With Summary")
	assert.Contains(t, output, "Tracking API versioning")
}

func TestRunChainsGenerate_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := chainsGenerateCmd.RunE(chainsGenerateCmd, nil)
	assert.Error(t, err)
}

func TestRunChainsArchive_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := chainsArchiveCmd.RunE(chainsArchiveCmd, []string{"1"})
	assert.Error(t, err)
}

func TestRunChainsArchive_NonexistentID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	chainsArchiveCmd.SetOut(buf)

	// UpdateChainStatus may not error for nonexistent IDs (no rows affected)
	// Just verify it doesn't panic
	err := chainsArchiveCmd.RunE(chainsArchiveCmd, []string{"99999"})
	if err != nil {
		assert.Contains(t, err.Error(), "archiving")
	} else {
		assert.Contains(t, buf.String(), "archived")
	}
}
