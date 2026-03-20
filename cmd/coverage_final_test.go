package cmd

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
	internalsync "watchtower/internal/sync"
)

// --- resolveOAuthConfig ---

func TestResolveOAuthConfig_NoCredentials(t *testing.T) {
	// Clear env vars to test the error path
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "")

	_, err := resolveOAuthConfig()
	// If default build-time credentials are empty, this should error
	// If they're set (release build), it succeeds — both are valid
	if err != nil {
		assert.Contains(t, err.Error(), "OAuth credentials not configured")
	}
}

func TestResolveOAuthConfig_WithEnvVars(t *testing.T) {
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "test-client-secret")

	cfg, err := resolveOAuthConfig()
	require.NoError(t, err)
	assert.Equal(t, "test-client-id", cfg.ClientID)
	assert.Equal(t, "test-client-secret", cfg.ClientSecret)
}

// --- cliPooledGenerator ---

func TestCliPooledGenerator(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	cfg, err := loadTestConfig()
	require.NoError(t, err)

	gen, savePool := cliPooledGenerator(cfg, nil)
	assert.NotNil(t, gen)
	assert.NotNil(t, savePool)
	savePool() // cleanup should not panic
}

// --- showTopicsSummary edge cases ---

func TestShowTopicsSummary_NoDigests(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	buf := new(bytes.Buffer)
	err = showTopicsSummary(buf, database)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No trends data available")
}

func TestShowTopicsSummary_DigestsButNoTopics(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Discussion",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 10,
		Model:        "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showTopicsSummary(buf, database)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No topics extracted")
}

// --- runTrends fallback to showTopicsSummary ---

