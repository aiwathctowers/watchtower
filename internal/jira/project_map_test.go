package jira

import (
	"fmt"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

func setupProjectMapTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func projectMapConfig(enabled bool) *config.Config {
	return &config.Config{
		Jira: config.JiraConfig{
			Enabled: true,
			Features: config.JiraFeatureToggles{
				EpicProgress: enabled,
			},
		},
	}
}

// TestBuildProjectMap_ToggleOff verifies that disabled feature returns nil.
func TestBuildProjectMap_ToggleOff(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(false)

	result, err := BuildProjectMap(d, cfg, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when toggle is off, got %v", result)
	}
}

// TestBuildProjectMap_NoEpics verifies that no epics returns nil.
func TestBuildProjectMap_NoEpics(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)

	result, err := BuildProjectMap(d, cfg, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when no epics, got %v", result)
	}
}

// TestBuildProjectMap_EpicBelowThreshold verifies that epics with <3 children are excluded.
func TestBuildProjectMap_EpicBelowThreshold(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic issue.
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-100",
		ID:             "100",
		IssueType:      "Epic",
		Summary:        "Small Epic",
		Status:         "In Progress",
		StatusCategory: "in_progress",
	})

	// Only 2 children — below threshold.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-1", ID: "1", EpicKey: "PROJ-100",
		Status: "To Do", StatusCategory: "new",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-2", ID: "2", EpicKey: "PROJ-100",
		Status: "Done", StatusCategory: "done",
		ResolvedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
	})

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for epic with <3 children, got %d epics", len(result))
	}
}

// TestBuildProjectMap_BasicEpic verifies a basic epic with progress, participants, issues list.
func TestBuildProjectMap_BasicEpic(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic issue itself.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "PROJ-100",
		ID:                  "100",
		IssueType:           "Epic",
		Summary:             "Big Feature",
		Status:              "In Progress",
		StatusCategory:      "in_progress",
		AssigneeSlackID:     "U_OWNER",
		AssigneeDisplayName: "Owner Alice",
	})

	// 4 children: 2 done, 1 in_progress, 1 new.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-1", ID: "1", EpicKey: "PROJ-100",
		Summary: "Task 1", Status: "Done", StatusCategory: "done",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		ReporterSlackID: "U_REP", ReporterDisplayName: "Reporter Bob",
		ResolvedAt:              now.AddDate(0, 0, -2).Format(time.RFC3339),
		StatusCategoryChangedAt: now.AddDate(0, 0, -2).Format(time.RFC3339),
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-2", ID: "2", EpicKey: "PROJ-100",
		Summary: "Task 2", Status: "Done", StatusCategory: "done",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		ResolvedAt:              now.AddDate(0, 0, -5).Format(time.RFC3339),
		StatusCategoryChangedAt: now.AddDate(0, 0, -5).Format(time.RFC3339),
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-3", ID: "3", EpicKey: "PROJ-100",
		Summary: "Task 3", Status: "In Progress", StatusCategory: "in_progress",
		AssigneeSlackID: "U2", AssigneeDisplayName: "Bob",
		StatusCategoryChangedAt: now.AddDate(0, 0, -3).Format(time.RFC3339),
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-4", ID: "4", EpicKey: "PROJ-100",
		Summary: "Task 4", Status: "To Do", StatusCategory: "new",
		AssigneeSlackID: "U3", AssigneeDisplayName: "Carol",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
	})

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}

	epic := result[0]
	if epic.EpicKey != "PROJ-100" {
		t.Errorf("expected epic key PROJ-100, got %s", epic.EpicKey)
	}
	if epic.EpicName != "Big Feature" {
		t.Errorf("expected epic name 'Big Feature', got %s", epic.EpicName)
	}
	if epic.Owner == nil {
		t.Fatal("expected owner, got nil")
	}
	if epic.Owner.SlackUserID != "U_OWNER" {
		t.Errorf("expected owner U_OWNER, got %s", epic.Owner.SlackUserID)
	}
	if epic.TotalIssues != 4 {
		t.Errorf("expected total 4, got %d", epic.TotalIssues)
	}
	if epic.DoneIssues != 2 {
		t.Errorf("expected done 2, got %d", epic.DoneIssues)
	}
	if epic.InProgressIssues != 1 {
		t.Errorf("expected in_progress 1, got %d", epic.InProgressIssues)
	}
	if epic.ProgressPct != 50 {
		t.Errorf("expected 50%% progress, got %.1f%%", epic.ProgressPct)
	}
	if len(epic.Issues) != 4 {
		t.Errorf("expected 4 issues, got %d", len(epic.Issues))
	}

	// Participants: U1 (Alice), U_REP (Reporter Bob), U2 (Bob), U3 (Carol).
	if len(epic.Participants) < 3 {
		t.Errorf("expected >=3 participants, got %d", len(epic.Participants))
	}
}

