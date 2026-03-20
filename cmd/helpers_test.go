package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- Pure utility functions ---

func TestSanitizeWorkspaceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Company", "my-company"},
		{"  Spaces Around  ", "spaces-around"},
		{"Already-Good", "already-good"},
		{"UPPERCASE", "uppercase"},
		{"Special!@#$Chars", "special-chars"},
		{"multi---dashes", "multi---dashes"},
		{"", ""},
		{"a", "a"},
		{"hello_world", "hello_world"},
		{"café-naïve", "caf--na-ve"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeWorkspaceName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJoinTopics(t *testing.T) {
	assert.Equal(t, "", joinTopics(nil))
	assert.Equal(t, "", joinTopics([]string{}))
	assert.Equal(t, "one", joinTopics([]string{"one"}))
	assert.Equal(t, "one, two, three", joinTopics([]string{"one", "two", "three"}))
}

func TestAppendTopics(t *testing.T) {
	var sb strings.Builder
	appendTopics(&sb, `["api","deploy","kubernetes"]`)
	assert.Contains(t, sb.String(), "Topics: api, deploy, kubernetes")

	sb.Reset()
	appendTopics(&sb, `[]`)
	assert.Empty(t, sb.String())

	sb.Reset()
	appendTopics(&sb, `invalid json`)
	assert.Empty(t, sb.String())
}

func TestDisplayName(t *testing.T) {
	assert.Equal(t, "Alice Johnson", displayName(&db.User{Name: "alice", DisplayName: "Alice Johnson"}))
	assert.Equal(t, "bob", displayName(&db.User{Name: "bob"}))
	assert.Equal(t, "", displayName(&db.User{}))
}

func TestFormatBadges(t *testing.T) {
	assert.Equal(t, "", formatBadges("", ""))
	assert.Equal(t, "[verbose]", formatBadges("verbose", ""))
	assert.Equal(t, "[driver]", formatBadges("", "driver"))
	assert.Equal(t, "[verbose] [driver]", formatBadges("verbose", "driver"))
}

func TestDayOfWeek(t *testing.T) {
	assert.Equal(t, time.Sunday, dayOfWeek("sunday"))
	assert.Equal(t, time.Monday, dayOfWeek("monday"))
	assert.Equal(t, time.Tuesday, dayOfWeek("tuesday"))
	assert.Equal(t, time.Wednesday, dayOfWeek("wednesday"))
	assert.Equal(t, time.Thursday, dayOfWeek("thursday"))
	assert.Equal(t, time.Friday, dayOfWeek("friday"))
	assert.Equal(t, time.Saturday, dayOfWeek("saturday"))
	assert.Equal(t, time.Monday, dayOfWeek("invalid")) // fallback
}

func TestParseTrackID(t *testing.T) {
	id, err := parseTrackID("42")
	require.NoError(t, err)
	assert.Equal(t, 42, id)

	id, err = parseTrackID("1")
	require.NoError(t, err)
	assert.Equal(t, 1, id)

	_, err = parseTrackID("0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")

	_, err = parseTrackID("-5")
	assert.Error(t, err)

	_, err = parseTrackID("abc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid track ID")
}

func TestParseSnoozeUntil(t *testing.T) {
	// --hours
	result, err := parseSnoozeUntil("", 4)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(4*time.Hour), result, 5*time.Second)

	// --until tomorrow
	result, err = parseSnoozeUntil("tomorrow", 0)
	require.NoError(t, err)
	tomorrow := time.Now().AddDate(0, 0, 1)
	expected := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, tomorrow.Location())
	assert.Equal(t, expected, result)

	// --until next-week
	result, err = parseSnoozeUntil("next-week", 0)
	require.NoError(t, err)
	assert.Equal(t, time.Monday, result.Weekday())
	assert.Equal(t, 9, result.Hour())

	// weekday names
	for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		result, err = parseSnoozeUntil(day, 0)
		require.NoError(t, err)
		assert.Equal(t, 9, result.Hour())
		assert.True(t, result.After(time.Now()))
	}

	// YYYY-MM-DD in the future
	future := time.Now().AddDate(0, 0, 30)
	dateStr := future.Format("2006-01-02")
	result, err = parseSnoozeUntil(dateStr, 0)
	require.NoError(t, err)
	assert.Equal(t, 9, result.Hour())

	// Errors
	_, err = parseSnoozeUntil("tomorrow", 4)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not both")

	_, err = parseSnoozeUntil("", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify")

	_, err = parseSnoozeUntil("2020-01-01", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "in the past")

	_, err = parseSnoozeUntil("garbage", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --until")
}

func TestPrintJSONList(t *testing.T) {
	buf := new(bytes.Buffer)
	printJSONList(buf, "  Items: ", `["a","b","c"]`)
	assert.Equal(t, "  Items: a, b, c\n", buf.String())

	buf.Reset()
	printJSONList(buf, "  Items: ", `[]`)
	assert.Empty(t, buf.String())

	buf.Reset()
	printJSONList(buf, "  Items: ", "")
	assert.Empty(t, buf.String())

	buf.Reset()
	printJSONList(buf, "  Items: ", "not json")
	assert.Contains(t, buf.String(), "invalid JSON")
}

func TestBuildDigestContext_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	result := buildDigestContext(database)
	assert.Empty(t, result)
}

