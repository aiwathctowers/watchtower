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

func TestLearner_MuteOnHighDismissRate(t *testing.T) {
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

func TestLearner_BelowThresholdNoRule(t *testing.T) {
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

func TestLearner_DoesNotOverwriteUserRule(t *testing.T) {
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
