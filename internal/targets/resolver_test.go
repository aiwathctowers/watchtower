package targets

import (
	"context"
	"errors"
	"testing"
	"time"

	"watchtower/internal/db"
)

// --- URL regex tests ---

func TestExtract_SlackURLs(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantCount int
		wantChan  string
		wantTs    string
	}{
		{
			name:      "basic slack permalink",
			text:      "see https://myworkspace.slack.com/archives/C12345678/p1714567890123456",
			wantCount: 1,
			wantChan:  "C12345678",
			wantTs:    "1714567890.123456",
		},
		{
			name:      "slack permalink with thread param",
			text:      "https://team.slack.com/archives/C0ABC1234/p1714999000111222?thread_ts=1714998000.111111&cid=C0ABC1234",
			wantCount: 1,
			wantChan:  "C0ABC1234",
			wantTs:    "1714999000.111222",
		},
		{
			name:      "DM channel ID",
			text:      "https://ws.slack.com/archives/D123ABC/p1710000000000001",
			wantCount: 1,
			wantChan:  "D123ABC",
			wantTs:    "1710000000.000001",
		},
		{
			name:      "no slack URLs",
			text:      "nothing here https://example.com",
			wantCount: 0,
		},
		{
			name:      "duplicate deduplication",
			text:      "https://ws.slack.com/archives/C111/p1710000000000001 and https://ws.slack.com/archives/C111/p1710000000000001",
			wantCount: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Extract(tc.text)
			if len(got) != tc.wantCount {
				t.Fatalf("want %d matches, got %d: %+v", tc.wantCount, len(got), got)
			}
			if tc.wantCount > 0 {
				if got[0].Source != "slack" {
					t.Errorf("want source=slack, got %q", got[0].Source)
				}
				if tc.wantChan != "" && got[0].ChannelID != tc.wantChan {
					t.Errorf("want channelID=%q, got %q", tc.wantChan, got[0].ChannelID)
				}
				if tc.wantTs != "" && got[0].Ts != tc.wantTs {
					t.Errorf("want ts=%q, got %q", tc.wantTs, got[0].Ts)
				}
			}
		})
	}
}

func TestExtract_JiraURLs(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantCount int
		wantKey   string
	}{
		{
			name:      "basic jira URL",
			text:      "ticket https://myorg.atlassian.net/browse/PROJ-123",
			wantCount: 1,
			wantKey:   "PROJ-123",
		},
		{
			name:      "jira URL with trailing text",
			text:      "see https://mycompany.atlassian.net/browse/ABC-9999 for details",
			wantCount: 1,
			wantKey:   "ABC-9999",
		},
		{
			name:      "two jira keys",
			text:      "https://myorg.atlassian.net/browse/AA-1 and https://myorg.atlassian.net/browse/BB-2",
			wantCount: 2,
		},
		{
			name:      "no jira URLs",
			text:      "nothing https://github.com/org/repo/issues/1",
			wantCount: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Extract(tc.text)
			if len(got) != tc.wantCount {
				t.Fatalf("want %d matches, got %d: %+v", tc.wantCount, len(got), got)
			}
			if tc.wantCount == 1 && tc.wantKey != "" {
				if got[0].Key != tc.wantKey {
					t.Errorf("want key=%q, got %q", tc.wantKey, got[0].Key)
				}
				if got[0].Source != "jira" {
					t.Errorf("want source=jira, got %q", got[0].Source)
				}
			}
		})
	}
}

func TestExtract_Mixed(t *testing.T) {
	text := "see https://ws.slack.com/archives/C123/p1710000000000001 and https://myorg.atlassian.net/browse/FOO-42"
	got := Extract(text)
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %d", len(got))
	}
}

