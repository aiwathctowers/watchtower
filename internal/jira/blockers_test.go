package jira

import (
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

func setupBlockerTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func blockerConfig(enabled bool) *config.Config {
	return &config.Config{
		Jira: config.JiraConfig{
			Enabled: true,
			Features: config.JiraFeatureToggles{
				BlockerMap: enabled,
			},
		},
	}
}

func insertTestIssue(t *testing.T, d *db.DB, issue db.JiraIssue) {
	t.Helper()
	if err := d.UpsertJiraIssue(issue); err != nil {
		t.Fatalf("inserting test issue %s: %v", issue.Key, err)
	}
}

func insertTestLink(t *testing.T, d *db.DB, link db.JiraIssueLink) {
	t.Helper()
	if err := d.UpsertJiraIssueLink(link); err != nil {
		t.Fatalf("inserting test link %s: %v", link.ID, err)
	}
}

// TestBlockerMap_ToggleOff verifies that disabled feature returns nil.
func TestBlockerMap_ToggleOff(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(false)

	result, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil when toggle is off, got %v", result)
	}
}

// TestBlockerMap_BlockedIssueDetection verifies that issues with "block" in status are found.
func TestBlockerMap_BlockedIssueDetection(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// Blocked issue.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-1",
		ID:                      "10001",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -3).Format(time.RFC3339),
		Summary:                 "Blocked task",
		AssigneeSlackID:         "U111",
		AssigneeDisplayName:     "Alice",
	})

	// Normal issue (should NOT appear).
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-2",
		ID:             "10002",
		Status:         "In Progress",
		StatusCategory: "in_progress",
		Summary:        "Normal task",
	})

	// Done blocked issue (should NOT appear).
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-3",
		ID:             "10003",
		Status:         "Blocked",
		StatusCategory: "done",
		Summary:        "Done blocked",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 blocked entry, got %d", len(entries))
	}
	if entries[0].IssueKey != "PROJ-1" {
		t.Errorf("expected PROJ-1, got %s", entries[0].IssueKey)
	}
	if entries[0].BlockerType != "blocked" {
		t.Errorf("expected blocker_type 'blocked', got %s", entries[0].BlockerType)
	}
	if entries[0].BlockedDays != 3 {
		t.Errorf("expected 3 blocked days, got %d", entries[0].BlockedDays)
	}
}

// TestBlockerMap_StaleIssueDetection verifies that stale in-progress issues are found.
func TestBlockerMap_StaleIssueDetection(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// Stale issue (in_progress for 10 days).
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-10",
		ID:                      "10010",
		Status:                  "In Progress",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -10).Format(time.RFC3339),
		Summary:                 "Stale task",
		AssigneeSlackID:         "U222",
		AssigneeDisplayName:     "Bob",
	})

	// Fresh issue (in_progress for 2 days — should NOT be stale).
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-11",
		ID:                      "10011",
		Status:                  "In Progress",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -2).Format(time.RFC3339),
		Summary:                 "Fresh task",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 stale entry, got %d", len(entries))
	}
	if entries[0].IssueKey != "PROJ-10" {
		t.Errorf("expected PROJ-10, got %s", entries[0].IssueKey)
	}
	if entries[0].BlockerType != "stale" {
		t.Errorf("expected blocker_type 'stale', got %s", entries[0].BlockerType)
	}
}

// TestBlockerMap_StaleExcludesBlocked verifies stale detection doesn't include already-blocked issues.
func TestBlockerMap_StaleExcludesBlocked(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// Issue that is both "blocked" status AND stale (>7 days in_progress).
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-20",
		ID:                      "10020",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -10).Format(time.RFC3339),
		Summary:                 "Blocked and stale",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should appear exactly once (as "blocked", not duplicated as "stale").
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].BlockerType != "blocked" {
		t.Errorf("expected 'blocked', got %s", entries[0].BlockerType)
	}
}