func TestBuildDigestContext_WithChannelDigests(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Team discussed deployment plans",
		Topics:       `["deploy","infra"]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 15,
		Model:        "haiku",
	})
	require.NoError(t, err)

	result := buildDigestContext(database)
	assert.Contains(t, result, "#general")
	assert.Contains(t, result, "deployment plans")
	assert.Contains(t, result, "15 msgs")
}

func TestBuildDigestContext_PrefersDaily(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := float64(time.Now().Unix())

	// Insert both a channel and a daily digest
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "channel summary",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 5,
		Model:        "haiku",
	})
	require.NoError(t, err)

	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 86400,
		PeriodTo:     now,
		Type:         "daily",
		Summary:      "daily rollup summary",
		Topics:       `["api","deploy"]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 50,
		Model:        "haiku",
	})
	require.NoError(t, err)

	result := buildDigestContext(database)
	assert.Contains(t, result, "Daily summary: daily rollup summary")
	assert.Contains(t, result, "Topics: api, deploy")
	// Should NOT contain channel summary since daily takes priority
	assert.NotContains(t, result, "channel summary")
}

func TestChannelNameFromDB(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	assert.Equal(t, "general", channelNameFromDB(database, "C001"))
	assert.Equal(t, "C999", channelNameFromDB(database, "C999")) // fallback to ID
}

func TestMaskValueEdgeCases(t *testing.T) {
	assert.Equal(t, "****", maskValue(""))
	assert.Equal(t, "****", maskValue("abc"))
	assert.Equal(t, "xoxb-****", maskValue("xoxb-another-token"))
}

func TestPrintLastLines(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/test.log"

	// Create a log file with 10 lines
	var content strings.Builder
	for i := 1; i <= 10; i++ {
		content.WriteString("line " + strings.Repeat("x", i) + "\n")
	}
	require.NoError(t, os.WriteFile(logFile, []byte(content.String()), 0o600))

	buf := new(bytes.Buffer)
	err := printLastLines(buf, logFile, 3)
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	assert.Len(t, lines, 3)
	assert.Contains(t, lines[0], "line xxxxxxxx")

	// Read all lines
	buf.Reset()
	err = printLastLines(buf, logFile, 100)
	require.NoError(t, err)
	lines = strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, 10)
}


func TestDbFileSize_Nonexistent(t *testing.T) {
	assert.Equal(t, int64(0), dbFileSize("/nonexistent/file/path"))
}

func TestPrintDigestDetails_Empty(t *testing.T) {
	buf := new(bytes.Buffer)
	d := db.Digest{Decisions: "[]", ActionItems: "[]"}
	printDigestDetails(buf, d)
	assert.Empty(t, buf.String())
}

func TestPrintDigestDetails_WithContent(t *testing.T) {
	buf := new(bytes.Buffer)
	d := db.Digest{
		Decisions:   `[{"text":"Use Go for backend","by":"Alice"},{"text":"No breaking changes"}]`,
		ActionItems: `[{"text":"Deploy by Friday","assignee":"Bob"},{"text":"Review PR"}]`,
	}
	printDigestDetails(buf, d)

	output := buf.String()
	assert.Contains(t, output, "Use Go for backend")
	assert.Contains(t, output, "(by Alice)")
	assert.Contains(t, output, "No breaking changes")
	assert.Contains(t, output, "Deploy by Friday")
	assert.Contains(t, output, "-> Bob")
	assert.Contains(t, output, "Review PR")
}
