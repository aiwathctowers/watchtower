package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func setupRendererDB(t *testing.T) (*db.DB, time.Time) {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Reference time: 2025-02-26 14:30 UTC
	refTime := time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC)

	// Workspace
	require.NoError(t, database.UpsertWorkspace(db.Workspace{
		ID:     "T001",
		Name:   "test-corp",
		Domain: "test-corp",
	}))

	// Users
	require.NoError(t, database.UpsertUser(db.User{
		ID: "U001", Name: "alice", DisplayName: "Alice Smith",
	}))

	// Channels
	require.NoError(t, database.UpsertChannel(db.Channel{
		ID: "C001", Name: "engineering", Type: "public",
	}))
	require.NoError(t, database.UpsertChannel(db.Channel{
		ID: "C002", Name: "design-team", Type: "public",
	}))

	// Messages at known timestamps
	tsUnix := float64(refTime.Unix())
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        "1740580200.000100",
		UserID:    "U001",
		Text:      "Deploying v2.3 to production",
		TSUnix:    tsUnix,
	}))

	// A second message in design 5 minutes earlier
	designTS := float64(refTime.Add(-5 * time.Minute).Unix())
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C002",
		TS:        "1740579900.000200",
		UserID:    "U001",
		Text:      "New mockups ready for review",
		TSUnix:    designTS,
	}))

	return database, refTime
}

func TestNewResponseRenderer(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")
	assert.NotNil(t, rr)
	assert.Equal(t, "test-corp", rr.domain)
}

func TestExtractRefs_SingleRef(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := rr.extractRefs("Alice mentioned in #engineering 2025-02-26 14:30 that deployment was ready.")
	require.Len(t, refs, 1)
	assert.Equal(t, "#engineering 2025-02-26 14:30", refs[0].fullMatch)
	assert.Equal(t, "engineering", refs[0].channelName)
	assert.Equal(t, 2025, refs[0].timestamp.Year())
	assert.Equal(t, time.February, refs[0].timestamp.Month())
	assert.Equal(t, 26, refs[0].timestamp.Day())
	assert.Equal(t, 14, refs[0].timestamp.Hour())
	assert.Equal(t, 30, refs[0].timestamp.Minute())
}

func TestExtractRefs_MultipleRefs(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	text := "In #engineering 2025-02-26 14:30 Alice deployed. Also in #design-team 2025-02-26 14:25 mockups were shared."
	refs := rr.extractRefs(text)
	require.Len(t, refs, 2)
	assert.Equal(t, "engineering", refs[0].channelName)
	assert.Equal(t, "design-team", refs[1].channelName)
}

func TestExtractRefs_DuplicatesIgnored(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	text := "See #engineering 2025-02-26 14:30 and again #engineering 2025-02-26 14:30 for details."
	refs := rr.extractRefs(text)
	assert.Len(t, refs, 1)
}

func TestExtractRefs_NoRefs(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := rr.extractRefs("Just a plain response with no message references.")
	assert.Nil(t, refs)
}

func TestExtractRefs_InvalidDate(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	// Invalid month 13 — time.Parse will reject it
	refs := rr.extractRefs("See #engineering 2025-13-26 14:30 for details.")
	assert.Len(t, refs, 0)
}

func TestExtractRefs_ChannelWithUnderscore(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := rr.extractRefs("Check #my_channel 2025-02-26 10:00 for context.")
	require.Len(t, refs, 1)
	assert.Equal(t, "my_channel", refs[0].channelName)
}

func TestResolveRefs_Found(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{
			fullMatch:   "#engineering 2025-02-26 14:30",
			channelName: "engineering",
			timestamp:   time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC),
		},
	}

	resolved := rr.resolveRefs(refs)
	require.Len(t, resolved, 1)
	assert.Contains(t, resolved[0].permalink, "test-corp.slack.com")
	assert.Contains(t, resolved[0].permalink, "C001")
}

func TestResolveRefs_ChannelNotFound(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{
			fullMatch:   "#nonexistent 2025-02-26 14:30",
			channelName: "nonexistent",
			timestamp:   time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC),
		},
	}

	resolved := rr.resolveRefs(refs)
	assert.Len(t, resolved, 0)
}

func TestResolveRefs_MessageNotFound(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	// Timestamp far from any message
	refs := []messageRef{
		{
			fullMatch:   "#engineering 2020-01-01 00:00",
			channelName: "engineering",
			timestamp:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	resolved := rr.resolveRefs(refs)
	assert.Len(t, resolved, 0)
}

func TestReplaceRefs(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{
			fullMatch:   "#engineering 2025-02-26 14:30",
			channelName: "engineering",
			timestamp:   time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC),
			permalink:   "https://test-corp.slack.com/archives/C001/p1740580200000100",
		},
	}

	result := rr.replaceRefs("See #engineering 2025-02-26 14:30 for details.", refs)
	assert.Contains(t, result, "[#engineering 2025-02-26 14:30](https://test-corp.slack.com/archives/C001/p1740580200000100)")
	assert.NotContains(t, result, "See #engineering 2025-02-26 14:30 for")
}

func TestReplaceRefs_NoPermalink(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{
			fullMatch:   "#engineering 2025-02-26 14:30",
			channelName: "engineering",
			permalink:   "", // not resolved
		},
	}

	original := "See #engineering 2025-02-26 14:30 for details."
	result := rr.replaceRefs(original, refs)
	assert.Equal(t, original, result)
}

