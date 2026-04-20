package jira

import (
	"fmt"
	"testing"

	"watchtower/internal/db"
)

func setupWhoToPingTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestComputeWhoToPing_PriorityOrder verifies correct priority:
// assignee_blocker > assignee > expert > reporter > slack_participant.
func TestComputeWhoToPing_PriorityOrder(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	// Issue with assignee and reporter.
	issue := db.JiraIssue{
		Key:                 "TEST-1",
		ID:                  "1001",
		Status:              "Blocked",
		StatusCategory:      "in_progress",
		Summary:             "Test issue",
		AssigneeSlackID:     "U-ASSIGNEE",
		AssigneeDisplayName: "Assignee",
		ReporterSlackID:     "U-REPORTER",
		ReporterDisplayName: "Reporter",
		Components:          `["backend"]`,
	}
	insertTestIssue(t, d, issue)

	// Root cause issue.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "TEST-ROOT",
		ID:                  "1002",
		Status:              "Open",
		StatusCategory:      "todo",
		Summary:             "Root cause",
		AssigneeSlackID:     "U-ROOT",
		AssigneeDisplayName: "Root",
	})

	// Expert: resolved issue with same component by different user.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "TEST-RESOLVED",
		ID:                  "1003",
		Status:              "Done",
		StatusCategory:      "done",
		Summary:             "Resolved issue",
		AssigneeSlackID:     "U-EXPERT",
		AssigneeDisplayName: "Expert",
		Components:          `["backend"]`,
	})

	chain := []string{"TEST-1", "TEST-ROOT"}
	targets := ComputeWhoToPing(d, issue, chain)

	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d: %+v", len(targets), targets)
	}

	// 1st: root cause assignee.
	if targets[0].SlackUserID != "U-ROOT" || targets[0].Reason != "assignee_blocker" {
		t.Errorf("target[0] should be root assignee_blocker, got %+v", targets[0])
	}
	// 2nd: issue assignee.
	if targets[1].SlackUserID != "U-ASSIGNEE" || targets[1].Reason != "assignee" {
		t.Errorf("target[1] should be assignee, got %+v", targets[1])
	}
	// 3rd: expert (not reporter, because expert has higher priority).
	if targets[2].SlackUserID != "U-EXPERT" || targets[2].Reason != "expert" {
		t.Errorf("target[2] should be expert, got %+v", targets[2])
	}
}

// TestComputeWhoToPing_Dedup verifies deduplication by SlackUserID.
func TestComputeWhoToPing_Dedup(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	// Issue where assignee == reporter.
	issue := db.JiraIssue{
		Key:                 "TEST-D",
		ID:                  "2001",
		Status:              "Blocked",
		StatusCategory:      "in_progress",
		Summary:             "Dedup test",
		AssigneeSlackID:     "U-SAME",
		AssigneeDisplayName: "Same Person",
		ReporterSlackID:     "U-SAME",
		ReporterDisplayName: "Same Person",
	}
	insertTestIssue(t, d, issue)

	targets := ComputeWhoToPing(d, issue, []string{"TEST-D"})

	if len(targets) != 1 {
		t.Fatalf("expected 1 target (deduped), got %d: %+v", len(targets), targets)
	}
	if targets[0].SlackUserID != "U-SAME" {
		t.Errorf("expected U-SAME, got %s", targets[0].SlackUserID)
	}
}

