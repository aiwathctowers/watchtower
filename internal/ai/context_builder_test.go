package ai

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// setupTestDB creates an in-memory DB with test data and returns the DB plus
// a reference timestamp used as the base for message timestamps.
func setupTestDB(t *testing.T) (*db.DB, time.Time) {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Reference time: 2025-02-26 12:00:00 UTC
	refTime := time.Date(2025, 2, 26, 12, 0, 0, 0, time.UTC)

	// Workspace
	err = database.UpsertWorkspace(db.Workspace{
		ID:     "T001",
		Name:   "test-corp",
		Domain: "test-corp",
	})
	require.NoError(t, err)

	// Users
	users := []db.User{
		{ID: "U001", Name: "alice", DisplayName: "Alice Smith", RealName: "Alice Smith", Email: "alice@test.com"},
		{ID: "U002", Name: "bob", DisplayName: "Bob Jones", RealName: "Bob Jones", Email: "bob@test.com"},
		{ID: "U003", Name: "carol", DisplayName: "Carol White", RealName: "Carol White", Email: "carol@test.com"},
	}
	for _, u := range users {
		require.NoError(t, database.UpsertUser(u))
	}

	// Channels
	channels := []db.Channel{
		{ID: "C001", Name: "engineering", Type: "public", NumMembers: 50, IsMember: true},
		{ID: "C002", Name: "design", Type: "public", NumMembers: 20, IsMember: true},
		{ID: "C003", Name: "random", Type: "public", NumMembers: 100, IsMember: true},
		{ID: "C004", Name: "alerts", Type: "public", NumMembers: 10, IsMember: false, IsArchived: true},
	}
	for _, ch := range channels {
		require.NoError(t, database.UpsertChannel(ch))
	}

	// Messages in #engineering (C001)
	engMessages := []struct {
		ts     string
		userID string
		text   string
		offset time.Duration
		reply  int
	}{
		{"1740567600.000100", "U001", "Deploying v2.3 to production today", -2 * time.Hour, 2},
		{"1740567600.000200", "U002", "Looks good, I'll monitor dashboards", -90 * time.Minute, 0},
		{"1740567600.000300", "U003", "Any breaking changes we should know about?", -80 * time.Minute, 0},
		{"1740567600.000400", "U001", "CI pipeline is green, merging now", -60 * time.Minute, 0},
		{"1740567600.000500", "U002", "Deployment successful, no errors in logs", -30 * time.Minute, 0},
	}

	for _, m := range engMessages {
		tsUnix := float64(refTime.Add(m.offset).Unix())
		ts := fmt.Sprintf("%.6f", tsUnix)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID:  "C001",
			TS:         ts,
			UserID:     m.userID,
			Text:       m.text,
			ReplyCount: m.reply,
			TSUnix:     tsUnix,
		}))
	}

	// Thread replies for first engineering message
	parentTS := fmt.Sprintf("%.6f", float64(refTime.Add(-2*time.Hour).Unix()))
	replyTS1 := fmt.Sprintf("%.6f", float64(refTime.Add(-110*time.Minute).Unix()))
	replyTS2 := fmt.Sprintf("%.6f", float64(refTime.Add(-100*time.Minute).Unix()))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        replyTS1,
		UserID:    "U002",
		Text:      "Sounds good, I'll keep an eye on metrics",
		ThreadTS:  sql.NullString{String: parentTS, Valid: true},
		TSUnix:    float64(refTime.Add(-110 * time.Minute).Unix()),
	}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        replyTS2,
		UserID:    "U003",
		Text:      "Will check for breaking changes in my service",
		ThreadTS:  sql.NullString{String: parentTS, Valid: true},
		TSUnix:    float64(refTime.Add(-100 * time.Minute).Unix()),
	}))

	// Messages in #design (C002)
	designMessages := []struct {
		userID string
		text   string
		offset time.Duration
	}{
		{"U003", "New mockups for the settings page are ready", -3 * time.Hour},
		{"U001", "These look great! Ship it", -150 * time.Minute},
	}
	for _, m := range designMessages {
		tsUnix := float64(refTime.Add(m.offset).Unix())
		ts := fmt.Sprintf("%.6f", tsUnix)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID: "C002",
			TS:        ts,
			UserID:    m.userID,
			Text:      m.text,
			TSUnix:    tsUnix,
		}))
	}

	// Messages in #random (C003) — more messages for broad context
	for i := 0; i < 10; i++ {
		tsUnix := float64(refTime.Add(-time.Duration(i*10) * time.Minute).Unix())
		ts := fmt.Sprintf("%.6f", tsUnix)
		uid := fmt.Sprintf("U00%d", (i%3)+1)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID: "C003",
			TS:        ts,
			UserID:    uid,
			Text:      fmt.Sprintf("Random message %d about stuff", i),
			TSUnix:    tsUnix,
		}))
	}

	return database, refTime
}

