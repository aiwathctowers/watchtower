package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/dayplan"
	"watchtower/internal/db"
	"watchtower/internal/digest"
)

// TestCLI_DayPlanShow verifies that show prints the plan header and items.
func TestCLI_DayPlanShow(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	planID, err := database.CreateDayPlan(&db.DayPlan{
		UserID:          "U001",
		PlanDate:        "2026-04-23",
		Status:          "active",
		GeneratedAt:     time.Now(),
		FeedbackHistory: "[]",
	})
	require.NoError(t, err)

	require.NoError(t, database.CreateDayPlanItems(planID, []db.DayPlanItem{
		{DayPlanID: planID, Kind: "backlog", SourceType: "manual", Title: "Drink water", Status: "pending", Tags: "[]"},
	}))
	database.Close()

	var buf bytes.Buffer
	dayPlanShowCmd.SetOut(&buf)

	err = dayPlanShowCmd.RunE(dayPlanShowCmd, []string{"2026-04-23"})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Day Plan")
	assert.Contains(t, out, "Drink water")
}

// TestCLI_DayPlanShow_NoExist verifies the "no plan" message.
func TestCLI_DayPlanShow_NoExist(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()

	var buf bytes.Buffer
	dayPlanShowCmd.SetOut(&buf)

	err = dayPlanShowCmd.RunE(dayPlanShowCmd, []string{"2026-01-01"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No day plan for")
}

// TestCLI_DayPlanList verifies list output and --limit flag.
func TestCLI_DayPlanList(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	for _, d := range []string{"2026-04-21", "2026-04-22", "2026-04-23"} {
		_, err := database.CreateDayPlan(&db.DayPlan{
			UserID:          "U001",
			PlanDate:        d,
			Status:          "active",
			GeneratedAt:     time.Now(),
			FeedbackHistory: "[]",
		})
		require.NoError(t, err)
	}
	database.Close()

	var buf bytes.Buffer
	dayPlanListCmd.SetOut(&buf)
	// Set limit flag to 2.
	require.NoError(t, dayPlanListCmd.Flags().Set("limit", "2"))

	err = dayPlanListCmd.RunE(dayPlanListCmd, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "2026-04-23")
	assert.Contains(t, out, "2026-04-22")
	assert.NotContains(t, out, "2026-04-21")

	// Reset flag for other tests.
	require.NoError(t, dayPlanListCmd.Flags().Set("limit", "7"))
}

// TestCLI_DayPlanReset verifies that reset deletes the plan from the DB.
func TestCLI_DayPlanReset(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	planID, err := database.CreateDayPlan(&db.DayPlan{
		UserID:          "U001",
		PlanDate:        "2026-04-23",
		Status:          "active",
		GeneratedAt:     time.Now(),
		FeedbackHistory: "[]",
	})
	require.NoError(t, err)
	require.NoError(t, database.CreateDayPlanItems(planID, []db.DayPlanItem{
		{DayPlanID: planID, Kind: "backlog", SourceType: "manual", Title: "x", Status: "pending", Tags: "[]"},
	}))
	database.Close()

	var buf bytes.Buffer
	dayPlanResetCmd.SetOut(&buf)

	err = dayPlanResetCmd.RunE(dayPlanResetCmd, []string{"2026-04-23"})
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Deleted day plan for 2026-04-23")

	// Verify plan is gone.
	database, err = openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	plan, err := database.GetDayPlan("U001", "2026-04-23")
	require.NoError(t, err)
	assert.Nil(t, plan)
}

// TestCLI_DayPlanCheckConflicts verifies conflict detection updates the plan.
func TestCLI_DayPlanCheckConflicts(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	today := time.Now().Format("2006-01-02")
	planID, err := database.CreateDayPlan(&db.DayPlan{
		UserID:          "U001",
		PlanDate:        today,
		Status:          "active",
		GeneratedAt:     time.Now(),
		FeedbackHistory: "[]",
	})
	require.NoError(t, err)

	start := time.Now().Truncate(time.Hour)
	require.NoError(t, database.CreateDayPlanItems(planID, []db.DayPlanItem{
		{
			DayPlanID:  planID,
			Kind:       "timeblock",
			SourceType: "focus",
			Title:      "Focus Block",
			StartTime:  sql.NullTime{Time: start, Valid: true},
			EndTime:    sql.NullTime{Time: start.Add(time.Hour), Valid: true},
			Status:     "pending",
			Tags:       "[]",
		},
	}))

	// Calendar parent required by FK.
	require.NoError(t, database.UpsertCalendar(db.CalendarCalendar{
		ID: "c1", Name: "Primary", IsPrimary: true,
	}))

	// Calendar event overlapping the timeblock.
	require.NoError(t, database.UpsertCalendarEvent(db.CalendarEvent{
		ID:         "e1",
		CalendarID: "c1",
		Title:      "Meeting",
		StartTime:  start.Add(15 * time.Minute).UTC().Format(time.RFC3339),
		EndTime:    start.Add(45 * time.Minute).UTC().Format(time.RFC3339),
		Attendees:  "[]",
	}))
	database.Close()

	var buf bytes.Buffer
	dayPlanCheckConflictsCmd.SetOut(&buf)

	err = dayPlanCheckConflictsCmd.RunE(dayPlanCheckConflictsCmd, []string{today})
	require.NoError(t, err)

	// Verify DB state.
	database, err = openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	plan, err := database.GetDayPlan("U001", today)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.True(t, plan.HasConflicts)
}

// TestCLI_DayPlanCommandRegistered verifies the command is on rootCmd.
func TestCLI_DayPlanCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "day-plan" {
			found = true
			break
		}
	}
	assert.True(t, found, "day-plan command should be registered on rootCmd")
}