func TestBuildSourcesSection_WithRefs(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{
			channelName: "engineering",
			timestamp:   time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC),
			permalink:   "https://test-corp.slack.com/archives/C001/p123",
		},
		{
			channelName: "design-team",
			timestamp:   time.Date(2025, 2, 26, 14, 25, 0, 0, time.UTC),
			permalink:   "https://test-corp.slack.com/archives/C002/p456",
		},
	}

	sources := rr.buildSourcesSection(refs)
	assert.Contains(t, sources, "Sources:")
	assert.Contains(t, sources, "[1] #engineering 2025-02-26 14:30")
	assert.Contains(t, sources, "[2] #design-team 2025-02-26 14:25")
	assert.Contains(t, sources, "https://test-corp.slack.com/archives/C001/p123")
}

func TestBuildSourcesSection_NoResolved(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	refs := []messageRef{
		{channelName: "engineering", permalink: ""},
	}
	sources := rr.buildSourcesSection(refs)
	assert.Empty(t, sources)
}

func TestBuildSourcesSection_Empty(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	sources := rr.buildSourcesSection(nil)
	assert.Empty(t, sources)
}

func TestRenderMarkdown(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	out, err := rr.renderMarkdown("**bold text** and _italic_")
	require.NoError(t, err)
	assert.NotEmpty(t, out)
	// Glamour renders bold/italic with ANSI codes; just verify it doesn't error
	// and the text content is preserved
	assert.Contains(t, out, "bold text")
	assert.Contains(t, out, "italic")
}

func TestRender_FullPipeline(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	response := "Alice deployed in #engineering 2025-02-26 14:30. The new version is live."
	out, err := rr.Render(response)
	require.NoError(t, err)

	// Should contain the text
	assert.Contains(t, out, "Alice deployed")
	assert.Contains(t, out, "new version is live")

	// Should have a Sources section with the resolved reference
	assert.Contains(t, out, "Sources:")
	assert.Contains(t, out, "#engineering 2025-02-26 14:30")
	assert.Contains(t, out, "test-corp.slack.com")
}

func TestRender_NoRefs(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	response := "Everything looks good, no specific messages to reference."
	out, err := rr.Render(response)
	require.NoError(t, err)

	assert.Contains(t, out, "Everything looks good")
	assert.NotContains(t, out, "Sources:")
}

func TestRender_UnresolvableRef(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	response := "Check #nonexistent-channel 2025-02-26 14:30 for details."
	out, err := rr.Render(response)
	require.NoError(t, err)

	// Should still render without error; just no sources
	assert.Contains(t, out, "Check")
	assert.NotContains(t, out, "Sources:")
}

func TestRender_MultipleRefsPartialResolve(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	response := "In #engineering 2025-02-26 14:30 and #nonexistent 2025-02-26 10:00 something happened."
	out, err := rr.Render(response)
	require.NoError(t, err)

	// Should have sources for the resolvable reference only
	assert.Contains(t, out, "Sources:")
	assert.Contains(t, out, "#engineering")
}

func TestGetMessageNear(t *testing.T) {
	database, refTime := setupRendererDB(t)

	// Exact timestamp match
	msg, err := database.GetMessageNear("C001", float64(refTime.Unix()))
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "Deploying v2.3 to production", msg.Text)

	// Slightly off timestamp (within tolerance)
	msg, err = database.GetMessageNear("C001", float64(refTime.Add(30*time.Second).Unix()))
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "Deploying v2.3 to production", msg.Text)

	// Way off timestamp (outside tolerance)
	msg, err = database.GetMessageNear("C001", float64(refTime.Add(2*time.Hour).Unix()))
	require.NoError(t, err)
	assert.Nil(t, msg)

	// Non-existent channel
	msg, err = database.GetMessageNear("CXXX", float64(refTime.Unix()))
	require.NoError(t, err)
	assert.Nil(t, msg)
}

func TestRefPattern(t *testing.T) {
	tests := []struct {
		input   string
		matches int
	}{
		{"#general 2025-02-26 14:30", 1},
		{"#my-channel 2025-01-01 00:00", 1},
		{"#my_channel 2025-06-15 23:59", 1},
		{"#a1 2025-02-26 14:30", 1},
		{"no ref here", 0},
		{"# 2025-02-26 14:30", 0},                   // no channel name
		{"#general 2025-02-26", 0},                    // no time
		{"#general 14:30", 0},                         // no date
		{"#general2025-02-26 14:30", 0},               // no space between channel and date
		{"#a 2025-02-26 14:30", 1},                    // single char channel name is valid
		{"text #chan 2025-02-26 14:30 more text", 1},  // embedded in text
	}

	for _, tt := range tests {
		matches := refPattern.FindAllStringSubmatch(tt.input, -1)
		assert.Equal(t, tt.matches, len(matches), "input: %s", tt.input)
	}
}

func TestRender_EmptyResponse(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	out, err := rr.Render("")
	require.NoError(t, err)
	assert.NotContains(t, out, "Sources:")
}

func TestRender_MarkdownPreserved(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	response := "## Summary\n\n- Item 1\n- Item 2\n\n**Important:** Check #engineering 2025-02-26 14:30."
	out, err := rr.Render(response)
	require.NoError(t, err)

	// Glamour renders headings, lists; content should be present
	assert.Contains(t, out, "Summary")
	assert.Contains(t, out, "Item 1")
	assert.Contains(t, out, "Item 2")
	assert.Contains(t, out, "Important")
	assert.Contains(t, out, "Sources:")
}

func TestExtractRefs_MultilineText(t *testing.T) {
	database, _ := setupRendererDB(t)
	rr := NewResponseRenderer(database, "test-corp")

	text := strings.Join([]string{
		"First paragraph mentioning #engineering 2025-02-26 14:30.",
		"",
		"Second paragraph with #design-team 2025-02-26 14:25.",
	}, "\n")

	refs := rr.extractRefs(text)
	assert.Len(t, refs, 2)
}
