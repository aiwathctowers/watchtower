package jira

import (
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func releaseDashTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func releaseDashConfig(enabled bool) *config.Config {
	return &config.Config{
		Jira: config.JiraConfig{
			Enabled: true,
			Features: config.JiraFeatureToggles{
				ReleaseDashboard: enabled,
			},
		},
	}
}

func seedRelease(t *testing.T, database *db.DB, id int, projectKey, name, releaseDate string, released, archived bool) {
	t.Helper()
	err := database.UpsertJiraRelease(db.JiraRelease{
		ID:          id,
		ProjectKey:  projectKey,
		Name:        name,
		ReleaseDate: releaseDate,
		Released:    released,
		Archived:    archived,
		SyncedAt:    "2026-04-01T00:00:00Z",
	})
	require.NoError(t, err)
}

func seedReleaseIssue(t *testing.T, database *db.DB, key, epicKey, status, statusCategory, fixVersions, syncedAt string) {
	t.Helper()
	err := database.UpsertJiraIssue(db.JiraIssue{
		Key:            key,
		ID:             key,
		Summary:        "Issue " + key,
		EpicKey:        epicKey,
		Status:         status,
		StatusCategory: statusCategory,
		FixVersions:    fixVersions,
		SyncedAt:       syncedAt,
	})
	require.NoError(t, err)
}

// --- BuildReleaseDashboard tests ---

func TestBuildReleaseDashboard_DisabledFeature(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(false)

	result, err := BuildReleaseDashboard(database, cfg, "", time.Now())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDashboard_JiraDisabled(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := &config.Config{
		Jira: config.JiraConfig{Enabled: false},
	}

	result, err := BuildReleaseDashboard(database, cfg, "", time.Now())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDashboard_NoReleases(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", time.Now())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDashboard_FilterReleasedAndArchived(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Released release — should be filtered.
	seedRelease(t, database, 1, "PROJ", "v1.0", "2026-03-01", true, false)
	// Archived release — should be filtered.
	seedRelease(t, database, 2, "PROJ", "v0.5-beta", "2026-02-01", false, true)

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDashboard_BasicProgress(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v2.0", "2026-05-01", false, false)

	// 4 issues: 2 done, 1 in progress, 1 to do
	seedReleaseIssue(t, database, "PROJ-1", "EPIC-1", "Done", "done", `["v2.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "PROJ-2", "EPIC-1", "Done", "done", `["v2.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "PROJ-3", "EPIC-1", "In Progress", "in_progress", `["v2.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "PROJ-4", "EPIC-2", "To Do", "to_do", `["v2.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)

	entry := result[0]
	assert.Equal(t, "v2.0", entry.Name)
	assert.Equal(t, "PROJ", entry.ProjectKey)
	assert.Equal(t, 4, entry.TotalIssues)
	assert.Equal(t, 2, entry.DoneIssues)
	assert.InDelta(t, 50.0, entry.ProgressPct, 0.01)
	assert.False(t, entry.IsOverdue)
	assert.False(t, entry.AtRisk)
	assert.Equal(t, 0, entry.BlockedCount)
}

func TestBuildReleaseDashboard_EpicGrouping(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v3.0", "2026-06-01", false, false)

	// Seed epic issues for name lookup.
	require.NoError(t, database.UpsertJiraIssue(db.JiraIssue{
		Key: "EPIC-A", ID: "EPIC-A", Summary: "Auth System",
	}))
	require.NoError(t, database.UpsertJiraIssue(db.JiraIssue{
		Key: "EPIC-B", ID: "EPIC-B", Summary: "Payment",
	}))

	// EPIC-A: 2/3 done = 66.7%
	seedReleaseIssue(t, database, "P-1", "EPIC-A", "Done", "done", `["v3.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "EPIC-A", "Done", "done", `["v3.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-3", "EPIC-A", "To Do", "to_do", `["v3.0"]`, "2026-04-01T00:00:00Z")

	// EPIC-B: 0/2 done = 0%
	seedReleaseIssue(t, database, "P-4", "EPIC-B", "To Do", "to_do", `["v3.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-5", "EPIC-B", "In Progress", "in_progress", `["v3.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)

	entry := result[0]
	assert.Equal(t, 5, entry.TotalIssues)
	assert.Equal(t, 2, entry.DoneIssues)
	require.Len(t, entry.EpicProgress, 2)

	// Sorted: behind first (EPIC-B 0%), then at_risk (EPIC-A 66.7%).
	assert.Equal(t, "EPIC-B", entry.EpicProgress[0].EpicKey)
	assert.Equal(t, "behind", entry.EpicProgress[0].StatusBadge)
	assert.Equal(t, 0, entry.EpicProgress[0].Done)
	assert.Equal(t, 2, entry.EpicProgress[0].Total)

	assert.Equal(t, "EPIC-A", entry.EpicProgress[1].EpicKey)
	assert.Equal(t, "at_risk", entry.EpicProgress[1].StatusBadge)
	assert.Equal(t, 2, entry.EpicProgress[1].Done)
	assert.Equal(t, 3, entry.EpicProgress[1].Total)
}

func TestBuildReleaseDashboard_Overdue(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Release date in the past, not released.
	seedRelease(t, database, 1, "PROJ", "v1.5", "2026-03-15", false, false)

	seedReleaseIssue(t, database, "P-1", "", "To Do", "to_do", `["v1.5"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.True(t, result[0].IsOverdue)
}

func TestBuildReleaseDashboard_AtRisk_BlockedIssues(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v4.0", "2026-06-01", false, false)

	// 3 issues: 2 blocked, 1 to do => 66% blocked > 30%
	seedReleaseIssue(t, database, "P-1", "", "Blocked", "to_do", `["v4.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "", "Blocked by QA", "in_progress", `["v4.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-3", "", "To Do", "to_do", `["v4.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.True(t, result[0].AtRisk)
	assert.Equal(t, "30%+ issues blocked", result[0].AtRiskReason)
	assert.Equal(t, 2, result[0].BlockedCount)
}

func TestBuildReleaseDashboard_AtRisk_DeadlineApproaching(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Release in 5 days, progress < 80%.
	seedRelease(t, database, 1, "PROJ", "v5.0", "2026-04-14", false, false)

	// 5 issues: 3 done (60%), 2 to do.
	seedReleaseIssue(t, database, "P-1", "", "Done", "done", `["v5.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "", "Done", "done", `["v5.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-3", "", "Done", "done", `["v5.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-4", "", "To Do", "to_do", `["v5.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-5", "", "To Do", "to_do", `["v5.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.True(t, result[0].AtRisk)
	assert.Equal(t, "deadline approaching, insufficient progress", result[0].AtRiskReason)
}

func TestBuildReleaseDashboard_NotAtRisk_HighProgress(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Release in 5 days, progress >= 80%.
	seedRelease(t, database, 1, "PROJ", "v5.1", "2026-04-14", false, false)

	// 5 issues: 4 done (80%), 1 to do.
	seedReleaseIssue(t, database, "P-1", "", "Done", "done", `["v5.1"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "", "Done", "done", `["v5.1"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-3", "", "Done", "done", `["v5.1"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-4", "", "Done", "done", `["v5.1"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-5", "", "To Do", "to_do", `["v5.1"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.False(t, result[0].AtRisk)
}

func TestBuildReleaseDashboard_BlockedDoneNotCounted(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v6.0", "2026-06-01", false, false)

	// "Blocked" status but status_category is "done" — should NOT count as blocked.
	seedReleaseIssue(t, database, "P-1", "", "Was Blocked", "done", `["v6.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "", "In Progress", "in_progress", `["v6.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, 0, result[0].BlockedCount)
}

func TestBuildReleaseDashboard_ScopeChanges(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v7.0", "2026-06-01", false, false)

	weekAgo := now.AddDate(0, 0, -7)

	// 2 issues synced recently (after week ago), 1 synced before.
	seedReleaseIssue(t, database, "P-1", "", "To Do", "to_do", `["v7.0"]`, now.AddDate(0, 0, -1).Format(time.RFC3339))
	seedReleaseIssue(t, database, "P-2", "", "To Do", "to_do", `["v7.0"]`, now.AddDate(0, 0, -2).Format(time.RFC3339))
	seedReleaseIssue(t, database, "P-3", "", "Done", "done", `["v7.0"]`, weekAgo.AddDate(0, 0, -3).Format(time.RFC3339))

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, 2, result[0].ScopeChanges.AddedLastWeek)
}

func TestBuildReleaseDashboard_SortByReleaseDate(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v3.0", "2026-07-01", false, false)
	seedRelease(t, database, 2, "PROJ", "v2.0", "2026-05-01", false, false)
	seedRelease(t, database, 3, "PROJ", "v2.5", "2026-06-01", false, false)

	// Seed at least one issue per release so they appear.
	seedReleaseIssue(t, database, "P-1", "", "To Do", "to_do", `["v3.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "", "To Do", "to_do", `["v2.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-3", "", "To Do", "to_do", `["v2.5"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.Equal(t, "v2.0", result[0].Name)
	assert.Equal(t, "v2.5", result[1].Name)
	assert.Equal(t, "v3.0", result[2].Name)
}

func TestBuildReleaseDashboard_AllProjects(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ-A", "v1.0", "2026-05-01", false, false)
	seedRelease(t, database, 2, "PROJ-B", "v1.0-B", "2026-06-01", false, false)

	seedReleaseIssue(t, database, "A-1", "", "To Do", "to_do", `["v1.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "B-1", "", "To Do", "to_do", `["v1.0-B"]`, "2026-04-01T00:00:00Z")

	// Empty projectKey => all projects.
	result, err := BuildReleaseDashboard(database, cfg, "", now)
	require.NoError(t, err)
	require.Len(t, result, 2)
}

func TestBuildReleaseDashboard_NoIssues(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Release with no issues — should still appear with 0 progress.
	seedRelease(t, database, 1, "PROJ", "v8.0", "2026-06-01", false, false)

	result, err := BuildReleaseDashboard(database, cfg, "PROJ", now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, 0, result[0].TotalIssues)
	assert.InDelta(t, 0.0, result[0].ProgressPct, 0.01)
}

// --- BuildReleaseDetail tests ---

func TestBuildReleaseDetail_DisabledFeature(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(false)

	result, err := BuildReleaseDetail(database, cfg, "v1.0", time.Now())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDetail_NotFound(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)

	result, err := BuildReleaseDetail(database, cfg, "nonexistent", time.Now())
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBuildReleaseDetail_BasicData(t *testing.T) {
	database := releaseDashTestDB(t)
	cfg := releaseDashConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	seedRelease(t, database, 1, "PROJ", "v9.0", "2026-06-01", false, false)
	seedReleaseIssue(t, database, "P-1", "EPIC-X", "Done", "done", `["v9.0"]`, "2026-04-01T00:00:00Z")
	seedReleaseIssue(t, database, "P-2", "EPIC-X", "To Do", "to_do", `["v9.0"]`, "2026-04-01T00:00:00Z")

	result, err := BuildReleaseDetail(database, cfg, "v9.0", now)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "v9.0", result.Name)
	assert.Equal(t, 2, result.TotalIssues)
	assert.Equal(t, 1, result.DoneIssues)
	assert.InDelta(t, 50.0, result.ProgressPct, 0.01)
}

// --- Helper function tests ---

func TestIsBlocked(t *testing.T) {
	tests := []struct {
		name     string
		issue    db.JiraIssue
		expected bool
	}{
		{"blocked status", db.JiraIssue{Status: "Blocked", StatusCategory: "to_do"}, true},
		{"blocked by qa", db.JiraIssue{Status: "Blocked by QA", StatusCategory: "in_progress"}, true},
		{"blocked but done", db.JiraIssue{Status: "Was Blocked", StatusCategory: "done"}, false},
		{"normal status", db.JiraIssue{Status: "In Progress", StatusCategory: "in_progress"}, false},
		{"to do", db.JiraIssue{Status: "To Do", StatusCategory: "to_do"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isBlocked(tt.issue))
		})
	}
}

func TestReleaseEpicBadge(t *testing.T) {
	assert.Equal(t, "on_track", releaseEpicBadge(100))
	assert.Equal(t, "at_risk", releaseEpicBadge(75))
	assert.Equal(t, "at_risk", releaseEpicBadge(50))
	assert.Equal(t, "behind", releaseEpicBadge(49))
	assert.Equal(t, "behind", releaseEpicBadge(0))
}

func TestParseReleaseDate(t *testing.T) {
	t.Run("ISO date", func(t *testing.T) {
		dt, err := parseReleaseDate("2026-04-09")
		require.NoError(t, err)
		assert.Equal(t, 2026, dt.Year())
		assert.Equal(t, time.April, dt.Month())
		assert.Equal(t, 9, dt.Day())
	})

	t.Run("RFC3339", func(t *testing.T) {
		dt, err := parseReleaseDate("2026-04-09T12:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, 2026, dt.Year())
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseReleaseDate("not-a-date")
		assert.Error(t, err)
	})
}

func TestParseFixVersions(t *testing.T) {
	assert.Nil(t, parseFixVersions(""))
	assert.Nil(t, parseFixVersions("[]"))
	assert.Equal(t, []string{"v1.0", "v2.0"}, parseFixVersions(`["v1.0","v2.0"]`))
}
