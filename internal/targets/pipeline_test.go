package targets

import (
	"context"
	"fmt"
	"testing"

	"watchtower/internal/db"
	"watchtower/internal/digest"
)

// mockGenerator implements digest.Generator for tests.
type mockGenerator struct {
	responses []string // returned in order; last is repeated if exhausted
	callCount int
	err       error
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	if m.err != nil {
		return "", nil, "", m.err
	}
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], &digest.Usage{InputTokens: 100, OutputTokens: 50}, "", nil
}

// --- helpers ---

func makeTestPipeline(t *testing.T, gen digest.Generator) (*Pipeline, *db.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	p := New(d, nil, gen, nil, nil)
	return p, d
}

// --- Extract tests ---

func TestPipeline_Extract_HappyPath(t *testing.T) {
	// Insert two active targets into DB so parent_id resolution works.
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	parentID, err := d.CreateTarget(db.Target{
		Text:        "Q2 OKR: grow revenue",
		Level:       "quarter",
		PeriodStart: "2026-04-01",
		PeriodEnd:   "2026-06-30",
		Status:      "todo",
		Priority:    "high",
		Ownership:   "mine",
		SourceType:  "manual",
	})
	if err != nil {
		t.Fatalf("create parent target: %v", err)
	}

	response := fmt.Sprintf(`{
		"extracted": [
			{
				"text": "Draft API spec for v2 endpoints",
				"intent": "Needed for Q2 milestone",
				"level": "week",
				"level_confidence": 0.9,
				"period_start": "2026-04-21",
				"period_end": "2026-04-27",
				"priority": "high",
				"due_date": "",
				"parent_id": %d,
				"secondary_links": [
					{"external_ref": "jira:PROJ-42", "relation": "contributes_to", "confidence": 0.8}
				]
			},
			{
				"text": "Review PR for onboarding changes",
				"level": "day",
				"level_confidence": 0.95,
				"period_start": "2026-04-23",
				"period_end": "2026-04-23",
				"priority": "medium",
				"due_date": "",
				"parent_id": null,
				"secondary_links": []
			}
		],
		"omitted_count": 0,
		"notes": ""
	}`, parentID)

	gen := &mockGenerator{responses: []string{response}}
	p := New(d, nil, gen, nil, nil)

	result, err := p.Extract(context.Background(), ExtractRequest{
		RawText:    "Need to draft API spec (PROJ-42) and review PR",
		EntryPoint: "cli",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(result.Extracted) != 2 {
		t.Fatalf("want 2 targets, got %d", len(result.Extracted))
	}

	// First target: parent_id resolved, external_ref link present.
	first := result.Extracted[0]
	if !first.ParentID.Valid || first.ParentID.Int64 != parentID {
		t.Errorf("want parent_id=%d, got %+v", parentID, first.ParentID)
	}
	if len(first.SecondaryLinks) != 1 {
		t.Fatalf("want 1 secondary link, got %d", len(first.SecondaryLinks))
	}
	if first.SecondaryLinks[0].ExternalRef != "jira:PROJ-42" {
		t.Errorf("want external_ref=jira:PROJ-42, got %q", first.SecondaryLinks[0].ExternalRef)
	}

	// Second target: no parent.
	second := result.Extracted[1]
	if second.ParentID.Valid {
		t.Errorf("want no parent, got %+v", second.ParentID)
	}
}

func TestPipeline_Extract_CapEnforcement(t *testing.T) {
	// AI returns 12 items — should be trimmed to 10, OmittedCount += 2.
	var items []string
	for i := 0; i < 12; i++ {
		items = append(items, fmt.Sprintf(`{
			"text": "Target %d",
			"level": "day",
			"level_confidence": 0.8,
			"period_start": "2026-04-23",
			"period_end": "2026-04-23",
			"priority": "medium",
			"due_date": "",
			"parent_id": null,
			"secondary_links": []
		}`, i))
	}
	// Build JSON manually.
	jsonItems := "["
	for i, item := range items {
		if i > 0 {
			jsonItems += ","
		}
		jsonItems += item
	}
	jsonItems += "]"

	response := fmt.Sprintf(`{"extracted": %s, "omitted_count": 0, "notes": ""}`, jsonItems)
	gen := &mockGenerator{responses: []string{response}}
	p, _ := makeTestPipeline(t, gen)

	result, err := p.Extract(context.Background(), ExtractRequest{RawText: "lots of tasks"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Extracted) != 10 {
		t.Errorf("want 10 extracted, got %d", len(result.Extracted))
	}
	if result.OmittedCount != 2 {
		t.Errorf("want OmittedCount=2, got %d", result.OmittedCount)
	}
}

func TestPipeline_Extract_MalformedJSONRetryThenFail(t *testing.T) {
	gen := &mockGenerator{responses: []string{"not json at all", "still not json"}}
	p, _ := makeTestPipeline(t, gen)

	_, err := p.Extract(context.Background(), ExtractRequest{RawText: "text"})
	if err == nil {
		t.Fatal("expected error after two malformed JSON responses")
	}
	if gen.callCount != 2 {
		t.Errorf("want 2 AI calls (1 initial + 1 retry), got %d", gen.callCount)
	}
}

func TestPipeline_Extract_UnknownParentIDNulled(t *testing.T) {
	response := `{
		"extracted": [{
			"text": "Task with bad parent",
			"level": "day",
			"level_confidence": 0.7,
			"period_start": "2026-04-23",
			"period_end": "2026-04-23",
			"priority": "low",
			"due_date": "",
			"parent_id": 99999,
			"secondary_links": []
		}],
		"omitted_count": 0,
		"notes": ""
	}`
	gen := &mockGenerator{responses: []string{response}}
	p, _ := makeTestPipeline(t, gen)

	result, err := p.Extract(context.Background(), ExtractRequest{RawText: "text"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Extracted) != 1 {
		t.Fatalf("want 1 target, got %d", len(result.Extracted))
	}
	if result.Extracted[0].ParentID.Valid {
		t.Errorf("want parent_id=NULL (unknown id nulled), got %+v", result.Extracted[0].ParentID)
	}
}

// Regression: a single Russian goal with sub-steps must parse into ONE
// target carrying the steps as sub_items, preserving the original language.
// (See docs/superpowers/plans/... and the extractor prompt GROUPING rule.)
func TestPipeline_Extract_SingleGoalWithSubItems(t *testing.T) {
	response := `{
		"extracted": [{
			"text": "Актуализировать и согласовать план найма по разработке",
			"intent": "Сверить видение с хедами и подать единый план",
			"level": "week",
			"level_confidence": 0.8,
			"period_start": "2026-04-21",
			"period_end": "2026-04-27",
			"priority": "high",
			"due_date": "",
			"parent_id": null,
			"sub_items": [
				{"text": "Пообщаться со всеми хедами по поводу плана найма"},
				{"text": "Подготовить план от всей разработки"},
				{"text": "Актуализировать план от QA и подать всем скопом"},
				{"text": "Учесть влияние LLM Dev Pipelines"}
			],
			"secondary_links": []
		}],
		"omitted_count": 0,
		"notes": ""
	}`
	gen := &mockGenerator{responses: []string{response}}
	p, _ := makeTestPipeline(t, gen)

	result, err := p.Extract(context.Background(), ExtractRequest{
		RawText: "Пообщаться со всеми хедами по поводу плана найма. Подготовить план от всей разработки. Забрили план от QA - надо актуализировать и подать сново всем скопом. Плюс учесть как LLM Dev Pipelines на это влияет",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Extracted) != 1 {
		t.Fatalf("want 1 target (single goal with steps), got %d", len(result.Extracted))
	}
	tgt := result.Extracted[0]
	if len(tgt.SubItems) != 4 {
		t.Fatalf("want 4 sub_items, got %d", len(tgt.SubItems))
	}
	if tgt.SubItems[0].Text != "Пообщаться со всеми хедами по поводу плана найма" {
		t.Errorf("sub_item 0 text not preserved verbatim: %q", tgt.SubItems[0].Text)
	}
	if tgt.Text == "" || !containsAnyCyrillic(tgt.Text) {
		t.Errorf("expected Russian target text, got %q", tgt.Text)
	}
}

// TestPipeline_Extract_SubItemsCapped ensures runaway AI responses are trimmed.
func TestPipeline_Extract_SubItemsCapped(t *testing.T) {
	var items []string
	for i := 0; i < 20; i++ {
		items = append(items, fmt.Sprintf(`{"text": "step %d"}`, i))
	}
	response := fmt.Sprintf(`{
		"extracted": [{
			"text": "big goal",
			"level": "week",
			"level_confidence": 0.7,
			"period_start": "2026-04-21",
			"period_end": "2026-04-27",
			"priority": "medium",
			"due_date": "",
			"parent_id": null,
			"sub_items": [%s],
			"secondary_links": []
		}],
		"omitted_count": 0,
		"notes": ""
	}`, stringJoin(items, ","))
	gen := &mockGenerator{responses: []string{response}}
	p, _ := makeTestPipeline(t, gen)

	result, err := p.Extract(context.Background(), ExtractRequest{RawText: "text"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Extracted[0].SubItems) != 15 {
		t.Errorf("want 15 sub_items (capped), got %d", len(result.Extracted[0].SubItems))
	}
}

// TestPipeline_CreateFromExtraction_PersistsSubItems verifies the store writes
// sub_items as JSON into the targets.sub_items column.
func TestPipeline_CreateFromExtraction_PersistsSubItems(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	items := []ProposedTarget{{
		Text:        "Goal with steps",
		Level:       "week",
		PeriodStart: "2026-04-21",
		PeriodEnd:   "2026-04-27",
		Priority:    "high",
		SubItems: []ProposedSubItem{
			{Text: "step one"},
			{Text: "step two"},
		},
	}}

	p := New(d, nil, nil, nil, nil)
	ids, err := p.CreateFromExtraction(context.Background(), items, "extract", "")
	if err != nil {
		t.Fatalf("CreateFromExtraction: %v", err)
	}
	got, err := d.GetTargetByID(int(ids[0]))
	if err != nil {
		t.Fatalf("GetTargetByID: %v", err)
	}
	if got.SubItems == "" || got.SubItems == "[]" {
		t.Fatalf("expected non-empty sub_items JSON, got %q", got.SubItems)
	}
	// Shape check without pulling a schema struct here.
	for _, needle := range []string{`"text":"step one"`, `"text":"step two"`, `"done":false`} {
		if !contains(got.SubItems, needle) {
			t.Errorf("sub_items JSON missing %q, got %q", needle, got.SubItems)
		}
	}
}

func containsAnyCyrillic(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if haystack[i:i+n] == needle {
			return i
		}
	}
	return -1
}

func stringJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += sep + parts[i]
	}
	return out
}

func TestPipeline_Extract_SecondaryLinksCapped(t *testing.T) {
	response := `{
		"extracted": [{
			"text": "Task with many links",
			"level": "week",
			"level_confidence": 0.8,
			"period_start": "2026-04-21",
			"period_end": "2026-04-27",
			"priority": "medium",
			"due_date": "",
			"parent_id": null,
			"secondary_links": [
				{"external_ref": "jira:A-1", "relation": "contributes_to"},
				{"external_ref": "jira:A-2", "relation": "related"},
				{"external_ref": "jira:A-3", "relation": "blocks"},
				{"external_ref": "jira:A-4", "relation": "duplicates"}
			]
		}],
		"omitted_count": 0,
		"notes": ""
	}`
	gen := &mockGenerator{responses: []string{response}}
	p, _ := makeTestPipeline(t, gen)

	result, err := p.Extract(context.Background(), ExtractRequest{RawText: "text"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Extracted[0].SecondaryLinks) != 3 {
		t.Errorf("want 3 secondary links (capped), got %d", len(result.Extracted[0].SecondaryLinks))
	}
}

// --- CreateFromExtraction tests ---

func TestPipeline_CreateFromExtraction_HappyPath(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	items := []ProposedTarget{
		{
			Text:        "Write unit tests",
			Level:       "day",
			PeriodStart: "2026-04-23",
			PeriodEnd:   "2026-04-23",
			Priority:    "high",
		},
		{
			Text:        "Ship feature",
			Level:       "week",
			PeriodStart: "2026-04-21",
			PeriodEnd:   "2026-04-27",
			Priority:    "medium",
		},
	}

	p := New(d, nil, nil, nil, nil)
	ids, err := p.CreateFromExtraction(context.Background(), items, "extract", "")
	if err != nil {
		t.Fatalf("CreateFromExtraction: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 ids, got %d", len(ids))
	}

	// Verify DB.
	got, err := d.GetTargetByID(int(ids[0]))
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if got.Text != "Write unit tests" {
		t.Errorf("unexpected text: %q", got.Text)
	}
}

func TestPipeline_CreateFromExtraction_TxRollbackOnFailure(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	// Use an invalid level to trigger a DB CHECK constraint failure.
	items := []ProposedTarget{
		{
			Text:        "Valid target",
			Level:       "day",
			PeriodStart: "2026-04-23",
			PeriodEnd:   "2026-04-23",
			Priority:    "high",
		},
		{
			Text:        "Invalid level target",
			Level:       "invalid_level", // violates CHECK constraint
			PeriodStart: "2026-04-23",
			PeriodEnd:   "2026-04-23",
			Priority:    "medium",
		},
	}

	p := New(d, nil, nil, nil, nil)
	_, err = p.CreateFromExtraction(context.Background(), items, "extract", "")
	if err == nil {
		t.Fatal("expected error on constraint violation")
	}

	// First target should not be in DB (rollback).
	targets, err := d.GetTargets(db.TargetFilter{IncludeDone: true})
	if err != nil {
		t.Fatalf("GetTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("want 0 targets after rollback, got %d", len(targets))
	}
}