// TestComputeWhoToPing_MaxTargets verifies at most 3 targets are returned.
func TestComputeWhoToPing_MaxTargets(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	issue := db.JiraIssue{
		Key:                 "TEST-M",
		ID:                  "3001",
		Status:              "Blocked",
		StatusCategory:      "in_progress",
		Summary:             "Max test",
		AssigneeSlackID:     "U1",
		AssigneeDisplayName: "User1",
		ReporterSlackID:     "U4",
		ReporterDisplayName: "User4",
		Components:          `["frontend"]`,
	}
	insertTestIssue(t, d, issue)

	// Root cause with different assignee.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "TEST-MR",
		ID:                  "3002",
		Status:              "Open",
		StatusCategory:      "todo",
		Summary:             "Root",
		AssigneeSlackID:     "U2",
		AssigneeDisplayName: "User2",
	})

	// Expert.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "TEST-EXPERT",
		ID:                  "3003",
		Status:              "Done",
		StatusCategory:      "done",
		Summary:             "Resolved",
		AssigneeSlackID:     "U3",
		AssigneeDisplayName: "User3",
		Components:          `["frontend"]`,
	})

	chain := []string{"TEST-M", "TEST-MR"}
	targets := ComputeWhoToPing(d, issue, chain)

	if len(targets) != 3 {
		t.Fatalf("expected exactly 3 targets, got %d: %+v", len(targets), targets)
	}
	// Reporter U4 should NOT appear (maxPingTargets=3 already reached).
	for _, tgt := range targets {
		if tgt.SlackUserID == "U4" {
			t.Errorf("reporter should not appear when max targets reached, but found %+v", tgt)
		}
	}
}

// TestComputeWhoToPing_NoChain verifies behavior with single-element chain.
func TestComputeWhoToPing_NoChain(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	issue := db.JiraIssue{
		Key:                 "TEST-NC",
		ID:                  "4001",
		Status:              "Blocked",
		StatusCategory:      "in_progress",
		Summary:             "No chain",
		AssigneeSlackID:     "U-A",
		AssigneeDisplayName: "Alice",
		ReporterSlackID:     "U-B",
		ReporterDisplayName: "Bob",
	}
	insertTestIssue(t, d, issue)

	// Single-element chain: no root cause lookup.
	targets := ComputeWhoToPing(d, issue, []string{"TEST-NC"})

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %+v", len(targets), targets)
	}
	if targets[0].Reason != "assignee" {
		t.Errorf("first should be assignee, got %s", targets[0].Reason)
	}
	if targets[1].Reason != "reporter" {
		t.Errorf("second should be reporter, got %s", targets[1].Reason)
	}
}

// TestComputeWhoToPing_EmptyIssue verifies no panic on empty issue.
func TestComputeWhoToPing_EmptyIssue(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	issue := db.JiraIssue{
		Key:            "TEST-E",
		ID:             "5001",
		Status:         "Blocked",
		StatusCategory: "in_progress",
		Summary:        "Empty",
	}
	insertTestIssue(t, d, issue)

	targets := ComputeWhoToPing(d, issue, nil)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for empty issue, got %d: %+v", len(targets), targets)
	}
}

// TestComputeWhoToPing_ExpertFromComponent verifies expert detection.
func TestComputeWhoToPing_ExpertFromComponent(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	// Create several resolved issues with same component to establish expertise.
	for i := 0; i < 3; i++ {
		insertTestIssue(t, d, db.JiraIssue{
			Key:                 fmt.Sprintf("RES-%d", i),
			ID:                  fmt.Sprintf("6%03d", i),
			Status:              "Done",
			StatusCategory:      "done",
			Summary:             "Resolved",
			AssigneeSlackID:     "U-EXPERT",
			AssigneeDisplayName: "Expert",
			Components:          `["auth"]`,
		})
	}

	// Issue with same component but no assignee/reporter.
	issue := db.JiraIssue{
		Key:            "TEST-EXP",
		ID:             "6100",
		Status:         "Blocked",
		StatusCategory: "in_progress",
		Summary:        "Need expert",
		Components:     `["auth"]`,
	}
	insertTestIssue(t, d, issue)

	targets := ComputeWhoToPing(d, issue, []string{"TEST-EXP"})

	if len(targets) != 1 {
		t.Fatalf("expected 1 target (expert), got %d: %+v", len(targets), targets)
	}
	if targets[0].SlackUserID != "U-EXPERT" || targets[0].Reason != "expert" {
		t.Errorf("expected expert U-EXPERT, got %+v", targets[0])
	}
}