func TestNewContextBuilder(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")
	assert.NotNil(t, cb)
	assert.Equal(t, 150000, cb.budget)
	assert.Equal(t, "test-corp", cb.domain)
}

func TestNewContextBuilder_DefaultBudget(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 0, "test-corp")
	assert.Equal(t, 150000, cb.budget)
}

func TestBuild_EmptyQuery(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{RawText: ""}
	result, err := cb.Build(query)
	require.NoError(t, err)

	// Should still produce workspace summary and broad context
	assert.Contains(t, result, "Workspace Summary")
	assert.Contains(t, result, "test-corp")
}

func TestBuild_WorkspaceSummary(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{RawText: "what's happening"}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "=== Workspace Summary ===")
	assert.Contains(t, result, "Workspace: test-corp")
	assert.Contains(t, result, "Channels: 4")
	assert.Contains(t, result, "Users: 3")
}

func TestBuild_WithWatchList(t *testing.T) {
	database, refTime := setupTestDB(t)

	// Add watches
	require.NoError(t, database.AddWatch("channel", "C001", "engineering", "high"))
	require.NoError(t, database.AddWatch("user", "U001", "alice", "high"))

	cb := NewContextBuilder(database, 150000, "test-corp")

	// Query with time range covering our test data
	query := ParsedQuery{
		RawText: "what happened",
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentCatchup,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	// Workspace summary should mention watch items
	assert.Contains(t, result, "engineering [high]")
	assert.Contains(t, result, "alice [high]")

	// Priority context should have watched channel messages
	assert.Contains(t, result, "Priority Context")
	assert.Contains(t, result, "#engineering")
}

func TestBuild_ChannelQuery(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText:  "summarize #engineering",
		Channels: []string{"engineering"},
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentChannel,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "Relevant Context")
	assert.Contains(t, result, "#engineering")
	assert.Contains(t, result, "Deploying v2.3")
}

func TestBuild_UserQuery(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText: "what did alice say",
		Users:   []string{"alice"},
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentPerson,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "Relevant Context")
	assert.Contains(t, result, "@alice")
	assert.Contains(t, result, "Deploying v2.3")
}

func TestBuild_SearchQuery(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText: "find messages about deployment",
		Topics:  []string{"deployment"},
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentSearch,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "Relevant Context")
	assert.Contains(t, result, "Search Results")
}

func TestBuild_BroadContext(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText: "what's going on",
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentCatchup,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "Activity Overview")
	assert.Contains(t, result, "Active channels")
}

func TestBuild_TokenBudgetRespected(t *testing.T) {
	database, refTime := setupTestDB(t)

	// Very small budget
	cb := NewContextBuilder(database, 500, "test-corp")

	query := ParsedQuery{
		RawText: "what happened",
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	// Result should be limited — estimate tokens and check
	tokens := estimateTokens(result)
	// Allow some headroom since each section independently respects its budget
	assert.Less(t, tokens, 1000, "total tokens should be small with 500 budget")
}

func TestBuild_DefaultTimeRange(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	// No time range specified — should default to last 24h
	query := ParsedQuery{
		RawText: "what's happening",
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	// Should still have content from our test data (which is in last 24h by default)
	assert.Contains(t, result, "Workspace Summary")
}

func TestFormatMessage(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	tsUnix := float64(time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC).Unix())
	msg := db.Message{
		ChannelID: "C001",
		TS:        "1740577800.000000",
		UserID:    "U001",
		Text:      "Test message content",
		TSUnix:    tsUnix,
	}

	line := cb.formatMessage("engineering", msg)
	assert.Contains(t, line, "#engineering")
	assert.Contains(t, line, "2025-02-26 14:30")
	assert.Contains(t, line, "@alice")
	assert.Contains(t, line, "Alice Smith")
	assert.Contains(t, line, "Test message content")
}

func TestFormatMessage_LongText(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	longText := strings.Repeat("x", 600)
	msg := db.Message{
		ChannelID: "C001",
		TS:        "1740577800.000000",
		UserID:    "U001",
		Text:      longText,
		TSUnix:    float64(time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC).Unix()),
	}

	line := cb.formatMessage("engineering", msg)
	// Should be truncated to ~500 chars + "..."
	assert.Contains(t, line, "...")
	assert.Less(t, len(line), 700)
}

func TestFormatMessage_UnknownUser(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	msg := db.Message{
		ChannelID: "C001",
		TS:        "1740577800.000000",
		UserID:    "UUNKNOWN",
		Text:      "Message from unknown user",
		TSUnix:    float64(time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC).Unix()),
	}

	line := cb.formatMessage("engineering", msg)
	assert.Contains(t, line, "@UUNKNOWN")
}

func TestEstimateTokens(t *testing.T) {
	assert.Equal(t, 1, estimateTokens("hi"))
	assert.Equal(t, 1, estimateTokens("test"))
	assert.Equal(t, 2, estimateTokens("hello"))
	assert.Equal(t, 3, estimateTokens("hello world!"))
}

