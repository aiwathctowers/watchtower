package dayplan

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
)

// ── mock generator ─────────────────────────────────────────────────────────────

type mockGenerator struct{ response string }

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{InputTokens: 10, OutputTokens: 5}, "s1", nil
}

// ── helpers ────────────────────────────────────────────────────────────────────

func pipeTestCfg() *config.Config {
	return &config.Config{DayPlan: config.DayPlanConfig{
		Enabled:           true,
		Hour:              8,
		WorkingHoursStart: "09:00",
		WorkingHoursEnd:   "19:00",
		MaxTimeblocks:     3,
		MinBacklog:        3,
		MaxBacklog:        8,
	}}
}

func validResponse() string {
	return `{"timeblocks":[{"source_type":"focus","source_id":null,"title":"Deep work",` +
		`"description":"Block for PR review","rationale":"3 PRs stacked",` +
		`"start_time_local":"10:00","end_time_local":"11:30","priority":"high"}],` +
		`"backlog":[{"source_type":"focus","source_id":null,"title":"Task","description":"x","rationale":"y","priority":"medium"}],` +
		`"summary":"Focused morning"}`
}

func newTestPipeline(d *db.DB, gen digest.Generator) *Pipeline {
	return &Pipeline{
		db:        d,
		cfg:       pipeTestCfg(),
		generator: gen,
	}
}

// ── tests ──────────────────────────────────────────────────────────────────────

// TestRun_InitialGenerate seeds 1 active task id=42, mockGenerator returns
// validResponse, Run → plan created with 2 items (1 timeblock focus + 1 backlog task).
func TestRun_InitialGenerate(t *testing.T) {
	d := gatherTestDB(t)
	gen := &mockGenerator{response: validResponse()}
	p := newTestPipeline(d, gen)

	// Seed a task with id that we can predict. Insert via CreateTask.
	taskID, err := d.CreateTask(db.Task{
		Text: "Review PRs", Status: "todo", Priority: "high",
		Ownership: "mine", SourceType: "manual",
		Tags: "[]", SubItems: "[]", Notes: "[]",
	})
	require.NoError(t, err)
	_ = taskID // id=1 in fresh DB; AI response uses "42" which won't match but that's fine for this test

	today := time.Now().Format("2006-01-02")
	plan, err := p.Run(context.Background(), RunOptions{UserID: "U1", Date: today})
	require.NoError(t, err)
	require.NotNil(t, plan)

	items, err := d.GetDayPlanItems(plan.ID)
	require.NoError(t, err)
	require.Len(t, items, 2, "expected 1 timeblock + 1 backlog item")

	var timeblocks, backlogs int
	for _, it := range items {
		switch it.Kind {
		case "timeblock":
			timeblocks++
			require.Equal(t, "focus", it.SourceType)
		case "backlog":
			backlogs++
		}
	}
	require.Equal(t, 1, timeblocks)
	require.Equal(t, 1, backlogs)
}

// TestRun_SkipsIfExistsAndNotForced seeds an existing plan; Run (no Force, no
// Feedback) returns the existing plan without inserting new items.
func TestRun_SkipsIfExistsAndNotForced(t *testing.T) {
	d := gatherTestDB(t)
	gen := &mockGenerator{response: validResponse()}
	p := newTestPipeline(d, gen)

	today := time.Now().Format("2006-01-02")

	// Pre-seed an existing plan.
	existingID, err := d.CreateDayPlan(&db.DayPlan{
		UserID: "U1", PlanDate: today, Status: "active",
		GeneratedAt: time.Now(), FeedbackHistory: "[]",
	})
	require.NoError(t, err)

	plan, err := p.Run(context.Background(), RunOptions{UserID: "U1", Date: today})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Equal(t, existingID, plan.ID, "should return existing plan unchanged")

	// No items should have been created.
	items, err := d.GetDayPlanItems(existingID)
	require.NoError(t, err)
	require.Empty(t, items, "no items should be inserted on skip")
}

