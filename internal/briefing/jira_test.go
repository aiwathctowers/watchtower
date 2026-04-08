package briefing

import (
	"io"
	"log"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jiraTestConfig() *config.Config {
	return &config.Config{
		Digest:   config.DigestConfig{Enabled: true, Language: "English"},
		Briefing: config.BriefingConfig{Enabled: true, Hour: 8},
		Jira: config.JiraConfig{
			Enabled: true,
			Features: config.JiraFeatureToggles{
				MyIssuesInBriefing: true,
				AwaitingMyInput:    true,
				IterationProgress:  true,
			},
		},
	}
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// --- gatherJiraContext ---

func TestGatherJiraContext_Disabled(t *testing.T) {
	database := testDB(t)
	cfg := jiraTestConfig()
	cfg.Jira.Enabled = false

	pipe := New(database, cfg, &mockGenerator{}, discardLogger())
	result := pipe.gatherJiraContext("U001")
	assert.Equal(t, "", result)
}

func TestGatherJiraContext_EmptyData(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))

	cfg := jiraTestConfig()
	pipe := New(database, cfg, &mockGenerator{}, discardLogger())
	result := pipe.gatherJiraContext("U001")
	assert.Equal(t, "", result)
}

// --- formatMyIssues ---

func TestFormatMyIssues_Empty(t *testing.T) {
	result := formatMyIssues(nil)
	assert.Equal(t, "", result)
}

func TestFormatMyIssues_WithIssues(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "PROJ-1", Summary: "Fix login bug", Status: "In Progress", StatusCategory: "in_progress", Priority: "High"},
		{Key: "PROJ-2", Summary: "Add tests", Status: "To Do", StatusCategory: "todo", Priority: "Medium"},
	}
	result := formatMyIssues(issues)
	assert.Contains(t, result, "PROJ-1")
	assert.Contains(t, result, "Fix login bug")
	assert.Contains(t, result, "PROJ-2")
	assert.Contains(t, result, "Add tests")
}

// --- stale detection ---

func TestGatherStaleAndOverdue_StaleIssue(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	// Changed 10 days ago — should be stale.
	changedAt := now.AddDate(0, 0, -10).Format(time.RFC3339)

	issues := []db.JiraIssue{
		{
			Key:                     "PROJ-10",
			Summary:                 "Stale feature",
			Status:                  "In Progress",
			StatusCategory:          "in_progress",
			StatusCategoryChangedAt: changedAt,
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.Contains(t, result, "STALE JIRA ISSUES")
	assert.Contains(t, result, "PROJ-10")
}

func TestGatherStaleAndOverdue_NotStaleYet(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	// Changed 3 days ago — not stale yet.
	changedAt := now.AddDate(0, 0, -3).Format(time.RFC3339)

	issues := []db.JiraIssue{
		{
			Key:                     "PROJ-11",
			Summary:                 "Recent work",
			Status:                  "In Progress",
			StatusCategory:          "in_progress",
			StatusCategoryChangedAt: changedAt,
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.NotContains(t, result, "STALE")
	assert.NotContains(t, result, "PROJ-11")
}

// --- overdue detection ---

func TestGatherStaleAndOverdue_OverdueIssue(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	issues := []db.JiraIssue{
		{
			Key:            "PROJ-20",
			Summary:        "Overdue task",
			Status:         "To Do",
			StatusCategory: "todo",
			DueDate:        "2026-04-01",
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.Contains(t, result, "OVERDUE JIRA ISSUES")
	assert.Contains(t, result, "PROJ-20")
}

func TestGatherStaleAndOverdue_OverdueWithTimestamp(t *testing.T) {
	// M2 fix: DueDate with time component should still work.
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	issues := []db.JiraIssue{
		{
			Key:            "PROJ-21",
			Summary:        "Overdue with timestamp",
			Status:         "In Progress",
			StatusCategory: "in_progress",
			DueDate:        "2026-04-01T10:00:00Z",
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.Contains(t, result, "OVERDUE JIRA ISSUES")
	assert.Contains(t, result, "PROJ-21")
}

func TestGatherStaleAndOverdue_NotOverdue(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	issues := []db.JiraIssue{
		{
			Key:            "PROJ-22",
			Summary:        "Future task",
			Status:         "To Do",
			StatusCategory: "todo",
			DueDate:        "2026-04-15",
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.NotContains(t, result, "OVERDUE")
}

func TestGatherStaleAndOverdue_DoneNotOverdue(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	issues := []db.JiraIssue{
		{
			Key:            "PROJ-23",
			Summary:        "Done task with past due",
			Status:         "Done",
			StatusCategory: "done",
			DueDate:        "2026-04-01",
		},
	}

	result := gatherStaleAndOverdueAt(issues, now, discardLogger())
	assert.Equal(t, "", result)
}

func TestGatherStaleAndOverdue_EmptyIssues(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	result := gatherStaleAndOverdueAt(nil, now, discardLogger())
	assert.Equal(t, "", result)
}