// TestFormatDayPlanShow_Timeblocks verifies timeline section rendering.
func TestFormatDayPlanShow_Timeblocks(t *testing.T) {
	start := time.Date(2026, 4, 23, 9, 0, 0, 0, time.Local)
	end := time.Date(2026, 4, 23, 9, 30, 0, 0, time.Local)

	plan := &db.DayPlan{
		PlanDate:        "2026-04-23",
		GeneratedAt:     time.Date(2026, 4, 23, 8, 3, 0, 0, time.Local),
		FeedbackHistory: "[]",
	}
	items := []db.DayPlanItem{
		{
			Kind:       "timeblock",
			SourceType: "calendar",
			Title:      "Standup",
			StartTime:  sql.NullTime{Time: start, Valid: true},
			EndTime:    sql.NullTime{Time: end, Valid: true},
			Status:     "pending",
		},
	}

	out := formatDayPlanShow(plan, items)
	assert.Contains(t, out, "TIMELINE")
	assert.Contains(t, out, "Standup")
	assert.Contains(t, out, "09:00")
}

// TestFormatDayPlanShow_Conflicts verifies conflict banner rendering.
func TestFormatDayPlanShow_Conflicts(t *testing.T) {
	plan := &db.DayPlan{
		PlanDate:        "2026-04-23",
		GeneratedAt:     time.Now(),
		HasConflicts:    true,
		ConflictSummary: sql.NullString{String: "Focus overlaps Meeting", Valid: true},
		FeedbackHistory: "[]",
	}
	out := formatDayPlanShow(plan, nil)
	assert.Contains(t, out, "Conflicts:")
	assert.Contains(t, out, "Focus overlaps Meeting")
}

// dayPlanMockGen is a mock digest.Generator for CLI day-plan generate tests.
type dayPlanMockGen struct{ response string }

func (m *dayPlanMockGen) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{InputTokens: 10, OutputTokens: 5}, "s1", nil
}

// TestCLI_DayPlanGenerate_JSON verifies that generate --json produces valid JSON
// with a plan and at least one item.
func TestCLI_DayPlanGenerate_JSON(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	// Seed a target so the backlog item source_id is valid.
	_, err = database.CreateTarget(db.Target{
		Text:        "Write tests",
		Level:       "day",
		PeriodStart: "2026-04-23",
		PeriodEnd:   "2026-04-23",
		Priority:    "medium",
		Status:      "todo",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	require.NoError(t, err)
	database.Close()

	mockResp := `{"timeblocks":[],"backlog":[{"source_type":"task","source_id":"1","title":"Write tests","description":"get it done","rationale":"important","priority":"medium"}],"summary":"ok"}`

	oldFactory := newDayPlanPipelineFactory
	t.Cleanup(func() { newDayPlanPipelineFactory = oldFactory })
	newDayPlanPipelineFactory = func(d *db.DB, c *config.Config, l *log.Logger) (*dayplan.Pipeline, error) {
		return dayplan.New(d, c, &dayPlanMockGen{response: mockResp}, l), nil
	}

	var buf bytes.Buffer
	dayPlanGenerateCmd.SetOut(&buf)
	require.NoError(t, dayPlanGenerateCmd.Flags().Set("date", "2026-04-23"))
	require.NoError(t, dayPlanGenerateCmd.Flags().Set("json", "true"))

	err = dayPlanGenerateCmd.RunE(dayPlanGenerateCmd, nil)
	require.NoError(t, err)

	var payload struct {
		Plan  *db.DayPlan      `json:"plan"`
		Items []db.DayPlanItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, "2026-04-23", payload.Plan.PlanDate)
	assert.NotEmpty(t, payload.Items)

	// Reset flags for other tests.
	require.NoError(t, dayPlanGenerateCmd.Flags().Set("date", ""))
	require.NoError(t, dayPlanGenerateCmd.Flags().Set("json", "false"))
}
