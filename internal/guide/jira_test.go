package guide

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

func TestGatherJiraDelivery_Disabled(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{} // Jira not enabled
	result, err := gatherJiraDelivery(database, cfg, "U123", "2026-04-01", "2026-04-08")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGatherJiraDelivery_NoData(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{}
	cfg.Jira.Enabled = true
	cfg.Jira.Features.MyIssuesInBriefing = true

	result, err := gatherJiraDelivery(database, cfg, "U123", "2026-04-01", "2026-04-08")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGatherJiraDelivery_WithData(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{}
	cfg.Jira.Enabled = true
	cfg.Jira.Features.MyIssuesInBriefing = true

	// Insert resolved issues in the period.
	for i, key := range []string{"PROJ-1", "PROJ-2", "PROJ-3"} {
		err := database.UpsertJiraIssue(db.JiraIssue{
			Key:             key,
			ID:              key,
			ProjectKey:      "PROJ",
			Summary:         "Issue " + key,
			Status:          "Done",
			StatusCategory:  "done",
			AssigneeSlackID: "U123",
			Priority:        "Medium",
			ResolvedAt:      "2026-04-0" + string(rune('3'+i)),
			CreatedAt:       "2026-03-20",
			UpdatedAt:       "2026-04-05",
			Labels:          `["backend"]`,
			Components:      `["api"]`,
			SyncedAt:        "2026-04-07",
		})
		require.NoError(t, err)
	}

	// Insert an open issue.
	err := database.UpsertJiraIssue(db.JiraIssue{
		Key:             "PROJ-10",
		ID:              "PROJ-10",
		ProjectKey:      "PROJ",
		Summary:         "Open issue",
		Status:          "In Progress",
		StatusCategory:  "indeterminate",
		AssigneeSlackID: "U123",
		Priority:        "High",
		DueDate:         "2026-04-01", // overdue
		CreatedAt:       "2026-03-25",
		UpdatedAt:       "2026-04-06",
		Labels:          `[]`,
		Components:      `[]`,
		SyncedAt:        "2026-04-07",
	})
	require.NoError(t, err)

	result, err := gatherJiraDelivery(database, cfg, "U123", "2026-04-01", "2026-04-08")
	require.NoError(t, err)

	assert.Contains(t, result, "=== JIRA DELIVERY ===")
	assert.Contains(t, result, "Issues closed: 3")
	assert.Contains(t, result, "Open issues: 1")
	assert.Contains(t, result, "Overdue: 1")
	assert.Contains(t, result, "Expertise: [api, backend]")
	assert.Contains(t, result, "Recent accomplishments:")
	assert.Contains(t, result, "PROJ-1")
	assert.Contains(t, result, "overdue issue(s)")
}

func TestGatherJiraDelivery_MaxAccomplishments(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{}
	cfg.Jira.Enabled = true
	cfg.Jira.Features.MyIssuesInBriefing = true

	// Insert 15 resolved issues.
	for i := 0; i < 15; i++ {
		key := "PROJ-" + string(rune('A'+i))
		err := database.UpsertJiraIssue(db.JiraIssue{
			Key:             key,
			ID:              key,
			ProjectKey:      "PROJ",
			Summary:         "Issue " + key,
			Status:          "Done",
			StatusCategory:  "done",
			AssigneeSlackID: "U123",
			Priority:        "Medium",
			ResolvedAt:      "2026-04-05",
			CreatedAt:       "2026-03-20",
			UpdatedAt:       "2026-04-05",
			Labels:          `[]`,
			Components:      `[]`,
			SyncedAt:        "2026-04-07",
		})
		require.NoError(t, err)
	}

	result, err := gatherJiraDelivery(database, cfg, "U123", "2026-04-01", "2026-04-08")
	require.NoError(t, err)

	// Count "Resolved" lines — should be capped at maxAccomplishments.
	lines := strings.Split(result, "\n")
	resolvedCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- Resolved ") {
			resolvedCount++
		}
	}
	assert.LessOrEqual(t, resolvedCount, maxAccomplishments)
}

func TestBuildWorkloadSignals_Compound(t *testing.T) {
	stats := &db.DeliveryStats{
		OpenIssues:    12,
		OverdueIssues: 2,
	}
	signals := buildWorkloadSignals(stats)

	hasCompound := false
	for _, s := range signals {
		if strings.Contains(s, "burnout risk") {
			hasCompound = true
		}
	}
	assert.True(t, hasCompound, "expected compound burnout risk signal")
}

func TestBuildWorkloadSignals_NoSignals(t *testing.T) {
	stats := &db.DeliveryStats{
		OpenIssues:       3,
		OverdueIssues:    0,
		AvgCycleTimeDays: 5,
	}
	signals := buildWorkloadSignals(stats)
	assert.Empty(t, signals)
}
