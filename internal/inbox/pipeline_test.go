package inbox

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recentTS returns a Slack-style timestamp string relative to now.
func recentTS(minutesAgo int) string {
	t := time.Now().Add(-time.Duration(minutesAgo) * time.Minute)
	return fmt.Sprintf("%d.000100", t.Unix())
}

type mockGenerator struct {
	response string
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func testConfig() *config.Config {
	return &config.Config{
		Digest: config.DigestConfig{
			Enabled: true,
		},
		Inbox: config.InboxConfig{
			Enabled:             true,
			MaxItemsPerRun:      100,
			InitialLookbackDays: 7,
		},
	}
}

// seedWorkspaceAndUser inserts a workspace and sets the current user.
func seedWorkspaceAndUser(t *testing.T, database *db.DB, userID string) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO workspace (id, name, current_user_id) VALUES ('T1', 'Test', ?)`, userID)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO users (id, name) VALUES (?, 'testuser')`, userID)
	require.NoError(t, err)
}

func TestPipeline_Run_NoUser(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	p := New(database, cfg, nil, log.Default())

	created, resolved, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created)
	assert.Equal(t, 0, resolved)
}

func TestPipeline_Run_DetectMentions(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30) // 30 minutes ago
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, permalink) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> review please', 'https://slack.com/p1')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "mention", items[0].TriggerType)
	assert.Equal(t, "pending", items[0].Status)
}

func TestPipeline_Run_DetectDMs(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type, dm_user_id) VALUES ('D1', 'dm-other', 'dm', 'U_OTHER')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', ?, 'U_OTHER', 'Hey, got a minute?')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "dm", items[0].TriggerType)
}

func TestInbox02_AutoResolveSlackOnUserReply(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User replies in Slack → mention/dm/thread_reply auto-resolves.
	// Do not weaken or remove without explicit owner approval.
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts1 := recentTS(30)
	ts2 := recentTS(20) // reply 10 minutes later
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> check this')`, ts1)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_ME', 'Done!')`, ts2)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, resolved, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)
	assert.Equal(t, 1, resolved)

	items, err := database.GetInboxItems(db.InboxFilter{IncludeResolved: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "resolved", items[0].Status)
}

func TestPipeline_Run_NoDuplicates(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> check this')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())

	created1, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created1)

	// Second run — should not create duplicates (FindPendingMentions has NOT EXISTS)
	created2, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created2)
}

func TestPipeline_Run_WithAI(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> urgent blocker')`, ts)
	require.NoError(t, err)

	gen := &mockGenerator{
		response: `{"items": [{"id": 1, "priority": "high", "reason": "Production blocker from team lead", "resolved": false}]}`,
	}

	p := New(database, cfg, gen, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "high", items[0].Priority)
	assert.Equal(t, "Production blocker from team lead", items[0].AIReason)
}

func TestParseAIResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{
			name:    "valid JSON",
			input:   `{"items": [{"id": 1, "priority": "high", "reason": "urgent", "resolved": false}]}`,
			wantLen: 1,
		},
		{
			name:    "with markdown fences",
			input:   "```json\n{\"items\": [{\"id\": 1, \"priority\": \"high\", \"reason\": \"urgent\", \"resolved\": false}]}\n```",
			wantLen: 1,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAIResult(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, result.Items, tt.wantLen)
		})
	}
}

func TestPipeline_LastProcessedTS(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	p := New(database, cfg, nil, log.Default())
	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	ts, err := database.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Greater(t, ts, float64(0))
}

func TestIsClosingSignal(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		// English
		{"thanks", true},
		{"Thank you", true},
		{"Thanks!", true},
		{"Thanks!!", true},
		{"thx", true},
		{"ty", true},
		{"got it", true},
		{"ok", true},
		{"Ok.", true},
		{"okay", true},
		{"cool", true},
		{"great", true},
		{"perfect", true},
		{"awesome", true},
		{"np", true},
		{"no problem", true},
		{"will do", true},
		{"sounds good", true},
		{"noted", true},
		{"ack", true},
		// Russian
		{"спасибо", true},
		{"Спасибо!", true},
		{"спс", true},
		{"ок", true},
		{"понял", true},
		{"понятно", true},
		{"принял", true},
		{"ясно", true},
		{"хорошо", true},
		{"отлично", true},
		{"ладно", true},
		{"круто", true},
		{"пон", true},
		// Emoji
		{"👍", true},
		{"🙏", true},
		{"🙌", true},
		{"👌", true},
		{"✅", true},
		// Whitespace/punctuation variations
		{" thanks ", true},
		{"Thanks...", true},
		{"Ok,", true},
		// NOT closing signals
		{"thanks but also need the API docs updated", false},
		{"ok can you also check the other PR", false},
		{"", false},
		{"Can you review this?", false},
		{"I need help with deployment", false},
		// Too long (>80 chars)
		{"thanks for looking into this and also please check the other thing that I mentioned earlier in the thread about the deployment", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, isClosingSignal(tt.text), "isClosingSignal(%q)", tt.text)
		})
	}
}