// --- SlackResolver tests ---

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSlackResolver_LocalHit(t *testing.T) {
	d := newTestDB(t)

	// Insert user.
	_, err := d.Exec(`INSERT INTO users(id, name, display_name, real_name) VALUES ('U001','alice','alice','Alice Smith')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// Insert channel.
	_, err = d.Exec(`INSERT INTO channels(id, name, type) VALUES ('C001','general','public')`)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	// Insert message.
	_, err = d.Exec(`INSERT INTO messages(channel_id, ts, user_id, text) VALUES ('C001','1714567890.123456','U001','hello team')`)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	r := &SlackResolver{db: d}
	m := URLMatch{Source: "slack", RawURL: "https://ws.slack.com/archives/C001/p1714567890123456", ChannelID: "C001", Ts: "1714567890.123456"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Error != "" {
		t.Errorf("want no error field, got %q", e.Error)
	}
	if e.Source != "local" {
		t.Errorf("want source=local, got %q", e.Source)
	}
	if e.Ref != "slack:C001:1714567890.123456" {
		t.Errorf("unexpected ref: %q", e.Ref)
	}
	if e.Body == "" || e.Body == "[slack url not in local DB]" {
		t.Errorf("expected body with message content, got %q", e.Body)
	}
}

func TestSlackResolver_LocalMiss(t *testing.T) {
	d := newTestDB(t)

	r := &SlackResolver{db: d}
	m := URLMatch{Source: "slack", RawURL: "https://ws.slack.com/archives/C999/p1714999999999999", ChannelID: "C999", Ts: "1714999999.999999"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Body != "[slack url not in local DB]" {
		t.Errorf("want annotated body, got %q", e.Body)
	}
}

// --- JiraResolver tests ---

type mockMCPClient struct {
	issue  *db.JiraIssue
	err    error
	delay  time.Duration
	called bool
}

func (m *mockMCPClient) GetJiraIssue(ctx context.Context, key string) (*db.JiraIssue, error) {
	m.called = true
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.issue, m.err
}

func TestJiraResolver_LocalHit(t *testing.T) {
	d := newTestDB(t)

	_, err := d.Exec(`INSERT INTO jira_issues(key, id, project_key, summary, status, priority, status_category, is_deleted, created_at, updated_at, synced_at)
		VALUES ('PROJ-1','1001','PROJ','Fix the bug','In Progress','High','in_progress',0,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert jira issue: %v", err)
	}

	r := &JiraResolver{db: d, mcp: nil, timeout: 5 * time.Second}
	m := URLMatch{Source: "jira", RawURL: "https://org.atlassian.net/browse/PROJ-1", Key: "PROJ-1"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Source != "local" {
		t.Errorf("want source=local, got %q", e.Source)
	}
	if e.Ref != "jira:PROJ-1" {
		t.Errorf("unexpected ref: %q", e.Ref)
	}
	if e.Body == "" {
		t.Error("expected non-empty body")
	}
}

func TestJiraResolver_LocalMiss_MCPHit(t *testing.T) {
	d := newTestDB(t)

	mcp := &mockMCPClient{
		issue: &db.JiraIssue{
			Key:     "PROJ-42",
			Summary: "New feature",
			Status:  "To Do",
		},
	}

	r := &JiraResolver{db: d, mcp: mcp, timeout: 5 * time.Second}
	m := URLMatch{Source: "jira", RawURL: "https://org.atlassian.net/browse/PROJ-42", Key: "PROJ-42"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mcp.called {
		t.Error("expected MCP to be called")
	}
	if e.Source != "mcp" {
		t.Errorf("want source=mcp, got %q", e.Source)
	}
}

func TestJiraResolver_MCPTimeout(t *testing.T) {
	d := newTestDB(t)

	mcp := &mockMCPClient{
		delay: 1 * time.Hour, // will be cancelled
	}

	r := &JiraResolver{db: d, mcp: mcp, timeout: 10 * time.Millisecond}
	m := URLMatch{Source: "jira", RawURL: "https://org.atlassian.net/browse/PROJ-99", Key: "PROJ-99"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Body == "" {
		t.Error("want annotated body")
	}
	if e.Source != "mcp" {
		t.Errorf("want source=mcp, got %q", e.Source)
	}
	// Should contain timeout annotation.
	if e.Body != "[jira fetch failed: timeout]" {
		t.Errorf("want timeout annotation, got %q", e.Body)
	}
}

func TestJiraResolver_MCPError(t *testing.T) {
	d := newTestDB(t)

	mcp := &mockMCPClient{
		err: errors.New("network failure"),
	}

	r := &JiraResolver{db: d, mcp: mcp, timeout: 5 * time.Second}
	m := URLMatch{Source: "jira", RawURL: "https://org.atlassian.net/browse/Z-1", Key: "Z-1"}
	e, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Body != "[jira fetch failed: network failure]" {
		t.Errorf("unexpected body: %q", e.Body)
	}
}
