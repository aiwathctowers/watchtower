package inbox

import (
	"context"
	"testing"
	"time"

	"watchtower/internal/db"
)

// seedJiraIssue inserts a jira_issues row assigned to the given account ID.
// updated is used as both updated_at and synced_at.
func seedJiraIssue(t *testing.T, d *db.DB, key, assigneeAccountID string, updated time.Time) {
	t.Helper()
	ts := updated.UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO jira_issues
		(key, id, project_key, summary, status, status_category,
		 assignee_account_id, created_at, updated_at, synced_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		key, key, "WT", "test issue", "In Progress", "in_progress",
		assigneeAccountID, ts, ts, ts)
	if err != nil {
		t.Fatalf("seedJiraIssue: %v", err)
	}
}


func TestJiraDetector_AssignedToMe(t *testing.T) {
	d := testDB(t)
	seedJiraIssue(t, d, "WT-123", "alice", time.Now().Add(-1*time.Hour))

	n, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 new inbox item, got %d", n)
	}
	got := queryInboxByTrigger(t, d, "jira_assigned")
	if len(got) != 1 {
		t.Fatalf("want 1 jira_assigned item, got %d", len(got))
	}
	// SenderUserID stores the issue key as the "sender" for Jira items.
	if got[0].SenderUserID != "WT-123" {
		t.Errorf("expected SenderUserID=WT-123, got %q", got[0].SenderUserID)
	}
	if got[0].ItemClass != "actionable" {
		t.Errorf("expected actionable class, got %q", got[0].ItemClass)
	}
}

func TestJiraDetector_CommentMention(t *testing.T) {
	d := testDB(t)
	// No jira_comments table in schema — jira_comment_mention is a no-op (TODO v2).
	// This test verifies DetectJira returns 0 for comment mentions gracefully.
	seedJiraIssue(t, d, "WT-200", "bob", time.Now().Add(-1*time.Hour))

	n, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// alice is not the assignee, so no jira_assigned item.
	// jira_comment_mention is a no-op (schema not available).
	if n != 0 {
		t.Errorf("expected 0 items for non-assignee with no comment schema, got %d", n)
	}
	got := queryInboxByTrigger(t, d, "jira_comment_mention")
	if len(got) != 0 {
		t.Errorf("expected no comment mention items, got %d", len(got))
	}
}

func TestJiraDetector_StatusChange(t *testing.T) {
	// jira_status_change requires jira_issue_history table which does not exist yet.
	// This test documents the no-op behavior until schema v2.
	d := testDB(t)
	seedJiraIssue(t, d, "WT-300", "alice", time.Now().Add(-1*time.Hour))

	n, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Only jira_assigned fires; no status_change items.
	got := queryInboxByTrigger(t, d, "jira_status_change")
	if len(got) != 0 {
		t.Errorf("expected no status_change items (schema not available), got %d", len(got))
	}
	_ = n
}

func TestJiraDetector_NoDoubleDetection(t *testing.T) {
	d := testDB(t)
	seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-1*time.Hour))

	n1, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 {
		t.Fatalf("first run: want 1, got %d", n1)
	}

	// Second run with same sinceTS — existing inbox_item blocks re-insert.
	n2, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second run: expected 0 (no duplicates), got %d", n2)
	}
}

func TestJiraDetector_EmptyUserID(t *testing.T) {
	d := testDB(t)
	n, err := DetectJira(context.Background(), d, "", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 for empty userID, got %d", n)
	}
}

func TestJiraDetector_NotMyIssue(t *testing.T) {
	d := testDB(t)
	// Issue assigned to "bob", not "alice"
	seedJiraIssue(t, d, "WT-999", "bob", time.Now().Add(-1*time.Hour))

	n, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 items (not alice's issue), got %d", n)
	}
}
