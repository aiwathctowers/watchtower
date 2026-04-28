# INBOX-04 — Gradual Explicit Feedback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close INBOX-04 inventory gap: refactor `internal/inbox/feedback.go` so non-`never_show` ratings stop creating rules instantly, extend `internal/inbox/learner.go` to aggregate explicit feedback alongside implicit dismissals, and add migration v72 to drop legacy `source='explicit_feedback'` rules. Promote INBOX-04 from `Partial` to `Enforced`.

**Architecture:** Single source of truth for automatic rule writes is the implicit learner. `feedback.go` writes only raw `inbox_feedback` rows (audit trail), with two exceptions: (1) `(-1, never_show)` upserts a `source='user_rule'` mute weight `-1.0` as a one-click escape hatch; (2) `(-1, wrong_class)` flips this item's class to `ambient` (per-item correction, not learning). The learner reads from `inbox_items` AND `inbox_feedback` over a 30-day window, computes per-sender and per-channel positive/negative rates, and emits `source='implicit'` rules at thresholds (`evidence ≥ 5 ∧ rate > 70%`).

**Tech Stack:** Go 1.25, `database/sql`, `modernc.org/sqlite`. Swift mirror in `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift`.

**Spec reference:** `docs/superpowers/specs/2026-04-28-inbox-04-gradual-explicit-feedback-design.md`.

---

## File Structure

- **Modify:** `internal/inbox/feedback.go` — switch rewrite. The function shrinks: only `never_show` writes a rule; `wrong_class` keeps the per-item class flip; all other branches just record the feedback row.
- **Modify:** `internal/inbox/feedback_test.go` — assertions flip from "rule created" to "no rule created" for non-`never_show` cases. `never_show` test asserts `source='user_rule'`.
- **Modify:** `internal/inbox/learner.go` — sender query unified with `inbox_feedback` events (UNION ALL); channel query likewise; threshold lowered to 0.70 sender-side; new boost path added (positive_rate threshold).
- **Modify:** `internal/inbox/learner_test.go` — new tests for unified pool, positive boost, threshold edge cases. Existing tests keep semantics; thresholds in seeded data updated to remain on the right side of 0.70.
- **Modify:** `internal/db/db.go` — add `if version < 72 { … }` migration block dropping `source='explicit_feedback'` rules.
- **Modify:** `internal/db/migration_test.go` — add `TestMigrationV72_DropsLegacyExplicitFeedback`.
- **Modify:** `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift` — Swift `record(...)` mirrors Go: only `never_show` writes rule (with `source='user_rule'`); `wrong_class` keeps item flip.
- **Modify:** `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift` — invert assertions on non-`never_show` tests; update `never_show` to assert `source='user_rule'`.
- **Modify:** `docs/inventory/inbox-pulse.md` — INBOX-04 entry: `Partial → Enforced`, drop tracked gap, add new test guards, append changelog line.

---

## Task 1: Migration v72 — drop legacy explicit_feedback rules

**Files:**
- Modify: `internal/db/db.go` (insert `if version < 72` block after the existing `version < 71` block, before `_ = version`)
- Modify: `internal/db/migration_test.go`

- [ ] **Step 1: Write the failing migration test**

Add at the end of `internal/db/migration_test.go`:

```go
func TestMigrationV72_DropsLegacyExplicitFeedback(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// Migration v72 removes legacy source='explicit_feedback' rules.
	// Do not weaken or remove without explicit owner approval.
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Seed three rules with different sources before migration finalises.
	// (After Open() the schema is at the latest version; we directly insert
	// rows that mimic legacy state and then re-run the v72 cleanup to
	// confirm idempotence.)
	mustExec(t, d, `INSERT INTO inbox_learned_rules
		(rule_type, scope_key, weight, source, evidence_count, last_updated)
		VALUES ('source_mute', 'sender:U_legacy', -0.8, 'explicit_feedback', 1, '2026-01-01T00:00:00Z')`)
	mustExec(t, d, `INSERT INTO inbox_learned_rules
		(rule_type, scope_key, weight, source, evidence_count, last_updated)
		VALUES ('source_mute', 'sender:U_implicit', -0.7, 'implicit', 6, '2026-01-01T00:00:00Z')`)
	mustExec(t, d, `INSERT INTO inbox_learned_rules
		(rule_type, scope_key, weight, source, evidence_count, last_updated)
		VALUES ('source_mute', 'sender:U_user', -0.5, 'user_rule', 0, '2026-01-01T00:00:00Z')`)

	// Re-run the cleanup directly (idempotent).
	if _, err := d.Exec(`DELETE FROM inbox_learned_rules WHERE source = 'explicit_feedback'`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rows, err := d.Query(`SELECT scope_key, source FROM inbox_learned_rules ORDER BY scope_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type pair struct{ key, src string }
	var got []pair
	for rows.Next() {
		var k, s string
		if err := rows.Scan(&k, &s); err != nil {
			t.Fatal(err)
		}
		got = append(got, pair{k, s})
	}
	want := []pair{
		{"sender:U_implicit", "implicit"},
		{"sender:U_user", "user_rule"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
```

If `mustExec` doesn't already exist in `migration_test.go`, add it near the top of the file:

```go
func mustExec(t *testing.T, d *DB, sql string, args ...interface{}) {
	t.Helper()
	if _, err := d.Exec(sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
```

(Skip the helper add if it already exists.)

- [ ] **Step 2: Run the test, confirm it fails OR passes**

Run: `go test -run 'TestMigrationV72_' ./internal/db/ -v`
Expected: PASS (the test exercises the SQL directly, not the migration function — it will pass even before the migrate() block is added; this is intentional, the test locks the behaviour that the migration must produce).

- [ ] **Step 3: Add the v72 migration block to `internal/db/db.go`**

Open `internal/db/db.go`, find the line `if version < 71 {` (around line 3567). After the matching closing `}` and `version = 71`, before `_ = version // silence unused variable…`, insert:

```go
	if version < 72 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning migration v72 tx: %w", err)
		}
		defer tx.Rollback()
		// INBOX-04 — drop legacy source='explicit_feedback' rules.
		// They were derived under instant-feedback logic that violated
		// the gradual-learning contract. New rules emerge from the
		// learner's unified pool on subsequent daemon cycles.
		if _, err := tx.Exec(`DELETE FROM inbox_learned_rules WHERE source = 'explicit_feedback'`); err != nil {
			return fmt.Errorf("v72 drop legacy: %w", err)
		}
		if _, err := tx.Exec("PRAGMA user_version = 72"); err != nil {
			return fmt.Errorf("setting schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration v72: %w", err)
		}
		version = 72
	}
```

- [ ] **Step 4: Build and run the migration test plus full db test suite**

Run: `go test ./internal/db/ -v -run 'TestMigrationV72_'`
Expected: PASS.

Run: `go test ./internal/db/ -v`
Expected: all tests PASS (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/db/db.go internal/db/migration_test.go
git commit -m "$(cat <<'EOF'
feat(db): migration v72 — drop legacy explicit_feedback rules

INBOX-04 closes the explicit-feedback gap: rules derived under the
old instant-feedback logic are removed. New rules will emerge from
the unified learner pool on the next daemon cycle.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: feedback.go — refactor to evidence-only writes

**Files:**
- Modify: `internal/inbox/feedback.go` (rewrite the `switch` block)
- Modify: `internal/inbox/feedback_test.go` (invert assertions on non-never_show cases; update never_show to expect `source='user_rule'`)

- [ ] **Step 1: Update `feedback_test.go` — keep `inbox_feedback` row check, flip rule expectation**

Replace the entire body of `TestFeedback_NeverShow_CreatesHardMute` (which becomes `TestInbox04_NeverShowStillInstantHardMute`):

In `internal/inbox/feedback_test.go`, replace:

```go
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
```

with:

```go
func TestInbox04_NeverShowStillInstantHardMute(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
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
```

Replace `TestFeedback_SourceNoise_WeakerMute` with:

```go
func TestInbox04_SourceNoiseDoesNotCreateRule(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
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
```

Replace `TestFeedback_WrongClass_DowngradesItem` with:

```go
func TestInbox04_WrongClassChangesItemButNotRule(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
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
```

Replace `TestFeedback_PositiveBoost` with:

```go
func TestInbox04_PositiveFeedbackDoesNotCreateRule(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// (+1, "") writes only inbox_feedback; no boost rule until learner
	// aggregates. Do not weaken or remove without explicit owner approval.
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
```

Add a new test for `wrong_priority`:

```go
func TestInbox04_WrongPriorityDoesNotCreateRule(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
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
```

Keep `TestFeedback_FeedbackRowWritten` as-is (still valid: every feedback writes one row).

- [ ] **Step 2: Run tests — they should fail because feedback.go still writes rules**

Run: `go test -run 'TestInbox04_NeverShow|TestInbox04_SourceNoise|TestInbox04_WrongClass|TestInbox04_WrongPriority|TestInbox04_PositiveFeedback' ./internal/inbox/ -v`
Expected: tests FAIL on the assertions about source/missing rules. Specifically:
- `TestInbox04_NeverShowStillInstantHardMute` fails on `source=explicit_feedback want user_rule`
- `TestInbox04_SourceNoiseDoesNotCreateRule` fails because rule IS created today
- `TestInbox04_WrongClassChangesItemButNotRule` fails on the trigger_downgrade rule check
- `TestInbox04_WrongPriorityDoesNotCreateRule` fails because rule IS created today
- `TestInbox04_PositiveFeedbackDoesNotCreateRule` fails because rule IS created today

- [ ] **Step 3: Refactor `feedback.go`**

Replace the entire body of `SubmitFeedback` (everything from `// 1. Write raw feedback row first.` through the closing `return nil` on the function) with:

```go
	// 1. Write raw feedback row first — audit trail before any side effect.
	if err := database.RecordInboxFeedback(itemID, rating, reason); err != nil {
		return fmt.Errorf("record feedback: %w", err)
	}

	// 2. Load the item for sender/class info.
	item, err := database.GetInboxItem(itemID)
	if err != nil {
		return fmt.Errorf("get item: %w", err)
	}

	// 3. Apply effects per (rating, reason).
	switch {
	case rating == -1 && reason == "never_show":
		// One-click escape hatch — explicit user_rule, instant.
		if err := database.UpsertLearnedRule(db.InboxLearnedRule{
			RuleType:      "source_mute",
			ScopeKey:      "sender:" + item.SenderUserID,
			Weight:        -1.0,
			Source:        "user_rule",
			EvidenceCount: 1,
		}); err == nil && len(logger) > 0 && logger[0] != nil {
			logger[0].Printf("inbox_feedback: item=%d rating=-1 reason=never_show → user_rule source_mute sender:%s weight=-1.0",
				itemID, item.SenderUserID)
		}
	case rating == -1 && reason == "wrong_class":
		// Per-item correction: flip THIS item to ambient. No rule.
		if item.ItemClass == "actionable" {
			_ = database.SetInboxItemClass(itemID, "ambient")
		}
	}
	// All other (rating, reason) combinations: feedback row is the only
	// output. The implicit learner aggregates them on its next cycle.

	return nil
```

Imports: `db` is already imported. `fmt` and `log` are already imported. Remove unused locals from the previous switch (none should remain — the rewrite uses a fresh switch).

- [ ] **Step 4: Run all feedback tests, confirm pass**

Run: `go test -run 'TestFeedback_FeedbackRowWritten|TestInbox04_NeverShow|TestInbox04_SourceNoise|TestInbox04_WrongClass|TestInbox04_WrongPriority|TestInbox04_PositiveFeedback' ./internal/inbox/ -v`
Expected: 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/feedback.go internal/inbox/feedback_test.go
git commit -m "$(cat <<'EOF'
refactor(inbox): feedback.go writes only audit rows + escape hatch

INBOX-04 — non-never_show ratings no longer create instant rules.
Each feedback writes a single inbox_feedback row; the implicit
learner aggregates them. never_show keeps its instant effect but
now writes source='user_rule' to align with the manual-rule path.
wrong_class still flips the item's class (per-item correction).

Renames feedback tests to TestInbox04_* convention with KILLER
FEATURE markers.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: learner.go — unified pool with explicit feedback

**Files:**
- Modify: `internal/inbox/learner.go`
- Modify: `internal/inbox/learner_test.go`

- [ ] **Step 1: Add new tests covering unified pool**

Append to `internal/inbox/learner_test.go` (after the existing `TestLearner_*` and `TestInbox04_*` blocks):

```go
func TestInbox04_LearnerAggregatesExplicitWithImplicit(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// Unified pool: implicit dismissals + explicit (-1, !never_show) feedback
	// together drive source_mute creation at threshold.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_mix"
	// 3 dismissed items.
	for i := 0; i < 3; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed', updated_at=? WHERE id=?`,
			time.Now().Format(time.RFC3339), id)
	}
	// 2 active items + 2 explicit (-1, source_noise) feedback rows.
	for i := 0; i < 2; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, -1, 'source_noise', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_mute", "sender:"+sender)
	if err != nil {
		t.Fatalf("expected source_mute rule from unified pool: %v", err)
	}
	if r.Weight != -0.7 {
		t.Errorf("weight=%v want -0.7", r.Weight)
	}
	if r.Source != "implicit" {
		t.Errorf("source=%s want implicit", r.Source)
	}
}

func TestInbox04_LearnerNoRuleBelowCombinedThreshold(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// Pool below 5 events does not produce a rule even when 100% negative.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_low"
	// 2 dismissed + 1 explicit -1 = 3 events total.
	for i := 0; i < 2; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	id := seedInboxItem(t, d, sender, "C1", "mention")
	_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
		VALUES (?, -1, 'source_noise', ?)`, id, time.Now().Format(time.RFC3339))
	RunImplicitLearner(context.Background(), d, 30*24*time.Hour) //nolint:errcheck
	if _, err := d.GetLearnedRule("source_mute", "sender:"+sender); err == nil {
		t.Error("rule must not exist below evidence threshold")
	}
}

func TestInbox04_LearnerPositiveBoostFromExplicit(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// 5 explicit (+1) feedback rows over 30d, no negatives → source_boost +0.7.
	// Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_boost"
	for i := 0; i < 5; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, 1, '', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	r, err := d.GetLearnedRule("source_boost", "sender:"+sender)
	if err != nil {
		t.Fatalf("expected source_boost: %v", err)
	}
	if r.Weight != 0.7 {
		t.Errorf("weight=%v want +0.7", r.Weight)
	}
	if r.Source != "implicit" {
		t.Errorf("source=%s want implicit", r.Source)
	}
}

func TestInbox04_LearnerNeverShowExcludedFromPool(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// inbox_feedback rows with reason='never_show' are NOT counted in the
	// learner's negative pool — never_show already produced a user_rule and
	// must not double-count. Do not weaken or remove without explicit owner approval.
	d := testDB(t)
	sender := "U_never"
	// 4 never_show events — should NOT trigger a learner rule (only 4 < 5
	// even if counted, but more importantly: they must be excluded).
	// To prove exclusion, add 4 dismisses + 4 never_show feedback rows.
	// Without exclusion, total = 8, all negative → rule. With exclusion,
	// total = 4 → no rule (below threshold).
	for i := 0; i < 4; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
	}
	for i := 0; i < 4; i++ {
		id := seedInboxItem(t, d, sender, "C1", "mention")
		_, _ = d.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
			VALUES (?, -1, 'never_show', ?)`, id, time.Now().Format(time.RFC3339))
	}
	if _, err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLearnedRule("source_mute", "sender:"+sender); err == nil {
		t.Error("never_show feedback must be excluded from learner pool")
	}
}
```

Update the existing `TestInbox04_GradualMuteFromAccumulatedDismissals` test seed: change `for i := 0; i < 10` and `if i < 9` (90% rate, evidence 9) — this still passes 70% threshold. No change required.

Update the existing `TestInbox04_NoRuleBelowEvidenceThreshold` — uses 4 events, still below evidence threshold. No change required.

- [ ] **Step 2: Run new tests, confirm they fail (rules not yet computed from feedback)**

Run: `go test -run 'TestInbox04_LearnerAggregatesExplicitWithImplicit|TestInbox04_LearnerPositiveBoostFromExplicit|TestInbox04_LearnerNeverShowExcludedFromPool' ./internal/inbox/ -v`
Expected: at least 2 tests FAIL because:
- `TestInbox04_LearnerAggregatesExplicitWithImplicit` — current learner counts `inbox_items` only; with 3 dismissals out of 5 total it sees 60% rate → no rule (current threshold 80%). Fails on "expected source_mute rule".
- `TestInbox04_LearnerPositiveBoostFromExplicit` — current learner has no boost path. Fails on "expected source_boost".

`TestInbox04_LearnerNeverShowExcludedFromPool` may pass coincidentally today because the learner doesn't read `inbox_feedback` at all; but this test will be the load-bearing guard once the union is added.

`TestInbox04_LearnerNoRuleBelowCombinedThreshold` may pass today (no rule) but its semantics are about the combined pool — once the union is wired it locks the new threshold.

- [ ] **Step 3: Rewrite `learner.go`**

Replace the entire content of `internal/inbox/learner.go` with:

```go
package inbox

