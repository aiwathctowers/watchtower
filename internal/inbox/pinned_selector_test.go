package inbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"watchtower/internal/db"
	"watchtower/internal/digest"
)

// mockGen is a minimal digest.Generator mock for PinnedSelector tests.
type mockGen struct {
	respJSON string
	err      error
}

func (m *mockGen) Generate(_ context.Context, _, _ string, _ string) (string, *digest.Usage, string, error) {
	if m.err != nil {
		return "", nil, "", m.err
	}
	return m.respJSON, &digest.Usage{InputTokens: 10, OutputTokens: 5}, "", nil
}

// jsonArray converts a slice of int64 to a JSON array string like "[1,2,3]".
func jsonArray(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestPinnedSelector_MaxFive(t *testing.T) {
	d := newTestDB(t)
	var ids []int64
	for i := 0; i < 20; i++ {
		id := seedInboxItem(t, d, fmt.Sprintf("U%d", i), "C1", "mention")
		ids = append(ids, id)
		d.Exec(`UPDATE inbox_items SET item_class='actionable', priority='high' WHERE id=?`, id) //nolint:errcheck
	}
	mock := &mockGen{respJSON: fmt.Sprintf(`{"pinned_ids":%s,"reason":"urgent"}`, jsonArray(ids[:10]))}
	p := NewPinnedSelector(d, mock, "")
	n, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n > 5 {
		t.Errorf("capped at 5, got %d", n)
	}
	pinned, _ := d.ListInboxPinned()
	if len(pinned) > 5 {
		t.Errorf("pinned list >5: %d", len(pinned))
	}
}

func TestPinnedSelector_AIFailureKeepsState(t *testing.T) {
	d := newTestDB(t)
	existing := seedInboxItem(t, d, "U1", "C1", "mention")
	d.Exec(`UPDATE inbox_items SET item_class='actionable' WHERE id=?`, existing) //nolint:errcheck
	if err := d.SetInboxPinned([]int64{existing}); err != nil {
		t.Fatal(err)
	}
	mock := &mockGen{err: errors.New("boom")}
	p := NewPinnedSelector(d, mock, "")
	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pinned, _ := d.ListInboxPinned()
	if len(pinned) != 1 || int64(pinned[0].ID) != existing {
		t.Errorf("pinned state should be preserved on AI failure, got %v", pinned)
	}
}

func TestPinnedSelector_InvalidJSONFallback(t *testing.T) {
	d := newTestDB(t)
	id := seedInboxItem(t, d, "U1", "C1", "mention")
	d.Exec(`UPDATE inbox_items SET item_class='actionable' WHERE id=?`, id) //nolint:errcheck
	mock := &mockGen{respJSON: "not-json"}
	p := NewPinnedSelector(d, mock, "")
	if _, err := p.Run(context.Background()); err != nil {
		t.Error("should not fail pipeline on invalid JSON")
	}
}

func TestInbox03_MutedSourcesNotPinned(t *testing.T) {
	// KILLER FEATURE INBOX-03 — see docs/inventory/inbox-pulse.md
	// Muted sources are filtered from pinned regardless of AI suggestion.
	// Do not weaken or remove without explicit owner approval.
	d := newTestDB(t)
	muted := seedInboxItem(t, d, "Umuted", "C1", "mention")
	ok := seedInboxItem(t, d, "Uok", "C1", "mention")
	d.Exec(`UPDATE inbox_items SET item_class='actionable' WHERE id IN (?,?)`, muted, ok) //nolint:errcheck
	if err := d.UpsertLearnedRule(db.InboxLearnedRule{
		RuleType: "source_mute",
		ScopeKey: "sender:Umuted",
		Weight:   -0.9,
		Source:   "user_rule",
	}); err != nil {
		t.Fatal(err)
	}
	mock := &mockGen{respJSON: fmt.Sprintf(`{"pinned_ids":[%d],"reason":"AI still tried"}`, muted)}
	p := NewPinnedSelector(d, mock, "")
	p.Run(context.Background()) //nolint:errcheck
	pinned, _ := d.ListInboxPinned()
	for _, it := range pinned {
		if int64(it.ID) == muted {
			t.Error("muted item was pinned despite rule")
		}
	}
}
