package inbox

import (
	"context"
	"testing"
)

func TestFeedback_NeverShow_CreatesHardMute(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U1", "C1", "mention")
	err := SubmitFeedback(context.Background(), d, id, -1, "never_show")
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "sender:U1")
	if err != nil {
		t.Fatalf("no mute rule: %v", err)
	}
	if r.Weight != -1.0 {
		t.Errorf("weight=%v want -1.0", r.Weight)
	}
	if r.Source != "explicit_feedback" {
		t.Errorf("source=%s", r.Source)
	}
}

func TestFeedback_SourceNoise_WeakerMute(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U2", "C1", "mention")
	SubmitFeedback(context.Background(), d, id, -1, "source_noise") //nolint:errcheck
	r, _ := d.GetLearnedRule("source_mute", "sender:U2")
	if r.Weight != -0.8 {
		t.Errorf("weight=%v want -0.8", r.Weight)
	}
}

func TestFeedback_WrongClass_DowngradesItem(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U3", "C1", "mention") // default actionable
	SubmitFeedback(context.Background(), d, id, -1, "wrong_class")  //nolint:errcheck
	var cls string
	d.QueryRow(`SELECT item_class FROM inbox_items WHERE id=?`, id).Scan(&cls) //nolint:errcheck
	if cls != "ambient" {
		t.Errorf("class=%s, want ambient", cls)
	}
}

func TestFeedback_PositiveBoost(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	id := seedInboxItem(t, d, "U4", "C1", "mention")
	SubmitFeedback(context.Background(), d, id, 1, "") //nolint:errcheck
	r, err := d.GetLearnedRule("source_boost", "sender:U4")
	if err != nil {
		t.Fatalf("expected boost rule: %v", err)
	}
	if r.Weight != 0.6 {
		t.Errorf("weight=%v want 0.6", r.Weight)
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