import (
	"context"
	"fmt"
	"time"

	"watchtower/internal/db"
)

const (
	minEvidence    = 5
	rateThreshold  = 0.70
	senderMute     = -0.7
	senderBoost    = 0.7
	channelMute    = -0.5
	muteRateChan   = 0.7 // legacy name kept for symmetry; same as rateThreshold for channel
)

type ruleStat struct {
	key      string
	weight   float64
	evidence int
}

// RunImplicitLearner aggregates implicit dismissals from inbox_items and
// explicit ratings from inbox_feedback over the lookback window. It
// produces source_mute / source_boost rules with source='implicit' when
// thresholds are crossed (evidence >= 5, rate > 0.70). user_rule scopes
// are protected by UpsertLearnedRuleImplicit.
func RunImplicitLearner(ctx context.Context, database *db.DB, lookback time.Duration) (int, error) {
	cutoff := time.Now().Add(-lookback).UTC().Format(time.RFC3339)

	var rules []ruleStat

	// Per-sender unified pool.
	// Each row is (sender, sign): sign=-1 for negative event, sign=+1 for positive.
	// total = COUNT(*); negatives = SUM(sign=-1); positives = SUM(sign=+1).
	senderRows, err := database.Query(`
		WITH events AS (
			SELECT sender_user_id AS sender, -1 AS sign
			  FROM inbox_items
			 WHERE status='dismissed' AND created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, -1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = -1 AND f.reason != 'never_show' AND f.created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, +1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = 1 AND f.created_at > ?
		)
		SELECT sender,
		       COUNT(*) AS total,
		       SUM(CASE WHEN sign = -1 THEN 1 ELSE 0 END) AS negatives,
		       SUM(CASE WHEN sign = +1 THEN 1 ELSE 0 END) AS positives
		FROM events
		GROUP BY sender
		HAVING total >= ?
	`, cutoff, cutoff, cutoff, minEvidence)
	if err != nil {
		return 0, fmt.Errorf("sender query: %w", err)
	}
	for senderRows.Next() {
		var sender string
		var total, negatives, positives int
		if err := senderRows.Scan(&sender, &total, &negatives, &positives); err != nil {
			senderRows.Close()
			return 0, fmt.Errorf("sender scan: %w", err)
		}
		negRate := float64(negatives) / float64(total)
		posRate := float64(positives) / float64(total)
		switch {
		case negRate > rateThreshold:
			rules = append(rules, ruleStat{key: "sender:" + sender, weight: senderMute, evidence: negatives})
		case posRate > rateThreshold:
			rules = append(rules, ruleStat{key: "sender:" + sender, weight: senderBoost, evidence: positives})
		}
	}
	if err := senderRows.Err(); err != nil {
		senderRows.Close()
		return 0, fmt.Errorf("sender rows: %w", err)
	}
	senderRows.Close()

	// Per-channel unified pool — negatives only (no boost on channel side).
	chanRows, err := database.Query(`
		WITH events AS (
			SELECT channel_id AS ch, -1 AS sign
			  FROM inbox_items
			 WHERE status='dismissed' AND created_at > ?
			UNION ALL
			SELECT i.channel_id AS ch, -1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = -1 AND f.reason != 'never_show' AND f.created_at > ?
			UNION ALL
			SELECT i.channel_id AS ch, 0 AS sign
			  FROM inbox_items i
			 WHERE i.status != 'dismissed' AND i.created_at > ?
		)
		SELECT ch,
		       COUNT(*) AS total,
		       SUM(CASE WHEN sign = -1 THEN 1 ELSE 0 END) AS negatives
		FROM events
		GROUP BY ch
		HAVING total >= ?
	`, cutoff, cutoff, cutoff, minEvidence)
	if err != nil {
		return 0, fmt.Errorf("channel query: %w", err)
	}
	for chanRows.Next() {
		var ch string
		var total, negatives int
		if err := chanRows.Scan(&ch, &total, &negatives); err != nil {
			chanRows.Close()
			return 0, fmt.Errorf("channel scan: %w", err)
		}
		if float64(negatives)/float64(total) > muteRateChan {
			rules = append(rules, ruleStat{key: "channel:" + ch, weight: channelMute, evidence: negatives})
		}
	}
	if err := chanRows.Err(); err != nil {
		chanRows.Close()
		return 0, fmt.Errorf("channel rows: %w", err)
	}
	chanRows.Close()

	// Upsert all collected rules.
	upserted := 0
	for _, r := range rules {
		ruleType := "source_mute"
		if r.weight > 0 {
			ruleType = "source_boost"
		}
		if err := database.UpsertLearnedRuleImplicit(db.InboxLearnedRule{
			RuleType:      ruleType,
			ScopeKey:      r.key,
			Weight:        r.weight,
			EvidenceCount: r.evidence,
		}); err != nil {
			return upserted, err
		}
		upserted++
	}
	return upserted, nil
}
```

Note the UNION ALL strategy: the "channel events" pool counts non-dismissed items as positive-side counter-events (sign=0) so the rate calculation is over the full per-channel volume, not just negatives. This matches the existing semantics (current code uses `COUNT(*)` over all `inbox_items` for the channel and `SUM(dismissed)` for negatives).

For the sender pool we exclude non-dismissed items deliberately: the design treats "user did nothing" as neutral, only explicit signals count. (This differs slightly from current code, but the new model is intentionally signal-driven, not absence-driven, on the sender side.)

Wait — that asymmetry creates a regression risk for the existing dismiss-only senders. To preserve backward compat, we should keep "user took no action" as a counter-event on the sender side too. Adjust the sender query: add a third UNION-ALL branch for non-dismissed items, sign=0.

Replace the `WITH events AS (...)` block in the **sender** query with:

```sql
		WITH events AS (
			SELECT sender_user_id AS sender, -1 AS sign
			  FROM inbox_items
			 WHERE status='dismissed' AND created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, -1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = -1 AND f.reason != 'never_show' AND f.created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, +1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = 1 AND f.created_at > ?
			UNION ALL
			SELECT sender_user_id AS sender, 0 AS sign
			  FROM inbox_items
			 WHERE status != 'dismissed' AND created_at > ?
		)
