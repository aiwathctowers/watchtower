package briefing

import (
	"io"
	"log"
	"strings"
	"testing"

	"watchtower/internal/db"
	"watchtower/internal/prompts"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccumulatedUsage_ZeroByDefault(t *testing.T) {
	p := &Pipeline{}
	in, out, cost, total := p.AccumulatedUsage()
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, float64(0), cost)
	assert.Equal(t, 0, total)
}

func TestAccumulatedUsage_AfterUpdate(t *testing.T) {
	p := &Pipeline{
		lastInputTokens:     10,
		lastOutputTokens:    20,
		lastTotalAPITokens:  30,
	}
	in, out, _, total := p.AccumulatedUsage()
	assert.Equal(t, 10, in)
	assert.Equal(t, 20, out)
	assert.Equal(t, 30, total)
}

func TestSetPromptStore_AssignsField(t *testing.T) {
	p := &Pipeline{}
	store := &prompts.Store{}
	p.SetPromptStore(store)
	assert.Same(t, store, p.promptStore)
}

func openMemoryDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestFormatCalendarEvent_TimedEventWithAttendees(t *testing.T) {
	database := openMemoryDB(t)

	require.NoError(t, database.UpsertUser(db.User{
		ID:          "U123",
		Name:        "alice",
		DisplayName: "Alice",
	}))

	ev := db.CalendarEvent{
		ID:        "e1",
		Title:     "Standup",
		StartTime: "2026-04-02T09:00:00Z",
		EndTime:   "2026-04-02T09:30:00Z",
		Attendees: `[{"email":"alice@x","display_name":"Alice DN","slack_user_id":"U123"},{"email":"bob@x","display_name":"Bob"}]`,
	}
	got := formatCalendarEvent(ev, database)
	assert.Contains(t, got, "Standup")
	// Has a time range like 09:00-09:30 (in local time).
	assert.Contains(t, got, "-")
	assert.Contains(t, got, "Alice")
	assert.Contains(t, got, "Bob")
}

func TestFormatCalendarEvent_AllDayEvent(t *testing.T) {
	database := openMemoryDB(t)
	ev := db.CalendarEvent{
		ID:        "e1",
		Title:     "Holiday",
		IsAllDay:  true,
		StartTime: "2026-04-02T00:00:00Z",
		EndTime:   "2026-04-03T00:00:00Z",
		Attendees: "[]",
	}
	got := formatCalendarEvent(ev, database)
	assert.Contains(t, got, "All day")
	assert.Contains(t, got, "Holiday")
}

func TestFormatCalendarEvent_AttendeeFallbackName(t *testing.T) {
	database := openMemoryDB(t)
	// User without DisplayName falls back to Name.
	require.NoError(t, database.UpsertUser(db.User{
		ID:   "U200",
		Name: "fred-name",
	}))

	ev := db.CalendarEvent{
		Title:     "Meeting",
		StartTime: "2026-04-02T09:00:00Z",
		EndTime:   "2026-04-02T09:30:00Z",
		Attendees: `[{"email":"fred@x","slack_user_id":"U200"}]`,
	}
	got := formatCalendarEvent(ev, database)
	assert.Contains(t, got, "fred-name")
}

func TestFormatCalendarEvent_BadAttendeeJSON(t *testing.T) {
	database := openMemoryDB(t)
	ev := db.CalendarEvent{
		Title:     "Meeting",
		StartTime: "2026-04-02T09:00:00Z",
		EndTime:   "2026-04-02T09:30:00Z",
		Attendees: "not json",
	}
	got := formatCalendarEvent(ev, database)
	// Bad JSON => no attendee names but still produces output.
	assert.Contains(t, got, "Meeting")
}

func newTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	database := openMemoryDB(t)
	return &Pipeline{
		db:     database,
		cfg:    nil,
		logger: log.New(io.Discard, "", 0),
	}
}

