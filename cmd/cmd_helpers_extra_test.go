package cmd

import (
	"bytes"
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"testing"

	"watchtower/internal/calendar"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ansiRE matches ANSI escape sequences emitted by the markdown renderer.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestValidateModel_AlwaysOK(t *testing.T) {
	require.NoError(t, validateModel(&config.Config{}))
	require.NoError(t, validateModel(nil))
}

func TestEmitError_WritesErrorAndDoneEvents(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	require.NoError(t, emitError(enc, "boom"))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)

	var ev1, ev2 aiStreamEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &ev1))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &ev2))

	assert.Equal(t, "error", ev1.Type)
	assert.Equal(t, "boom", ev1.Error)
	assert.Equal(t, "done", ev2.Type)
}

func TestResolveGoogleOAuthConfig_FromEnv(t *testing.T) {
	t.Setenv("WATCHTOWER_GOOGLE_CLIENT_ID", "env-client-id")
	t.Setenv("WATCHTOWER_GOOGLE_CLIENT_SECRET", "env-secret")

	got := resolveGoogleOAuthConfig()
	assert.Equal(t, "env-client-id", got.ClientID)
	assert.Equal(t, "env-secret", got.ClientSecret)
}

func TestResolveGoogleOAuthConfig_FromBuildDefaults(t *testing.T) {
	t.Setenv("WATCHTOWER_GOOGLE_CLIENT_ID", "")
	t.Setenv("WATCHTOWER_GOOGLE_CLIENT_SECRET", "")

	prevID, prevSecret := calendar.DefaultGoogleClientID, calendar.DefaultGoogleClientSecret
	calendar.DefaultGoogleClientID = "build-id"
	calendar.DefaultGoogleClientSecret = "build-secret"
	defer func() {
		calendar.DefaultGoogleClientID, calendar.DefaultGoogleClientSecret = prevID, prevSecret
	}()

	got := resolveGoogleOAuthConfig()
	assert.Equal(t, "build-id", got.ClientID)
	assert.Equal(t, "build-secret", got.ClientSecret)
}

func TestPrintCalendarEvents_GroupsByDate(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	events := []db.CalendarEvent{
		{ID: "e1", CalendarID: "primary", Title: "Morning", StartTime: "2026-04-02T08:00:00Z", EndTime: "2026-04-02T09:00:00Z"},
		{ID: "e2", CalendarID: "primary", Title: "Afternoon", StartTime: "2026-04-02T14:00:00Z", EndTime: "2026-04-02T15:00:00Z"},
		{ID: "e3", CalendarID: "primary", Title: "Tomorrow", StartTime: "2026-04-03T10:00:00Z", EndTime: "2026-04-03T11:00:00Z"},
	}
	printCalendarEvents(cmd, events)

	out := buf.String()
	assert.Contains(t, out, "Morning")
	assert.Contains(t, out, "Afternoon")
	assert.Contains(t, out, "Tomorrow")

	// Two distinct date headers (one per day).
	dateLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Thu, Apr 2") || strings.HasPrefix(line, "Fri, Apr 3") {
			dateLines++
		}
	}
	assert.Equal(t, 2, dateLines, "expected one header per date, got: %q", out)
}

func TestPrintCalendarEvents_AllDay(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	events := []db.CalendarEvent{
		{ID: "e1", CalendarID: "primary", Title: "Holiday", IsAllDay: true,
			StartTime: "2026-04-02T00:00:00Z", EndTime: "2026-04-03T00:00:00Z"},
	}
	printCalendarEvents(cmd, events)
	assert.Contains(t, buf.String(), "All day")
	assert.Contains(t, buf.String(), "Holiday")
}

func TestPrintCalendarEvents_Empty(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	printCalendarEvents(cmd, nil)
	assert.Empty(t, buf.String())
}

func TestPrintBriefing_RendersSections(t *testing.T) {
	b := &db.Briefing{
		Date:         "2026-04-02",
		Role:         "Engineering Manager",
		Attention:    `[{"text":"Critical bug","priority":"high","reason":"prod down"}]`,
		YourDay:      `[{"text":"Review track","priority":"high","due_date":"2026-04-03"}]`,
		WhatHappened: `[{"text":"Migration shipped","item_type":"digest","channel_name":"eng"}]`,
		TeamPulse:    `[{"text":"Bob is blocked","signal_type":"red_flag","detail":"awaiting design"}]`,
		Coaching:     `[{"text":"Schedule 1:1 with Alice","category":"agenda"}]`,
		CreatedAt:    "2026-04-02T08:00:00Z",
	}
	var buf bytes.Buffer
	printBriefing(&buf, b)

	// Renderer wraps text in ANSI sequences and may split adjacent words across
	// segments — strip ANSI and collapse whitespace before substring matching.
	clean := strings.Join(strings.Fields(stripANSI(buf.String())), " ")
	for _, expected := range []string{
		"2026-04-02",
		"Engineering Manager",
		"Attention",
		"Critical bug",
		"prod down",
		"Your Day",
		"Review track",
		"due:",
		"What Happened",
		"Migration shipped",
		"eng",
		"Team Pulse",
		"Bob is blocked",
		"red_flag",
		"Coaching Tips",
		"Schedule 1:1 with Alice",
		"agenda",
	} {
		assert.Contains(t, clean, expected, "missing %q in briefing output", expected)
	}
}

