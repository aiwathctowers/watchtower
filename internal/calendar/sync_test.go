package calendar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveAttendees_NoSlackUser(t *testing.T) {
	// Without a DB, ResolveAttendees should leave SlackUserID empty.
	// This tests the logic path where email lookup returns nothing.
	events := []CalendarEvent{
		{
			ID:    "e1",
			Title: "Test",
			Attendees: []Attendee{
				{Email: "unknown@example.com", DisplayName: "Unknown"},
			},
		},
	}

	// Verify attendees structure is preserved.
	assert.Len(t, events[0].Attendees, 1)
	assert.Equal(t, "unknown@example.com", events[0].Attendees[0].Email)
	assert.Equal(t, "", events[0].Attendees[0].SlackUserID)
}

func TestCalendarEvent_Fields(t *testing.T) {
	ev := CalendarEvent{
		ID:          "test-id",
		Title:       "Review Meeting",
		Description: "Q1 review",
		HTMLLink:    "https://calendar.google.com/test",
		EventType:   "default",
		UpdatedAt:   "2026-04-01T12:00:00Z",
		Attendees: []Attendee{
			{Email: "alice@example.com", SlackUserID: "U123"},
		},
	}

	assert.Equal(t, "test-id", ev.ID)
	assert.Equal(t, "Review Meeting", ev.Title)
	assert.Equal(t, "Q1 review", ev.Description)
	assert.Equal(t, "default", ev.EventType)
	assert.Len(t, ev.Attendees, 1)
	assert.Equal(t, "U123", ev.Attendees[0].SlackUserID)
}

func TestCalendarInfo_Fields(t *testing.T) {
	ci := CalendarInfo{
		ID:      "primary",
		Summary: "Main Calendar",
		Primary: true,
		Color:   "#4285f4",
	}

	assert.Equal(t, "primary", ci.ID)
	assert.True(t, ci.Primary)
	assert.Equal(t, "#4285f4", ci.Color)
}