// TestBuildProjectMap_StaleDetection verifies stale issue detection.
func TestBuildProjectMap_StaleDetection(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-200", ID: "200", IssueType: "Epic",
		Summary: "Stale Epic", Status: "In Progress", StatusCategory: "in_progress",
	})

	// 3 children: 1 stale (10 days in status), 1 normal, 1 done.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-10", ID: "10", EpicKey: "PROJ-200",
		Summary: "Stale task", Status: "In Progress", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -10).Format(time.RFC3339),
		AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-11", ID: "11", EpicKey: "PROJ-200",
		Summary: "Fresh task", Status: "In Progress", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -2).Format(time.RFC3339),
		AssigneeSlackID:         "U2", AssigneeDisplayName: "Bob",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-12", ID: "12", EpicKey: "PROJ-200",
		Summary: "Done task", Status: "Done", StatusCategory: "done",
		ResolvedAt:              now.AddDate(0, 0, -1).Format(time.RFC3339),
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U3", AssigneeDisplayName: "Carol",
	})

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}

	epic := result[0]
	if epic.StaleCount != 1 {
		t.Errorf("expected stale count 1, got %d", epic.StaleCount)
	}
	if len(epic.StaleIssues) != 1 {
		t.Fatalf("expected 1 stale issue, got %d", len(epic.StaleIssues))
	}
	if epic.StaleIssues[0].Key != "PROJ-10" {
		t.Errorf("expected stale issue PROJ-10, got %s", epic.StaleIssues[0].Key)
	}
	if !epic.StaleIssues[0].IsStale {
		t.Error("expected stale issue to have IsStale=true")
	}
	if epic.StaleIssues[0].DaysInStatus < 10 {
		t.Errorf("expected >=10 days in status, got %d", epic.StaleIssues[0].DaysInStatus)
	}
}

// TestBuildProjectMap_BlockedDetection verifies blocked issue detection.
func TestBuildProjectMap_BlockedDetection(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-300", ID: "300", IssueType: "Epic",
		Summary: "Blocked Epic", Status: "In Progress", StatusCategory: "in_progress",
	})

	// 3 children: 1 blocked, 2 normal.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-20", ID: "20", EpicKey: "PROJ-300",
		Summary: "Blocked task", Status: "Blocked", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -3).Format(time.RFC3339),
		AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-21", ID: "21", EpicKey: "PROJ-300",
		Summary: "Normal task", Status: "In Progress", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U2", AssigneeDisplayName: "Bob",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-22", ID: "22", EpicKey: "PROJ-300",
		Summary: "Done task", Status: "Done", StatusCategory: "done",
		ResolvedAt:              now.AddDate(0, 0, -1).Format(time.RFC3339),
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U3", AssigneeDisplayName: "Carol",
	})

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}

	epic := result[0]
	if epic.BlockedCount != 1 {
		t.Errorf("expected blocked count 1, got %d", epic.BlockedCount)
	}

	// Find the blocked issue.
	found := false
	for _, issue := range epic.Issues {
		if issue.Key == "PROJ-20" {
			found = true
			if !issue.IsBlocked {
				t.Error("expected PROJ-20 to be blocked")
			}
		}
	}
	if !found {
		t.Error("PROJ-20 not found in issues list")
	}
}

// TestBuildProjectMap_SlackLinks verifies Slack discussions and decisions count.
func TestBuildProjectMap_SlackLinks(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-400", ID: "400", IssueType: "Epic",
		Summary: "Linked Epic", Status: "In Progress", StatusCategory: "in_progress",
	})

	// 3 children.
	for i, key := range []string{"PROJ-30", "PROJ-31", "PROJ-32"} {
		insertTestIssue(t, d, db.JiraIssue{
			Key: key, ID: fmt.Sprintf("30%d", i), EpicKey: "PROJ-400",
			Summary: "Task", Status: "In Progress", StatusCategory: "in_progress",
			StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
			AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
		})
	}

	// Slack links: 2 mentions, 1 decision.
	trackID := 42
	if err := d.UpsertJiraSlackLink(db.JiraSlackLink{
		IssueKey:  "PROJ-30",
		ChannelID: "C001",
		MessageTS: "1234.5678",
		TrackID:   &trackID,
		LinkType:  "mention",
	}); err != nil {
		t.Fatalf("inserting slack link: %v", err)
	}
	if err := d.UpsertJiraSlackLink(db.JiraSlackLink{
		IssueKey:  "PROJ-31",
		ChannelID: "C002",
		MessageTS: "2345.6789",
		LinkType:  "decision",
	}); err != nil {
		t.Fatalf("inserting slack link: %v", err)
	}

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}

	epic := result[0]
	if len(epic.SlackDiscussions) != 2 {
		t.Errorf("expected 2 slack discussions, got %d", len(epic.SlackDiscussions))
	}
	if epic.KeyDecisionsCount != 1 {
		t.Errorf("expected 1 decision, got %d", epic.KeyDecisionsCount)
	}

	// Check track_id is set on one of the links.
	foundTrack := false
	for _, ref := range epic.SlackDiscussions {
		if ref.TrackID != nil && *ref.TrackID == 42 {
			foundTrack = true
		}
	}
	if !foundTrack {
		t.Error("expected one discussion with track_id=42")
	}
}