// TestBlockerMap_ChainBuilding verifies chain: A blocked by B blocked by C.
func TestBlockerMap_ChainBuilding(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// A is blocked.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-A",
		ID:                      "100A",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -1).Format(time.RFC3339),
		Summary:                 "Issue A",
	})

	// B blocks A.
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-B",
		ID:             "100B",
		Status:         "In Progress",
		StatusCategory: "in_progress",
		Summary:        "Issue B",
	})

	// C blocks B (root cause).
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-C",
		ID:             "100C",
		Status:         "Open",
		StatusCategory: "todo",
		Summary:        "Issue C (root cause)",
	})

	// Links: B blocks A, C blocks B.
	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-1",
		SourceKey: "PROJ-B",
		TargetKey: "PROJ-A",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})
	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-2",
		SourceKey: "PROJ-C",
		TargetKey: "PROJ-B",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	chain := entries[0].BlockingChain
	if len(chain) != 3 {
		t.Fatalf("expected chain length 3, got %d: %v", len(chain), chain)
	}
	if chain[0] != "PROJ-A" || chain[1] != "PROJ-B" || chain[2] != "PROJ-C" {
		t.Errorf("expected chain [PROJ-A, PROJ-B, PROJ-C], got %v", chain)
	}
}

// TestBlockerMap_DownstreamCount verifies counting of transitively blocked issues.
func TestBlockerMap_DownstreamCount(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// X is blocked (our entry).
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-X",
		ID:                      "100X",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -1).Format(time.RFC3339),
		Summary:                 "Issue X",
	})

	// Y and Z are downstream of X.
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-Y",
		ID:             "100Y",
		Status:         "To Do",
		StatusCategory: "todo",
		Summary:        "Issue Y (blocked by X)",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-Z",
		ID:             "100Z",
		Status:         "To Do",
		StatusCategory: "todo",
		Summary:        "Issue Z (blocked by Y)",
	})

	// X blocks Y, Y blocks Z (transitive).
	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-xy",
		SourceKey: "PROJ-X",
		TargetKey: "PROJ-Y",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})
	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-yz",
		SourceKey: "PROJ-Y",
		TargetKey: "PROJ-Z",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].DownstreamCount != 2 {
		t.Errorf("expected downstream_count=2, got %d", entries[0].DownstreamCount)
	}
}

// TestBlockerMap_Urgency verifies urgency logic.
func TestBlockerMap_Urgency(t *testing.T) {
	tests := []struct {
		name            string
		blockedDays     int
		downstreamCount int
		wantUrgency     BlockerUrgency
	}{
		{"red from days", 6, 0, UrgencyRed},
		{"red from downstream", 1, 3, UrgencyRed},
		{"red from both", 10, 5, UrgencyRed},
		{"yellow", 3, 0, UrgencyYellow},
		{"gray fresh", 1, 0, UrgencyGray},
		{"gray zero", 0, 0, UrgencyGray},
		{"boundary red days", 5, 0, UrgencyYellow},     // >5 is red, 5 is yellow
		{"boundary yellow days", 2, 0, UrgencyGray},    // >2 is yellow, 2 is gray
		{"boundary red downstream", 0, 2, UrgencyGray}, // >2 is red, 2 is gray
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeUrgency(tt.blockedDays, tt.downstreamCount)
			if got != tt.wantUrgency {
				t.Errorf("computeUrgency(%d, %d) = %s, want %s", tt.blockedDays, tt.downstreamCount, got, tt.wantUrgency)
			}
		})
	}
}

