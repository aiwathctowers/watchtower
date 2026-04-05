package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndGetCalendars(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertCalendar(CalendarCalendar{
		ID: "primary", Name: "Main", IsPrimary: true, IsSelected: true, Color: "#4285f4", SyncedAt: "2026-04-01T00:00:00Z",
	})
	require.NoError(t, err)

	err = db.UpsertCalendar(CalendarCalendar{
		ID: "work@example.com", Name: "Work", IsPrimary: false, IsSelected: true, Color: "#0b8043", SyncedAt: "2026-04-01T00:00:00Z",
	})
	require.NoError(t, err)

	cals, err := db.GetCalendars()
	require.NoError(t, err)
	assert.Len(t, cals, 2)
	// Primary first (ORDER BY is_primary DESC).
	assert.Equal(t, "primary", cals[0].ID)
	assert.True(t, cals[0].IsPrimary)
	assert.Equal(t, "work@example.com", cals[1].ID)
}

func TestUpsertCalendar_UpdatesOnConflict(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertCalendar(CalendarCalendar{ID: "cal1", Name: "Old Name", IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z"})
	require.NoError(t, err)

	err = db.UpsertCalendar(CalendarCalendar{ID: "cal1", Name: "New Name", IsSelected: true, SyncedAt: "2026-04-02T00:00:00Z"})
	require.NoError(t, err)

	cals, err := db.GetCalendars()
	require.NoError(t, err)
	assert.Len(t, cals, 1)
	assert.Equal(t, "New Name", cals[0].Name)
	assert.Equal(t, "2026-04-02T00:00:00Z", cals[0].SyncedAt)
}

func TestGetSelectedCalendarIDs(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal1", Name: "C1", IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z"}))
	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal2", Name: "C2", IsSelected: false, SyncedAt: "2026-04-01T00:00:00Z"}))
	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal3", Name: "C3", IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z"}))

	ids, err := db.GetSelectedCalendarIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "cal1")
	assert.Contains(t, ids, "cal3")
}

func TestSetCalendarSelected(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal1", Name: "C1", IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z"}))

	err := db.SetCalendarSelected("cal1", false)
	require.NoError(t, err)

	ids, err := db.GetSelectedCalendarIDs()
	require.NoError(t, err)
	assert.Empty(t, ids)

	err = db.SetCalendarSelected("cal1", true)
	require.NoError(t, err)

	ids, err = db.GetSelectedCalendarIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 1)
}

func TestUpsertAndGetCalendarEvents(t *testing.T) {
	db := openTestDB(t)

	// Need a calendar first (foreign key).
	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))

	ev := CalendarEvent{
		ID:             "evt1",
		CalendarID:     "primary",
		Title:          "Team Standup",
		Description:    "Daily standup",
		Location:       "Room 42",
		StartTime:      "2026-04-02T09:00:00Z",
		EndTime:        "2026-04-02T09:30:00Z",
		OrganizerEmail: "alice@example.com",
		Attendees:      `[{"email":"bob@example.com"}]`,
		IsRecurring:    true,
		IsAllDay:       false,
		EventStatus:    "confirmed",
		EventType:      "default",
		HTMLLink:       "https://calendar.google.com/event?id=evt1",
		RawJSON:        `{"id":"evt1"}`,
		UpdatedAt:      "2026-04-01T12:00:00Z",
	}

	err := db.UpsertCalendarEvent(ev)
	require.NoError(t, err)

	got, err := db.GetCalendarEventByID("evt1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Team Standup", got.Title)
	assert.Equal(t, "Daily standup", got.Description)
	assert.Equal(t, "Room 42", got.Location)
	assert.Equal(t, "2026-04-02T09:00:00Z", got.StartTime)
	assert.Equal(t, "alice@example.com", got.OrganizerEmail)
	assert.True(t, got.IsRecurring)
	assert.Equal(t, "default", got.EventType)
}

