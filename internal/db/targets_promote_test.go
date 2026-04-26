package db

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strPtr is a helper for building PromoteOverrides literals in tests.
func strPtr(s string) *string { return &s }

// makeParentWithSubItems creates a parent target with the given sub-item texts
// (all undone, no due_date) and returns its ID.
func makeParentWithSubItems(t *testing.T, db *DB, texts ...string) int64 {
	t.Helper()
	items := make([]promoteSubItem, 0, len(texts))
	for _, s := range texts {
		items = append(items, promoteSubItem{Text: s, Done: false})
	}
	buf, err := json.Marshal(items)
	require.NoError(t, err)

	parent := Target{
		Text:        "Parent target",
		Intent:      "the why",
		Status:      "in_progress",
		Priority:    "high",
		Ownership:   "mine",
		Level:       "week",
		PeriodStart: "2026-04-20",
		PeriodEnd:   "2026-04-26",
		BallOn:      "alice",
		Tags:        `["a","b"]`,
		SubItems:    string(buf),
		SourceType:  "manual",
	}
	id, err := db.CreateTarget(parent)
	require.NoError(t, err)
	return id
}

// ── Happy path: defaults inherited ──────────────────────────────────────────

func TestPromoteSubItemToChild_DefaultsInherited(t *testing.T) {
	db := openTestDB(t)

	parentID := makeParentWithSubItems(t, db, "first", "second", "third")

	childID, err := db.PromoteSubItemToChild(int(parentID), 1, PromoteOverrides{})
	require.NoError(t, err)
	require.Greater(t, childID, int64(0))

	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "second", child.Text, "text inherited from sub-item")
	assert.Equal(t, "the why", child.Intent, "intent inherited from parent")
	assert.Equal(t, "week", child.Level, "level inherited from parent")
	assert.Equal(t, "high", child.Priority, "priority inherited from parent")
	assert.Equal(t, "mine", child.Ownership, "ownership inherited from parent")
	assert.Equal(t, "2026-04-20", child.PeriodStart)
	assert.Equal(t, "2026-04-26", child.PeriodEnd)
	assert.Equal(t, `["a","b"]`, child.Tags, "tags inherited from parent")
	assert.Equal(t, "todo", child.Status, "fresh sub-item promotes to todo")
	assert.Equal(t, "promoted_subitem", child.SourceType)
	assert.Equal(t, "1:1", child.SourceID, "source_id encodes parent:idx")
	require.True(t, child.ParentID.Valid)
	assert.Equal(t, parentID, child.ParentID.Int64)
}

// ── Sub-item removed from parent ─────────────────────────────────────────────

func TestPromoteSubItemToChild_RemovesSubItemFromParent(t *testing.T) {
	db := openTestDB(t)

	parentID := makeParentWithSubItems(t, db, "alpha", "beta", "gamma")

	_, err := db.PromoteSubItemToChild(int(parentID), 1, PromoteOverrides{})
	require.NoError(t, err)

	parent, err := db.GetTargetByID(int(parentID))
	require.NoError(t, err)

	var remaining []promoteSubItem
	require.NoError(t, json.Unmarshal([]byte(parent.SubItems), &remaining))
	require.Len(t, remaining, 2)
	assert.Equal(t, "alpha", remaining[0].Text)
	assert.Equal(t, "gamma", remaining[1].Text, "middle item is removed, order preserved")
}

// ── Sub-item due_date carries over to child ─────────────────────────────────

func TestPromoteSubItemToChild_SubItemDueDateInherited(t *testing.T) {
	db := openTestDB(t)

	items := []promoteSubItem{
		{Text: "no date"},
		{Text: "with date", DueDate: "2026-05-01T10:00"},
	}
	buf, _ := json.Marshal(items)
	parent := Target{
		Text:       "P",
		Status:     "todo",
		Priority:   "medium",
		Ownership:  "mine",
		Level:      "day",
		SourceType: "manual",
		SubItems:   string(buf),
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	childID, err := db.PromoteSubItemToChild(int(parentID), 1, PromoteOverrides{})
	require.NoError(t, err)
	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "2026-05-01T10:00", child.DueDate)
}

// ── Done sub-item promotes to done child ────────────────────────────────────