// TestBuildProjectMap_MultipleEpicsSorted verifies sorting: behind/at_risk before on_track.
func TestBuildProjectMap_MultipleEpicsSorted(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic A: 3 done out of 3 => on_track (complete).
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-500", ID: "500", IssueType: "Epic",
		Summary: "Complete Epic", Status: "Done", StatusCategory: "done",
	})
	for i := 0; i < 3; i++ {
		insertTestIssue(t, d, db.JiraIssue{
			Key: fmt.Sprintf("A-%d", i+1), ID: fmt.Sprintf("50%d", i),
			EpicKey: "PROJ-500", Summary: "Done task",
			Status: "Done", StatusCategory: "done",
			ResolvedAt:              now.AddDate(0, 0, -3).Format(time.RFC3339),
			StatusCategoryChangedAt: now.AddDate(0, 0, -3).Format(time.RFC3339),
			AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
		})
	}

	// Epic B: 0 done out of 3, no velocity => behind.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-600", ID: "600", IssueType: "Epic",
		Summary: "Behind Epic", Status: "In Progress", StatusCategory: "in_progress",
	})
	for i := 0; i < 3; i++ {
		insertTestIssue(t, d, db.JiraIssue{
			Key: fmt.Sprintf("B-%d", i+1), ID: fmt.Sprintf("60%d", i),
			EpicKey: "PROJ-600", Summary: "Todo task",
			Status: "To Do", StatusCategory: "new",
			StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
			AssigneeSlackID:         "U2", AssigneeDisplayName: "Bob",
		})
	}

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 epics, got %d", len(result))
	}

	// Behind epic should come first.
	if result[0].EpicKey != "PROJ-600" {
		t.Errorf("expected behind epic (PROJ-600) first, got %s", result[0].EpicKey)
	}
	if result[0].StatusBadge != "behind" {
		t.Errorf("expected 'behind' badge, got %s", result[0].StatusBadge)
	}
	if result[1].EpicKey != "PROJ-500" {
		t.Errorf("expected complete epic (PROJ-500) second, got %s", result[1].EpicKey)
	}
	if result[1].StatusBadge != "on_track" {
		t.Errorf("expected 'on_track' badge, got %s", result[1].StatusBadge)
	}
}

// TestBuildProjectMap_NoOwnerWhenNoAssignee verifies owner is nil when epic has no assignee.
func TestBuildProjectMap_NoOwnerWhenNoAssignee(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic without assignee.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-700", ID: "700", IssueType: "Epic",
		Summary: "Unowned Epic", Status: "In Progress", StatusCategory: "in_progress",
	})

	for i := 0; i < 3; i++ {
		insertTestIssue(t, d, db.JiraIssue{
			Key: fmt.Sprintf("C-%d", i+1), ID: fmt.Sprintf("70%d", i),
			EpicKey: "PROJ-700", Summary: "Task",
			Status: "In Progress", StatusCategory: "in_progress",
			StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
			AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
		})
	}

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}
	if result[0].Owner != nil {
		t.Errorf("expected nil owner for unassigned epic, got %v", result[0].Owner)
	}
}

