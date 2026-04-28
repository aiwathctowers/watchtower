package inbox

import (
	"context"
	"testing"
)

func TestInbox04_NeverShowStillInstantHardMute(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// never_show is the one-click escape hatch: creates source='user_rule'
	// weight -1.0 instantly. Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U1", "C1", "mention")
	if err := SubmitFeedback(context.Background(), d, id, -1, "never_show"); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "sender:U1")
	if err != nil {
		t.Fatalf("no mute rule: %v", err)
	}
	if r.Weight != -1.0 {
		t.Errorf("weight=%v want -1.0", r.Weight)
	}
	if r.Source != "user_rule" {
		t.Errorf("source=%s want user_rule", r.Source)
	}
}

func TestInbox04_SourceNoiseDoesNotCreateRule(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// (-1, source_noise) writes only inbox_feedback; no learned-rule row.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U2", "C1", "mention")
	if err := SubmitFeedback(context.Background(), d, id, -1, "source_noise"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLearnedRule("source_mute", "sender:U2"); err == nil {
		t.Error("rule must not be created instantly for source_noise feedback")
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM inbox_feedback WHERE inbox_item_id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("inbox_feedback rows=%d want 1", n)
	}
}

func TestInbox04_WrongClassChangesItemButNotRule(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// (-1, wrong_class) flips THIS item to ambient (per-item correction)
	// but does NOT create a learned rule. Do not weaken or remove without
	// explicit owner approval.
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U3", "C1", "mention") // default actionable
	if err := SubmitFeedback(context.Background(), d, id, -1, "wrong_class"); err != nil {
		t.Fatal(err)
	}
	var cls string
	if err := d.QueryRow(`SELECT item_class FROM inbox_items WHERE id=?`, id).Scan(&cls); err != nil {
		t.Fatal(err)
	}
	if cls != "ambient" {
		t.Errorf("class=%s want ambient", cls)
	}
	if _, err := d.GetLearnedRule("trigger_downgrade", "trigger:mention:sender:U3"); err == nil {
		t.Error("trigger_downgrade rule must not be created instantly for wrong_class feedback")
	}
	if _, err := d.GetLearnedRule("source_mute", "sender:U3"); err == nil {
		t.Error("source_mute rule must not be created instantly for wrong_class feedback")
	}
}

func TestInbox04_PositiveFeedbackDoesNotCreateRule(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// (+1, "") writes only inbox_feedback; no boost rule until learner aggregates.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U4", "C1", "mention")
	if err := SubmitFeedback(context.Background(), d, id, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLearnedRule("source_boost", "sender:U4"); err == nil {
		t.Error("source_boost must not be created instantly for positive feedback")
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM inbox_feedback WHERE inbox_item_id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("inbox_feedback rows=%d want 1", n)
	}
}

func TestFeedback_FeedbackRowWritten(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U5", "C1", "mention")
	SubmitFeedback(context.Background(), d, id, -1, "source_noise") //nolint:errcheck
	fbs, _ := d.GetFeedbackForItem(id)
	if len(fbs) != 1 {
		t.Fatalf("want 1 feedback row, got %d", len(fbs))
	}
	if fbs[0].Rating != -1 || fbs[0].Reason != "source_noise" {
		t.Errorf("bad feedback: %+v", fbs[0])
	}
}

func TestInbox04_WrongPriorityDoesNotCreateRule(t *testing.T) {
	// BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
	// (-1, wrong_priority) writes only inbox_feedback; no rule.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U5", "C1", "mention")
	if err := SubmitFeedback(context.Background(), d, id, -1, "wrong_priority"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLearnedRule("trigger_downgrade", "sender:U5"); err == nil {
		t.Error("rule must not be created instantly for wrong_priority feedback")
	}
}
