package jira

import (
	"strings"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

func fixedNow() time.Time {
	return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
}

func init() {
	nowFunc = fixedNow
}

// --- BuildIssueContext ---

func TestBuildIssueContext_Empty(t *testing.T) {
	if got := BuildIssueContext(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
	if got := BuildIssueContext([]db.JiraIssue{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func TestBuildIssueContext_Basic(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "PROJ-123", Status: "In Progress", Priority: "High", SprintName: "Sprint 5", DueDate: "2026-04-10", StatusCategory: "in progress", Summary: "Fix payment bug"},
	}
	got := BuildIssueContext(issues)
	want := `- [PROJ-123 status=In Progress priority=High sprint="Sprint 5" due=2026-04-10] Fix payment bug`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildIssueContext_Overdue(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "PROJ-456", Status: "To Do", Priority: "Medium", DueDate: "2026-04-01", StatusCategory: "to do", Summary: "Add rate limiting"},
	}
	got := BuildIssueContext(issues)
	if !strings.Contains(got, "OVERDUE:2026-04-01") {
		t.Errorf("expected OVERDUE marker, got: %s", got)
	}
}

func TestBuildIssueContext_DoneNotOverdue(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "PROJ-789", Status: "Done", Priority: "Low", DueDate: "2026-04-01", StatusCategory: "done", Summary: "Completed task"},
	}
	got := BuildIssueContext(issues)
	if strings.Contains(got, "OVERDUE") {
		t.Errorf("done issues should not be marked OVERDUE, got: %s", got)
	}
	if !strings.Contains(got, "due=2026-04-01") {
		t.Errorf("expected due= for done issue, got: %s", got)
	}
}

func TestBuildIssueContext_MultipleIssues(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "A-1", Status: "Open", Summary: "First"},
		{Key: "A-2", Status: "Closed", Summary: "Second"},
	}
	got := BuildIssueContext(issues)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %s", len(lines), got)
	}
}

func TestBuildIssueContext_NoPriority(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "X-1", Status: "Open", Summary: "No priority"},
	}
	got := BuildIssueContext(issues)
	if strings.Contains(got, "priority=") {
		t.Errorf("empty priority should be omitted, got: %s", got)
	}
}

// --- BuildSprintContext ---

func TestBuildSprintContext_Empty(t *testing.T) {
	if got := BuildSprintContext(db.SprintStats{}); got != "" {
		t.Errorf("expected empty string for zero stats, got %q", got)
	}
}

func TestBuildSprintContext_Basic(t *testing.T) {
	stats := db.SprintStats{
		SprintName: "Sprint 5",
		Total:      15,
		Done:       8,
		InProgress: 5,
		Todo:       2,
		DaysLeft:   3,
	}
	got := BuildSprintContext(stats)
	want := `Sprint "Sprint 5": 15 total (8 done, 5 in progress, 2 todo), 3 days left`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildSprintContext_OneDay(t *testing.T) {
	stats := db.SprintStats{SprintName: "S1", Total: 1, Done: 0, InProgress: 1, Todo: 0, DaysLeft: 1}
	got := BuildSprintContext(stats)
	if !strings.Contains(got, "1 day left") {
		t.Errorf("expected singular 'day left', got: %s", got)
	}
}

// --- BuildDeliveryContext ---

func TestBuildDeliveryContext_Empty(t *testing.T) {
	if got := BuildDeliveryContext(db.DeliveryStats{}); got != "" {
		t.Errorf("expected empty string for zero stats, got %q", got)
	}
}

func TestBuildDeliveryContext_Basic(t *testing.T) {
	stats := db.DeliveryStats{
		IssuesClosed:         12,
		AvgCycleTimeDays:     3.2,
		StoryPointsCompleted: 28.0,
		OpenIssues:           5,
		OverdueIssues:        1,
		Components:           []string{"api", "backend"},
		Labels:               []string{"payments"},
	}
	got := BuildDeliveryContext(stats)
	if !strings.Contains(got, "Issues closed: 12") {
		t.Errorf("missing issues closed, got: %s", got)
	}
	if !strings.Contains(got, "Expertise: [api, backend, payments]") {
		t.Errorf("missing expertise, got: %s", got)
	}
}

func TestBuildDeliveryContext_NoExpertise(t *testing.T) {
	stats := db.DeliveryStats{IssuesClosed: 1}
	got := BuildDeliveryContext(stats)
	if strings.Contains(got, "Expertise") {
		t.Errorf("should not contain Expertise when no components/labels, got: %s", got)
	}
}

func TestBuildDeliveryContext_DedupExpertise(t *testing.T) {
	stats := db.DeliveryStats{
		IssuesClosed: 1,
		Components:   []string{"api"},
		Labels:       []string{"api", "backend"},
	}
	got := BuildDeliveryContext(stats)
	if !strings.Contains(got, "Expertise: [api, backend]") {
		t.Errorf("expected deduplicated expertise, got: %s", got)
	}
}

// --- BuildIssueListForCLI ---

func TestBuildIssueListForCLI_Empty(t *testing.T) {
	if got := BuildIssueListForCLI(nil); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildIssueListForCLI_Basic(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "PROJ-123", Status: "In Progress", Priority: "P2", SprintName: "5", DueDate: "2026-04-10", StatusCategory: "in progress", Summary: "Fix payment bug"},
	}
	got := BuildIssueListForCLI(issues)
	if !strings.Contains(got, "[PROJ-123 In Progress P2 Sprint:5 Due:Apr 10]") {
		t.Errorf("unexpected CLI format, got: %s", got)
	}
}

