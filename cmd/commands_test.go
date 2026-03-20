package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- Test all commands are registered on rootCmd ---
func TestAllCommandsRegistered(t *testing.T) {
	expectedCommands := []string{
		"sync", "status", "channels", "users", "watch",
		"digest", "decisions", "trends", "people", "tracks",
		"feedback", "prompts", "tune", "profile", "ask",
		"catchup", "version", "config", "db", "auth", "logs",
		"chains", "actions",
	}

	registeredNames := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		registeredNames[cmd.Name()] = true
	}

	for _, name := range expectedCommands {
		assert.True(t, registeredNames[name], "command %q should be registered on rootCmd", name)
	}
}

// --- Digest edge cases ---
func TestRunDigest_NegativeDays(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = -5 // should be clamped to 1

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No digests available")
}

func TestRunDigest_LargeDays(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 10000 // should be clamped to 3650

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
}

// --- Decisions edge cases ---
func TestRunDecisions_NegativeDays(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = -1 // should be clamped to 7

	err := decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)
}

// --- People edge cases ---
func TestRunPeople_NegativeWeeks(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = -1 // should be clamped to 1

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
}

// --- ShowUserDetail with all fields populated ---
func TestShowUserDetail_FullAnalysis(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       100,
		ChannelsActive:     5,
		ThreadsInitiated:   20,
		ThreadsReplied:     30,
		AvgMessageLength:   75.0,
		VolumeChangePct:    -10.0,
		Summary:            "Alice has been focused on code reviews",
		CommunicationStyle: "analytical",
		DecisionRole:       "approver",
		RedFlags:           `["Ignoring deadlines"]`,
		Highlights:         `["Excellent code reviews"]`,
		Accomplishments:    `["Shipped authentication service"]`,
		HowToCommunicate:   "Prefers data-driven arguments",
		DecisionStyle:      "Careful approver, needs evidence",
		Tactics:            `["If proposing changes, bring data","If deadline missed, discuss priorities"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showUserDetail(buf, database, "alice", from, to)
	require.NoError(t, err)

	output := buf.String()
	// Output goes through ui.RenderMarkdown with ANSI codes,
	// so check for short strings that won't be split
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "100")
	assert.Contains(t, output, "analytical")
	database.Close()
}

func TestShowUserDetail_NoAnalysis(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	buf := new(bytes.Buffer)
	err = showUserDetail(buf, database, "alice", from, to)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No people card available")
}

func TestShowUserDetail_UserNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	err = showUserDetail(new(bytes.Buffer), database, "nonexistent", from, to)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- ShowPeopleList with analyses ---
func TestShowPeopleList_WithRedFlagsAndConcerns(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       50,
		ChannelsActive:     3,
		VolumeChangePct:    25.0,
		Summary:            "Very active week",
		CommunicationStyle: "verbose",
		DecisionRole:       "driver",
		RedFlags:           `["Overcommitting on tasks"]`,
		Highlights:         `["Led sprint planning"]`,
		Accomplishments:    `[]`,
		HowToCommunicate:   "Be prepared for detailed discussions",
		DecisionStyle:      "Drives decisions proactively",
		Tactics:            `[]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showPeopleList(peopleCmd, buf, database, nil, from, to,
		time.Unix(int64(from), 0), time.Unix(int64(to), 0))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	database.Close()
}

// --- Chains detail with refs ---
func TestShowChainDetail_WithRefs(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Test Chain",
		Slug:       "test-chain",
		Status:     "active",
		Summary:    "A test chain",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  2,
	})
	require.NoError(t, err)

	// Insert a track for reference
	trackID, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Related track",
		Status:         "active",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)

	// Insert chain ref (track type)
	err = database.InsertChainRef(db.ChainRef{
		ChainID:   int(chainID),
		RefType:   "track",
		TrackID:   int(trackID),
		ChannelID: "C001",
		Timestamp: now,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showChainDetail(database, buf, int(chainID))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Test Chain")
	assert.Contains(t, output, "Timeline")
	assert.Contains(t, output, "Related track")

	database.Close()
}

// --- Test version variables ---
func TestVersionValues(t *testing.T) {
	assert.NotEmpty(t, Version)
}

// --- ShowTopicsSummary with data ---
func TestShowTopicsSummary_WithTopics(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	// Insert channel digests with topics
	for i, topic := range []string{`["api","testing"]`, `["api","deploy"]`, `["deploy","monitoring"]`} {
		_, err = database.UpsertDigest(db.Digest{
			ChannelID:    "C001",
			PeriodFrom:   now - 3600 - float64(i*10),
			PeriodTo:     now - float64(i*10),
			Type:         "channel",
			Summary:      "Discussion",
			Topics:       topic,
			Decisions:    `[]`,
			ActionItems:  `[]`,
			MessageCount: 10,
			Model:        "haiku",
		})
		require.NoError(t, err)
	}

	buf := new(bytes.Buffer)
	err = showTopicsSummary(buf, database)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Trending Topics")
	assert.Contains(t, output, "api")
	assert.Contains(t, output, "deploy")

	database.Close()
}

// --- Config path helpers ---
func TestDefaultConfigPath_ReturnsNonEmpty(t *testing.T) {
	path := defaultConfigPath()
	assert.NotEmpty(t, path)
	assert.True(t, strings.Contains(path, "config.yaml"))
}


// --- Digest stats edge cases ---
func TestRunDigestStats_NegativeDays(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestStatsCmd.SetOut(buf)
	digestStatsFlagDays = -1 // should be clamped to 7

	err := digestStatsCmd.RunE(digestStatsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No digests generated")
}

// --- Chains list with different statuses ---
func TestRunChains_StatusIcons(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	for _, status := range []string{"active", "resolved", "stale"} {
		_, err = database.CreateChain(db.Chain{
			Title:      status + " chain",
			Slug:       status + "-chain",
			Status:     status,
			ChannelIDs: `[]`,
			FirstSeen:  now - 86400,
			LastSeen:   now,
			ItemCount:  1,
		})
		require.NoError(t, err)
	}
	database.Close()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err = chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "active chain")
	assert.Contains(t, output, "resolved chain")
	assert.Contains(t, output, "stale chain")
}

// --- Tracks with "all" status ---
func TestRunTracks_AllStatus(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	for _, status := range []string{"inbox", "active", "done", "dismissed"} {
		_, err = database.UpsertTrack(db.Track{
			ChannelID:      "C001",
			AssigneeUserID: "U001",
			Text:           status + " track",
			Status:         status,
			Priority:       "medium",
			PeriodFrom:     now - 86400,
			PeriodTo:       now,
			Model:          "haiku",
			Ownership:      "mine",
		})
		require.NoError(t, err)
	}
	database.Close()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = "all"
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""
	defer func() { tracksFlagStatus = "" }()

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "inbox track")
	assert.Contains(t, output, "active track")
	assert.Contains(t, output, "done track")
	assert.Contains(t, output, "dismissed track")
}

// --- Feedback with comment ---
func TestRunFeedback_WithComment(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = "This is great analysis"

	err := feedbackCmd.RunE(feedbackCmd, []string{"good", "digest", "100"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")

	feedbackFlagComment = ""
}