// TestBuildProjectMapForEpic_Basic verifies single epic retrieval.
func TestBuildProjectMapForEpic_Basic(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	// Epic issue.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-900", ID: "900", IssueType: "Epic",
		Summary: "Target Epic", Status: "In Progress", StatusCategory: "in_progress",
		AssigneeSlackID: "U_OWNER", AssigneeDisplayName: "Owner",
	})

	// 3 children: 1 done, 1 in_progress, 1 new.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-91", ID: "91", EpicKey: "PROJ-900",
		Summary: "Done task", Status: "Done", StatusCategory: "done",
		ResolvedAt:              now.AddDate(0, 0, -2).Format(time.RFC3339),
		StatusCategoryChangedAt: now.AddDate(0, 0, -2).Format(time.RFC3339),
		AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-92", ID: "92", EpicKey: "PROJ-900",
		Summary: "Active task", Status: "In Progress", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U2", AssigneeDisplayName: "Bob",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-93", ID: "93", EpicKey: "PROJ-900",
		Summary: "New task", Status: "To Do", StatusCategory: "new",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U3", AssigneeDisplayName: "Carol",
	})

	epic, err := BuildProjectMapForEpic(d, cfg, "PROJ-900", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if epic == nil {
		t.Fatal("expected epic, got nil")
		return // staticcheck SA5011
	}
	if epic.EpicKey != "PROJ-900" {
		t.Errorf("expected PROJ-900, got %s", epic.EpicKey)
	}
	if epic.EpicName != "Target Epic" {
		t.Errorf("expected 'Target Epic', got %s", epic.EpicName)
	}
	if epic.Owner == nil || epic.Owner.SlackUserID != "U_OWNER" {
		t.Errorf("expected owner U_OWNER")
	}
	if epic.TotalIssues != 3 {
		t.Errorf("expected total 3, got %d", epic.TotalIssues)
	}
	if epic.DoneIssues != 1 {
		t.Errorf("expected done 1, got %d", epic.DoneIssues)
	}
	if epic.InProgressIssues != 1 {
		t.Errorf("expected in_progress 1, got %d", epic.InProgressIssues)
	}
	if len(epic.Issues) != 3 {
		t.Errorf("expected 3 issues, got %d", len(epic.Issues))
	}
	if len(epic.Participants) < 3 {
		t.Errorf("expected >=3 participants, got %d", len(epic.Participants))
	}
}

// TestBuildProjectMapForEpic_ToggleOff verifies disabled feature returns nil.
func TestBuildProjectMapForEpic_ToggleOff(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(false)

	result, err := BuildProjectMapForEpic(d, cfg, "PROJ-1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when toggle off, got %v", result)
	}
}

// TestBuildProjectMapForEpic_NotFound verifies missing epic returns nil.
func TestBuildProjectMapForEpic_NotFound(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)

	result, err := BuildProjectMapForEpic(d, cfg, "NONEXIST-1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for missing epic, got %v", result)
	}
}

// TestBuildProjectMapForEpic_BelowThreshold verifies epic with <3 children returns nil.
func TestBuildProjectMapForEpic_BelowThreshold(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-950", ID: "950", IssueType: "Epic",
		Summary: "Small", Status: "In Progress", StatusCategory: "in_progress",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-951", ID: "951", EpicKey: "PROJ-950",
		Status: "To Do", StatusCategory: "new",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
	})

	result, err := BuildProjectMapForEpic(d, cfg, "PROJ-950", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for epic with <3 children, got %v", result)
	}
}

// TestBuildProjectMap_DoneIssueNotStaleOrBlocked verifies done issues aren't flagged.
func TestBuildProjectMap_DoneIssueNotStaleOrBlocked(t *testing.T) {
	d := setupProjectMapTestDB(t)
	cfg := projectMapConfig(true)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	insertTestIssue(t, d, db.JiraIssue{
		Key: "PROJ-800", ID: "800", IssueType: "Epic",
		Summary: "Epic", Status: "In Progress", StatusCategory: "in_progress",
	})

	// Done issue with "blocked" in status and old changed_at — should NOT be stale or blocked.
	insertTestIssue(t, d, db.JiraIssue{
		Key: "D-1", ID: "81", EpicKey: "PROJ-800",
		Summary: "Was blocked", Status: "Blocked then Done", StatusCategory: "done",
		StatusCategoryChangedAt: now.AddDate(0, 0, -20).Format(time.RFC3339),
		ResolvedAt:              now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U1", AssigneeDisplayName: "Alice",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "D-2", ID: "82", EpicKey: "PROJ-800",
		Summary: "Normal", Status: "In Progress", StatusCategory: "in_progress",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U2", AssigneeDisplayName: "Bob",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key: "D-3", ID: "83", EpicKey: "PROJ-800",
		Summary: "Another", Status: "To Do", StatusCategory: "new",
		StatusCategoryChangedAt: now.AddDate(0, 0, -1).Format(time.RFC3339),
		AssigneeSlackID:         "U3", AssigneeDisplayName: "Carol",
	})

	result, err := BuildProjectMap(d, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(result))
	}

	epic := result[0]
	if epic.StaleCount != 0 {
		t.Errorf("expected 0 stale, got %d", epic.StaleCount)
	}
	if epic.BlockedCount != 0 {
		t.Errorf("expected 0 blocked, got %d", epic.BlockedCount)
	}
}
