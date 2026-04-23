package targets

import (
	"context"
	"testing"

	"watchtower/internal/db"
)

func TestLinkExisting_HappyPath(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	// Insert parent target.
	parentID, err := d.CreateTarget(db.Target{
		Text:        "Q2 OKR: ship product",
		Level:       "quarter",
		PeriodStart: "2026-04-01",
		PeriodEnd:   "2026-06-30",
		Status:      "todo",
		Priority:    "high",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	// Insert the target we want to link.
	targetID, err := d.CreateTarget(db.Target{
		Text:        "Write API spec",
		Level:       "week",
		PeriodStart: "2026-04-21",
		PeriodEnd:   "2026-04-27",
		Status:      "todo",
		Priority:    "medium",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	response := `{
		"parent_id": ` + itoa(parentID) + `,
		"secondary_links": [
			{"target_id": ` + itoa(parentID) + `, "relation": "contributes_to", "confidence": 0.85}
		]
	}`

	gen := &mockGenerator{responses: []string{response}}
	p := New(d, nil, gen, nil, nil)

	result, err := p.LinkExisting(context.Background(), targetID)
	if err != nil {
		t.Fatalf("LinkExisting: %v", err)
	}

	if !result.ParentID.Valid || result.ParentID.Int64 != parentID {
		t.Errorf("want parent_id=%d, got %+v", parentID, result.ParentID)
	}
	if len(result.SecondaryLinks) != 1 {
		t.Fatalf("want 1 secondary link, got %d", len(result.SecondaryLinks))
	}
	if result.SecondaryLinks[0].Relation != "contributes_to" {
		t.Errorf("unexpected relation: %q", result.SecondaryLinks[0].Relation)
	}
}

func TestLinkExisting_UnknownParentNulled(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	targetID, err := d.CreateTarget(db.Target{
		Text:        "A standalone task",
		Level:       "day",
		PeriodStart: "2026-04-23",
		PeriodEnd:   "2026-04-23",
		Status:      "todo",
		Priority:    "low",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	// AI proposes a parent_id that doesn't exist in snapshot.
	response := `{"parent_id": 99999, "secondary_links": []}`
	gen := &mockGenerator{responses: []string{response}}
	p := New(d, nil, gen, nil, nil)

	result, err := p.LinkExisting(context.Background(), targetID)
	if err != nil {
		t.Fatalf("LinkExisting: %v", err)
	}
	if result.ParentID.Valid {
		t.Errorf("want parent_id=NULL for unknown id, got %+v", result.ParentID)
	}
}

func TestLinkExisting_ExternalRefLink(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	targetID, err := d.CreateTarget(db.Target{
		Text:        "Review PR for PROJ-7",
		Level:       "day",
		PeriodStart: "2026-04-23",
		PeriodEnd:   "2026-04-23",
		Status:      "todo",
		Priority:    "medium",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	response := `{
		"parent_id": null,
		"secondary_links": [
			{"external_ref": "jira:PROJ-7", "relation": "related"}
		]
	}`
	gen := &mockGenerator{responses: []string{response}}
	p := New(d, nil, gen, nil, nil)

	result, err := p.LinkExisting(context.Background(), targetID)
	if err != nil {
		t.Fatalf("LinkExisting: %v", err)
	}
	if len(result.SecondaryLinks) != 1 {
		t.Fatalf("want 1 link, got %d", len(result.SecondaryLinks))
	}
	if result.SecondaryLinks[0].ExternalRef != "jira:PROJ-7" {
		t.Errorf("unexpected external_ref: %q", result.SecondaryLinks[0].ExternalRef)
	}
}

func TestBuildLinkPrompt_ContainsTargetInfo(t *testing.T) {
	target := db.Target{
		ID:          5,
		Text:        "Finish the report",
		Intent:      "For board meeting",
		Level:       "week",
		PeriodStart: "2026-04-21",
		PeriodEnd:   "2026-04-27",
		Status:      "todo",
		Priority:    "high",
	}
	snapshot := []db.Target{
		{ID: 1, Text: "Q2 OKR", Level: "quarter", PeriodStart: "2026-04-01", PeriodEnd: "2026-06-30", Status: "todo", Priority: "high"},
	}
	prompt := buildLinkPrompt(target, snapshot)

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Target info present.
	for _, want := range []string{"Finish the report", "For board meeting", "id=5", "Q2 OKR"} {
		if !containsStr(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// itoa converts int64 to string for test JSON building.
func itoa(n int64) string {
	return itoa64(n)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
