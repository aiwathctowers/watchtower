package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PromoteOverrides controls which inherited fields are overridden when
// promoting a sub-item to a standalone child target. A nil pointer means
// "inherit the default" (from the parent or the sub-item itself for text/due_date).
//
// Inheritance rules used by PromoteSubItemToChild:
//   - text       — sub-item.text (override beats it)
//   - intent     — parent.intent (override beats it)
//   - level      — parent.level (override beats it; switching away from "custom"
//     also clears custom_label)
//   - priority   — parent.priority (override beats it)
//   - ownership  — parent.ownership (override beats it)
//   - period     — parent.period_start / period_end (override beats it)
//   - tags       — parent.tags (override beats it; pass empty JSON array to clear)
//   - due_date   — sub-item.due_date when set, else parent.due_date (override beats both)
//   - ball_on    — parent.ball_on (no override knob; the new child carries the same ball)
//   - blocking   — always cleared on the child
//   - snooze_until — always cleared on the child
//   - status     — "done" if the sub-item was already marked done, else "todo"
//     (keeps parent progress stable across the promote)
type PromoteOverrides struct {
	Text        *string
	Intent      *string
	Level       *string
	Priority    *string
	DueDate     *string
	PeriodStart *string
	PeriodEnd   *string
	Ownership   *string
	Tags        *string // raw JSON string, e.g. `["a","b"]`; pass `"[]"` to clear
}

// promoteSubItem mirrors the JSON shape persisted in targets.sub_items.
type promoteSubItem struct {
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	DueDate string `json:"due_date,omitempty"`
}

// PromoteSubItemToChild atomically converts the sub-item at index idx of the
// parent target into a standalone child target with parent_id = parentID. The
// sub-item is removed from the parent's sub_items JSON and the parent's
// progress is recomputed against the new child set. All side effects (insert
// child, update parent.sub_items, recompute progress) execute inside a single
// transaction; any failure rolls every step back.
//
// The new child has source_type="promoted_subitem" and
// source_id="<parentID>:<originalIdx>" for audit.
//
// Returns the new child target's ID. Errors:
//   - parent not found: returns sql.ErrNoRows wrapped
//   - idx out of range: returns a descriptive error
//   - parent.sub_items not valid JSON: returns a parse error
func (db *DB) PromoteSubItemToChild(parentID int64, idx int, overrides PromoteOverrides) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning promote tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Load parent state needed for inheritance and validation.
	var (
		parentIntent      string
		parentLevel       string
		parentCustomLabel string
		parentPeriodStart string
		parentPeriodEnd   string
		parentPriority    string
		parentOwnership   string
		parentBallOn      string
		parentDueDate     string
		parentTags        string
		parentSubItems    string
	)
	err = tx.QueryRow(`SELECT intent, level, custom_label, period_start, period_end,
		priority, ownership, ball_on, due_date, tags, sub_items
		FROM targets WHERE id = ?`, parentID).Scan(
		&parentIntent, &parentLevel, &parentCustomLabel,
		&parentPeriodStart, &parentPeriodEnd,
		&parentPriority, &parentOwnership, &parentBallOn, &parentDueDate,
		&parentTags, &parentSubItems,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("parent target %d not found: %w", parentID, err)
		}
		return 0, fmt.Errorf("loading parent target %d: %w", parentID, err)
	}

	// Parse sub_items JSON.
	var items []promoteSubItem
	if parentSubItems != "" && parentSubItems != "[]" {
		if err := json.Unmarshal([]byte(parentSubItems), &items); err != nil {
			return 0, fmt.Errorf("parsing parent sub_items: %w", err)
		}
	}
	if idx < 0 || idx >= len(items) {
		return 0, fmt.Errorf("sub-item index %d out of range [0, %d)", idx, len(items))
	}
	original := items[idx]

	// Build the child target with inheritance + overrides.
	childText := original.Text
	if overrides.Text != nil {
		childText = *overrides.Text
	}
	childIntent := parentIntent
	if overrides.Intent != nil {
		childIntent = *overrides.Intent
	}
	childLevel := parentLevel
	if overrides.Level != nil {
		childLevel = *overrides.Level
	}
	childPriority := parentPriority
	if overrides.Priority != nil {
		childPriority = *overrides.Priority
	}
	childOwnership := parentOwnership
	if overrides.Ownership != nil {
		childOwnership = *overrides.Ownership
	}
	childPeriodStart := parentPeriodStart
	if overrides.PeriodStart != nil {
		childPeriodStart = *overrides.PeriodStart
	}
	childPeriodEnd := parentPeriodEnd
	if overrides.PeriodEnd != nil {
		childPeriodEnd = *overrides.PeriodEnd
	}
	// Due date precedence: sub-item's own due_date > parent's due_date >
	// override. The override beats either inherited value when set.
	childDueDate := original.DueDate
	if childDueDate == "" {
		childDueDate = parentDueDate
	}
	if overrides.DueDate != nil {
		childDueDate = *overrides.DueDate
	}
	childTags := parentTags
	if overrides.Tags != nil {
		childTags = *overrides.Tags
	}

	// Custom label is meaningful only when the level stays "custom".
	childCustomLabel := ""
	if childLevel == "custom" {
		childCustomLabel = parentCustomLabel
	}

	// Initial status mirrors the sub-item's done flag so that a sub-item already
	// checked off becomes a "done" child (not a new todo). This keeps the parent
	// progress stable across the promote operation.
	childStatus := "todo"
	if original.Done {
		childStatus = "done"
	}
	childProgress := statusToProgress(childStatus)

	sourceID := fmt.Sprintf("%d:%d", parentID, idx)

	res, err := tx.Exec(`INSERT INTO targets
		(text, intent, level, custom_label, period_start, period_end, parent_id,
		 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
		 tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '',
		        ?, '[]', '[]', ?, 'promoted_subitem', ?, NULL)`,
		childText, childIntent, childLevel, childCustomLabel,
		childPeriodStart, childPeriodEnd, parentID,
		childStatus, childPriority, childOwnership, parentBallOn, childDueDate,
		childTags, childProgress, sourceID,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting promoted child: %w", err)
	}
	childID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting child id: %w", err)
	}

	// Remove the promoted sub-item from the parent's sub_items JSON.
	remaining := make([]promoteSubItem, 0, len(items)-1)
	remaining = append(remaining, items[:idx]...)
	remaining = append(remaining, items[idx+1:]...)
	newSubItemsJSON := "[]"
	if len(remaining) > 0 {
		buf, err := json.Marshal(remaining)
		if err != nil {
			return 0, fmt.Errorf("marshaling remaining sub_items: %w", err)
		}
		newSubItemsJSON = string(buf)
	}

	if _, err := tx.Exec(`UPDATE targets SET sub_items = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ?`, newSubItemsJSON, parentID); err != nil {
		return 0, fmt.Errorf("updating parent sub_items: %w", err)
	}

	// Recompute parent progress *inside* the same transaction so any failure
	// rolls back the child INSERT and the sub_items UPDATE together with the
	// progress write — true atomicity, not best-effort fixup after commit.
	if err := recomputeParentProgressOn(tx, parentID); err != nil {
		return 0, fmt.Errorf("recomputing parent progress: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing promote tx: %w", err)
	}

	return childID, nil
}