// TestRun_RegenerateWithFeedback_PreservesManual seeds an existing plan with
// 1 manual + 1 AI focus item; calls Run with Feedback; verifies the manual item
// is preserved, AI item is replaced, regenerate_count incremented, and
// feedback_history contains the feedback string.
func TestRun_RegenerateWithFeedback_PreservesManual(t *testing.T) {
	d := gatherTestDB(t)
	gen := &mockGenerator{response: validResponse()}
	p := newTestPipeline(d, gen)

	today := time.Now().Format("2006-01-02")

	planID, err := d.CreateDayPlan(&db.DayPlan{
		UserID: "U1", PlanDate: today, Status: "active",
		GeneratedAt: time.Now(), FeedbackHistory: "[]",
	})
	require.NoError(t, err)

	// Insert 1 manual + 1 AI focus item.
	err = d.CreateDayPlanItems(planID, []db.DayPlanItem{
		{
			DayPlanID: planID, Kind: "backlog", SourceType: "manual",
			Title: "Manual pinned task", Priority: sql.NullString{Valid: true, String: "medium"},
			Status: "pending", Tags: "[]",
		},
		{
			DayPlanID: planID, Kind: "timeblock", SourceType: "focus",
			Title: "Old AI focus", Priority: sql.NullString{Valid: true, String: "high"},
			Status: "pending", Tags: "[]",
		},
	})
	require.NoError(t, err)

	feedback := "разгрузи вечер"
	plan, err := p.Run(context.Background(), RunOptions{
		UserID:   "U1",
		Date:     today,
		Feedback: feedback,
	})
	require.NoError(t, err)
	require.NotNil(t, plan)

	items, err := d.GetDayPlanItems(plan.ID)
	require.NoError(t, err)

	// manual item preserved, "Old AI focus" replaced by new AI items.
	var manualCount int
	var oldAIFound bool
	for _, it := range items {
		if it.SourceType == "manual" {
			manualCount++
			require.Equal(t, "Manual pinned task", it.Title)
		}
		if it.Title == "Old AI focus" {
			oldAIFound = true
		}
	}
	require.Equal(t, 1, manualCount, "manual item must be preserved")
	require.False(t, oldAIFound, "old AI item must be replaced")

	// Reload plan to check regenerate_count and feedback_history.
	updated, err := d.GetDayPlanByID(plan.ID)
	require.NoError(t, err)
	require.Greater(t, updated.RegenerateCount, 0, "regenerate_count must be incremented")
	require.Contains(t, updated.FeedbackHistory, feedback, "feedback must appear in history")
}

// TestRun_GracefulInvalidAIResponse checks that when mockGenerator returns
// "not json at all", Run returns an error and NO plan row is created.
func TestRun_GracefulInvalidAIResponse(t *testing.T) {
	d := gatherTestDB(t)
	gen := &mockGenerator{response: "not json at all"}
	p := newTestPipeline(d, gen)

	today := time.Now().Format("2006-01-02")
	plan, err := p.Run(context.Background(), RunOptions{UserID: "U1", Date: today})
	require.Error(t, err, "should return error for invalid JSON")
	require.Nil(t, plan)

	// No plan row should have been created.
	existing, err2 := d.GetDayPlan("U1", today)
	require.NoError(t, err2)
	require.Nil(t, existing, "no plan row should be persisted on AI error")
}

// TestRun_DropsTimeblockOverlappingCalendar seeds a calendar event 10:00-10:45;
// mockGenerator proposes a 10:00-11:30 focus block; after Run that focus block
// is NOT persisted (dropped as conflict). Plan is still created (backlog items remain).
func TestRun_DropsTimeblockOverlappingCalendar(t *testing.T) {
	d := gatherTestDB(t)

	now := time.Now()
	today := now.Format("2006-01-02")

	// Build event times in local timezone so they overlap with local-parsed timeblocks.
	evStart := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local)
	evEnd := time.Date(now.Year(), now.Month(), now.Day(), 10, 45, 0, 0, time.Local)

	// Seed a calendar with an event 10:00-10:45 local.
	require.NoError(t, d.UpsertCalendar(db.CalendarCalendar{
		ID: "cal-001", Name: "Primary", IsPrimary: true,
	}))
	require.NoError(t, d.UpsertCalendarEvent(db.CalendarEvent{
		ID:         "evt-overlap",
		CalendarID: "cal-001",
		Title:      "Standup",
		StartTime:  evStart.Format(time.RFC3339),
		EndTime:    evEnd.Format(time.RFC3339),
		Attendees:  "[]",
	}))

	// AI proposes a 10:00-11:30 focus block which overlaps the calendar event.
	response := `{"timeblocks":[{"source_type":"focus","source_id":null,"title":"Conflict block",` +
		`"description":"Overlaps calendar","rationale":"bad","start_time_local":"10:00","end_time_local":"11:30","priority":"high"}],` +
		`"backlog":[{"source_type":"focus","source_id":null,"title":"Async task","description":"ok","rationale":"fine","priority":"low"}],` +
		`"summary":"test"}`

	gen := &mockGenerator{response: response}
	p := newTestPipeline(d, gen)

	plan, err := p.Run(context.Background(), RunOptions{UserID: "U1", Date: today})
	require.NoError(t, err)
	require.NotNil(t, plan, "plan should be created even if timeblock is dropped")

	items, err := d.GetDayPlanItems(plan.ID)
	require.NoError(t, err)

	for _, it := range items {
		// Calendar-sourced timeblocks are legitimate (added by syncCalendarItems).
		// Only AI-generated timeblocks that overlap the calendar event must be dropped.
		if it.Kind == "timeblock" && it.SourceType != "calendar" {
			t.Errorf("AI timeblock must not be persisted when it overlaps a calendar event, got item: %+v", it)
		}
	}

	// Backlog item should survive.
	var backlogCount int
	for _, it := range items {
		if it.Kind == "backlog" {
			backlogCount++
		}
	}
	require.Equal(t, 1, backlogCount, "backlog item should be persisted")
}