func TestPrintBriefing_SkipsEmptySections(t *testing.T) {
	b := &db.Briefing{
		Date:         "2026-04-02",
		Attention:    `[]`,
		YourDay:      `[]`,
		WhatHappened: `[]`,
		TeamPulse:    `[]`,
		Coaching:     `[]`,
	}
	var buf bytes.Buffer
	printBriefing(&buf, b)

	clean := strings.Join(strings.Fields(stripANSI(buf.String())), " ")
	assert.Contains(t, clean, "Daily Briefing")
	assert.NotContains(t, clean, "Attention Required")
	assert.NotContains(t, clean, "Your Day")
	assert.NotContains(t, clean, "What Happened")
	assert.NotContains(t, clean, "Team Pulse")
	assert.NotContains(t, clean, "Coaching Tips")
}

func TestPrintBriefing_RoleOptional(t *testing.T) {
	b := &db.Briefing{
		Date:      "2026-04-02",
		Attention: "[]", YourDay: "[]", WhatHappened: "[]", TeamPulse: "[]", Coaching: "[]",
	}
	var buf bytes.Buffer
	printBriefing(&buf, b)
	assert.NotContains(t, buf.String(), "Role:")
}

func TestNeedsSync_FreshDB(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	logger := log.New(&bytes.Buffer{}, "", 0)
	assert.True(t, needsSync(database, logger))
}

func TestNeedsSync_StaleSync(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec(`INSERT INTO workspace (name, synced_at) VALUES ('test', '2020-01-01T00:00:00Z')`)
	require.NoError(t, err)

	logger := log.New(&bytes.Buffer{}, "", 0)
	assert.True(t, needsSync(database, logger), "old timestamp must trigger sync")
}

func TestNeedsSync_BadTimestamp(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec(`INSERT INTO workspace (name, synced_at) VALUES ('test', 'not a date')`)
	require.NoError(t, err)

	logger := log.New(&bytes.Buffer{}, "", 0)
	assert.True(t, needsSync(database, logger), "unparseable timestamp must trigger sync")
}

func TestPrintDigestTopics_RendersStructured(t *testing.T) {
	cfg := &config.Config{}
	topics := []db.DigestTopic{
		{
			Title:       "Deploy plan",
			Summary:     "Discussed Friday rollout.",
			Decisions:   `[{"text":"Ship Friday","by":"alice"},{"text":"No breaking changes"}]`,
			ActionItems: `[{"text":"Notify ops","assignee":"bob","status":"pending"}]`,
		},
	}
	var buf bytes.Buffer
	printDigestTopics(&buf, topics, cfg, nil)

	out := buf.String()
	assert.Contains(t, out, "### Deploy plan")
	assert.Contains(t, out, "Discussed Friday rollout.")
	assert.Contains(t, out, "**Decision:** Ship Friday (by alice)")
	assert.Contains(t, out, "**Decision:** No breaking changes")
	assert.Contains(t, out, "[pending] Notify ops -> bob")
}

func TestPrintDigestTopics_EmptyDecisionsAndActions(t *testing.T) {
	cfg := &config.Config{}
	topics := []db.DigestTopic{
		{Title: "Quiet topic", Summary: "Nothing happened.", Decisions: "[]", ActionItems: "[]"},
	}
	var buf bytes.Buffer
	printDigestTopics(&buf, topics, cfg, nil)

	out := buf.String()
	assert.Contains(t, out, "### Quiet topic")
	assert.Contains(t, out, "Nothing happened.")
	assert.NotContains(t, out, "**Decision:**")
	assert.NotContains(t, out, "[pending]")
}

func TestPrintDigestTopics_BadJSONIsSkipped(t *testing.T) {
	cfg := &config.Config{}
	topics := []db.DigestTopic{
		{Title: "Glitchy", Decisions: "not json", ActionItems: "also not json"},
	}
	var buf bytes.Buffer
	printDigestTopics(&buf, topics, cfg, nil)

	out := buf.String()
	assert.Contains(t, out, "### Glitchy")
	assert.NotContains(t, out, "**Decision:**")
}

func TestPrintDigestLegacy_RendersTopicsDecisionsActions(t *testing.T) {
	cfg := &config.Config{}
	d := db.Digest{
		Topics:      `["api","deploy"]`,
		Decisions:   `[{"text":"Ship Friday","by":"alice"},{"text":"No breaking changes"}]`,
		ActionItems: `[{"text":"Review PR","assignee":"bob","status":"pending"},{"text":"Notify ops"}]`,
	}
	var buf bytes.Buffer
	printDigestLegacy(&buf, d, cfg, nil)

	out := buf.String()
	assert.Contains(t, out, "Topics:")
	assert.Contains(t, out, "api, deploy")
	assert.Contains(t, out, "Decisions:")
	assert.Contains(t, out, "Ship Friday (by alice)")
	assert.Contains(t, out, "No breaking changes")
	assert.Contains(t, out, "Review PR")
	assert.Contains(t, out, "Notify ops")
}

func TestPrintDigestLegacy_EmptySectionsOmitted(t *testing.T) {
	cfg := &config.Config{}
	d := db.Digest{Topics: "[]", Decisions: "[]", ActionItems: "[]"}
	var buf bytes.Buffer
	printDigestLegacy(&buf, d, cfg, nil)

	assert.Empty(t, buf.String())
}

func TestPrintDigestLegacy_MalformedJSONIgnored(t *testing.T) {
	cfg := &config.Config{}
	d := db.Digest{Topics: "garbage", Decisions: "garbage", ActionItems: "garbage"}
	var buf bytes.Buffer
	printDigestLegacy(&buf, d, cfg, nil)

	// Non-JSON inputs are silently skipped — no panic and no output.
	assert.Empty(t, buf.String())
}