// TestBlockerMap_WhoToPing verifies ping target selection.
func TestBlockerMap_WhoToPing(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// Blocked issue with assignee and reporter.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-P",
		ID:                      "100P",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -3).Format(time.RFC3339),
		Summary:                 "Blocked with people",
		AssigneeSlackID:         "U-ASSIGNEE",
		AssigneeDisplayName:     "Assignee Person",
		ReporterSlackID:         "U-REPORTER",
		ReporterDisplayName:     "Reporter Person",
	})

	// Root cause issue with different assignee.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "PROJ-R",
		ID:                  "100R",
		Status:              "Open",
		StatusCategory:      "todo",
		Summary:             "Root cause",
		AssigneeSlackID:     "U-ROOT",
		AssigneeDisplayName: "Root Person",
	})

	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-rp",
		SourceKey: "PROJ-R",
		TargetKey: "PROJ-P",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	targets := entries[0].WhoToPing
	if len(targets) != 3 {
		t.Fatalf("expected 3 ping targets, got %d: %+v", len(targets), targets)
	}

	// First should be root cause assignee.
	if targets[0].SlackUserID != "U-ROOT" || targets[0].Reason != "assignee_blocker" {
		t.Errorf("first target should be root assignee with reason assignee_blocker, got %+v", targets[0])
	}
	// Second should be issue's own assignee.
	if targets[1].SlackUserID != "U-ASSIGNEE" || targets[1].Reason != "assignee" {
		t.Errorf("second target should be issue assignee, got %+v", targets[1])
	}
	// Third should be reporter.
	if targets[2].SlackUserID != "U-REPORTER" || targets[2].Reason != "reporter" {
		t.Errorf("third target should be reporter, got %+v", targets[2])
	}
}

// TestBlockerMap_SlackContext verifies Slack snippet retrieval.
func TestBlockerMap_SlackContext(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-S",
		ID:                      "100S",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -1).Format(time.RFC3339),
		Summary:                 "Issue with Slack context",
	})

	// Insert a Slack message and link.
	if err := d.UpsertMessage(db.Message{
		ChannelID: "C123",
		TS:        "1234567890.123456",
		UserID:    "U-SLACK",
		Text:      "We need to discuss PROJ-S, it's been stuck for a while now",
	}); err != nil {
		t.Fatalf("inserting message: %v", err)
	}

	if err := d.UpsertJiraSlackLink(db.JiraSlackLink{
		IssueKey:  "PROJ-S",
		ChannelID: "C123",
		MessageTS: "1234567890.123456",
		LinkType:  "mention",
	}); err != nil {
		t.Fatalf("inserting slack link: %v", err)
	}

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].SlackContext == "" {
		t.Error("expected non-empty Slack context")
	}
	if entries[0].SlackContext != "We need to discuss PROJ-S, it's been stuck for a while now" {
		t.Errorf("unexpected Slack context: %s", entries[0].SlackContext)
	}
}

// TestBlockerMap_Sorting verifies entries are sorted by downstream_count DESC, blocked_days DESC.
func TestBlockerMap_Sorting(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	// Issue with more blocked days but no downstream.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-OLD",
		ID:                      "100OLD",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -10).Format(time.RFC3339),
		Summary:                 "Old blocked",
	})

	// Issue with downstream.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                     "PROJ-DOWN",
		ID:                      "100DOWN",
		Status:                  "Blocked",
		StatusCategory:          "in_progress",
		StatusCategoryChangedAt: nowFunc().AddDate(0, 0, -2).Format(time.RFC3339),
		Summary:                 "Has downstream",
	})

	// Issue blocked by PROJ-DOWN.
	insertTestIssue(t, d, db.JiraIssue{
		Key:            "PROJ-CHILD",
		ID:             "100CHILD",
		Status:         "To Do",
		StatusCategory: "todo",
		Summary:        "Child issue",
	})

	insertTestLink(t, d, db.JiraIssueLink{
		ID:        "link-dc",
		SourceKey: "PROJ-DOWN",
		TargetKey: "PROJ-CHILD",
		LinkType:  "Blocks",
		SyncedAt:  "2026-04-01",
	})

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// PROJ-DOWN should come first (downstream_count=1 > 0).
	if entries[0].IssueKey != "PROJ-DOWN" {
		t.Errorf("expected PROJ-DOWN first (has downstream), got %s", entries[0].IssueKey)
	}
	if entries[1].IssueKey != "PROJ-OLD" {
		t.Errorf("expected PROJ-OLD second, got %s", entries[1].IssueKey)
	}
}

// TestBlockerMap_EmptyDB verifies empty result on empty database.
func TestBlockerMap_EmptyDB(t *testing.T) {
	d := setupBlockerTestDB(t)
	cfg := blockerConfig(true)

	entries, err := ComputeBlockerMap(d, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on empty DB, got %d", len(entries))
	}
}