func TestGatherInbox_Empty(t *testing.T) {
	p := newTestPipeline(t)
	got, real := p.gatherInbox()
	assert.False(t, real)
	assert.Contains(t, got, "No pending inbox items")
}

func TestGatherInbox_WithItems(t *testing.T) {
	p := newTestPipeline(t)
	require.NoError(t, p.db.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := p.db.CreateInboxItem(db.InboxItem{
		ChannelID:    "C1",
		MessageTS:    "1.000001",
		SenderUserID: "U1",
		TriggerType:  "mention",
		Snippet:      "PR review needed",
		Status:       "pending",
		Priority:     "high",
		AIReason:     "blocking deploy",
	})
	require.NoError(t, err)

	got, real := p.gatherInbox()
	assert.True(t, real)
	assert.Contains(t, got, "@mention")
	assert.Contains(t, got, "PR review needed")
	assert.Contains(t, got, "blocking deploy")
}

func TestGatherInbox_DMTriggerType(t *testing.T) {
	p := newTestPipeline(t)
	require.NoError(t, p.db.UpsertChannel(db.Channel{ID: "C2", Name: "alice", Type: "dm"}))

	_, err := p.db.CreateInboxItem(db.InboxItem{
		ChannelID:    "C2",
		MessageTS:    "2.000001",
		SenderUserID: "U2",
		TriggerType:  "dm",
		Snippet:      "Got a sec?",
		Status:       "pending",
		Priority:     "medium",
	})
	require.NoError(t, err)

	got, real := p.gatherInbox()
	assert.True(t, real)
	assert.Contains(t, got, "DM")
	assert.Contains(t, got, "Got a sec?")
}

func TestGatherTracks_Empty(t *testing.T) {
	p := newTestPipeline(t)
	got, real := p.gatherTracks()
	assert.False(t, real)
	assert.Contains(t, got, "No active tracks yet")
}

func TestGatherDigests_BadDate(t *testing.T) {
	p := newTestPipeline(t)
	got := p.gatherDigests("not-a-date")
	assert.Empty(t, got)
}

func TestGatherDigests_NoDigests(t *testing.T) {
	p := newTestPipeline(t)
	got := p.gatherDigests("2026-04-02")
	assert.Empty(t, got)
}

func TestGatherDigests_FiltersMutedChannels(t *testing.T) {
	p := newTestPipeline(t)
	now := float64(1712025600) // 2024-04-02 in unix
	require.NoError(t, p.db.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, p.db.UpsertChannel(db.Channel{ID: "C2", Name: "muted", Type: "public"}))
	require.NoError(t, p.db.SetMuteForLLM("C2", true))

	for _, ch := range []string{"C1", "C2"} {
		_, err := p.db.UpsertDigest(db.Digest{
			ChannelID:    ch,
			PeriodFrom:   now,
			PeriodTo:     now + 3600,
			Type:         "channel",
			Summary:      "summary for " + ch,
			Topics:       "[]",
			Decisions:    "[]",
			ActionItems:  "[]",
			MessageCount: 5,
			Model:        "haiku",
		})
		require.NoError(t, err)
	}

	// Backdate to 2024 so it falls in the date window we'll query.
	got := p.gatherDigests("2024-04-02")
	if got != "" {
		assert.NotContains(t, got, "summary for C2", "muted channel should be filtered out")
	}
}

func TestGatherTracks_TruncatesLongContext(t *testing.T) {
	p := newTestPipeline(t)
	long := strings.Repeat("x", 300)

	_, err := p.db.UpsertTrack(db.Track{
		Text:         "Track A",
		Priority:     "high",
		Ownership:    "mine",
		Context:      long,
		Participants: `[{"user_id":"U1"}]`,
	})
	require.NoError(t, err)

	got, real := p.gatherTracks()
	assert.True(t, real)
	assert.Contains(t, got, "Track A")
	assert.Contains(t, got, "...")
}
