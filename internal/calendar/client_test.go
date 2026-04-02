package calendar

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertEvent(t *testing.T) {
	item := googleEvent{
		ID:          "evt1",
		Status:      "confirmed",
		Summary:     "Team Meeting",
		Description: "Weekly sync",
		Location:    "Room 5",
		HTMLLink:    "https://calendar.google.com/evt1",
		EventType:   "default",
		Updated:     "2026-04-01T12:00:00Z",
		Start:       &googleTime{DateTime: "2026-04-02T09:00:00Z"},
		End:         &googleTime{DateTime: "2026-04-02T10:00:00Z"},
		Organizer:   &googlePerson{Email: "alice@example.com"},
		Attendees: []googlePerson{
			{Email: "alice@example.com", DisplayName: "Alice", Self: true, ResponseStatus: "accepted"},
			{Email: "bob@example.com", DisplayName: "Bob", ResponseStatus: "tentative"},
		},
		RecurringEventID: "recurring123",
	}

	ev := convertEvent(item, "primary")

	assert.Equal(t, "evt1", ev.ID)
	assert.Equal(t, "Team Meeting", ev.Title)
	assert.Equal(t, "Weekly sync", ev.Description)
	assert.Equal(t, "Room 5", ev.Location)
	assert.Equal(t, "primary", ev.CalendarID)
	assert.Equal(t, "alice@example.com", ev.Organizer)
	assert.Equal(t, "accepted", ev.ResponseStatus)
	assert.Equal(t, "confirmed", ev.EventStatus) // event lifecycle status, not user response
	assert.True(t, ev.Recurring)
	assert.False(t, ev.IsAllDay)
	assert.Len(t, ev.Attendees, 2)
	assert.Equal(t, "Bob", ev.Attendees[1].DisplayName)
	assert.Equal(t, "https://calendar.google.com/evt1", ev.HTMLLink)
	assert.Equal(t, "default", ev.EventType)

	expectedStart, _ := time.Parse(time.RFC3339, "2026-04-02T09:00:00Z")
	assert.Equal(t, expectedStart, ev.StartTime)
}

func TestConvertEvent_AllDay(t *testing.T) {
	item := googleEvent{
		ID:      "allday1",
		Summary: "Holiday",
		Start:   &googleTime{Date: "2026-04-02"},
		End:     &googleTime{Date: "2026-04-03"},
	}

	ev := convertEvent(item, "primary")

	assert.True(t, ev.IsAllDay)
	assert.Equal(t, 2, ev.StartTime.Day())
}

func TestConvertEvent_NoOrganizer(t *testing.T) {
	item := googleEvent{
		ID:      "noorg",
		Summary: "Solo Event",
		Start:   &googleTime{DateTime: "2026-04-02T09:00:00Z"},
		End:     &googleTime{DateTime: "2026-04-02T10:00:00Z"},
	}

	ev := convertEvent(item, "cal1")
	assert.Equal(t, "", ev.Organizer)
	assert.Equal(t, "accepted", ev.ResponseStatus) // default user response
	assert.Equal(t, "confirmed", ev.EventStatus)   // default event status
}

func TestSanitizeTitle(t *testing.T) {
	assert.Equal(t, "Team Meeting", sanitizeTitle("  Team Meeting  "))
	assert.Equal(t, "", sanitizeTitle(""))
}

func TestGoogleEventsListParsing(t *testing.T) {
	body := `{
		"items": [
			{"id": "e1", "summary": "Test Event", "start": {"dateTime": "2026-04-02T09:00:00Z"}, "end": {"dateTime": "2026-04-02T10:00:00Z"}},
			{"id": "e2", "status": "cancelled", "summary": "Cancelled"}
		],
		"nextPageToken": ""
	}`

	var result googleEventsList
	err := json.Unmarshal([]byte(body), &result)
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
	assert.Equal(t, "e1", result.Items[0].ID)
	assert.Equal(t, "cancelled", result.Items[1].Status)
	assert.Empty(t, result.NextPageToken)

	// Verify cancelled events would be skipped in convertEvent flow.
	var events []CalendarEvent
	for _, item := range result.Items {
		if item.Status == "cancelled" {
			continue
		}
		events = append(events, convertEvent(item, "primary"))
	}
	assert.Len(t, events, 1)
	assert.Equal(t, "Test Event", events[0].Title)
}

func TestGoogleCalendarListParsing(t *testing.T) {
	body := `{"items":[
		{"id":"primary","summary":"Main","primary":true,"backgroundColor":"#4285f4"},
		{"id":"work@example.com","summary":"Work","primary":false,"backgroundColor":"#0b8043"}
	]}`

	var result googleCalendarList
	err := json.Unmarshal([]byte(body), &result)
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
	assert.Equal(t, "primary", result.Items[0].ID)
	assert.True(t, result.Items[0].Primary)
	assert.Equal(t, "#4285f4", result.Items[0].BackgroundColor)
	assert.Equal(t, "work@example.com", result.Items[1].ID)
	assert.False(t, result.Items[1].Primary)
}