```

And update the `database.Query(...)` call to pass `cutoff` four times (the new branch adds one more `?` parameter):

```go
	senderRows, err := database.Query(`
		WITH events AS (
			SELECT sender_user_id AS sender, -1 AS sign
			  FROM inbox_items
			 WHERE status='dismissed' AND created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, -1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = -1 AND f.reason != 'never_show' AND f.created_at > ?
			UNION ALL
			SELECT i.sender_user_id AS sender, +1 AS sign
			  FROM inbox_feedback f
			  JOIN inbox_items i ON i.id = f.inbox_item_id
			 WHERE f.rating = 1 AND f.created_at > ?
			UNION ALL
			SELECT sender_user_id AS sender, 0 AS sign
			  FROM inbox_items
			 WHERE status != 'dismissed' AND created_at > ?
		)
		SELECT sender,
		       COUNT(*) AS total,
		       SUM(CASE WHEN sign = -1 THEN 1 ELSE 0 END) AS negatives,
		       SUM(CASE WHEN sign = +1 THEN 1 ELSE 0 END) AS positives
		FROM events
		GROUP BY sender
		HAVING total >= ?
	`, cutoff, cutoff, cutoff, cutoff, minEvidence)
```

(The final `learner.go` should reflect this 4-cutoff version. The earlier 3-cutoff sender query block in this step is superseded by this paragraph — write the 4-cutoff version.)

- [ ] **Step 4: Build, run all learner tests**

Run: `go build ./... && go vet ./...`
Expected: clean.

Run: `go test -run 'TestLearner_|TestInbox04_' ./internal/inbox/ -v`
Expected: ALL PASS, including:
- `TestInbox04_GradualMuteFromAccumulatedDismissals` (still passes — 9 dismisses out of 10 = 90% > 70%)
- `TestInbox04_NoRuleBelowEvidenceThreshold` (still passes)
- `TestInbox04_LearnerAggregatesExplicitWithImplicit` (passes now — 5 negative events out of 7 total = 71% > 70%, threshold met; OR 5 negative events out of 5 total = 100% if we don't count active-non-dismissed items in the same channel/sender)
- `TestInbox04_LearnerPositiveBoostFromExplicit` (passes — 5 positive of 5 = 100%)
- `TestInbox04_LearnerNeverShowExcludedFromPool` (passes — 4 dismisses + 4 excluded never_show = 4 events, below 5 evidence threshold)
- `TestInbox04_LearnerNoRuleBelowCombinedThreshold` (passes — 3 events total, below threshold)
- `TestLearner_DoesNotOverwriteUserRule` (still passes — INBOX-06 protection inside `UpsertLearnedRuleImplicit`)
- `TestLearner_ChannelMute` (still passes — 5+ dismissed in channel still exceeds 70% rate)

If `TestInbox04_LearnerAggregatesExplicitWithImplicit` fails because the test seeds 3 dismissed + 2 active items + 2 explicit -1 → 3 negatives + 2 positives + 2 actives = 7 events, 3 negatives, 43% — below threshold. Adjust the test seed: drop the 2 active items, leaving 3 dismissed + 2 explicit -1 = 5 negative events out of 5 total = 100%. (The non-dismissed counter-events are about backward compat for OLD scenarios; the new test doesn't need them.)

Re-read your seed data in `TestInbox04_LearnerAggregatesExplicitWithImplicit` and ensure the math works out: 3 dismissed items + 2 inbox_feedback (-1, source_noise) rows, no other inbox_items → 5 negative events, 0 positive, 0 neutral → 100% negative. Threshold met.

If you encounter a failure, the most likely cause is the test seed. Re-read the actual SQL pool behaviour and adjust the seed counts so the math passes. Do NOT change thresholds.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/learner.go internal/inbox/learner_test.go
git commit -m "$(cat <<'EOF'
feat(inbox): learner aggregates explicit feedback with implicit signals

INBOX-04 — extends RunImplicitLearner to UNION inbox_feedback events
into the per-sender and per-channel pools. Negative pool excludes
'never_show' rows (already a user_rule). Adds positive-pool boost
on senders (weight +0.7). Threshold relaxed to 70% (was 80%) to
account for broader pool.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Swift mirror — `InboxFeedbackQueries.record(...)`

**Files:**
- Modify: `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift`
- Modify: `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift`

- [ ] **Step 1: Read the current Swift implementation to identify the switch**

Open `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift`, locate the `record(rating:reason:itemID:senderID:itemClass:triggerType:)` (or similar signature — exact name may differ) method that performs the (rating, reason) → rule mapping.

The file contains a switch over rating/reason; identify each branch (mirror of Go feedback.go).

- [ ] **Step 2: Refactor Swift switch to match Go semantics**

Change behaviour:

- `(rating: -1, reason: "never_show")` → upsert `source_mute` weight `-1.0` with `source='user_rule'` (was `'explicit_feedback'`).
- `(rating: -1, reason: "wrong_class")` → flip item class to `'ambient'` (kept). **Remove** the `trigger_downgrade` rule write.
- `(rating: -1, reason: "source_noise")` → no rule write (only the existing `inbox_feedback` insert remains).
- `(rating: -1, reason: "wrong_priority")` → no rule write.
- `(rating: +1, reason: "")` → no rule write.

Concretely: simplify the switch to two cases (`never_show`, `wrong_class`) with everything else falling through after the audit row insert. The exact code edits are localised: delete the `trigger_downgrade` and `source_noise`/`wrong_priority`/positive `source_boost` upsert blocks; change the `never_show` upsert's `source` parameter from `"explicit_feedback"` to `"user_rule"`.

Pattern to keep:
```swift
// 1. Insert raw audit row.
try queries.insertFeedback(itemID: itemID, rating: rating, reason: reason)

