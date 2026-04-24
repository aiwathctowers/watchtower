package inbox

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_JiraMentionFlow verifies the full Jira-mention → inbox item → feedback → learned rule path.
//
// Flow:
//  1. Seed a Jira issue and a jira_comment that mentions the current user.
//  2. Run the pipeline — expects a jira_comment_mention inbox item to be created.
//  3. Call SubmitFeedback with rating=-1 / reason="never_show".
//  4. Assert that a source_mute learned rule with scope_key="sender:<senderID>" and weight=-1.0 was created.
func TestE2E_JiraMentionFlow(t *testing.T) {
	d := newTestDB(t)

	const (
		userID   = "alice"
		issueKey = "WT-42"
	)
	const senderID = "bob"

	seedWorkspaceAndUser(t, d, userID)

	// Seed the Jira issue assigned to alice so the jira_assigned detector fires.
	seedJiraIssue(t, d, issueKey, userID, time.Now().Add(-2*time.Hour))

	// Seed a Jira comment by bob mentioning alice ([~alice]).
	seedJiraComment(t, d, issueKey, senderID, "hey [~alice] please review", time.Now().Add(-30*time.Minute))

	cfg := testConfig()
	// mockGenerator returns an empty pinned_ids response so AI prioritize is a no-op.
	gen := &mockGenerator{response: `{"pinned_ids":[]}`}
	p := New(d, cfg, gen, log.Default())
	p.SetCurrentUser(userID, "alice@x.com")

	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	// At minimum the jira_comment_mention item must have been created.
	require.Greater(t, created, 0, "expected at least one inbox item created")

	// Drain cursor: get the jira_comment_mention item.
	// Note: the Jira detector sets sender_user_id = issue key (routing/display convention).
	var itemID int64
	var senderUserID string
	err = d.QueryRow(`SELECT id, sender_user_id FROM inbox_items WHERE trigger_type='jira_comment_mention' LIMIT 1`).
		Scan(&itemID, &senderUserID)
	require.NoError(t, err, "jira_comment_mention item should exist")
	// sender_user_id is the Jira issue key by detector convention.
	assert.Equal(t, issueKey, senderUserID)

	// Submit negative feedback: never_show → should create source_mute rule for sender.
	err = SubmitFeedback(context.Background(), d, itemID, -1, "never_show")
	require.NoError(t, err)

	// Verify learned rule was created with scope_key matching the item's sender_user_id (issue key).
	expectedScope := fmt.Sprintf("sender:%s", issueKey)
	rule, err := d.GetLearnedRule("source_mute", expectedScope)
	require.NoError(t, err, "learned rule should exist after never_show feedback")
	assert.Equal(t, "source_mute", rule.RuleType)
	assert.Equal(t, expectedScope, rule.ScopeKey)
	assert.InDelta(t, -1.0, rule.Weight, 0.001, "weight should be -1.0 for never_show")
	assert.Equal(t, "explicit_feedback", rule.Source)
}

// TestE2E_AmbientAutoArchive verifies that a high-importance decision_made item created via
// the Watchtower internal detector is auto-archived after 7 days when the pipeline runs.
//
// Flow:
//  1. Seed a digest with a high-importance decision situation.
//  2. Run the pipeline — expects a decision_made (ambient) inbox item to be created.
//  3. Fast-forward the item's created_at to 8 days ago.
//  4. Run the pipeline again — expects the item to be archived with reason='seen_expired'.
func TestE2E_AmbientAutoArchive(t *testing.T) {
	d := newTestDB(t)
	seedWorkspaceAndUser(t, d, "U1")

	// Seed a digest with a high-importance decision so DetectWatchtowerInternal creates an item.
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"Launch plan finalized","importance":"high"}]`,
		time.Now().Add(-5*time.Minute))

	cfg := testConfig()
	gen := &mockGenerator{response: `{"pinned_ids":[]}`}
	p := New(d, cfg, gen, log.Default())
	p.SetCurrentUser("U1", "u1@test.com")

	// First run: detect the decision_made item.
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	require.Greater(t, created, 0, "expected decision_made item to be created")

	// Verify the item exists and is classified as ambient.
	var itemID int64
	var itemClass string
	err = d.QueryRow(`SELECT id, item_class FROM inbox_items WHERE trigger_type='decision_made' LIMIT 1`).
		Scan(&itemID, &itemClass)
	require.NoError(t, err, "decision_made item must exist after first run")
	assert.Equal(t, "ambient", itemClass, "decision_made items should be ambient")

	// Fast-forward the item's created_at to 8 days ago (beyond the 7-day archive threshold).
	oldTime := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err = d.Exec(`UPDATE inbox_items SET created_at=?, updated_at=? WHERE id=?`, oldTime, oldTime, itemID)
	require.NoError(t, err, "fast-forward created_at")

	// Second run: the auto-archive phase should pick it up.
	_, _, err = p.Run(context.Background())
	require.NoError(t, err)

	// Verify the item is archived with reason='seen_expired'.
	var archiveReason string
	err = d.QueryRow(`SELECT COALESCE(archive_reason,'') FROM inbox_items WHERE id=?`, itemID).
		Scan(&archiveReason)
	require.NoError(t, err)
	assert.Equal(t, "seen_expired", archiveReason, "ambient item older than 7 days should be archived as seen_expired")
}