// TestComputeWhoToPingForEpic_Basic verifies epic ping targets.
func TestComputeWhoToPingForEpic_Basic(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	// Epic issue.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "EPIC-1",
		ID:                  "7001",
		IssueType:           "Epic",
		Status:              "In Progress",
		StatusCategory:      "in_progress",
		Summary:             "Epic issue",
		AssigneeSlackID:     "U-OWNER",
		AssigneeDisplayName: "Owner",
		Components:          `["api"]`,
	})

	// Child issues.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "CHILD-1",
		ID:                  "7002",
		EpicKey:             "EPIC-1",
		Status:              "In Progress",
		StatusCategory:      "in_progress",
		Summary:             "Child 1",
		AssigneeSlackID:     "U-DEV1",
		AssigneeDisplayName: "Dev1",
		Components:          `["api"]`,
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "CHILD-2",
		ID:                  "7003",
		EpicKey:             "EPIC-1",
		Status:              "To Do",
		StatusCategory:      "todo",
		Summary:             "Child 2",
		AssigneeSlackID:     "U-DEV2",
		AssigneeDisplayName: "Dev2",
	})

	// Expert: resolved issues in same component.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "DONE-API",
		ID:                  "7004",
		Status:              "Done",
		StatusCategory:      "done",
		Summary:             "Done api work",
		AssigneeSlackID:     "U-EXPERT",
		AssigneeDisplayName: "Expert",
		Components:          `["api"]`,
	})

	targets, err := ComputeWhoToPingForEpic(d, "EPIC-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d: %+v", len(targets), targets)
	}

	// 1st: epic owner.
	if targets[0].SlackUserID != "U-OWNER" || targets[0].Reason != "assignee" {
		t.Errorf("target[0] should be epic owner, got %+v", targets[0])
	}
	// 2nd: expert.
	if targets[1].SlackUserID != "U-EXPERT" || targets[1].Reason != "expert" {
		t.Errorf("target[1] should be expert, got %+v", targets[1])
	}
	// 3rd: child assignee.
	if targets[2].SlackUserID != "U-DEV1" && targets[2].SlackUserID != "U-DEV2" {
		t.Errorf("target[2] should be a child assignee, got %+v", targets[2])
	}
}

// TestComputeWhoToPingForEpic_NotFound verifies nil return for missing epic.
func TestComputeWhoToPingForEpic_NotFound(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	targets, err := ComputeWhoToPingForEpic(d, "NONEXISTENT-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets != nil {
		t.Errorf("expected nil for missing epic, got %+v", targets)
	}
}

// TestComputeWhoToPingForEpic_Dedup verifies deduplication in epic context.
func TestComputeWhoToPingForEpic_Dedup(t *testing.T) {
	d := setupWhoToPingTestDB(t)

	// Epic where owner is also a child assignee.
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "EPIC-D",
		ID:                  "8001",
		IssueType:           "Epic",
		Status:              "In Progress",
		StatusCategory:      "in_progress",
		Summary:             "Dedup epic",
		AssigneeSlackID:     "U-SAME",
		AssigneeDisplayName: "Same",
	})
	insertTestIssue(t, d, db.JiraIssue{
		Key:                 "CHILD-D1",
		ID:                  "8002",
		EpicKey:             "EPIC-D",
		Status:              "In Progress",
		StatusCategory:      "in_progress",
		Summary:             "Child",
		AssigneeSlackID:     "U-SAME",
		AssigneeDisplayName: "Same",
	})

	targets, err := ComputeWhoToPingForEpic(d, "EPIC-D")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target (deduped), got %d: %+v", len(targets), targets)
	}
}

// TestParseComponents verifies JSON component parsing.
func TestParseComponents(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`["backend","frontend"]`, 2},
		{`["single"]`, 1},
		{`[]`, 0},
		{``, 0},
		{`invalid`, 0},
	}
	for _, tt := range tests {
		got := parseComponents(tt.input)
		if len(got) != tt.want {
			t.Errorf("parseComponents(%q) returned %d items, want %d", tt.input, len(got), tt.want)
		}
	}
}