func TestPipeline_ClosingSignalSkipped(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// User replied first, then other person says "спасибо".
	ts1 := recentTS(30)
	ts2 := recentTS(20)
	ts3 := recentTS(10) // "спасибо"

	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> can you check?', ?)`, ts1, ts1)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', ?, 'U_ME', 'Done!', ?)`, ts2, ts1)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', ?, 'U_OTHER', 'Спасибо!', ?)`, ts3, ts1)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)

	// The "спасибо" should be skipped (closing signal + user replied before).
	// The original mention should be auto-resolved (user replied after).
	items, err := database.GetInboxItems(db.InboxFilter{IncludeResolved: true})
	require.NoError(t, err)

	// Only the original mention should have been created, not the "спасибо".
	for _, item := range items {
		assert.NotContains(t, item.Snippet, "Спасибо", "closing signal should not create an inbox item")
	}
}

func TestPipeline_ClosingSignalNoUserReply(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	_, err := database.Exec(`INSERT INTO channels (id, name, type, dm_user_id) VALUES ('D1', 'dm-other', 'dm', 'U_OTHER')`)
	require.NoError(t, err)

	// Other person says "thanks" but user NEVER replied — should still create item (safety).
	ts1 := recentTS(30)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', ?, 'U_OTHER', 'thanks')`, ts1)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created, "closing signal without prior user reply should create item")
}

func TestPipeline_Run_OrderedPhases(t *testing.T) {
	d := newTestDB(t)
	seedWorkspaceAndUser(t, d, "alice")

	// Seed: a jira issue assigned to alice, a calendar invite for alice, a high-importance digest decision.
	seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-5*time.Minute))
	seedCalendarEvent(t, d, "evt-1", "Sync", `[{"email":"alice@x.com","rsvp_status":"needsAction"}]`, "confirmed",
		time.Now().Add(-10*time.Minute), time.Now().Add(-10*time.Minute))
	seedDigestWithHighImportance(t, d, "C1", `[{"type":"decision","topic":"Launch","importance":"high"}]`,
		time.Now().Add(-5*time.Minute))

	cfg := testConfig()
	gen := &mockGenerator{response: `{"pinned_ids":[]}`}
	p := New(d, cfg, gen, log.Default())
	p.SetCurrentUser("alice", "alice@x.com")

	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	mustCount := func(trig string, want int) {
		t.Helper()
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type=?`, trig).Scan(&n) //nolint:errcheck
		assert.Equal(t, want, n, "trigger_type=%s", trig)
	}
	mustCount("jira_assigned", 1)
	mustCount("calendar_invite", 1)
	mustCount("decision_made", 1)

	// decision_made should be classified as ambient
	var cls string
	d.QueryRow(`SELECT item_class FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&cls) //nolint:errcheck
	assert.Equal(t, "ambient", cls, "decision_made item_class")
}

func TestPipeline_Run_AutoArchiveRuns(t *testing.T) {
	d := newTestDB(t)
	seedWorkspaceAndUser(t, d, "U1")

	// Insert an 8-days-old ambient decision_made item.
	oldT := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
		VALUES ('C1','1.0','U1','decision_made','pending','low','ambient',?,?)`, oldT, oldT)
	require.NoError(t, err)

	p := New(d, testConfig(), &mockGenerator{response: `{}`}, log.Default())
	p.SetCurrentUser("U1", "u1@test.com")
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)

	var reason string
	d.QueryRow(`SELECT archive_reason FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&reason) //nolint:errcheck
	assert.Equal(t, "seen_expired", reason)
}

func TestPipeline_AIResolvedField(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> thanks for fixing that')`, ts)
	require.NoError(t, err)

	gen := &mockGenerator{
		response: `{"items": [{"id": 1, "priority": "", "reason": "Closing signal — no reply needed", "resolved": true}]}`,
	}

	p := New(database, cfg, gen, log.Default())
	created, resolved, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)
	assert.Equal(t, 1, resolved, "AI resolved=true should resolve the item")

	items, err := database.GetInboxItems(db.InboxFilter{IncludeResolved: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "resolved", items[0].Status)
	assert.Contains(t, items[0].ResolvedReason, "AI:")
}

// newPipelineForTest creates a Pipeline with the given user identity pre-set.
func newPipelineForTest(t *testing.T, d *db.DB, userID, email string) *Pipeline {
	t.Helper()
	seedWorkspaceAndUser(t, d, userID)
	cfg := testConfig()
	p := New(d, cfg, &mockGenerator{response: `{"pinned_ids":[]}`}, log.Default())
	p.SetCurrentUser(userID, email)
	return p
}