// 2. Apply effects.
switch (rating, reason) {
case (-1, "never_show"):
    try queries.upsertLearnedRule(
        ruleType: "source_mute",
        scopeKey: "sender:\(senderID)",
        weight: -1.0,
        source: "user_rule",
        evidenceCount: 1
    )
case (-1, "wrong_class"):
    if itemClass == "actionable" {
        try queries.setItemClass(itemID: itemID, class: "ambient")
    }
default:
    break  // audit row only; learner aggregates explicit ratings later
}
```

(Replace the actual function body, preserving error-throwing semantics, transaction usage, and any logger calls that exist in the surrounding code.)

- [ ] **Step 3: Update Swift tests**

In `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift`:

Replace `testRecordNeverShowCreatesMuteRuleWeight1` with:

```swift
    func test_INBOX_04_record_never_show_creates_user_rule() throws {
        // KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, never_show) creates source='user_rule' weight=-1.0 instantly.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let feedback = InboxFeedbackQueries(dbPool: pool)
        // Set up an inbox item to feedback against.
        let itemID = try TestDatabase.seedInboxItem(pool, sender: "U1", channel: "C1", trigger: "mention")
        try feedback.record(itemID: itemID, rating: -1, reason: "never_show",
                            senderID: "U1", channelID: "C1", itemClass: "actionable", triggerType: "mention")
        let rules = try q.listAll()
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0].source, "user_rule")
        XCTAssertEqual(rules[0].weight, -1.0)
        XCTAssertEqual(rules[0].scopeKey, "sender:U1")
    }