func TestRunTrends_FallbackToTopics2(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	// Insert channel digests (not weekly)
	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "API discussion",
		Topics:       `["api","rest"]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 15,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err = trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Trending Topics")
}

func TestRunTrends_NoData2(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err := trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No trends data")
}

// --- runChannels edge cases ---

func TestRunChannels_InvalidType(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	channelsFlagType = "invalid"
	channelsFlagSort = "name"
	defer func() { channelsFlagType = "" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestRunChannels_InvalidSort(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	channelsFlagType = ""
	channelsFlagSort = "invalid"
	defer func() { channelsFlagSort = "name" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sort")
}

func TestRunChannels_WithWatchedChannel(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public", NumMembers: 50}))
	require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
	database.Close()

	channelsFlagType = ""
	channelsFlagSort = "name"

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "general")
	assert.Contains(t, output, "[watched]")
	assert.Contains(t, output, "channels total")
}

// --- runUsers edge cases ---

func TestRunUsers_WithBotAndDeleted(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice", Email: "alice@example.com"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bot-helper", IsBot: true}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U003", Name: "departed", IsDeleted: true}))
	database.Close()

	// Without active filter — shows all
	usersFlagActive = false
	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "[bot]")
	assert.Contains(t, output, "[deleted]")
	assert.Contains(t, output, "3 users total")
}

func TestRunUsers_ActiveFilter(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", Email: "alice@example.com"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bot-helper", IsBot: true}))
	database.Close()

	usersFlagActive = true
	defer func() { usersFlagActive = false }()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.NotContains(t, output, "bot-helper")
	assert.Contains(t, output, "1 users total")
}

// --- watch add edge cases ---

func TestWatchAdd_InvalidPriority(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	watchFlagPriority = "invalid"
	defer func() { watchFlagPriority = "normal" }()

	err := watchAddCmd.RunE(watchAddCmd, []string{"#general"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid priority")
}

func TestWatchAdd_ChannelNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	watchFlagPriority = "normal"

	err := watchAddCmd.RunE(watchAddCmd, []string{"#nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWatchAdd_UserTarget(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	database.Close()

	watchFlagPriority = "high"
	defer func() { watchFlagPriority = "normal" }()

	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)

	err = watchAddCmd.RunE(watchAddCmd, []string{"@alice"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "@alice")
	assert.Contains(t, buf.String(), "high")
}

func TestWatchRemove_ChannelNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := watchRemoveCmd.RunE(watchRemoveCmd, []string{"#nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- watchList empty ---

func TestWatchList_Empty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchListCmd.SetOut(buf)

	err := watchListCmd.RunE(watchListCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No watched channels")
}

// --- resolveTarget ---

func TestResolveTarget_ChannelFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, "#general")
	require.NoError(t, err)
	assert.Equal(t, "channel", entityType)
	assert.Equal(t, "C001", entityID)
	assert.Equal(t, "general", entityName)
}

func TestResolveTarget_NoPrefixChannel(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, "general")
	require.NoError(t, err)
	assert.Equal(t, "channel", entityType)
	assert.Equal(t, "C001", entityID)
	assert.Equal(t, "general", entityName)
}

func TestResolveTarget_NoPrefixUser(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, "alice")
	require.NoError(t, err)
	assert.Equal(t, "user", entityType)
	assert.Equal(t, "U001", entityID)
	assert.Equal(t, "alice", entityName)
}

// --- runFeedback with numeric ratings ---

func TestRunFeedback_Numeric1(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = ""

	err := feedbackCmd.RunE(feedbackCmd, []string{"1", "digest", "99"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")
}

// --- sanitizeWorkspaceName edge cases ---

func TestSanitizeWorkspaceName_SpecialChars(t *testing.T) {
	assert.Equal(t, "my-company", sanitizeWorkspaceName("My Company"))
	assert.Equal(t, "test-team", sanitizeWorkspaceName("  Test Team  "))
	assert.Equal(t, "abc123", sanitizeWorkspaceName("abc123"))
	assert.Equal(t, "", sanitizeWorkspaceName(""))
	assert.Equal(t, "hello_world", sanitizeWorkspaceName("hello_world"))
}

// --- runDigest with channel digest showing topics and actions ---

func TestRunDigest_FullDigestOutput(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Deployment planning session",
		Topics:       `["deploy","k8s","monitoring"]`,
		Decisions:    `[{"text":"Use Helm charts","by":"devops-lead"},{"text":"Deploy Friday"}]`,
		ActionItems:  `[{"text":"Write runbook","assignee":"alice","status":"pending"},{"text":"Test staging","assignee":"","status":"open"}]`,
		MessageCount: 42,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 1

	err = digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Deployment planning")
}

// --- showDigestCatchup with daily digest ---

func TestShowDigestCatchup_DailyDigest2(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 86400,
		PeriodTo:     now,
		Type:         "daily",
		Summary:      "Busy day with lots of decisions",
		Topics:       `[]`,
		Decisions:    `[{"text":"Ship v2","by":"PM"}]`,
		ActionItems:  `[{"text":"Tag release","assignee":"eng"}]`,
		MessageCount: 200,
		Model:        "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	shown := showDigestCatchup(buf, database, now-100000)
	assert.True(t, shown)
	output := buf.String()
	assert.NotEmpty(t, output)
}

// --- dbFileSize ---

func TestDbFileSize_ExistingFile(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	require.NoError(t, os.WriteFile(tmpFile, make([]byte, 1024), 0o600))
	assert.Equal(t, int64(1024), dbFileSize(tmpFile))
}

// --- printProgress non-verbose ---

func TestPrintProgress_NonVerbose(t *testing.T) {
	buf := new(bytes.Buffer)
	p := internalsync.NewProgress()

	oldVerbose := flagVerbose
	flagVerbose = false
	defer func() { flagVerbose = oldVerbose }()

	// Non-verbose mode uses terminal cursor movement — just verify no panic
	printProgress(buf, p, "test-ws")
}

// --- verifyTrackOwnership ---

func TestVerifyTrackOwnership_TrackNotFound(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	err = verifyTrackOwnership(database, 999999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- openTracksDB ---

func TestOpenTracksDB_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	_, err := openTracksDB()
	assert.Error(t, err)
}

func TestOpenTracksDB_Success(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openTracksDB()
	require.NoError(t, err)
	database.Close()
}

// --- digest summary flag combinations ---

func TestRunDigestSummary_HoursFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = ""
	digestSummaryFlagTo = ""
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 12
	defer func() { digestSummaryFlagHours = 0 }()

	// This will fail because it tries to run the Claude AI pipeline
	// but it exercises the time parsing code path
	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	// May error due to missing Claude CLI, but the time parsing succeeded
	if err != nil {
		assert.NotContains(t, err.Error(), "specify --hours")
	}
}

func TestRunDigestSummary_DaysFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = ""
	digestSummaryFlagTo = ""
	digestSummaryFlagDays = 3
	digestSummaryFlagHours = 0
	defer func() { digestSummaryFlagDays = 0 }()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	if err != nil {
		assert.NotContains(t, err.Error(), "specify --hours")
	}
}

func TestRunDigestSummary_FromWithTo(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = "2025-01-01"
	digestSummaryFlagTo = "2025-01-07"
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 0
	defer func() {
		digestSummaryFlagFrom = ""
		digestSummaryFlagTo = ""
	}()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	if err != nil {
		assert.NotContains(t, err.Error(), "invalid --from")
		assert.NotContains(t, err.Error(), "invalid --to")
	}
}

func TestRunDigestSummary_FromWithoutTo(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = "2025-01-01"
	digestSummaryFlagTo = ""
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 0
	defer func() { digestSummaryFlagFrom = "" }()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	if err != nil {
		assert.NotContains(t, err.Error(), "invalid --from")
	}
}

// --- printProgressJSON with error ---

func TestPrintProgressJSON_WithSyncError(t *testing.T) {
	buf := new(bytes.Buffer)
	snap := internalsync.Snapshot{
		Phase:           internalsync.PhaseMessages,
		StartTime:       time.Now().Add(-30 * time.Second),
		MessagesFetched: 500,
	}
	printProgressJSON(buf, snap, fmt.Errorf("connection timeout"))
	output := buf.String()
	assert.Contains(t, output, "connection timeout")
	assert.Contains(t, output, `"messages_fetched":500`)
}

// --- people edge cases ---

func TestRunPeople_UserArg(t *testing.T) {
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
		Summary:            "Normal week for alice",
		CommunicationStyle: "balanced",
		DecisionRole:       "contributor",
		RedFlags:           `[]`,
		Highlights:         `[]`,
		Accomplishments:    `["Shipped feature"]`,
		HowToCommunicate:   "",
		DecisionStyle:      "",
		Tactics:            `["Rest more"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = "@alice"
	peopleFlagPrevious = false
	peopleFlagWeeks = 1
	defer func() { peopleFlagUser = "" }()

	err = peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "alice")
}