// seedJiraComment creates the jira_comments table (if absent) and inserts a row.
func seedJiraComment(t *testing.T, d *db.DB, issueKey, authorID, body string, createdAt time.Time) {
	t.Helper()
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS jira_comments (
		id          TEXT PRIMARY KEY,
		issue_key   TEXT NOT NULL,
		author_id   TEXT NOT NULL,
		body        TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	)`)
	require.NoError(t, err, "create jira_comments table")
	ts := createdAt.UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT INTO jira_comments (id, issue_key, author_id, body, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`,
		fmt.Sprintf("%s-%s-%d", issueKey, authorID, createdAt.UnixNano()),
		issueKey, authorID, body, ts, ts)
	require.NoError(t, err, "insert jira_comment")
}

func TestInbox02_AutoResolveJiraOnUserComment(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User comments on a Jira issue → jira_comment_mention auto-resolves.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	// Open jira_comment_mention for WT-1, then user adds comment to the issue.
	seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-1*time.Hour))
	seedJiraComment(t, d, "WT-1", "bob", "hey [~alice]", time.Now().Add(-30*time.Minute))
	p := newPipelineForTest(t, d, "alice", "alice@x.com")
	_, _, err := p.Run(context.Background())
	require.NoError(t, err)
	// Now alice comments — seed her comment and run again.
	seedJiraComment(t, d, "WT-1", "alice", "got it", time.Now())
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)
	var status string
	d.QueryRow(`SELECT status FROM inbox_items WHERE trigger_type='jira_comment_mention' AND channel_id='WT-1'`).Scan(&status) //nolint:errcheck
	if status != "resolved" {
		t.Errorf("want resolved, got %q", status)
	}
}

func TestInbox02_AutoResolveCalendarOnUserRSVP(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User responds to a calendar invite → calendar_invite auto-resolves.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	seedCalendarEvent(t, d, "evt-1", "Sync",
		`[{"email":"alice@x.com","rsvp_status":"needsAction"}]`,
		"confirmed",
		time.Now().Add(-30*time.Minute), time.Now().Add(-30*time.Minute))
	p := newPipelineForTest(t, d, "alice", "alice@x.com")
	_, _, err := p.Run(context.Background())
	require.NoError(t, err)
	// Now alice responds — update attendees RSVP and run again.
	_, err = d.Exec(`UPDATE calendar_events SET attendees=? WHERE id='evt-1'`,
		`[{"email":"alice@x.com","rsvp_status":"accepted"}]`)
	require.NoError(t, err)
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)
	var status string
	d.QueryRow(`SELECT status FROM inbox_items WHERE trigger_type='calendar_invite'`).Scan(&status) //nolint:errcheck
	if status != "resolved" {
		t.Errorf("want resolved, got %q", status)
	}
}

// TestPipeline_RunFastDetection verifies that RunFastDetection picks up Slack
// DMs immediately, leaves the watermark untouched, and skips decision_made
// detection (which depends on digests written later in the daemon cycle).
func TestPipeline_RunFastDetection(t *testing.T) {
	d := newTestDB(t)
	seedWorkspaceAndUser(t, d, "alice")

	// A Slack DM addressed to alice — should be picked up by fast detection.
	dmTS := recentTS(20)
	_, err := d.Exec(`INSERT INTO channels (id, name, type, dm_user_id) VALUES ('D1', 'dm-bob', 'dm', 'U_BOB')`)
	require.NoError(t, err)
	_, err = d.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', ?, 'U_BOB', 'привет, есть минутка?')`, dmTS)
	require.NoError(t, err)

	// A digest with a high-importance decision — should NOT be picked up by fast
	// detection (DetectWatchtowerInternal is skipped); the full Run picks it up.
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"Migrate to v2","importance":"high"}]`,
		time.Now().Add(-5*time.Minute))

	cfg := testConfig()
	p := New(d, cfg, nil, log.Default())
	p.SetCurrentUser("alice", "alice@x.com")

	wmBefore, err := d.GetInboxLastProcessedTS()
	require.NoError(t, err)

	require.NoError(t, p.RunFastDetection(context.Background()))

	dmCount := func() int {
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type='dm'`).Scan(&n) //nolint:errcheck
		return n
	}
	decisionCount := func() int {
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&n) //nolint:errcheck
		return n
	}

	assert.Equal(t, 1, dmCount(), "DM should be detected by fast pass")
	assert.Equal(t, 0, decisionCount(), "decision_made must NOT be detected by fast pass")

	wmAfter, err := d.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Equal(t, wmBefore, wmAfter, "RunFastDetection must not advance the watermark")

	// Subsequent full Run must pick up the digest decision and advance the watermark.
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, dmCount(), "full Run must not duplicate the DM detected by fast pass")
	assert.Equal(t, 1, decisionCount(), "full Run must detect decision_made from the digest")

	wmAfterFull, err := d.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Greater(t, wmAfterFull, wmBefore, "full Run must advance the watermark")
}