func TestTruncateToTokens(t *testing.T) {
	long := strings.Repeat("a", 100)
	result := truncateToTokens(long, 10)
	assert.Equal(t, 40, len(result)) // 10 tokens * 4 chars/token

	short := "hello"
	result = truncateToTokens(short, 100)
	assert.Equal(t, short, result)
}

func TestBuildWorkspaceSummary_EmptyDB(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	cb := NewContextBuilder(database, 150000, "test-corp")
	summary, err := cb.buildWorkspaceSummary(1000)
	require.NoError(t, err)
	assert.Contains(t, summary, "Workspace Summary")
	assert.Contains(t, summary, "Channels: 0")
}

func TestBuildPriorityContext_NoWatchList(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
	}
	result, err := cb.buildPriorityContext(query, 50000)
	require.NoError(t, err)
	assert.Empty(t, result) // No watched items
}

func TestBuildPriorityContext_WithWatchedChannel(t *testing.T) {
	database, refTime := setupTestDB(t)
	require.NoError(t, database.AddWatch("channel", "C001", "engineering", "high"))

	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
	}
	result, err := cb.buildPriorityContext(query, 50000)
	require.NoError(t, err)
	assert.Contains(t, result, "Priority Context")
	assert.Contains(t, result, "#engineering")
	assert.Contains(t, result, "Deploying v2.3")
}

func TestBuildRelevantContext_NoMatchingData(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		Channels: []string{"nonexistent"},
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
	}
	result, err := cb.buildRelevantContext(query, 50000)
	require.NoError(t, err)
	assert.Empty(t, result) // Channel doesn't exist
}

func TestBuildBroadContext_ShowsActiveChannels(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
	}
	result, err := cb.buildBroadContext(query, 50000)
	require.NoError(t, err)
	assert.Contains(t, result, "Activity Overview")
	assert.Contains(t, result, "#random") // Should have most messages
	assert.Contains(t, result, "#engineering")
}

func TestResolveChannelName(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	assert.Equal(t, "engineering", cb.resolveChannelName("C001"))
	assert.Equal(t, "CUNKNOWN", cb.resolveChannelName("CUNKNOWN"))
}

func TestResolveUser(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	name, display := cb.resolveUser("U001")
	assert.Equal(t, "alice", name)
	assert.Equal(t, "Alice Smith", display)

	name, display = cb.resolveUser("UUNKNOWN")
	assert.Equal(t, "UUNKNOWN", name)
	assert.Equal(t, "", display)
}

func TestEffectiveTimeRange_WithQuery(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	from := time.Date(2025, 2, 25, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 2, 25, 23, 59, 59, 0, time.UTC)
	query := ParsedQuery{
		TimeRange: &TimeRange{From: from, To: to},
	}

	f, tt := cb.effectiveTimeRange(query)
	assert.Equal(t, float64(from.Unix()), f)
	assert.Equal(t, float64(to.Unix()), tt)
}

func TestEffectiveTimeRange_DefaultLast24h(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{}
	f, tt := cb.effectiveTimeRange(query)

	now := time.Now()
	// "from" should be roughly 24h ago
	assert.InDelta(t, float64(now.Add(-24*time.Hour).Unix()), f, 2.0)
	assert.InDelta(t, float64(now.Unix()), tt, 2.0)
}

func TestBuild_CombinedChannelAndUser(t *testing.T) {
	database, refTime := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText:  "what did alice say in engineering",
		Channels: []string{"engineering"},
		Users:    []string{"alice"},
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentPerson,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	assert.Contains(t, result, "#engineering")
	assert.Contains(t, result, "@alice")
}

func TestBuild_ThreadSummaryIncluded(t *testing.T) {
	database, refTime := setupTestDB(t)

	// Add a watch so priority context picks up the engineering channel with thread
	require.NoError(t, database.AddWatch("channel", "C001", "engineering", "high"))

	cb := NewContextBuilder(database, 150000, "test-corp")

	query := ParsedQuery{
		RawText: "catch me up",
		TimeRange: &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		},
		Intent: IntentCatchup,
	}
	result, err := cb.Build(query)
	require.NoError(t, err)

	// The first engineering message has 2 replies; thread summary should appear
	assert.Contains(t, result, "replies")
}

func TestFormatSearchResults_Dedup(t *testing.T) {
	database, _ := setupTestDB(t)
	cb := NewContextBuilder(database, 150000, "test-corp")

	msgs := []db.Message{
		{ChannelID: "C001", TS: "1.000", UserID: "U001", Text: "first", TSUnix: 1000},
		{ChannelID: "C001", TS: "2.000", UserID: "U002", Text: "second", TSUnix: 2000},
	}

	seen := map[string]bool{"C001|1.000": true} // First message already seen

	result := cb.formatSearchResults(msgs, 50000, seen)
	assert.NotContains(t, result, "first")
	assert.Contains(t, result, "second")
}