func TestPromoteSubItemToChild_DoneSubItemBecomesDoneChild(t *testing.T) {
	db := openTestDB(t)

	items := []promoteSubItem{{Text: "already finished", Done: true}}
	buf, _ := json.Marshal(items)
	parent := Target{
		Text:       "P",
		Status:     "in_progress",
		Priority:   "medium",
		Ownership:  "mine",
		Level:      "day",
		SourceType: "manual",
		SubItems:   string(buf),
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	childID, err := db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{})
	require.NoError(t, err)
	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "done", child.Status, "done sub-item should become a done child")
	assert.InDelta(t, 1.0, child.Progress, 0.001)
}

// ── Overrides win over inherited defaults ───────────────────────────────────

func TestPromoteSubItemToChild_OverridesApplied(t *testing.T) {
	db := openTestDB(t)

	parentID := makeParentWithSubItems(t, db, "rough text")

	childID, err := db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{
		Text:        strPtr("polished text"),
		Intent:      strPtr("custom intent"),
		Level:       strPtr("day"),
		Priority:    strPtr("low"),
		Ownership:   strPtr("delegated"),
		DueDate:     strPtr("2026-04-30T17:00"),
		PeriodStart: strPtr("2026-04-30"),
		PeriodEnd:   strPtr("2026-04-30"),
		Tags:        strPtr(`["x","y"]`),
	})
	require.NoError(t, err)

	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "polished text", child.Text)
	assert.Equal(t, "custom intent", child.Intent)
	assert.Equal(t, "day", child.Level)
	assert.Equal(t, "low", child.Priority)
	assert.Equal(t, "delegated", child.Ownership)
	assert.Equal(t, "2026-04-30T17:00", child.DueDate)
	assert.Equal(t, "2026-04-30", child.PeriodStart)
	assert.Equal(t, "2026-04-30", child.PeriodEnd)
	assert.Equal(t, `["x","y"]`, child.Tags)
}

// ── Integration: all three effects in a single happy path ───────────────────

// TestPromoteSubItemToChild_FullEffectsAtomic asserts that a successful
// promote produces all three side effects atomically:
//  1. the parent's sub_items shrinks by one (the promoted item removed),
//  2. a child target with parent_id = parentID and the right source ref appears,
//  3. the parent's progress is recomputed against the new child average.
func TestPromoteSubItemToChild_FullEffectsAtomic(t *testing.T) {
	db := openTestDB(t)

	items := []promoteSubItem{{Text: "alpha"}, {Text: "beta", Done: true}, {Text: "gamma"}}
	buf, _ := json.Marshal(items)
	parent := Target{
		Text:       "Parent",
		Status:     "in_progress",
		Priority:   "high",
		Ownership:  "mine",
		Level:      "week",
		SourceType: "manual",
		SubItems:   string(buf),
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	childID, err := db.PromoteSubItemToChild(int(parentID), 1, PromoteOverrides{})
	require.NoError(t, err)
	require.Greater(t, childID, int64(0))

	// (1) parent.sub_items shrank by 1, surviving items kept order.
	updated, err := db.GetTargetByID(int(parentID))
	require.NoError(t, err)
	var remaining []promoteSubItem
	require.NoError(t, json.Unmarshal([]byte(updated.SubItems), &remaining))
	require.Len(t, remaining, 2)
	assert.Equal(t, "alpha", remaining[0].Text)
	assert.Equal(t, "gamma", remaining[1].Text)

	// (2) child target exists with parent_id and audit source ref.
	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "beta", child.Text)
	require.True(t, child.ParentID.Valid)
	assert.Equal(t, parentID, child.ParentID.Int64)
	assert.Equal(t, "promoted_subitem", child.SourceType)
	assert.Equal(t, "1:1", child.SourceID, "source_id must encode parent:originalIdx")
	assert.Equal(t, "done", child.Status, "done sub-item promotes to a done child")

	// (3) parent progress recomputed: only one non-dismissed child (done=1.0) → AVG=1.0.
	assert.InDelta(t, 1.0, updated.Progress, 0.001,
		"parent progress should reflect AVG of the new child(ren), not the parent's own status")

	// Sanity: GetTargets with ParentID filter returns exactly the new child.
	pidPtr := parentID
	children, err := db.GetTargets(TargetFilter{ParentID: &pidPtr, IncludeDone: true})
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, childID, int64(children[0].ID))
}

// ── Parent progress recomputed after promote ─────────────────────────────────