func TestBuildIssueListForCLI_Overdue(t *testing.T) {
	issues := []db.JiraIssue{
		{Key: "X-1", Status: "Open", DueDate: "2026-04-01", StatusCategory: "to do", Summary: "Late"},
	}
	got := BuildIssueListForCLI(issues)
	if !strings.Contains(got, "OVERDUE:") {
		t.Errorf("expected OVERDUE in CLI output, got: %s", got)
	}
}

// --- FormatJiraBadge ---

func TestFormatJiraBadge(t *testing.T) {
	issue := db.JiraIssue{Key: "PROJ-123", Status: "In Progress"}
	got := FormatJiraBadge(issue)
	if got != "[PROJ-123 In Progress]" {
		t.Errorf("got %q", got)
	}
}

func TestFormatJiraBadge_EmptyStatus(t *testing.T) {
	issue := db.JiraIssue{Key: "X-1"}
	got := FormatJiraBadge(issue)
	if got != "[X-1 Unknown]" {
		t.Errorf("got %q", got)
	}
}

// --- IsFeatureEnabled ---

func TestIsFeatureEnabled_NilConfig(t *testing.T) {
	if IsFeatureEnabled(nil, "my_issues") {
		t.Error("nil config should return false")
	}
}

func TestIsFeatureEnabled_JiraDisabled(t *testing.T) {
	cfg := &config.Config{Jira: config.JiraConfig{Enabled: false, Features: config.JiraFeatureToggles{MyIssuesInBriefing: true}}}
	if IsFeatureEnabled(cfg, "my_issues") {
		t.Error("should return false when Jira is disabled")
	}
}

func TestIsFeatureEnabled_FeatureOn(t *testing.T) {
	cfg := &config.Config{Jira: config.JiraConfig{Enabled: true, Features: config.JiraFeatureToggles{BlockerMap: true}}}
	if !IsFeatureEnabled(cfg, "blocker_map") {
		t.Error("expected true for enabled feature")
	}
}

func TestIsFeatureEnabled_FeatureOff(t *testing.T) {
	cfg := &config.Config{Jira: config.JiraConfig{Enabled: true, Features: config.JiraFeatureToggles{BlockerMap: false}}}
	if IsFeatureEnabled(cfg, "blocker_map") {
		t.Error("expected false for disabled feature")
	}
}

func TestIsFeatureEnabled_UnknownFeature(t *testing.T) {
	cfg := &config.Config{Jira: config.JiraConfig{Enabled: true}}
	if IsFeatureEnabled(cfg, "nonexistent") {
		t.Error("unknown feature should return false")
	}
}