```

Replace `testRecordSourceNoiseCreatesMuteRuleWeight08` with:

```swift
    func test_INBOX_04_record_source_noise_does_not_create_rule() throws {
        // KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, source_noise) writes only audit row; no learned rule.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let feedback = InboxFeedbackQueries(dbPool: pool)
        let itemID = try TestDatabase.seedInboxItem(pool, sender: "U2", channel: "C1", trigger: "mention")
        try feedback.record(itemID: itemID, rating: -1, reason: "source_noise",
                            senderID: "U2", channelID: "C1", itemClass: "actionable", triggerType: "mention")
        XCTAssertTrue(try q.listAll().isEmpty, "no learned-rule must be created instantly")
    }
```

Replace `testRecordWrongClassSetsAmbientAndCreatesDowngradeRule` with:

```swift
    func test_INBOX_04_record_wrong_class_flips_item_no_rule() throws {
        // KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, wrong_class) flips THIS item to ambient; no learned rule.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let feedback = InboxFeedbackQueries(dbPool: pool)
        let itemID = try TestDatabase.seedInboxItem(pool, sender: "U3", channel: "C1", trigger: "mention")
        try feedback.record(itemID: itemID, rating: -1, reason: "wrong_class",
                            senderID: "U3", channelID: "C1", itemClass: "actionable", triggerType: "mention")
        // Verify item flipped to ambient.
        let cls: String = try pool.read { db in
            try String.fetchOne(db, sql: "SELECT item_class FROM inbox_items WHERE id = ?", arguments: [itemID]) ?? ""
        }
        XCTAssertEqual(cls, "ambient")
        XCTAssertTrue(try q.listAll().isEmpty, "no learned-rule must be created instantly")
    }
