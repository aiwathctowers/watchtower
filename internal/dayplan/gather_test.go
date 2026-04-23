package dayplan

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func gatherTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func testPipeline(d *db.DB) *Pipeline {
	return &Pipeline{db: d}
}

// TestGatherTasks_OnlyActive seeds 3 tasks (todo, in_progress, done) and
// verifies that gatherTasks returns only the 2 active ones.
func TestGatherTasks_OnlyActive(t *testing.T) {
	d := gatherTestDB(t)
	p := testPipeline(d)

	tasks := []db.Task{
		{Text: "todo task", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual", Tags: "[]", SubItems: "[]", Notes: "[]"},
		{Text: "in_progress task", Status: "in_progress", Priority: "high", Ownership: "mine", SourceType: "manual", Tags: "[]", SubItems: "[]", Notes: "[]"},
		{Text: "done task", Status: "done", Priority: "low", Ownership: "mine", SourceType: "manual", Tags: "[]", SubItems: "[]", Notes: "[]"},
	}
	for _, tk := range tasks {
		_, err := d.CreateTask(tk)
		require.NoError(t, err)
	}

	got, err := p.gatherTasks()
	require.NoError(t, err)
	require.Len(t, got, 2, "expected 2 active tasks (todo + in_progress)")

	statuses := map[string]bool{}
	for _, tk := range got {
		statuses[tk.Status] = true
	}
	require.True(t, statuses["todo"], "expected todo task in results")
	require.True(t, statuses["in_progress"], "expected in_progress task in results")
}

// TestGatherBriefing_FallbackYesterday seeds yesterday's briefing only and
// verifies that gatherBriefing(userID, today) returns yesterday's row.
func TestGatherBriefing_FallbackYesterday(t *testing.T) {
	d := gatherTestDB(t)
	p := testPipeline(d)

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	_, err := d.UpsertBriefing(db.Briefing{
		WorkspaceID:  "W1",
		UserID:       "U1",
		Date:         yesterday,
		Role:         "engineer",
		Attention:    "[]",
		YourDay:      "[]",
		WhatHappened: "[]",
		TeamPulse:    "[]",
		Coaching:     "[]",
	})
	require.NoError(t, err)

	got := p.gatherBriefing("U1", today)
	require.NotNil(t, got, "expected fallback to yesterday's briefing")
	require.Equal(t, yesterday, got.Date)
	require.Equal(t, "U1", got.UserID)
}

// TestGatherCalendarEvents_Today seeds 1 event starting today at 10:00 and
// verifies gatherCalendarEvents(today) returns that event.
func TestGatherCalendarEvents_Today(t *testing.T) {
	d := gatherTestDB(t)
	p := testPipeline(d)

	today := time.Now().UTC().Format("2006-01-02")

	// Calendar events have a FK to calendar_calendars; insert a parent calendar first.
	require.NoError(t, d.UpsertCalendar(db.CalendarCalendar{
		ID:        "cal-001",
		Name:      "Primary",
		IsPrimary: true,
	}))

	ev := db.CalendarEvent{
		ID:         "evt-001",
		CalendarID: "cal-001",
		Title:      "Morning standup",
		StartTime:  today + "T10:00:00Z",
		EndTime:    today + "T10:30:00Z",
		Attendees:  "[]",
	}
	require.NoError(t, d.UpsertCalendarEvent(ev))

	got, err := p.gatherCalendarEvents(today)
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 event for today")
	require.Equal(t, "evt-001", got[0].ID)
	require.Equal(t, "Morning standup", got[0].Title)
}

// TestGatherManualItems_FromExistingPlan seeds a plan with 1 manual + 1 focus
// item and verifies gatherManualItems returns only the manual item.
func TestGatherManualItems_FromExistingPlan(t *testing.T) {
	d := gatherTestDB(t)
	p := testPipeline(d)

	today := time.Now().UTC().Format("2006-01-02")
	plan := &db.DayPlan{
		UserID:      "U1",
		PlanDate:    today,
		Status:      "active",
		GeneratedAt: time.Now().UTC(),
	}
	planID, err := d.CreateDayPlan(plan)
	require.NoError(t, err)

	items := []db.DayPlanItem{
		{
			DayPlanID:  planID,
			Kind:       "backlog",
			SourceType: "manual",
			SourceID:   sql.NullString{},
			Title:      "Manual item",
			Priority:   sql.NullString{Valid: true, String: "medium"},
			Status:     "pending",
			Tags:       "[]",
		},
		{
			DayPlanID:  planID,
			Kind:       "timeblock",
			SourceType: "task",
			SourceID:   sql.NullString{Valid: true, String: "42"},
			Title:      "Focus item",
			Priority:   sql.NullString{Valid: true, String: "high"},
			Status:     "pending",
			Tags:       "[]",
		},
	}
	require.NoError(t, d.CreateDayPlanItems(planID, items))

	got, err := p.gatherManualItems(planID)
	require.NoError(t, err)
	require.Len(t, got, 1, "expected only the manual item")
	require.Equal(t, "manual", got[0].SourceType)
	require.Equal(t, "Manual item", got[0].Title)
}
