package inbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"watchtower/internal/db"
)

// seedInboxItem inserts a minimal inbox_item and returns its ID.
func seedInboxItem(t *testing.T, database *db.DB, senderUserID, channelID, triggerType string) int64 {
	t.Helper()
	ts := fmt.Sprintf("%d.000100", time.Now().UnixNano())
	id, err := database.CreateInboxItem(db.InboxItem{
		ChannelID:    channelID,
		MessageTS:    ts,
		SenderUserID: senderUserID,
		TriggerType:  triggerType,
		Status:       "pending",
	})
	if err != nil {
		t.Fatalf("seedInboxItem: %v", err)
	}
	return id
}

func TestInbox04_GradualMuteFromAccumulatedDismissals(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// Implicit mute requires accumulated dismiss evidence, not a single click.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U1"
	for i := 0; i < 10; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		if i < 9 {
			_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed', updated_at=? WHERE id=?`, time.Now().Format(time.RFC3339), id)
		}
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "sender:"+sender)
	if err != nil {
		t.Fatalf("expected rule: %v", err)
	}
	if r.Weight != -0.7 {
		t.Errorf("weight=%v want -0.7", r.Weight)
	}
	if r.EvidenceCount != 9 {
		t.Errorf("evidence=%d want 9", r.EvidenceCount)
	}
}

func TestInbox04_NoRuleBelowEvidenceThreshold(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// Below the evidence threshold no rule is created — preserves gradual
	// learning. Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	for i := 0; i < 4; i++ {
		id := seedInboxItem(t, d, "U2", "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	RunImplicitLearner(context.Background(), d, 30*24*time.Hour) //nolint:errcheck
	_, err := d.GetLearnedRule("source_mute", "sender:U2")
	if err == nil {
		t.Error("rule should not exist below evidence threshold")
	}
}

func TestInbox06_UserRuleProtectedFromImplicitOverwrite(t *testing.T) {
	// BEHAVIOR INBOX-06 — see docs/inventory/inbox-pulse.md
	// source='user_rule' is never overwritten by the implicit learner.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "sender:U3", Weight: -0.4, Source: "user_rule"})
	for i := 0; i < 10; i++ {
		id := seedInboxItem(t, d, "U3", "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	RunImplicitLearner(context.Background(), d, 30*24*time.Hour) //nolint:errcheck
	r, _ := d.GetLearnedRule("source_mute", "sender:U3")
	if r.Source != "user_rule" || r.Weight != -0.4 {
		t.Errorf("user_rule overwritten: %+v", r)
	}
}

func TestLearner_ChannelMute(t *testing.T) {
	d := testDB(t)
	// 10 items from channel C99, 8 dismissed → source_mute channel:C99 weight -0.5
	for i := 0; i < 10; i++ {
		id := seedInboxItem(t, d, "U1", "C99", "mention")
		if i < 8 {
			_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
		}
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "channel:C99")
	if err != nil {
		t.Fatalf("expected channel rule: %v", err)
	}
	if r.Weight != -0.5 {
		t.Errorf("weight=%v want -0.5", r.Weight)
	}
	if r.EvidenceCount != 8 {
		t.Errorf("evidence=%d want 8", r.EvidenceCount)
	}
}

func TestInbox04_LearnerAggregatesExplicitWithImplicit(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// Unified pool: implicit dismissals + explicit (-1, !never_show) feedback
	// together drive source_mute creation at threshold.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_mix"
	// 3 dismissed items.
	for i := 0; i < 3; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed', updated_at=? WHERE id=?`,
			time.Now().Format(time.RFC3339), id)
	}
	// 2 active items + 2 explicit (-1, source_noise) feedback rows.
	for i := 0; i < 2; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, -1, 'source_noise', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "sender:"+sender)
	if err != nil {
		t.Fatalf("expected source_mute rule from unified pool: %v", err)
	}
	if r.Weight != -0.7 {
		t.Errorf("weight=%v want -0.7", r.Weight)
	}
	if r.Source != "implicit" {
		t.Errorf("source=%s want implicit", r.Source)
	}
}

func TestInbox04_LearnerNoRuleBelowCombinedThreshold(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// Pool below 5 events does not produce a rule even when 100% negative.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_low"
	// 2 dismissed + 1 explicit -1 = 3 events total.
	for i := 0; i < 2; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	id := seedInboxItem(t, d, sender, "C1", "mention")
	_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
		VALUES (?, -1, 'source_noise', ?)`, id, time.Now().Format(time.RFC3339))
	RunImplicitLearner(context.Background(), d, 30*24*time.Hour) //nolint:errcheck
	if _, err := d.GetLearnedRule("source_mute", "sender:"+sender); err == nil {
		t.Error("rule must not exist below evidence threshold")
	}
}

func TestInbox04_LearnerPositiveBoostFromExplicit(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// 5 explicit (+1) feedback rows over 30d, no negatives → source_boost +0.7.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_boost"
	for i := 0; i < 5; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, 1, '', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_boost", "sender:"+sender)
	if err != nil {
		t.Fatalf("expected source_boost: %v", err)
	}
	if r.Weight != 0.7 {
		t.Errorf("weight=%v want +0.7", r.Weight)
	}
	if r.Source != "implicit" {
		t.Errorf("source=%s want implicit", r.Source)
	}
}

func TestInbox04_LearnerNeverShowExcludedFromPool(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// inbox_feedback rows with reason='never_show' are NOT counted in the
	// learner's negative pool — never_show already produced a user_rule and
	// must not double-count. Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_never"
	// 4 dismisses + 4 never_show feedback rows.
	// Without exclusion, total = 8, all negative → rule. With exclusion,
	// only the 4 dismisses count → below evidence threshold → no rule.
	for i := 0; i < 4; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	for i := 0; i < 4; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, -1, 'never_show', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLearnedRule("source_mute", "sender:"+sender); err == nil {
		t.Error("never_show feedback must be excluded from learner pool")
	}
}