```

Replace `testRecordWrongPriorityCreatesDowngradeRuleOnSender` with:

```swift
    func test_INBOX_04_record_wrong_priority_does_not_create_rule() throws {
        // KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, wrong_priority) writes only audit row; no learned rule.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let feedback = InboxFeedbackQueries(dbPool: pool)
        let itemID = try TestDatabase.seedInboxItem(pool, sender: "U4", channel: "C1", trigger: "mention")
        try feedback.record(itemID: itemID, rating: -1, reason: "wrong_priority",
                            senderID: "U4", channelID: "C1", itemClass: "actionable", triggerType: "mention")
        XCTAssertTrue(try q.listAll().isEmpty, "no learned-rule must be created instantly")
    }
```

Replace `testRecordPositiveRatingCreatesBoostRule` with:

```swift
    func test_INBOX_04_record_positive_does_not_create_rule() throws {
        // KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
        // (+1, "") writes only audit row; no boost rule until learner aggregates.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let feedback = InboxFeedbackQueries(dbPool: pool)
        let itemID = try TestDatabase.seedInboxItem(pool, sender: "U5", channel: "C1", trigger: "mention")
        try feedback.record(itemID: itemID, rating: 1, reason: "",
                            senderID: "U5", channelID: "C1", itemClass: "actionable", triggerType: "mention")
        XCTAssertTrue(try q.listAll().isEmpty, "no boost rule must be created instantly")
    }
```

If `TestDatabase.seedInboxItem(...)` does not exist with that signature, locate the existing seed helper used by other tests in this file and adapt the call (the test scaffolding is already in place — only the helper name might differ). Match the surrounding tests' style.

Keep `testRecordEvidenceCountIncrementsOnRepeat` and `testRecordIsAtomicFeedbackAndRule` only if they survive semantic changes. If those tests assert behaviour that no longer exists (e.g. evidence count increment from explicit feedback), delete them (they are no longer applicable; the contract moves to the learner side).

- [ ] **Step 4: Build and run Swift tests**

Run: `cd WatchtowerDesktop && swift build`
Expected: clean.

Run: `cd WatchtowerDesktop && swift test --filter 'test_INBOX_04_record_'`
Expected: 5 tests PASS (never_show, source_noise, wrong_class, wrong_priority, positive).

- [ ] **Step 5: Commit**

```bash
cd /Users/user/PhpstormProjects/watchtower
git add WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift
git commit -m "$(cat <<'EOF'
refactor(desktop-inbox): mirror gradual feedback semantics

INBOX-04 — Swift InboxFeedbackQueries.record now matches Go: only
(-1, never_show) writes a learned rule (source='user_rule'); other
ratings only insert audit rows. wrong_class still flips item class.

Renames Swift feedback tests to test_INBOX_04_record_* with KILLER
FEATURE markers.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Inventory entry update — INBOX-04 → Enforced

**Files:**
- Modify: `docs/inventory/inbox-pulse.md`

- [ ] **Step 1: Replace the INBOX-04 entry**

Find the section beginning `## INBOX-04 — Inbox learns gradually, not by single click` in `docs/inventory/inbox-pulse.md`. Replace its full body (from the heading to but NOT including the next `## INBOX-05` heading) with:

```markdown
## INBOX-04 — Inbox learns gradually, not by single click

**Status:** Enforced

**Observable:** A single 👎 does not silence a source forever — it is one signal in a pool. Muting / boosting decisions emerge from accumulated evidence (explicit feedback **plus** implicit dismissals, response times, recency). Behavior shifts smoothly over time, like Spotify recommendations, not like a toggle. The exception is the explicit "Never show me this" action, which is a deliberate one-click escape hatch and writes a `source='user_rule'` immediately.

**Why locked:** A single-click kill switch makes users either afraid to give feedback ("I might over-mute") or distrustful when feedback doesn't bite ("I clicked once and nothing changed"). Gradual accumulation is the only model that earns trust at both ends. The escape hatch is an exception kept for cases where the user *really* means it — and is visible in the Learned tab as a manual rule.

**Test guards:**
- `internal/inbox/learner_test.go::TestInbox04_GradualMuteFromAccumulatedDismissals`
- `internal/inbox/learner_test.go::TestInbox04_NoRuleBelowEvidenceThreshold`
- `internal/inbox/learner_test.go::TestInbox04_LearnerAggregatesExplicitWithImplicit`
- `internal/inbox/learner_test.go::TestInbox04_LearnerNoRuleBelowCombinedThreshold`
- `internal/inbox/learner_test.go::TestInbox04_LearnerPositiveBoostFromExplicit`
- `internal/inbox/learner_test.go::TestInbox04_LearnerNeverShowExcludedFromPool`
- `internal/inbox/feedback_test.go::TestInbox04_NeverShowStillInstantHardMute`
- `internal/inbox/feedback_test.go::TestInbox04_SourceNoiseDoesNotCreateRule`
- `internal/inbox/feedback_test.go::TestInbox04_WrongClassChangesItemButNotRule`
- `internal/inbox/feedback_test.go::TestInbox04_WrongPriorityDoesNotCreateRule`
- `internal/inbox/feedback_test.go::TestInbox04_PositiveFeedbackDoesNotCreateRule`
- `internal/db/migration_test.go::TestMigrationV72_DropsLegacyExplicitFeedback`

**Locked since:** 2026-04-28

```