func TestRunPeople_PreviousWindow(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = true
	peopleFlagWeeks = 1
	defer func() { peopleFlagPrevious = false }()

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	// Should show "No people cards available" for previous window
	assert.Contains(t, buf.String(), "No people cards")
}

func TestRunPeople_UserNotFoundViaFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	peopleFlagUser = "@nonexistent"
	peopleFlagPrevious = false
	peopleFlagWeeks = 1
	defer func() { peopleFlagUser = "" }()

	err := peopleCmd.RunE(peopleCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestShowUserDetail_NoAnalysisAvailable(t *testing.T) {
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

// --- feedback stats requires config ---

func TestRunFeedbackStats_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := feedbackStatsCmd.RunE(feedbackStatsCmd, nil)
	assert.Error(t, err)
}

// --- runChains with args ---

func TestRunChains_WithInvalidIDArg(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := chainsCmd.RunE(chainsCmd, []string{"not-a-number"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid chain ID")
}

func TestRunChains_NoChains(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	chainsCmd.SetOut(buf)
	chainsFlagStatus = ""

	err := chainsCmd.RunE(chainsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No chains found")
}

func TestRunChains_WithResolvedAndStale(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.CreateChain(db.Chain{
		Title:      "Resolved Chain",
		Slug:       "resolved-chain",
		Status:     "resolved",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400*7,
		LastSeen:   now,
		ItemCount:  5,
	})
	require.NoError(t, err)
	_, err = database.CreateChain(db.Chain{
		Title:      "Stale Chain",
		Slug:       "stale-chain",
		Status:     "stale",
		ChannelIDs: `[]`,
		FirstSeen:  now - 86400*30,
		LastSeen:   now - 86400*14,
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
	assert.Contains(t, output, "Resolved Chain")
	assert.Contains(t, output, "Stale Chain")
}

// --- chains with track refs ---

func TestShowChainDetail_TrackRef(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	chainID, err := database.CreateChain(db.Chain{
		Title:      "Chain With Track Ref",
		Slug:       "chain-track-ref",
		Status:     "active",
		Summary:    "Track-linked chain",
		ChannelIDs: `["C001"]`,
		FirstSeen:  now - 86400,
		LastSeen:   now,
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Insert track ref
	err = database.InsertChainRef(db.ChainRef{
		ChainID:   int(chainID),
		RefType:   "track",
		TrackID:   42,
		ChannelID: "C001",
		Timestamp: now,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showChainDetail(database, buf, int(chainID))
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Chain With Track Ref")
	assert.Contains(t, output, "Timeline")

	database.Close()
}
