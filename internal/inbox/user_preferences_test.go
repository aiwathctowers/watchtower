package inbox

import (
	"strings"
	"testing"

	"watchtower/internal/db"
)

func TestInbox03_UserPrefsRankedByRelevance(t *testing.T) {
	// KILLER FEATURE INBOX-03 — see docs/inventory/inbox-pulse.md
	// USER PREFERENCES block reaching AI prioritizes relevant rules.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "sender:U1", Weight: -0.9, Source: "user_rule"})
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "channel:C9", Weight: -0.5, Source: "implicit"})
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_boost", ScopeKey: "sender:U2", Weight: 0.7, Source: "explicit_feedback"})

	items := []db.InboxItem{
		{SenderUserID: "U1", ChannelID: "Cx"},
		{SenderUserID: "U2", ChannelID: "C9"},
	}
	block, err := buildUserPreferencesBlock(d, items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "sender:U1") {
		t.Errorf("missing U1 rule: %s", block)
	}
	if !strings.Contains(block, "sender:U2") {
		t.Errorf("missing U2 rule: %s", block)
	}
	if !strings.Contains(block, "channel:C9") {
		t.Errorf("missing C9 rule: %s", block)
	}
}

func TestBuildUserPrefs_EmptyWhenNoRules(t *testing.T) {
	d := testDB(t)
	items := []db.InboxItem{{SenderUserID: "U1", ChannelID: "C1"}}
	block, _ := buildUserPreferencesBlock(d, items)
	if strings.Contains(block, "Mutes:") || strings.Contains(block, "Boosts:") {
		t.Errorf("should be minimal when no rules: %s", block)
	}
}

func TestBuildUserPrefs_SortedByAbsWeight(t *testing.T) {
	d := testDB(t)
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "sender:Uweak", Weight: -0.1, Source: "implicit"})
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "sender:Ustrong", Weight: -0.9, Source: "user_rule"})

	items := []db.InboxItem{
		{SenderUserID: "Uweak", ChannelID: "Cx"},
		{SenderUserID: "Ustrong", ChannelID: "Cy"},
	}
	block, err := buildUserPreferencesBlock(d, items)
	if err != nil {
		t.Fatal(err)
	}
	iWeak := strings.Index(block, "sender:Uweak")
	iStrong := strings.Index(block, "sender:Ustrong")
	if iWeak == -1 || iStrong == -1 {
		t.Fatalf("expected both rules in block: %s", block)
	}
	if iStrong > iWeak {
		t.Errorf("expected stronger rule (Ustrong) to appear before weaker rule (Uweak)")
	}
}

func TestBuildUserPrefs_EmptyItems(t *testing.T) {
	d := testDB(t)
	block, err := buildUserPreferencesBlock(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if block != "" {
		t.Errorf("expected empty block for no items, got: %s", block)
	}
}

func TestBuildUserPrefs_DeduplicatesScopes(t *testing.T) {
	d := testDB(t)
	_ = d.UpsertLearnedRule(db.InboxLearnedRule{RuleType: "source_mute", ScopeKey: "sender:U1", Weight: -0.5, Source: "implicit"})

	// Two items with same sender — scope should not be duplicated.
	items := []db.InboxItem{
		{SenderUserID: "U1", ChannelID: "C1"},
		{SenderUserID: "U1", ChannelID: "C2"},
	}
	block, err := buildUserPreferencesBlock(d, items)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(block, "sender:U1"); count != 1 {
		t.Errorf("expected sender:U1 exactly once, got %d times in: %s", count, block)
	}
}
