package meeting

import (
	"context"
	"encoding/json"
	"testing"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGenerator implements digest.Generator for testing.
type mockGenerator struct {
	response string
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{}, "", nil
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func seedTestEvent(t *testing.T, database *db.DB) {
	t.Helper()
	require.NoError(t, database.UpsertCalendar(db.CalendarCalendar{
		ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z",
	}))
	require.NoError(t, database.UpsertCalendarEvent(db.CalendarEvent{
		ID:         "evt1",
		CalendarID: "primary",
		Title:      "1:1 with Alice",
		StartTime:  "2099-04-02T10:00:00Z",
		EndTime:    "2099-04-02T10:30:00Z",
		Attendees:  `[{"email":"alice@example.com","display_name":"Alice","slack_user_id":"U123"},{"email":"bob@example.com","display_name":"Bob"}]`,
	}))
}

func TestPrepareForEvent(t *testing.T) {
	database := openTestDB(t)
	seedTestEvent(t, database)

	mockResp := MeetingPrepResult{
		EventID: "evt1",
		Title:   "1:1 with Alice",
		TalkingPoints: []TalkingPoint{
			{Text: "Discuss project status", SourceType: "track", SourceID: "42", Priority: "high"},
		},
		PeopleNotes: []PersonNote{
			{UserID: "U123", Name: "Alice", CommunicationTip: "Prefers data-driven arguments"},
		},
		SuggestedPrep: []string{"Review track #42"},
	}
	respJSON, _ := json.Marshal(mockResp)

	gen := &mockGenerator{response: string(respJSON)}
	cfg := &config.Config{
		Digest: config.DigestConfig{Language: "English"},
	}
	pipe := New(database, cfg, gen, nil)

	result, err := pipe.PrepareForEvent(context.Background(), "evt1")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "evt1", result.EventID)
	assert.Equal(t, "1:1 with Alice", result.Title)
	assert.Len(t, result.TalkingPoints, 1)
	assert.Equal(t, "high", result.TalkingPoints[0].Priority)
	assert.Len(t, result.PeopleNotes, 1)
	assert.Len(t, result.SuggestedPrep, 1)
}

func TestPrepareForEvent_NotFound(t *testing.T) {
	database := openTestDB(t)
	gen := &mockGenerator{response: "{}"}
	cfg := &config.Config{}
	pipe := New(database, cfg, gen, nil)

	_, err := pipe.PrepareForEvent(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPrepareForNext(t *testing.T) {
	database := openTestDB(t)
	seedTestEvent(t, database)

	mockResp := MeetingPrepResult{
		EventID: "evt1",
		Title:   "1:1 with Alice",
	}
	respJSON, _ := json.Marshal(mockResp)

	gen := &mockGenerator{response: string(respJSON)}
	cfg := &config.Config{}
	pipe := New(database, cfg, gen, nil)

	result, err := pipe.PrepareForNext(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "evt1", result.EventID)
}

func TestPrepareForNext_NoEvents(t *testing.T) {
	database := openTestDB(t)
	gen := &mockGenerator{response: "{}"}
	cfg := &config.Config{}
	pipe := New(database, cfg, gen, nil)

	_, err := pipe.PrepareForNext(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no upcoming meetings")
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key": "val"}`, `{"key": "val"}`},
		{"```json\n{\"key\": \"val\"}\n```", `{"key": "val"}`},
		{"```\n{\"key\": \"val\"}\n```", `{"key": "val"}`},
		{"  {\"key\": \"val\"}  ", `{"key": "val"}`},
	}

	for _, tt := range tests {
		got := cleanJSON(tt.input)
		assert.Equal(t, tt.expected, got)
	}
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel...", truncate("hello world", 3))
}

func TestFormatProfile(t *testing.T) {
	assert.Equal(t, "", formatProfile(nil))

	p := &db.UserProfile{Role: "Engineering Manager", Team: "Platform"}
	result := formatProfile(p)
	assert.Contains(t, result, "Role: Engineering Manager")
	assert.Contains(t, result, "Team: Platform")
}