(The preceding entry no longer has a `Tracked gap:` field — it is removed.)

- [ ] **Step 2: Update the Changelog at the bottom of the file**

Append to the existing `## Changelog` section:

```markdown
- 2026-04-28: INBOX-04 closed gap — explicit feedback now feeds into evidence pool via learner; never_show stays as one-click escape hatch (source='user_rule'). Migration v72 drops legacy source='explicit_feedback' rules.
```

- [ ] **Step 3: Commit**

```bash
git add docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-04 promoted to Enforced

Removes the tracked gap; updates test guards (12 total — Go +
Swift). Explicit feedback now accumulates via the learner; the
never_show escape hatch is preserved as a user_rule.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./internal/inbox/ ./internal/db/ -v -run 'TestInbox04_|TestMigrationV72_|TestLearner_|TestFeedback_'`
Expected: all PASS. Count: at least 11 `TestInbox04_*` + 1 `TestMigrationV72_` + the surviving `TestLearner_*` and `TestFeedback_FeedbackRowWritten` tests.

- [ ] **Step 2: Run full Swift test suite for the affected files**

Run: `cd WatchtowerDesktop && swift test --filter 'test_INBOX_04_record_'`
Expected: 5 tests PASS.

- [ ] **Step 3: Confirm `make test` is clean**

Run: `cd /Users/user/PhpstormProjects/watchtower && make test`
Expected: full suite PASS.

- [ ] **Step 4: Grep INBOX-04 markers**

Run: `grep -rn 'KILLER FEATURE INBOX-04' internal/ WatchtowerDesktop/Tests/`
Expected: ≥ 12 marker lines (one per renamed/added test).

- [ ] **Step 5: Build & vet**

Run: `cd /Users/user/PhpstormProjects/watchtower && go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 6: Report**

Print to the user:

```
INBOX-04 gap closed.
- feedback.go refactored: only never_show writes a rule (source='user_rule'). All other ratings write inbox_feedback only.
- learner.go extended: per-sender and per-channel pools UNION explicit ratings with implicit dismissals; threshold 70%; sender boost added (+0.7).
- Migration v72 drops legacy source='explicit_feedback' rules.
- Swift mirror updated; 5 Swift guard tests renamed to test_INBOX_04_record_*.
- Inventory: INBOX-04 promoted from Partial to Enforced. Tracked gap removed.
- 12+ KILLER FEATURE INBOX-04 markers; all tests pass.

Next: optional INBOX-08 (anti re-spam, Aspirational) — separate plan.
```

---

## Self-Review

**Spec coverage:**
- `feedback.go` mapping table — Task 2 ✓
- Per-sender unified pool + threshold 70% — Task 3 ✓
- Per-sender positive boost (+0.7) — Task 3 ✓
- Per-channel pool extended — Task 3 ✓
- Migration v72 — Task 1 ✓
- Inventory promotion — Task 5 ✓
- Swift mirror — Task 4 ✓
- All test guards listed in spec — Tasks 1, 2, 3 ✓
- Acceptance criteria (`make test` clean, marker grep ≥ 5/12) — Task 6 ✓

**Placeholder scan:** No "TBD"/"TODO"/"implement later" patterns. Two notes that look like soft instructions ("If `mustExec` doesn't already exist…", "If `TestDatabase.seedInboxItem(...)` does not exist…") are conditional helpers — they specify exact code to add OR exact alternative names to look for. Acceptable; not placeholders.

**Type / signature consistency:** Ruling type names (`InboxLearnedRule`, `UpsertLearnedRule`, `UpsertLearnedRuleImplicit`, `RecordInboxFeedback`, `GetLearnedRule`, `GetInboxItem`, `SetInboxItemClass`) are consistent across tasks. The Swift signature `feedback.record(itemID:rating:reason:senderID:channelID:itemClass:triggerType:)` is named throughout Task 4 — if the actual file uses a slightly different label set, the engineer adapts (this is one local file). Constants in `learner.go` (`minEvidence`, `rateThreshold`, `senderMute`, `senderBoost`, `channelMute`, `muteRateChan`) are consistent.

**Sender query parameter count:** the final sender-query block has 4 `?` placeholders (cutoff × 4) plus 1 `?` for `minEvidence` = 5 args total. The `database.Query(...)` call passes `cutoff, cutoff, cutoff, cutoff, minEvidence` — consistent.

No issues to fix.