func TestPromoteSubItemToChild_RecomputesParentProgress(t *testing.T) {
	db := openTestDB(t)

	// Parent (in_progress = 0.5 by status) with one sub-item.
	items := []promoteSubItem{{Text: "the sub", Done: false}}
	buf, _ := json.Marshal(items)
	parent := Target{
		Text:       "Parent",
		Status:     "in_progress",
		Priority:   "high",
		Ownership:  "mine",
		Level:      "week",
		SourceType: "manual",
		SubItems:   string(buf),
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	// Initially no children → parent progress derived from status (0.5).
	got, err := db.GetTargetByID(int(parentID))
	require.NoError(t, err)
	assert.InDelta(t, 0.5, got.Progress, 0.001)

	// Promote with status=todo → child.progress = 0.0 → AVG = 0.0.
	_, err = db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{})
	require.NoError(t, err)

	got, err = db.GetTargetByID(int(parentID))
	require.NoError(t, err)
	assert.InDelta(t, 0.0, got.Progress, 0.001,
		"parent progress should be AVG of its single new child (todo=0.0)")
}

// ── Error: invalid index ────────────────────────────────────────────────────

func TestPromoteSubItemToChild_InvalidIndex(t *testing.T) {
	db := openTestDB(t)

	parentID := makeParentWithSubItems(t, db, "only one")

	_, err := db.PromoteSubItemToChild(int(parentID), 5, PromoteOverrides{})
	assert.Error(t, err, "out-of-range index must error")

	_, err = db.PromoteSubItemToChild(int(parentID), -1, PromoteOverrides{})
	assert.Error(t, err, "negative index must error")
}

// ── Error: empty sub_items ──────────────────────────────────────────────────

func TestPromoteSubItemToChild_EmptySubItems(t *testing.T) {
	db := openTestDB(t)

	parent := Target{
		Text:       "Empty",
		Status:     "todo",
		Priority:   "medium",
		Ownership:  "mine",
		Level:      "day",
		SourceType: "manual",
		SubItems:   "[]",
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	_, err = db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{})
	assert.Error(t, err, "empty sub_items must error on any index")
}

// ── Error: parent not found ─────────────────────────────────────────────────

func TestPromoteSubItemToChild_ParentNotFound(t *testing.T) {
	db := openTestDB(t)

	_, err := db.PromoteSubItemToChild(99999, 0, PromoteOverrides{})
	assert.Error(t, err, "missing parent must error")
}

// ── Custom label preserved only when level remains custom ───────────────────

func TestPromoteSubItemToChild_CustomLabel(t *testing.T) {
	db := openTestDB(t)

	items := []promoteSubItem{{Text: "child"}}
	buf, _ := json.Marshal(items)
	parent := Target{
		Text:        "P",
		Status:      "todo",
		Priority:    "medium",
		Ownership:   "mine",
		Level:       "custom",
		CustomLabel: "Q1 OKR",
		SourceType:  "manual",
		SubItems:    string(buf),
	}
	parentID, err := db.CreateTarget(parent)
	require.NoError(t, err)

	// Default: keep level=custom → custom_label inherited.
	childID, err := db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{})
	require.NoError(t, err)
	child, err := db.GetTargetByID(int(childID))
	require.NoError(t, err)
	assert.Equal(t, "custom", child.Level)
	assert.Equal(t, "Q1 OKR", child.CustomLabel)

	// Make a fresh sub-item to promote with override level=day → custom_label dropped.
	parent2 := parent
	parent2.SubItems = string(buf)
	parent2ID, err := db.CreateTarget(parent2)
	require.NoError(t, err)
	dayLevel := "day"
	child2ID, err := db.PromoteSubItemToChild(int(parent2ID), 0, PromoteOverrides{Level: &dayLevel})
	require.NoError(t, err)
	child2, err := db.GetTargetByID(int(child2ID))
	require.NoError(t, err)
	assert.Equal(t, "day", child2.Level)
	assert.Equal(t, "", child2.CustomLabel, "custom_label cleared when level switches away from custom")
}

// ── Sub-items JSON normalized to "[]" when last item promoted ───────────────

func TestPromoteSubItemToChild_LastItemLeavesEmptyArray(t *testing.T) {
	db := openTestDB(t)

	parentID := makeParentWithSubItems(t, db, "only one")

	_, err := db.PromoteSubItemToChild(int(parentID), 0, PromoteOverrides{})
	require.NoError(t, err)

	parent, err := db.GetTargetByID(int(parentID))
	require.NoError(t, err)
	assert.Equal(t, "[]", parent.SubItems, "removing the last sub-item leaves a normalized empty array")
}
