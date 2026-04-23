package inbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"watchtower/internal/db"
)

// newTestDB creates an in-memory DB for testing (alias used by this test file).
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	return testDB(t)
}

// seedDigestWithHighImportance inserts a digest row with the given situations JSON.
// createdAt sets the created_at timestamp of the digest.
func seedDigestWithHighImportance(t *testing.T, d *db.DB, channelID, situations string, createdAt time.Time) {
	t.Helper()
	ts := createdAt.UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO digests
		(channel_id, type, period_from, period_to, summary, situations, created_at)
		VALUES (?, 'channel', 0, 1, '', ?, ?)`,
		channelID, situations, ts)
	if err != nil {
		t.Fatalf("seedDigestWithHighImportance: %v", err)
	}
}

// seedBriefing inserts a briefing row for the given userID and date.
func seedBriefing(t *testing.T, d *db.DB, userID, date string) {
	t.Helper()
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO briefings
		(user_id, date, created_at)
		VALUES (?, ?, ?)`,
		userID, date, ts)
	if err != nil {
		t.Fatalf("seedBriefing: %v", err)
	}
}

func TestWatchtowerDetector_DecisionMade(t *testing.T) {
	d := newTestDB(t)
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"Release postponed","importance":"high"}]`,
		time.Now().Add(-30*time.Minute))

	n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 decision_made, got %d", n)
	}
}

func TestWatchtowerDetector_BriefingReady(t *testing.T) {
	d := newTestDB(t)
	seedBriefing(t, d, "alice", time.Now().Format("2006-01-02"))

	n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("want >=1 briefing_ready, got %d", n)
	}
}

func TestWatchtowerDetector_LowImportanceSkipped(t *testing.T) {
	d := newTestDB(t)
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"minor","importance":"low"}]`,
		time.Now())

	n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("low-importance should be skipped, got %d", n)
	}
}

func TestWatchtowerDetector_Dedup(t *testing.T) {
	d := newTestDB(t)
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"Release postponed","importance":"high"}]`,
		time.Now().Add(-30*time.Minute))

	// First run: creates 1 item.
	n1, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 {
		t.Fatalf("first run: want 1, got %d", n1)
	}

	// Second run: should not create a duplicate.
	n2, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second run: want 0 duplicates, got %d", n2)
	}
}

func TestWatchtowerDetector_MultipleDecisions(t *testing.T) {
	d := newTestDB(t)
	// Two high-importance decisions in one digest.
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"A","importance":"high"},{"type":"decision","topic":"B","importance":"high"},{"type":"decision","topic":"C","importance":"low"}]`,
		time.Now().Add(-30*time.Minute))

	n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want 2 high-importance decisions, got %d", n)
	}
}

func TestWatchtowerDetector_OlderThanSinceSkipped(t *testing.T) {
	d := newTestDB(t)
	// Digest created 2 hours ago, but sinceTS is 1 hour ago.
	seedDigestWithHighImportance(t, d, "C1",
		`[{"type":"decision","topic":"Old","importance":"high"}]`,
		time.Now().Add(-2*time.Hour))

	n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("old digest should be skipped, got %d items", n)
	}
}

func TestWatchtowerDetector_BriefingDedup(t *testing.T) {
	d := newTestDB(t)
	seedBriefing(t, d, "alice", time.Now().Format("2006-01-02"))

	n1, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n1 < 1 {
		t.Fatalf("first run: want >=1, got %d", n1)
	}

	n2, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Errorf("second run: want 0 duplicates, got %d", n2)
	}
}

// seedInboxItemForWT inserts a minimal inbox item and returns its ID (used by dedup tests).
func seedInboxItemForWT(t *testing.T, d *db.DB, channelID, msgTS, triggerType string) int64 {
	t.Helper()
	id, err := d.CreateInboxItem(db.InboxItem{
		ChannelID:    channelID,
		MessageTS:    msgTS,
		SenderUserID: "watchtower",
		TriggerType:  triggerType,
		Snippet:      fmt.Sprintf("test %s", triggerType),
		Status:       "pending",
	})
	if err != nil {
		t.Fatalf("seedInboxItemForWT: %v", err)
	}
	return id
}