func TestGetCalendarEventByID_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetCalendarEventByID("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetCalendarEvents_Filter(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal1", Name: "C1", SyncedAt: "2026-04-01T00:00:00Z"}))
	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "cal2", Name: "C2", SyncedAt: "2026-04-01T00:00:00Z"}))

	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e1", CalendarID: "cal1", Title: "Morning", StartTime: "2026-04-02T08:00:00Z", EndTime: "2026-04-02T09:00:00Z"}))
	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e2", CalendarID: "cal1", Title: "Afternoon", StartTime: "2026-04-02T14:00:00Z", EndTime: "2026-04-02T15:00:00Z"}))
	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e3", CalendarID: "cal2", Title: "Other Cal", StartTime: "2026-04-02T10:00:00Z", EndTime: "2026-04-02T11:00:00Z"}))

	// All events.
	all, err := db.GetCalendarEvents(CalendarEventFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Filter by calendar.
	cal1Events, err := db.GetCalendarEvents(CalendarEventFilter{CalendarID: "cal1"})
	require.NoError(t, err)
	assert.Len(t, cal1Events, 2)

	// Filter by time range.
	morning, err := db.GetCalendarEvents(CalendarEventFilter{
		FromTime: "2026-04-02T07:00:00Z",
		ToTime:   "2026-04-02T09:30:00Z",
	})
	require.NoError(t, err)
	assert.Len(t, morning, 1)
	assert.Equal(t, "Morning", morning[0].Title)

	// Limit.
	limited, err := db.GetCalendarEvents(CalendarEventFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

func TestGetCalendarEventsForDate(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))

	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e1", CalendarID: "primary", Title: "Today", StartTime: "2026-04-02T10:00:00Z", EndTime: "2026-04-02T11:00:00Z"}))
	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e2", CalendarID: "primary", Title: "Tomorrow", StartTime: "2026-04-03T10:00:00Z", EndTime: "2026-04-03T11:00:00Z"}))

	events, err := db.GetCalendarEventsForDate("2026-04-02")
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "Today", events[0].Title)
}

func TestGetNextEvent(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))

	// Event in the far future (should be returned).
	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "future", CalendarID: "primary", Title: "Future Event", StartTime: "2099-01-01T10:00:00Z", EndTime: "2099-01-01T11:00:00Z"}))

	ev, err := db.GetNextEvent()
	require.NoError(t, err)
	require.NotNil(t, ev)
	assert.Equal(t, "Future Event", ev.Title)
}

func TestUpsertCalendarEvents_Batch(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))

	events := []CalendarEvent{
		{ID: "b1", CalendarID: "primary", Title: "Event 1", StartTime: "2026-04-02T08:00:00Z", EndTime: "2026-04-02T09:00:00Z"},
		{ID: "b2", CalendarID: "primary", Title: "Event 2", StartTime: "2026-04-02T10:00:00Z", EndTime: "2026-04-02T11:00:00Z"},
	}

	err := db.UpsertCalendarEvents(events)
	require.NoError(t, err)

	all, err := db.GetCalendarEvents(CalendarEventFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestDeleteStaleCalendarEvents(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))

	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "old", CalendarID: "primary", Title: "Old", StartTime: "2026-04-02T08:00:00Z", EndTime: "2026-04-02T09:00:00Z"}))

	// Delete events synced before a future timestamp (should delete all).
	n, err := db.DeleteStaleCalendarEvents("primary", "2099-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	all, err := db.GetCalendarEvents(CalendarEventFilter{})
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestClearCalendarEvents(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertCalendar(CalendarCalendar{ID: "primary", Name: "Main", SyncedAt: "2026-04-01T00:00:00Z"}))
	require.NoError(t, db.UpsertCalendarEvent(CalendarEvent{ID: "e1", CalendarID: "primary", Title: "E1", StartTime: "2026-04-02T08:00:00Z", EndTime: "2026-04-02T09:00:00Z"}))
	require.NoError(t, db.UpsertAttendeeMap("alice@example.com", "U123"))

	err := db.ClearCalendarEvents()
	require.NoError(t, err)

	cals, _ := db.GetCalendars()
	assert.Empty(t, cals)

	events, _ := db.GetCalendarEvents(CalendarEventFilter{})
	assert.Empty(t, events)

	m, _ := db.GetAttendeeMap()
	assert.Empty(t, m)
}

func TestAttendeeMap(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertAttendeeMap("alice@example.com", "U123")
	require.NoError(t, err)

	err = db.UpsertAttendeeMap("bob@example.com", "U456")
	require.NoError(t, err)

	// Get full map.
	m, err := db.GetAttendeeMap()
	require.NoError(t, err)
	assert.Len(t, m, 2)
	assert.Equal(t, "U123", m["alice@example.com"])
	assert.Equal(t, "U456", m["bob@example.com"])

	// Get by email.
	uid, err := db.GetSlackUserIDByEmail("alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, "U123", uid)

	// Overwrite.
	err = db.UpsertAttendeeMap("alice@example.com", "U999")
	require.NoError(t, err)

	uid, err = db.GetSlackUserIDByEmail("alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, "U999", uid)
}
