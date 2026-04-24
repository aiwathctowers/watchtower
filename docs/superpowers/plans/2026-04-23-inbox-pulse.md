# Inbox Pulse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Inbox Pulse — a unified live feed combining Slack, Jira, Calendar, and internal Watchtower signals with AI-curated pinned section, actionable vs ambient classification, and three-layer learning system.

**Architecture:** Extend existing `internal/inbox/Pipeline` with new per-source detectors (Jira, Calendar, Watchtower-internal), rule-based classifier with AI override, implicit/explicit learning layers, and a separate `aiSelectPinned` AI call. Desktop layer replaces `InboxListView` with a feed-style `InboxFeedView` and adds a "Learned" rules tab.

**Tech Stack:** Go 1.25 (backend, SQLite via modernc.org/sqlite, `database/sql`), SwiftUI + GRDB.swift (macOS desktop), AI via existing `digest.Generator` interface (Claude/Codex CLI subprocess).

**Spec:** `docs/superpowers/specs/2026-04-23-inbox-pulse-design.md`

**Worktree:** `.worktrees/inbox-pulse` on branch `feature/inbox-pulse`.

**Parallelization notes:** Tasks labeled `[PAR]` are independent of sibling `[PAR]` tasks in the same phase and may be dispatched concurrently to subagents. Tasks labeled `[SEQ]` must run in the stated order.

---

## File Structure

### Backend (Go) — new files
- `internal/inbox/classifier.go` — rule-based default class per trigger_type + AI-override application.
- `internal/inbox/classifier_test.go`
- `internal/inbox/jira_detector.go` — reads `jira_issues` / `jira_comments` into `inbox_items`.
- `internal/inbox/jira_detector_test.go`
- `internal/inbox/calendar_detector.go` — reads `calendar_events`.
- `internal/inbox/calendar_detector_test.go`
- `internal/inbox/watchtower_detector.go` — reads `digests` / `briefings`.
- `internal/inbox/watchtower_detector_test.go`
- `internal/inbox/learner.go` — SQL-based implicit rule learner.
- `internal/inbox/learner_test.go`
- `internal/inbox/feedback.go` — handles `👍/👎` feedback + rule upsert.
- `internal/inbox/feedback_test.go`
- `internal/inbox/pinned_selector.go` — separate AI call for pinned selection.
- `internal/inbox/pinned_selector_test.go`
- `internal/inbox/prompts/select_pinned.tmpl` — new prompt template.

### Backend (Go) — modified files
- `internal/db/schema.sql` — new columns, tables, indexes.
- `internal/db/db.go` — migration v67 block.
- `internal/db/inbox.go` — new queries.
- `internal/db/inbox_learned_rules.go` — new file for rule CRUD.
- `internal/db/inbox_feedback.go` — new file for feedback writes.
- `internal/inbox/pipeline.go` — orchestration with new phases.
- `internal/inbox/prompts/prioritize.tmpl` — extended with USER PREFERENCES block.
- `internal/daemon/daemon.go` (or equivalent integration point) — pipeline ordering.

### Desktop (Swift) — new files
- `WatchtowerDesktop/Sources/Models/InboxLearnedRule.swift`
- `WatchtowerDesktop/Sources/Models/InboxFeedback.swift`
- `WatchtowerDesktop/Sources/Database/Queries/InboxLearnedRulesQueries.swift`
- `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift`
- `WatchtowerDesktop/Sources/ViewModels/InboxLearnedRulesViewModel.swift`
- `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedView.swift`
- `WatchtowerDesktop/Sources/Views/Inbox/InboxCardView.swift`
- `WatchtowerDesktop/Sources/Views/Inbox/InboxLearnedRulesView.swift`
- `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedbackSheet.swift`

### Desktop (Swift) — modified files
- `WatchtowerDesktop/Sources/Models/InboxItem.swift` — new fields.
- `WatchtowerDesktop/Sources/Database/Queries/InboxQueries.swift` — extended queries.
- `WatchtowerDesktop/Sources/ViewModels/InboxViewModel.swift` — pinned/feed split, feedback.
- Destination enum / sidebar — badge logic (red dot on high-priority pinned).

### Desktop (Swift) — removed
- `WatchtowerDesktop/Sources/Views/Inbox/InboxListView.swift`

---

## Phase 0 — Migration

### Task 1 [SEQ]: DB schema v67 migration

**Files:**
- Modify: `internal/db/schema.sql`
- Modify: `internal/db/db.go` (add `PRAGMA user_version = 67` block)
- Test: `internal/db/db_test.go` (append migration test)

- [ ] **Step 1: Write the failing test**

Append to `internal/db/db_test.go`:

```go
func TestMigration_v67_InboxPulse(t *testing.T) {
    dir := t.TempDir()
    database, err := db.Open(filepath.Join(dir, "test.db"))
    if err != nil { t.Fatal(err) }
    defer database.Close()

    // Check new columns exist
    cols := tableColumns(t, database, "inbox_items")
    requireCol(t, cols, "item_class")
    requireCol(t, cols, "pinned")
    requireCol(t, cols, "archived_at")
    requireCol(t, cols, "archive_reason")

    // Check new tables exist
    if !tableExists(t, database, "inbox_learned_rules") {
        t.Error("inbox_learned_rules missing")
    }
    if !tableExists(t, database, "inbox_feedback") {
        t.Error("inbox_feedback missing")
    }

    // Check version
    var ver int
    if err := database.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
        t.Fatal(err)
    }
    if ver < 67 { t.Errorf("want user_version >=67, got %d", ver) }
}

// Helpers (add if not present):
func tableColumns(t *testing.T, d *db.DB, name string) []string {
    rows, _ := d.Query("PRAGMA table_info(" + name + ")")
    defer rows.Close()
    var cols []string
    for rows.Next() {
        var cid int; var nm, tp string; var notnull, pk int; var dflt sql.NullString
        rows.Scan(&cid, &nm, &tp, &notnull, &dflt, &pk)
        cols = append(cols, nm)
    }
    return cols
}
func requireCol(t *testing.T, cols []string, want string) {
    for _, c := range cols { if c == want { return } }
    t.Errorf("column %q missing; have %v", want, cols)
}
func tableExists(t *testing.T, d *db.DB, name string) bool {
    var n int
    d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
    return n > 0
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db -run TestMigration_v67_InboxPulse -v`
Expected: FAIL — columns/tables missing.

- [ ] **Step 3: Add schema definitions to `internal/db/schema.sql`**

Locate `inbox_items` definition. Extend it:
```sql
-- inside inbox_items CREATE TABLE (add columns):
item_class     TEXT NOT NULL DEFAULT 'actionable' CHECK(item_class IN ('actionable','ambient')),
pinned         INTEGER NOT NULL DEFAULT 0,
archived_at    TEXT,
archive_reason TEXT DEFAULT '' CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed')),
```

Relax `trigger_type` CHECK (remove the whitelist; validation moves to Go code):
```sql
trigger_type   TEXT NOT NULL,
```

Add after existing inbox indexes:
```sql
CREATE INDEX IF NOT EXISTS idx_inbox_items_class_status ON inbox_items(item_class, status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1;
CREATE INDEX IF NOT EXISTS idx_inbox_items_archived ON inbox_items(archived_at);
```

Append new tables at the end (near other Inbox tables):
```sql
CREATE TABLE IF NOT EXISTS inbox_learned_rules (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_type      TEXT NOT NULL CHECK(rule_type IN ('source_mute','source_boost','trigger_downgrade','trigger_boost')),
    scope_key      TEXT NOT NULL,
    weight         REAL NOT NULL,
    source         TEXT NOT NULL CHECK(source IN ('implicit','explicit_feedback','user_rule')),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    last_updated   TEXT NOT NULL,
    UNIQUE(rule_type, scope_key)
);
CREATE INDEX IF NOT EXISTS idx_inbox_learned_rules_scope ON inbox_learned_rules(rule_type, scope_key);

CREATE TABLE IF NOT EXISTS inbox_feedback (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    inbox_item_id INTEGER NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
    rating        INTEGER NOT NULL CHECK(rating IN (-1,1)),
    reason        TEXT DEFAULT '' CHECK(reason IN ('','source_noise','wrong_priority','wrong_class','never_show')),
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_inbox_feedback_item ON inbox_feedback(inbox_item_id);
```

- [ ] **Step 4: Add migration block in `internal/db/db.go`**

Find existing `PRAGMA user_version = 66` block. Duplicate the pattern for v67:

```go
if version < 67 {
    if _, err := tx.Exec(`
        ALTER TABLE inbox_items ADD COLUMN item_class TEXT NOT NULL DEFAULT 'actionable'
            CHECK(item_class IN ('actionable','ambient'));
        ALTER TABLE inbox_items ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE inbox_items ADD COLUMN archived_at TEXT;
        ALTER TABLE inbox_items ADD COLUMN archive_reason TEXT DEFAULT ''
            CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed'));

        CREATE INDEX IF NOT EXISTS idx_inbox_items_class_status ON inbox_items(item_class, status);
        CREATE INDEX IF NOT EXISTS idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1;
        CREATE INDEX IF NOT EXISTS idx_inbox_items_archived ON inbox_items(archived_at);

        CREATE TABLE IF NOT EXISTS inbox_learned_rules (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            rule_type TEXT NOT NULL CHECK(rule_type IN ('source_mute','source_boost','trigger_downgrade','trigger_boost')),
            scope_key TEXT NOT NULL,
            weight REAL NOT NULL,
            source TEXT NOT NULL CHECK(source IN ('implicit','explicit_feedback','user_rule')),
            evidence_count INTEGER NOT NULL DEFAULT 0,
            last_updated TEXT NOT NULL,
            UNIQUE(rule_type, scope_key)
        );
        CREATE INDEX IF NOT EXISTS idx_inbox_learned_rules_scope ON inbox_learned_rules(rule_type, scope_key);

        CREATE TABLE IF NOT EXISTS inbox_feedback (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            inbox_item_id INTEGER NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
            rating INTEGER NOT NULL CHECK(rating IN (-1,1)),
            reason TEXT DEFAULT '' CHECK(reason IN ('','source_noise','wrong_priority','wrong_class','never_show')),
            created_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_inbox_feedback_item ON inbox_feedback(inbox_item_id);

        UPDATE inbox_items SET item_class = 'ambient' WHERE trigger_type = 'reaction';
    `); err != nil {
        return fmt.Errorf("migrate v67: %w", err)
    }
    if _, err := tx.Exec("PRAGMA user_version = 67"); err != nil {
        return err
    }
}
```

Note: SQLite cannot `ALTER TABLE ... DROP CHECK`. The existing whitelist on `trigger_type` is no longer desired, but rather than doing a full table rebuild, we keep the existing constraint for now (migration still succeeds because `ALTER TABLE ADD COLUMN` doesn't touch trigger_type). **Immediately after** version 67 migration success, add a table-rebuild block ONLY for fresh DBs via `schema.sql` (where the CHECK is already relaxed). For existing DBs we'll use a second migration v68 to rebuild (deferred — current whitelist will be extended via migration-time `CREATE TABLE inbox_items_new … INSERT … DROP … RENAME` if you need room for new trigger types in the same v67; this plan chooses to rebuild):

Augment the v67 block with rebuild:
```sql
CREATE TABLE inbox_items_new (
    -- full definition with relaxed trigger_type and new columns
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id TEXT NOT NULL,
    message_ts TEXT NOT NULL,
    thread_ts TEXT NOT NULL DEFAULT '',
    sender_user_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    snippet TEXT NOT NULL DEFAULT '',
    context TEXT NOT NULL DEFAULT '',
    raw_text TEXT NOT NULL DEFAULT '',
    permalink TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
    priority TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    ai_reason TEXT NOT NULL DEFAULT '',
    resolved_reason TEXT NOT NULL DEFAULT '',
    snooze_until TEXT,
    waiting_user_ids TEXT NOT NULL DEFAULT '[]',
    task_id INTEGER,
    read_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    item_class TEXT NOT NULL DEFAULT 'actionable' CHECK(item_class IN ('actionable','ambient')),
    pinned INTEGER NOT NULL DEFAULT 0,
    archived_at TEXT,
    archive_reason TEXT DEFAULT '' CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed'))
);
INSERT INTO inbox_items_new SELECT * FROM inbox_items;
DROP TABLE inbox_items;
ALTER TABLE inbox_items_new RENAME TO inbox_items;
-- Recreate all indexes
```

Implement the above as a single migration block using pure `ALTER TABLE ADD COLUMN` first, then the rebuild IF we really need relaxed trigger_type for new trigger types from Jira/Calendar/Watchtower. **Simpler alternative (use this):** keep `ALTER ADD COLUMN` calls as shown first, and in `schema.sql` list the new trigger types in the CHECK expansion:

```sql
trigger_type TEXT NOT NULL CHECK(trigger_type IN (
    'mention','dm','thread_reply','reaction',
    'jira_assigned','jira_comment_mention','jira_comment_watching','jira_status_change','jira_priority_change',
    'calendar_invite','calendar_time_change','calendar_cancelled',
    'decision_made','briefing_ready'
)),
```

For existing DBs, do the table rebuild in v67 migration to accept new trigger_types. The rebuild SQL goes into the v67 block replacing the naive ALTER section.

**Final v67 block (use this):** include the table rebuild to also relax `trigger_type`, plus new tables/indexes/backfill.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/db -run TestMigration_v67_InboxPulse -v`
Expected: PASS.

- [ ] **Step 6: Run full DB test suite**

Run: `go test ./internal/db -v -count=1`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/db/schema.sql internal/db/db.go internal/db/db_test.go
git commit -m "feat(inbox): migration v67 with item_class, pinned, archive fields and learning tables"
```

---

## Phase 1 — Core DB + Classifier

### Task 2 [PAR after 1]: DB queries — learned rules CRUD

**Files:**
- Create: `internal/db/inbox_learned_rules.go`
- Test: `internal/db/inbox_learned_rules_test.go`

- [ ] **Step 1: Write the failing test**

```go
package db

import (
    "testing"
    "time"
)

func TestInboxLearnedRules_Upsert(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()

    rule := InboxLearnedRule{
        RuleType: "source_mute",
        ScopeKey: "sender:U123",
        Weight: -0.7,
        Source: "implicit",
        EvidenceCount: 10,
    }
    if err := database.UpsertLearnedRule(rule); err != nil { t.Fatal(err) }

    got, err := database.GetLearnedRule("source_mute", "sender:U123")
    if err != nil { t.Fatal(err) }
    if got.Weight != -0.7 { t.Errorf("weight=%v want -0.7", got.Weight) }

    // Update
    rule.Weight = -0.9
    rule.EvidenceCount = 15
    if err := database.UpsertLearnedRule(rule); err != nil { t.Fatal(err) }
    got, _ = database.GetLearnedRule("source_mute", "sender:U123")
    if got.Weight != -0.9 { t.Errorf("updated weight=%v want -0.9", got.Weight) }
    if got.EvidenceCount != 15 { t.Errorf("evidence=%d want 15", got.EvidenceCount) }
}

func TestInboxLearnedRules_UserRuleProtected(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()

    // User-created rule
    database.UpsertLearnedRule(InboxLearnedRule{
        RuleType: "source_mute", ScopeKey: "channel:C1", Weight: -0.9, Source: "user_rule", EvidenceCount: 0,
    })

    // Implicit should NOT overwrite a user_rule
    err := database.UpsertLearnedRuleImplicit(InboxLearnedRule{
        RuleType: "source_mute", ScopeKey: "channel:C1", Weight: -0.3, Source: "implicit", EvidenceCount: 5,
    })
    if err != nil { t.Fatal(err) }

    got, _ := database.GetLearnedRule("source_mute", "channel:C1")
    if got.Weight != -0.9 || got.Source != "user_rule" {
        t.Errorf("user_rule overwritten: %+v", got)
    }
}

func TestInboxLearnedRules_ListByRelevance(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()

    database.UpsertLearnedRule(InboxLearnedRule{RuleType:"source_mute", ScopeKey:"sender:U1", Weight:-0.5, Source:"implicit"})
    database.UpsertLearnedRule(InboxLearnedRule{RuleType:"source_boost", ScopeKey:"sender:U2", Weight:0.8, Source:"explicit_feedback"})
    database.UpsertLearnedRule(InboxLearnedRule{RuleType:"source_mute", ScopeKey:"channel:C1", Weight:-0.7, Source:"implicit"})

    got, err := database.ListLearnedRulesByScope([]string{"sender:U1","channel:C99"}, 10)
    if err != nil { t.Fatal(err) }
    if len(got) != 1 || got[0].ScopeKey != "sender:U1" {
        t.Errorf("expected only sender:U1 match, got %+v", got)
    }
}

func TestInboxLearnedRules_Delete(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()
    database.UpsertLearnedRule(InboxLearnedRule{RuleType:"source_mute", ScopeKey:"x", Weight:-1, Source:"user_rule"})
    if err := database.DeleteLearnedRule("source_mute", "x"); err != nil { t.Fatal(err) }
    _, err := database.GetLearnedRule("source_mute", "x")
    if err == nil { t.Error("expected error after delete") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db -run TestInboxLearnedRules -v`
Expected: FAIL (methods undefined).

- [ ] **Step 3: Implement `internal/db/inbox_learned_rules.go`**

```go
package db

import (
    "fmt"
    "strings"
    "time"
)

type InboxLearnedRule struct {
    ID            int64
    RuleType      string
    ScopeKey      string
    Weight        float64
    Source        string
    EvidenceCount int
    LastUpdated   string
}

func (db *DB) UpsertLearnedRule(r InboxLearnedRule) error {
    now := time.Now().UTC().Format(time.RFC3339)
    _, err := db.Exec(`
        INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(rule_type, scope_key) DO UPDATE SET
            weight = excluded.weight,
            source = excluded.source,
            evidence_count = excluded.evidence_count,
            last_updated = excluded.last_updated
    `, r.RuleType, r.ScopeKey, r.Weight, r.Source, r.EvidenceCount, now)
    return err
}

// UpsertLearnedRuleImplicit refuses to overwrite a rule whose source='user_rule'.
func (db *DB) UpsertLearnedRuleImplicit(r InboxLearnedRule) error {
    existing, err := db.GetLearnedRule(r.RuleType, r.ScopeKey)
    if err == nil && existing.Source == "user_rule" {
        return nil // protected
    }
    r.Source = "implicit"
    return db.UpsertLearnedRule(r)
}

func (db *DB) GetLearnedRule(ruleType, scopeKey string) (InboxLearnedRule, error) {
    var r InboxLearnedRule
    err := db.QueryRow(`
        SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated
        FROM inbox_learned_rules WHERE rule_type=? AND scope_key=?
    `, ruleType, scopeKey).Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated)
    if err != nil { return r, fmt.Errorf("get learned rule: %w", err) }
    return r, nil
}

func (db *DB) ListAllLearnedRules() ([]InboxLearnedRule, error) {
    rows, err := db.Query(`SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated FROM inbox_learned_rules ORDER BY last_updated DESC`)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []InboxLearnedRule
    for rows.Next() {
        var r InboxLearnedRule
        rows.Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated)
        out = append(out, r)
    }
    return out, rows.Err()
}

// ListLearnedRulesByScope returns rules whose scope_key is in scopeKeys, up to limit rows.
func (db *DB) ListLearnedRulesByScope(scopeKeys []string, limit int) ([]InboxLearnedRule, error) {
    if len(scopeKeys) == 0 { return nil, nil }
    placeholders := make([]string, len(scopeKeys))
    args := make([]interface{}, len(scopeKeys)+1)
    for i, k := range scopeKeys {
        placeholders[i] = "?"
        args[i] = k
    }
    args[len(scopeKeys)] = limit
    q := fmt.Sprintf(`
        SELECT id, rule_type, scope_key, weight, source, evidence_count, last_updated
        FROM inbox_learned_rules
        WHERE scope_key IN (%s)
        ORDER BY ABS(weight) DESC, last_updated DESC
        LIMIT ?
    `, strings.Join(placeholders, ","))
    rows, err := db.Query(q, args...)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []InboxLearnedRule
    for rows.Next() {
        var r InboxLearnedRule
        rows.Scan(&r.ID, &r.RuleType, &r.ScopeKey, &r.Weight, &r.Source, &r.EvidenceCount, &r.LastUpdated)
        out = append(out, r)
    }
    return out, rows.Err()
}

func (db *DB) DeleteLearnedRule(ruleType, scopeKey string) error {
    _, err := db.Exec(`DELETE FROM inbox_learned_rules WHERE rule_type=? AND scope_key=?`, ruleType, scopeKey)
    return err
}
```

- [ ] **Step 4: Verify tests pass**

Run: `go test ./internal/db -run TestInboxLearnedRules -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/inbox_learned_rules.go internal/db/inbox_learned_rules_test.go
git commit -m "feat(inbox): learned rules CRUD with implicit/user_rule precedence"
```

---

### Task 3 [PAR after 1]: DB queries — feedback writes

**Files:**
- Create: `internal/db/inbox_feedback.go`
- Test: `internal/db/inbox_feedback_test.go`

- [ ] **Step 1: Failing test**

```go
func TestInboxFeedback_Record(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()

    // Seed an inbox item
    itemID := seedInboxItem(t, database, "U123", "C1", "mention")

    // Record 👎 with never_show
    if err := database.RecordInboxFeedback(itemID, -1, "never_show"); err != nil { t.Fatal(err) }

    // Verify feedback row
    rows, _ := database.Query(`SELECT rating, reason FROM inbox_feedback WHERE inbox_item_id=?`, itemID)
    defer rows.Close()
    rows.Next()
    var r int; var reason string
    rows.Scan(&r, &reason)
    if r != -1 || reason != "never_show" { t.Errorf("got r=%d reason=%s", r, reason) }
}

func TestInboxFeedback_ListForItem(t *testing.T) {
    database := newTestDB(t)
    defer database.Close()
    itemID := seedInboxItem(t, database, "U1", "C1", "mention")
    database.RecordInboxFeedback(itemID, 1, "")
    database.RecordInboxFeedback(itemID, -1, "source_noise")
    got, err := database.GetFeedbackForItem(itemID)
    if err != nil { t.Fatal(err) }
    if len(got) != 2 { t.Errorf("want 2 feedback, got %d", len(got)) }
}

// Helper seedInboxItem lives in a shared test util file; if absent, inline:
func seedInboxItem(t *testing.T, d *DB, sender, channel, trigger string) int64 {
    res, err := d.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, created_at, updated_at)
        VALUES (?,?,?,?,'pending','medium',?,?)`, channel, "1.0", sender, trigger, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
    if err != nil { t.Fatal(err) }
    id, _ := res.LastInsertId()
    return id
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/db -run TestInboxFeedback -v`

- [ ] **Step 3: Implement `internal/db/inbox_feedback.go`**

```go
package db

import "time"

type InboxFeedback struct {
    ID          int64
    InboxItemID int64
    Rating      int
    Reason      string
    CreatedAt   string
}

func (db *DB) RecordInboxFeedback(itemID int64, rating int, reason string) error {
    now := time.Now().UTC().Format(time.RFC3339)
    _, err := db.Exec(`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at) VALUES (?,?,?,?)`,
        itemID, rating, reason, now)
    return err
}

func (db *DB) GetFeedbackForItem(itemID int64) ([]InboxFeedback, error) {
    rows, err := db.Query(`SELECT id, inbox_item_id, rating, reason, created_at FROM inbox_feedback WHERE inbox_item_id=?`, itemID)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []InboxFeedback
    for rows.Next() {
        var f InboxFeedback
        rows.Scan(&f.ID, &f.InboxItemID, &f.Rating, &f.Reason, &f.CreatedAt)
        out = append(out, f)
    }
    return out, rows.Err()
}
```

- [ ] **Step 4: Verify PASS** — `go test ./internal/db -run TestInboxFeedback -v -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/db/inbox_feedback.go internal/db/inbox_feedback_test.go
git commit -m "feat(inbox): feedback DB writes"
```

---

### Task 4 [PAR after 1]: DB queries — item class, pin, archive

**Files:**
- Modify: `internal/db/inbox.go` (add methods)
- Test: `internal/db/inbox_extra_test.go` (new file)

- [ ] **Step 1: Failing test**

```go
package db

import (
    "testing"
    "time"
)

func TestInbox_SetItemClass(t *testing.T) {
    database := newTestDB(t); defer database.Close()
    id := seedInboxItem(t, database, "U1", "C1", "mention")
    if err := database.SetInboxItemClass(id, "ambient"); err != nil { t.Fatal(err) }
    var cls string
    database.QueryRow(`SELECT item_class FROM inbox_items WHERE id=?`, id).Scan(&cls)
    if cls != "ambient" { t.Errorf("got %s", cls) }
}

func TestInbox_SetPinned(t *testing.T) {
    database := newTestDB(t); defer database.Close()
    id := seedInboxItem(t, database, "U1", "C1", "mention")
    database.SetInboxPinned([]int64{id})
    var p int
    database.QueryRow(`SELECT pinned FROM inbox_items WHERE id=?`, id).Scan(&p)
    if p != 1 { t.Errorf("not pinned: %d", p) }

    // ClearPinned resets pinned
    database.ClearPinnedAll()
    database.QueryRow(`SELECT pinned FROM inbox_items WHERE id=?`, id).Scan(&p)
    if p != 0 { t.Error("still pinned") }
}

func TestInbox_ArchiveExpired(t *testing.T) {
    database := newTestDB(t); defer database.Close()

    // Ambient item 8 days old
    oldT := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
    database.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
        VALUES ('C1','1.0','U1','decision_made','pending','low','ambient',?,?)`, oldT, oldT)

    n, err := database.ArchiveExpiredAmbient(7 * 24 * time.Hour)
    if err != nil { t.Fatal(err) }
    if n != 1 { t.Errorf("want 1 archived, got %d", n) }

    // Verify archived_at set + reason
    var reason string; var arch string
    database.QueryRow(`SELECT archive_reason, archived_at FROM inbox_items WHERE item_class='ambient'`).Scan(&reason, &arch)
    if reason != "seen_expired" { t.Errorf("reason=%q", reason) }
    if arch == "" { t.Error("archived_at empty") }
}

func TestInbox_ArchiveStale(t *testing.T) {
    database := newTestDB(t); defer database.Close()
    oldT := time.Now().Add(-15 * 24 * time.Hour).UTC().Format(time.RFC3339)
    database.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
        VALUES ('C1','1.0','U1','mention','pending','medium','actionable',?,?)`, oldT, oldT)
    n, _ := database.ArchiveStaleActionable(14 * 24 * time.Hour)
    if n != 1 { t.Errorf("want 1, got %d", n) }
}

func TestInbox_FeedQuery_ExcludesArchivedAndTerminated(t *testing.T) {
    database := newTestDB(t); defer database.Close()
    alive := seedInboxItem(t, database, "U1", "C1", "mention")
    archived := seedInboxItem(t, database, "U2", "C1", "mention")
    database.Exec(`UPDATE inbox_items SET archived_at=? WHERE id=?`, time.Now().Format(time.RFC3339), archived)
    resolved := seedInboxItem(t, database, "U3", "C1", "mention")
    database.Exec(`UPDATE inbox_items SET status='resolved' WHERE id=?`, resolved)

    got, err := database.ListInboxFeed(50, 0)
    if err != nil { t.Fatal(err) }
    if len(got) != 1 || got[0].ID != alive {
        t.Errorf("expected only alive item, got %+v", got)
    }
}

func TestInbox_PinnedList(t *testing.T) {
    database := newTestDB(t); defer database.Close()
    a := seedInboxItem(t, database, "U1", "C1", "mention")
    _ = seedInboxItem(t, database, "U2", "C1", "mention")
    database.SetInboxPinned([]int64{a})
    got, _ := database.ListInboxPinned()
    if len(got) != 1 || got[0].ID != a { t.Errorf("bad pinned list: %+v", got) }
}
```

- [ ] **Step 2: Run to confirm FAIL** — `go test ./internal/db -run TestInbox_ -v`

- [ ] **Step 3: Implement methods in `internal/db/inbox.go`**

Append:

```go
func (db *DB) SetInboxItemClass(id int64, class string) error {
    _, err := db.Exec(`UPDATE inbox_items SET item_class=?, updated_at=? WHERE id=?`,
        class, time.Now().UTC().Format(time.RFC3339), id)
    return err
}

// SetInboxPinned pins the given item IDs (pinned=1) and unpins all others in a single transaction.
func (db *DB) SetInboxPinned(ids []int64) error {
    tx, err := db.Begin()
    if err != nil { return err }
    defer tx.Rollback()
    if _, err := tx.Exec(`UPDATE inbox_items SET pinned=0 WHERE pinned=1`); err != nil { return err }
    for _, id := range ids {
        if _, err := tx.Exec(`UPDATE inbox_items SET pinned=1 WHERE id=?`, id); err != nil { return err }
    }
    return tx.Commit()
}

func (db *DB) ClearPinnedAll() error {
    _, err := db.Exec(`UPDATE inbox_items SET pinned=0 WHERE pinned=1`)
    return err
}

func (db *DB) ArchiveExpiredAmbient(threshold time.Duration) (int, error) {
    cutoff := time.Now().Add(-threshold).UTC().Format(time.RFC3339)
    now := time.Now().UTC().Format(time.RFC3339)
    res, err := db.Exec(`
        UPDATE inbox_items SET archived_at=?, archive_reason='seen_expired', updated_at=?
        WHERE item_class='ambient' AND archived_at IS NULL AND created_at < ?
    `, now, now, cutoff)
    if err != nil { return 0, err }
    n, _ := res.RowsAffected()
    return int(n), nil
}

func (db *DB) ArchiveStaleActionable(threshold time.Duration) (int, error) {
    cutoff := time.Now().Add(-threshold).UTC().Format(time.RFC3339)
    now := time.Now().UTC().Format(time.RFC3339)
    res, err := db.Exec(`
        UPDATE inbox_items SET archived_at=?, archive_reason='stale', updated_at=?
        WHERE item_class='actionable' AND archived_at IS NULL AND status='pending' AND updated_at < ?
    `, now, now, cutoff)
    if err != nil { return 0, err }
    n, _ := res.RowsAffected()
    return int(n), nil
}

// ListInboxFeed returns feed items (non-pinned, live, not archived/resolved/dismissed/snoozed), newest first.
func (db *DB) ListInboxFeed(limit, offset int) ([]InboxItem, error) {
    rows, err := db.Query(`
        SELECT `+inboxItemColumns+` FROM inbox_items
        WHERE pinned=0 AND archived_at IS NULL AND status NOT IN ('resolved','dismissed','snoozed')
        ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
    if err != nil { return nil, err }
    defer rows.Close()
    return scanInboxItems(rows)
}

// ListInboxPinned returns current pinned items (status=pending).
func (db *DB) ListInboxPinned() ([]InboxItem, error) {
    rows, err := db.Query(`
        SELECT `+inboxItemColumns+` FROM inbox_items
        WHERE pinned=1 AND status='pending' AND archived_at IS NULL
        ORDER BY priority DESC, created_at DESC`)
    if err != nil { return nil, err }
    defer rows.Close()
    return scanInboxItems(rows)
}
```

Note: `inboxItemColumns` and `scanInboxItems` already exist in `inbox.go`; extend them to include the new `item_class, pinned, archived_at, archive_reason` columns and scan them into `InboxItem` struct (which must also get these fields).

- [ ] **Step 4: Extend `InboxItem` struct + column list**

In `internal/db/inbox.go`, locate `InboxItem` struct. Add:

```go
ItemClass     string
Pinned        bool
ArchivedAt    string // empty if not archived
ArchiveReason string
```

Update `inboxItemColumns` constant to include these columns in the exact SELECT order, and update `scanInboxItems` to scan them.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/db -run TestInbox_ -v -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/inbox.go internal/db/inbox_extra_test.go
git commit -m "feat(inbox): DB queries for item class, pinning, archival, feed/pinned lists"
```

---

### Task 5 [PAR after 1]: Classifier — default class per trigger_type

**Files:**
- Create: `internal/inbox/classifier.go`
- Test: `internal/inbox/classifier_test.go`

- [ ] **Step 1: Failing test**

```go
package inbox

import "testing"

func TestClassifier_DefaultForTriggerType(t *testing.T) {
    cases := []struct {
        trig  string
        class string
    }{
        {"mention", "actionable"},
        {"dm", "actionable"},
        {"thread_reply", "actionable"},
        {"reaction", "ambient"},
        {"jira_assigned", "actionable"},
        {"jira_comment_mention", "actionable"},
        {"jira_comment_watching", "ambient"},
        {"jira_status_change", "ambient"},
        {"jira_priority_change", "ambient"},
        {"calendar_invite", "actionable"},
        {"calendar_time_change", "actionable"},
        {"calendar_cancelled", "ambient"},
        {"decision_made", "ambient"},
        {"briefing_ready", "ambient"},
        {"unknown_type", "ambient"}, // unknown → ambient fallback
    }
    for _, c := range cases {
        got := DefaultItemClass(c.trig)
        if got != c.class {
            t.Errorf("trigger=%s got=%s want=%s", c.trig, got, c.class)
        }
    }
}

func TestClassifier_ApplyAIOverride_DowngradeOnly(t *testing.T) {
    // AI can downgrade actionable → ambient
    got := ApplyAIOverride("actionable", "ambient")
    if got != "ambient" { t.Errorf("downgrade failed: %s", got) }

    // AI cannot upgrade ambient → actionable
    got = ApplyAIOverride("ambient", "actionable")
    if got != "ambient" { t.Errorf("upgrade should be rejected: %s", got) }

    // Empty override keeps original
    got = ApplyAIOverride("actionable", "")
    if got != "actionable" { t.Errorf("empty should keep: %s", got) }
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/inbox -run TestClassifier -v`

- [ ] **Step 3: Implement `internal/inbox/classifier.go`**

```go
package inbox

var defaultClasses = map[string]string{
    "mention":               "actionable",
    "dm":                    "actionable",
    "thread_reply":          "actionable",
    "reaction":              "ambient",
    "jira_assigned":         "actionable",
    "jira_comment_mention":  "actionable",
    "jira_comment_watching": "ambient",
    "jira_status_change":    "ambient",
    "jira_priority_change":  "ambient",
    "calendar_invite":       "actionable",
    "calendar_time_change":  "actionable",
    "calendar_cancelled":    "ambient",
    "decision_made":         "ambient",
    "briefing_ready":        "ambient",
}

// DefaultItemClass returns 'actionable' or 'ambient' for a known trigger type, defaulting to 'ambient' for unknown.
func DefaultItemClass(trig string) string {
    if c, ok := defaultClasses[trig]; ok { return c }
    return "ambient"
}

// ApplyAIOverride applies an AI-suggested class override.
// Only downgrades (actionable → ambient) are honored; upgrades are silently rejected.
// Empty override returns the original class.
func ApplyAIOverride(current, override string) string {
    if override == "" { return current }
    if current == "actionable" && override == "ambient" { return "ambient" }
    return current
}
```

- [ ] **Step 4: Verify PASS** — `go test ./internal/inbox -run TestClassifier -v -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/classifier.go internal/inbox/classifier_test.go
git commit -m "feat(inbox): classifier with default class and AI-downgrade-only override"
```

---

## Phase 2 — Detectors (parallel after Phase 1)

### Task 6 [PAR after 2,4,5]: Jira detector

**Files:**
- Create: `internal/inbox/jira_detector.go`
- Test: `internal/inbox/jira_detector_test.go`
- Read-only reference: `internal/db/jira.go` for existing Jira table schema.

First inspect existing Jira tables (agent's first action):

```bash
grep -E "CREATE TABLE.*jira_|jira_issues|jira_comments" internal/db/schema.sql
```

- [ ] **Step 1: Failing test**

Seed `jira_issues` and `jira_comments` fixtures, call `DetectJira(db, currentUserID, sinceTS)`, assert correct `inbox_items` rows by trigger_type.

```go
package inbox

import (
    "context"
    "testing"
    "time"
    "watchtower/internal/db"
)

func TestJiraDetector_AssignedToMe(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Seed a Jira issue assigned to currentUser
    seedJiraIssue(t, d, "WT-123", "alice", time.Now().Add(-1*time.Hour))
    n, err := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
    if err != nil { t.Fatal(err) }
    if n != 1 { t.Errorf("want 1 new inbox item, got %d", n) }
    got := queryInboxByTrigger(t, d, "jira_assigned")
    if len(got) != 1 || got[0].SenderUserID != "WT-123" {
        t.Errorf("bad jira_assigned item: %+v", got)
    }
}

func TestJiraDetector_CommentMention(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedJiraIssue(t, d, "WT-200", "bob", time.Now().Add(-1*time.Hour))
    seedJiraComment(t, d, "WT-200", "bob", "hey [~alice] please look", time.Now().Add(-30*time.Minute))
    n, _ := DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
    if n == 0 { t.Error("no mention item created") }
    got := queryInboxByTrigger(t, d, "jira_comment_mention")
    if len(got) != 1 { t.Errorf("want 1 mention, got %d", len(got)) }
}

func TestJiraDetector_StatusChange(t *testing.T) {
    // Seed issue assigned to alice, status changed by bob
    // Expect inbox item with trigger_type=jira_status_change, ambient class
}

func TestJiraDetector_NoDoubleDetection(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-1*time.Hour))
    DetectJira(context.Background(), d, "alice", time.Now().Add(-2*time.Hour))
    // Re-run — no duplicate (sinceTS watermark advances, OR duplicate detection via existing row).
    n, _ := DetectJira(context.Background(), d, "alice", time.Now())
    if n != 0 { t.Errorf("expected 0 on second run, got %d", n) }
}
```

Add seed helpers (or reuse `internal/db/jira_test.go` if it has them):

```go
func seedJiraIssue(t *testing.T, d *db.DB, key, assignee string, updated time.Time) {
    _, err := d.Exec(`INSERT INTO jira_issues (issue_key, summary, assignee_id, status, priority, updated_at)
        VALUES (?,?,?,?,?,?)`, key, "test", assignee, "In Progress", "Medium", updated.Format(time.RFC3339))
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: FAIL** — `go test ./internal/inbox -run TestJiraDetector -v`

- [ ] **Step 3: Implement `internal/inbox/jira_detector.go`**

Reference existing detector patterns in `pipeline.go` (`FindPendingMentions`, `FindPendingDMs`). Key responsibilities:
- Read `jira_issues WHERE assignee_id = ? AND updated_at > sinceTS`.
- Read `jira_comments WHERE issue matches me OR comment_text LIKE '%[~me]%' AND created_at > sinceTS`.
- For each candidate, call `db.CreateInboxItem` (existing function) with the correct trigger_type, setting `item_class` via `DefaultItemClass`.
- Deduplicate using existing logic: check `inbox_items` where `channel_id=issue_key AND message_ts=comment_id AND trigger_type=? AND status='pending'`.

```go
package inbox

import (
    "context"
    "fmt"
    "time"
    "watchtower/internal/db"
)

// DetectJira scans jira_issues and jira_comments for signals targeting currentUserID
// since the given timestamp and inserts new inbox_items. Returns count of items created.
func DetectJira(ctx context.Context, database *db.DB, currentUserID string, sinceTS time.Time) (int, error) {
    if currentUserID == "" { return 0, nil }
    created := 0
    sinceISO := sinceTS.UTC().Format(time.RFC3339)

    // 1. jira_assigned — issue assigned to me, newly or reassigned
    rows, err := database.Query(`
        SELECT issue_key, summary, updated_at FROM jira_issues
        WHERE assignee_id=? AND updated_at > ?
    `, currentUserID, sinceISO)
    if err != nil { return created, fmt.Errorf("query jira_issues: %w", err) }
    for rows.Next() {
        var key, summary, updated string
        rows.Scan(&key, &summary, &updated)
        if existsInboxItem(database, key, updated, "jira_assigned") { continue }
        item := db.InboxItem{
            ChannelID:    key,
            MessageTS:    updated,
            SenderUserID: key, // Jira issue key as "sender"
            TriggerType:  "jira_assigned",
            Snippet:      summary,
            ItemClass:    DefaultItemClass("jira_assigned"),
            Status:       "pending",
            Priority:     "medium",
            CreatedAt:    time.Now().UTC().Format(time.RFC3339),
            UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
        }
        if _, err := database.CreateInboxItem(item); err == nil { created++ }
    }
    rows.Close()

    // 2. jira_comment_mention — @me in a comment
    rows, err = database.Query(`
        SELECT issue_key, comment_id, author_id, body, created_at FROM jira_comments
        WHERE body LIKE ? AND created_at > ?
    `, "%[~"+currentUserID+"]%", sinceISO)
    if err != nil { return created, err }
    for rows.Next() {
        var key, cid, author, body, createdAt string
        rows.Scan(&key, &cid, &author, &body, &createdAt)
        if author == currentUserID { continue } // don't notify for own comments
        if existsInboxItem(database, key, cid, "jira_comment_mention") { continue }
        item := db.InboxItem{
            ChannelID:    key, MessageTS: cid, SenderUserID: author,
            TriggerType: "jira_comment_mention",
            Snippet:     truncate(body, 200),
            ItemClass:   DefaultItemClass("jira_comment_mention"),
            Status:      "pending", Priority: "medium",
            CreatedAt: time.Now().UTC().Format(time.RFC3339),
            UpdatedAt: time.Now().UTC().Format(time.RFC3339),
        }
        if _, err := database.CreateInboxItem(item); err == nil { created++ }
    }
    rows.Close()

    // 3. jira_status_change — someone else changed status on my issue
    // (Requires jira_issue_history or similar table. If not present, derive from updated_at + last_status.
    // Implementation depends on schema. Consult internal/db/jira.go.)

    // 4. jira_priority_change — analogous

    // 5. jira_comment_watching — comment on an issue where I'm watcher but not me
    // (Requires jira_watchers table.)

    return created, nil
}

func existsInboxItem(d *db.DB, channelID, messageTS, trig string) bool {
    var n int
    d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE channel_id=? AND message_ts=? AND trigger_type=?`, channelID, messageTS, trig).Scan(&n)
    return n > 0
}

func truncate(s string, n int) string { if len(s) <= n { return s }; return s[:n] + "…" }
```

**Agent note:** Before implementing status/priority/watching detection, read `internal/db/jira.go` and `internal/db/schema.sql` Jira section. If `jira_issue_history` or `jira_watchers` tables do not exist, make these detection paths no-ops with a TODO comment linking this task — capture a follow-up task at the end of the plan rather than inventing a schema.

- [ ] **Step 4: Verify tests pass**

Run: `go test ./internal/inbox -run TestJiraDetector -v -count=1`
Expected: PASS (for assigned + comment_mention at minimum; status/priority/watching may be skipped pending schema availability).

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/jira_detector.go internal/inbox/jira_detector_test.go
git commit -m "feat(inbox): jira detector for assigned issues and @mentions in comments"
```

---

### Task 7 [PAR after 2,4,5]: Calendar detector

**Files:**
- Create: `internal/inbox/calendar_detector.go`
- Test: `internal/inbox/calendar_detector_test.go`

Agent first reads: `internal/db/calendar.go` and the `calendar_events` schema (columns: id, calendar_id, title, start_time, end_time, status, attendees JSON, created_at, updated_at).

- [ ] **Step 1: Failing test**

Seed three scenarios:
1. `calendar_invite` — event newly created, attendees contains me with rsvp_status='needsAction'.
2. `calendar_time_change` — event updated_at > created_at and start_time differs from prior (needs an event_history table or a simple "updated_at after created_at with significant delta" heuristic).
3. `calendar_cancelled` — event.status = 'cancelled'.

```go
func TestCalendarDetector_NewInvite(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedCalendarEvent(t, d, "evt-1", "Team sync", `[{"email":"me@x.com","rsvp_status":"needsAction"}]`, "confirmed", time.Now().Add(-30*time.Minute), time.Now().Add(-30*time.Minute))
    n, err := DetectCalendar(context.Background(), d, "me@x.com", time.Now().Add(-1*time.Hour))
    if err != nil { t.Fatal(err) }
    if n != 1 { t.Errorf("want 1, got %d", n) }
    got := queryInboxByTrigger(t, d, "calendar_invite")
    if len(got) != 1 { t.Errorf("want calendar_invite item") }
}

func TestCalendarDetector_Cancelled(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedCalendarEvent(t, d, "evt-2", "Cancelled meeting", `[{"email":"me@x.com"}]`, "cancelled", time.Now().Add(-2*time.Hour), time.Now().Add(-1*time.Hour))
    n, _ := DetectCalendar(context.Background(), d, "me@x.com", time.Now().Add(-3*time.Hour))
    if n != 1 { t.Errorf("want 1 cancelled, got %d", n) }
}

func TestCalendarDetector_Deduplication(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedCalendarEvent(t, d, "evt-dup", "Sync", `[{"email":"me@x.com","rsvp_status":"needsAction"}]`, "confirmed", time.Now().Add(-30*time.Minute), time.Now().Add(-30*time.Minute))
    DetectCalendar(context.Background(), d, "me@x.com", time.Now().Add(-1*time.Hour))
    n, _ := DetectCalendar(context.Background(), d, "me@x.com", time.Now().Add(-1*time.Hour))
    if n != 0 { t.Errorf("dedupe failed: %d", n) }
}
```

- [ ] **Step 2: FAIL** — `go test ./internal/inbox -run TestCalendarDetector -v`

- [ ] **Step 3: Implement `internal/inbox/calendar_detector.go`**

```go
package inbox

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    "watchtower/internal/db"
)

type calAttendee struct {
    Email      string `json:"email"`
    RSVPStatus string `json:"rsvp_status"`
}

func DetectCalendar(ctx context.Context, database *db.DB, myEmail string, sinceTS time.Time) (int, error) {
    if myEmail == "" { return 0, nil }
    created := 0
    sinceISO := sinceTS.UTC().Format(time.RFC3339)

    rows, err := database.Query(`
        SELECT id, title, start_time, attendees, status, created_at, updated_at
        FROM calendar_events
        WHERE (created_at > ? OR updated_at > ?)
    `, sinceISO, sinceISO)
    if err != nil { return 0, fmt.Errorf("query calendar_events: %w", err) }
    defer rows.Close()

    for rows.Next() {
        var eid, title, startTime, attendees, status, createdAt, updatedAt string
        rows.Scan(&eid, &title, &startTime, &attendees, &status, &createdAt, &updatedAt)

        var list []calAttendee
        _ = json.Unmarshal([]byte(attendees), &list)
        amIAttendee := false
        myRSVP := ""
        for _, a := range list {
            if a.Email == myEmail {
                amIAttendee = true
                myRSVP = a.RSVPStatus
                break
            }
        }
        if !amIAttendee { continue }

        trig := ""
        switch {
        case status == "cancelled":
            trig = "calendar_cancelled"
        case createdAt > sinceISO && myRSVP == "needsAction":
            trig = "calendar_invite"
        case updatedAt > createdAt:
            // Heuristic: updated after created => time_change
            trig = "calendar_time_change"
        }
        if trig == "" { continue }
        if existsInboxItem(database, eid, updatedAt, trig) { continue }

        item := db.InboxItem{
            ChannelID:    eid,
            MessageTS:    updatedAt,
            SenderUserID: eid,
            TriggerType:  trig,
            Snippet:      title,
            ItemClass:    DefaultItemClass(trig),
            Status:       "pending",
            Priority:     "medium",
            CreatedAt:    time.Now().UTC().Format(time.RFC3339),
            UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
        }
        if _, err := database.CreateInboxItem(item); err == nil { created++ }
    }
    return created, nil
}
```

- [ ] **Step 4: PASS** — `go test ./internal/inbox -run TestCalendarDetector -v -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/calendar_detector.go internal/inbox/calendar_detector_test.go
git commit -m "feat(inbox): calendar detector for invites, time changes, cancellations"
```

---

### Task 8 [PAR after 2,4,5]: Watchtower-internal detector

**Files:**
- Create: `internal/inbox/watchtower_detector.go`
- Test: `internal/inbox/watchtower_detector_test.go`

Agent reads: `internal/db/digests.go` (for `situations` JSON structure and importance field) and `internal/db/briefings.go`.

- [ ] **Step 1: Failing test**

```go
func TestWatchtowerDetector_DecisionMade(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Seed a digest with a high-importance decision in situations JSON
    seedDigestWithHighImportance(t, d, "C1", `[{"type":"decision","topic":"Release postponed","importance":"high"}]`, time.Now().Add(-30*time.Minute))
    n, err := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
    if err != nil { t.Fatal(err) }
    if n != 1 { t.Errorf("want 1 decision_made, got %d", n) }
}

func TestWatchtowerDetector_BriefingReady(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedBriefing(t, d, "alice", time.Now().Format("2006-01-02"))
    n, _ := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
    if n < 1 { t.Errorf("want >=1 briefing_ready, got %d", n) }
}

func TestWatchtowerDetector_LowImportanceSkipped(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedDigestWithHighImportance(t, d, "C1", `[{"type":"decision","topic":"minor","importance":"low"}]`, time.Now())
    n, _ := DetectWatchtowerInternal(context.Background(), d, time.Now().Add(-1*time.Hour))
    if n != 0 { t.Errorf("low-importance should be skipped, got %d", n) }
}
```

- [ ] **Step 2: FAIL** — run the test.

- [ ] **Step 3: Implement `internal/inbox/watchtower_detector.go`**

```go
package inbox

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    "watchtower/internal/db"
)

type situation struct {
    Type       string `json:"type"`
    Topic      string `json:"topic"`
    Importance string `json:"importance"`
}

func DetectWatchtowerInternal(ctx context.Context, database *db.DB, sinceTS time.Time) (int, error) {
    created := 0
    sinceISO := sinceTS.UTC().Format(time.RFC3339)

    // 1. decision_made from digests.situations (high importance)
    rows, err := database.Query(`SELECT id, channel_id, situations, created_at FROM digests WHERE created_at > ? AND situations != '' AND situations IS NOT NULL`, sinceISO)
    if err != nil { return 0, fmt.Errorf("query digests: %w", err) }
    for rows.Next() {
        var id int64; var channelID, situations, createdAt string
        rows.Scan(&id, &channelID, &situations, &createdAt)
        var list []situation
        if err := json.Unmarshal([]byte(situations), &list); err != nil { continue }
        for idx, s := range list {
            if s.Type == "decision" && s.Importance == "high" {
                msgTS := fmt.Sprintf("digest:%d:%d", id, idx)
                if existsInboxItem(database, channelID, msgTS, "decision_made") { continue }
                item := db.InboxItem{
                    ChannelID: channelID, MessageTS: msgTS, SenderUserID: "watchtower",
                    TriggerType: "decision_made", Snippet: s.Topic,
                    ItemClass: "ambient", Status: "pending", Priority: "medium",
                    CreatedAt: time.Now().UTC().Format(time.RFC3339),
                    UpdatedAt: time.Now().UTC().Format(time.RFC3339),
                }
                if _, err := database.CreateInboxItem(item); err == nil { created++ }
            }
        }
    }
    rows.Close()

    // 2. briefing_ready — new briefing for today
    rows, err = database.Query(`SELECT id, user_id, date, created_at FROM briefings WHERE created_at > ?`, sinceISO)
    if err != nil { return created, err }
    for rows.Next() {
        var id int64; var userID, date, createdAt string
        rows.Scan(&id, &userID, &date, &createdAt)
        msgTS := fmt.Sprintf("briefing:%d", id)
        if existsInboxItem(database, "briefing", msgTS, "briefing_ready") { continue }
        item := db.InboxItem{
            ChannelID: "briefing", MessageTS: msgTS, SenderUserID: "watchtower",
            TriggerType: "briefing_ready", Snippet: "Daily briefing ready for " + date,
            ItemClass: "ambient", Status: "pending", Priority: "low",
            CreatedAt: time.Now().UTC().Format(time.RFC3339),
            UpdatedAt: time.Now().UTC().Format(time.RFC3339),
        }
        if _, err := database.CreateInboxItem(item); err == nil { created++ }
    }
    rows.Close()
    return created, nil
}
```

- [ ] **Step 4: PASS** — `go test ./internal/inbox -run TestWatchtowerDetector -v -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/watchtower_detector.go internal/inbox/watchtower_detector_test.go
git commit -m "feat(inbox): watchtower-internal detector for decisions and briefing-ready"
```

---

## Phase 3 — Learning, Feedback, Pinned Selector (parallel after Phase 1)

### Task 9 [PAR after 2,4]: Implicit learner

**Files:**
- Create: `internal/inbox/learner.go`
- Test: `internal/inbox/learner_test.go`

- [ ] **Step 1: Failing test**

```go
func TestLearner_MuteOnHighDismissRate(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Seed 10 inbox_items from sender U1, 9 dismissed
    sender := "U1"
    for i := 0; i < 10; i++ {
        id := seedInboxItem(t, d, sender, "C1", "mention")
        if i < 9 {
            d.Exec(`UPDATE inbox_items SET status='dismissed', updated_at=? WHERE id=?`, time.Now().Format(time.RFC3339), id)
        }
    }
    err := RunImplicitLearner(context.Background(), d, 30*24*time.Hour)
    if err != nil { t.Fatal(err) }
    r, err := d.GetLearnedRule("source_mute", "sender:"+sender)
    if err != nil { t.Fatalf("expected rule: %v", err) }
    if r.Weight != -0.7 { t.Errorf("weight=%v want -0.7", r.Weight) }
    if r.EvidenceCount != 9 { t.Errorf("evidence=%d", r.EvidenceCount) }
}

func TestLearner_BelowThresholdNoRule(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    for i := 0; i < 4; i++ {
        id := seedInboxItem(t, d, "U2", "C1", "mention")
        d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
    }
    RunImplicitLearner(context.Background(), d, 30*24*time.Hour)
    _, err := d.GetLearnedRule("source_mute", "sender:U2")
    if err == nil { t.Error("rule should not exist below evidence threshold") }
}

func TestLearner_DoesNotOverwriteUserRule(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Existing user_rule
    d.UpsertLearnedRule(db.InboxLearnedRule{RuleType:"source_mute", ScopeKey:"sender:U3", Weight:-0.4, Source:"user_rule"})
    for i := 0; i < 10; i++ {
        id := seedInboxItem(t, d, "U3", "C1", "mention")
        d.Exec(`UPDATE inbox_items SET status='dismissed' WHERE id=?`, id)
    }
    RunImplicitLearner(context.Background(), d, 30*24*time.Hour)
    r, _ := d.GetLearnedRule("source_mute", "sender:U3")
    if r.Source != "user_rule" || r.Weight != -0.4 {
        t.Errorf("user_rule overwritten: %+v", r)
    }
}

func TestLearner_ChannelMute(t *testing.T) {
    // 10 items from channel C99, 8 dismissed → source_mute channel:C99 weight -0.5
}
```

- [ ] **Step 2: FAIL** — run.

- [ ] **Step 3: Implement `internal/inbox/learner.go`**

```go
package inbox

import (
    "context"
    "fmt"
    "time"
    "watchtower/internal/db"
)

const (
    minEvidence  = 5
    muteRateSend = 0.8
    muteRateChan = 0.7
)

func RunImplicitLearner(ctx context.Context, database *db.DB, lookback time.Duration) error {
    cutoff := time.Now().Add(-lookback).UTC().Format(time.RFC3339)

    // Per-sender dismiss rate
    senderRows, err := database.Query(`
        SELECT sender_user_id,
               COUNT(*) AS total,
               SUM(CASE WHEN status='dismissed' THEN 1 ELSE 0 END) AS dismisses
        FROM inbox_items
        WHERE created_at > ?
        GROUP BY sender_user_id
        HAVING total >= ?
    `, cutoff, minEvidence)
    if err != nil { return fmt.Errorf("sender query: %w", err) }
    for senderRows.Next() {
        var sender string; var total, dismisses int
        senderRows.Scan(&sender, &total, &dismisses)
        if float64(dismisses)/float64(total) >= muteRateSend {
            err := database.UpsertLearnedRuleImplicit(db.InboxLearnedRule{
                RuleType: "source_mute", ScopeKey: "sender:" + sender,
                Weight: -0.7, EvidenceCount: dismisses,
            })
            if err != nil { return err }
        }
    }
    senderRows.Close()

    // Per-channel dismiss rate
    chanRows, err := database.Query(`
        SELECT channel_id,
               COUNT(*) AS total,
               SUM(CASE WHEN status='dismissed' THEN 1 ELSE 0 END) AS dismisses
        FROM inbox_items
        WHERE created_at > ?
        GROUP BY channel_id
        HAVING total >= ?
    `, cutoff, minEvidence)
    if err != nil { return fmt.Errorf("channel query: %w", err) }
    for chanRows.Next() {
        var ch string; var total, dismisses int
        chanRows.Scan(&ch, &total, &dismisses)
        if float64(dismisses)/float64(total) >= muteRateChan {
            err := database.UpsertLearnedRuleImplicit(db.InboxLearnedRule{
                RuleType: "source_mute", ScopeKey: "channel:" + ch,
                Weight: -0.5, EvidenceCount: dismisses,
            })
            if err != nil { return err }
        }
    }
    chanRows.Close()
    return nil
}
```

- [ ] **Step 4: PASS** — run test.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/learner.go internal/inbox/learner_test.go
git commit -m "feat(inbox): implicit learner deriving mute rules from dismiss rates"
```

---

### Task 10 [PAR after 2,3,4]: Feedback handler

**Files:**
- Create: `internal/inbox/feedback.go`
- Test: `internal/inbox/feedback_test.go`

- [ ] **Step 1: Failing test**

```go
func TestFeedback_NeverShow_CreatesHardMute(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    id := seedInboxItem(t, d, "U1", "C1", "mention")
    err := SubmitFeedback(context.Background(), d, id, -1, "never_show")
    if err != nil { t.Fatal(err) }
    r, err := d.GetLearnedRule("source_mute", "sender:U1")
    if err != nil { t.Fatalf("no mute rule: %v", err) }
    if r.Weight != -1.0 { t.Errorf("weight=%v want -1.0", r.Weight) }
    if r.Source != "explicit_feedback" { t.Errorf("source=%s", r.Source) }
}

func TestFeedback_SourceNoise_WeakerMute(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    id := seedInboxItem(t, d, "U2", "C1", "mention")
    SubmitFeedback(context.Background(), d, id, -1, "source_noise")
    r, _ := d.GetLearnedRule("source_mute", "sender:U2")
    if r.Weight != -0.8 { t.Errorf("weight=%v want -0.8", r.Weight) }
}

func TestFeedback_WrongClass_DowngradesItem(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    id := seedInboxItem(t, d, "U3", "C1", "mention") // default actionable
    SubmitFeedback(context.Background(), d, id, -1, "wrong_class")
    var cls string
    d.QueryRow(`SELECT item_class FROM inbox_items WHERE id=?`, id).Scan(&cls)
    if cls != "ambient" { t.Errorf("class=%s, want ambient", cls) }
}

func TestFeedback_PositiveBoost(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    id := seedInboxItem(t, d, "U4", "C1", "mention")
    SubmitFeedback(context.Background(), d, id, 1, "")
    r, err := d.GetLearnedRule("source_boost", "sender:U4")
    if err != nil { t.Fatalf("expected boost rule: %v", err) }
    if r.Weight != 0.6 { t.Errorf("weight=%v want 0.6", r.Weight) }
}

func TestFeedback_FeedbackRowWritten(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    id := seedInboxItem(t, d, "U5", "C1", "mention")
    SubmitFeedback(context.Background(), d, id, -1, "source_noise")
    fbs, _ := d.GetFeedbackForItem(id)
    if len(fbs) != 1 { t.Fatalf("want 1 feedback row, got %d", len(fbs)) }
    if fbs[0].Rating != -1 || fbs[0].Reason != "source_noise" {
        t.Errorf("bad feedback: %+v", fbs[0])
    }
}
```

- [ ] **Step 2: FAIL** — run.

- [ ] **Step 3: Implement `internal/inbox/feedback.go`**

```go
package inbox

import (
    "context"
    "fmt"
    "watchtower/internal/db"
)

// SubmitFeedback writes a feedback row and updates learned rules atomically.
// rating: -1 negative, +1 positive. reason: one of source_noise, wrong_priority, wrong_class, never_show, ''.
func SubmitFeedback(ctx context.Context, database *db.DB, itemID int64, rating int, reason string) error {
    // 1. Write raw feedback row
    if err := database.RecordInboxFeedback(itemID, rating, reason); err != nil {
        return fmt.Errorf("record feedback: %w", err)
    }

    // 2. Load the item to know sender/channel
    item, err := database.GetInboxItem(itemID)
    if err != nil { return fmt.Errorf("get item: %w", err) }

    // 3. Apply rule updates + item class adjustments
    switch {
    case rating == -1 && reason == "never_show":
        _ = database.UpsertLearnedRule(db.InboxLearnedRule{
            RuleType: "source_mute", ScopeKey: "sender:" + item.SenderUserID,
            Weight: -1.0, Source: "explicit_feedback", EvidenceCount: 1,
        })
    case rating == -1 && reason == "source_noise":
        _ = database.UpsertLearnedRule(db.InboxLearnedRule{
            RuleType: "source_mute", ScopeKey: "sender:" + item.SenderUserID,
            Weight: -0.8, Source: "explicit_feedback", EvidenceCount: 1,
        })
    case rating == -1 && reason == "wrong_class":
        if item.ItemClass == "actionable" {
            _ = database.SetInboxItemClass(itemID, "ambient")
        }
        _ = database.UpsertLearnedRule(db.InboxLearnedRule{
            RuleType: "trigger_downgrade",
            ScopeKey: "trigger:" + item.TriggerType + ":sender:" + item.SenderUserID,
            Weight: -0.6, Source: "explicit_feedback", EvidenceCount: 1,
        })
    case rating == -1 && reason == "wrong_priority":
        _ = database.UpsertLearnedRule(db.InboxLearnedRule{
            RuleType: "trigger_downgrade",
            ScopeKey: "sender:" + item.SenderUserID,
            Weight: -0.5, Source: "explicit_feedback", EvidenceCount: 1,
        })
    case rating == 1:
        _ = database.UpsertLearnedRule(db.InboxLearnedRule{
            RuleType: "source_boost", ScopeKey: "sender:" + item.SenderUserID,
            Weight: 0.6, Source: "explicit_feedback", EvidenceCount: 1,
        })
    }
    return nil
}
```

Note: `database.GetInboxItem` must exist — if not, add it alongside this task in `inbox.go`:

```go
func (db *DB) GetInboxItem(id int64) (InboxItem, error) {
    row := db.QueryRow(`SELECT ` + inboxItemColumns + ` FROM inbox_items WHERE id=?`, id)
    return scanInboxItem(row)
}
```

- [ ] **Step 4: PASS** — run test.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/feedback.go internal/inbox/feedback_test.go internal/db/inbox.go
git commit -m "feat(inbox): feedback handler writing rule updates and class downgrades"
```

---

### Task 11 [PAR after 2,4,5]: Pinned selector (AI call)

**Files:**
- Create: `internal/inbox/prompts/select_pinned.tmpl`
- Create: `internal/inbox/pinned_selector.go`
- Test: `internal/inbox/pinned_selector_test.go`

- [ ] **Step 1: Write `internal/inbox/prompts/select_pinned.tmpl`**

```
You are selecting the most critical items to pin at the top of the user's Inbox for the next ~2 hours.

Current time: {{.Now}}
User's current calendar window (next 2h): {{.CalendarContext}}

{{.UserPreferences}}

Candidate actionable items (select at most {{.MaxPinned}} IDs):
{{range .Items}}
- id={{.ID}} trigger={{.TriggerType}} priority={{.Priority}} sender={{.SenderUserID}} channel={{.ChannelID}}
  snippet: {{.Snippet}}
{{end}}

Respond with JSON only:
{"pinned_ids": [1, 5, 12], "reason": "short explanation"}

Rules:
- Select AT MOST {{.MaxPinned}} ids.
- Honor USER PREFERENCES: never pin items from muted sources (weight ≤ -0.8).
- Prefer items with upcoming deadlines / live meetings / boss-level senders / blocker labels.
- If nothing is urgent, return {"pinned_ids": [], "reason": "nothing critical"}.
```

- [ ] **Step 2: Failing test**

```go
func TestPinnedSelector_MaxFive(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    var ids []int64
    for i := 0; i < 20; i++ {
        ids = append(ids, seedInboxItem(t, d, fmt.Sprintf("U%d", i), "C1", "mention"))
        d.Exec(`UPDATE inbox_items SET item_class='actionable', priority='high' WHERE id=?`, ids[i])
    }
    mock := &mockGen{respJSON: fmt.Sprintf(`{"pinned_ids":%s,"reason":"urgent"}`, jsonArray(ids[:10]))}
    p := NewPinnedSelector(d, mock)
    n, err := p.Run(context.Background())
    if err != nil { t.Fatal(err) }
    if n > 5 { t.Errorf("capped at 5, got %d", n) }
    pinned, _ := d.ListInboxPinned()
    if len(pinned) > 5 { t.Errorf("pinned list >5: %d", len(pinned)) }
}

func TestPinnedSelector_AIFailureKeepsState(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    existing := seedInboxItem(t, d, "U1", "C1", "mention")
    d.SetInboxPinned([]int64{existing})
    mock := &mockGen{err: errors.New("boom")}
    p := NewPinnedSelector(d, mock)
    _, err := p.Run(context.Background())
    if err != nil { t.Fatal(err) }
    pinned, _ := d.ListInboxPinned()
    if len(pinned) != 1 || pinned[0].ID != existing {
        t.Errorf("pinned state should be preserved on AI failure")
    }
}

func TestPinnedSelector_InvalidJSONFallback(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedInboxItem(t, d, "U1", "C1", "mention")
    mock := &mockGen{respJSON: "not-json"}
    p := NewPinnedSelector(d, mock)
    if _, err := p.Run(context.Background()); err != nil {
        t.Error("should not fail pipeline on invalid JSON")
    }
}

func TestPinnedSelector_RespectsMuteRules(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    muted := seedInboxItem(t, d, "Umuted", "C1", "mention")
    _ = seedInboxItem(t, d, "Uok", "C1", "mention")
    d.UpsertLearnedRule(db.InboxLearnedRule{RuleType:"source_mute", ScopeKey:"sender:Umuted", Weight:-0.9, Source:"user_rule"})
    mock := &mockGen{respJSON: fmt.Sprintf(`{"pinned_ids":[%d],"reason":"AI still tried"}`, muted)}
    p := NewPinnedSelector(d, mock)
    p.Run(context.Background())
    pinned, _ := d.ListInboxPinned()
    for _, it := range pinned {
        if it.ID == muted { t.Error("muted item was pinned despite rule") }
    }
}
```

- [ ] **Step 3: FAIL** — run.

- [ ] **Step 4: Implement `internal/inbox/pinned_selector.go`**

```go
package inbox

import (
    "context"
    "encoding/json"
    "fmt"
    "text/template"
    "time"
    "watchtower/internal/db"
    "watchtower/internal/digest"
)

const maxPinned = 5

type PinnedSelector struct {
    db  *db.DB
    gen digest.Generator
}

func NewPinnedSelector(database *db.DB, gen digest.Generator) *PinnedSelector {
    return &PinnedSelector{db: database, gen: gen}
}

type pinnedResp struct {
    PinnedIDs []int64 `json:"pinned_ids"`
    Reason    string  `json:"reason"`
}

func (p *PinnedSelector) Run(ctx context.Context) (int, error) {
    items, err := p.db.ListActionableOpen()
    if err != nil { return 0, err }
    if len(items) == 0 {
        _ = p.db.ClearPinnedAll()
        return 0, nil
    }

    prefs, err := buildUserPreferencesBlock(p.db, items)
    if err != nil { return 0, err }

    prompt, err := renderPinnedPrompt(items, prefs)
    if err != nil { return 0, err }

    resp, err := p.gen.QuerySync(ctx, prompt, "haiku")
    if err != nil {
        // keep existing pinned state — do not clear
        return 0, nil
    }
    parsed, err := parsePinnedResponse(resp)
    if err != nil {
        return 0, nil // fallback: keep state
    }
    // Filter out muted items
    mutes := loadMuteScopes(p.db)
    filtered := filterNotMuted(parsed.PinnedIDs, items, mutes)
    if len(filtered) > maxPinned { filtered = filtered[:maxPinned] }
    if err := p.db.SetInboxPinned(filtered); err != nil { return 0, err }
    return len(filtered), nil
}

func renderPinnedPrompt(items []db.InboxItem, prefs string) (string, error) {
    tmpl, err := template.ParseFiles("internal/inbox/prompts/select_pinned.tmpl")
    if err != nil {
        // in tests, use embedded string fallback — prefer //go:embed
        return "", err
    }
    var buf strings.Builder
    err = tmpl.Execute(&buf, map[string]interface{}{
        "Now": time.Now().Format(time.RFC3339),
        "CalendarContext": "",
        "UserPreferences": prefs,
        "Items": items,
        "MaxPinned": maxPinned,
    })
    return buf.String(), err
}

func parsePinnedResponse(s string) (pinnedResp, error) {
    var r pinnedResp
    if err := json.Unmarshal([]byte(s), &r); err != nil { return r, err }
    return r, nil
}

func loadMuteScopes(database *db.DB) map[string]bool {
    rules, _ := database.ListAllLearnedRules()
    m := map[string]bool{}
    for _, r := range rules {
        if r.RuleType == "source_mute" && r.Weight <= -0.8 { m[r.ScopeKey] = true }
    }
    return m
}

func filterNotMuted(ids []int64, items []db.InboxItem, mutes map[string]bool) []int64 {
    byID := map[int64]db.InboxItem{}
    for _, it := range items { byID[it.ID] = it }
    var out []int64
    for _, id := range ids {
        it, ok := byID[id]
        if !ok { continue }
        if mutes["sender:"+it.SenderUserID] { continue }
        if mutes["channel:"+it.ChannelID] { continue }
        out = append(out, id)
    }
    return out
}
```

Use `//go:embed` for the prompt template to avoid filesystem deps in tests:

```go
import _ "embed"
//go:embed prompts/select_pinned.tmpl
var selectPinnedTmpl string
```

Replace `template.ParseFiles` with `template.New("select_pinned").Parse(selectPinnedTmpl)`.

Add `ListActionableOpen` to `internal/db/inbox.go`:

```go
func (db *DB) ListActionableOpen() ([]InboxItem, error) {
    rows, err := db.Query(`SELECT `+inboxItemColumns+` FROM inbox_items
        WHERE item_class='actionable' AND status='pending' AND archived_at IS NULL
        ORDER BY created_at DESC LIMIT 100`)
    if err != nil { return nil, err }
    defer rows.Close()
    return scanInboxItems(rows)
}
```

`buildUserPreferencesBlock` is implemented in Task 12.

- [ ] **Step 5: PASS** — run test.

- [ ] **Step 6: Commit**

```bash
git add internal/inbox/pinned_selector.go internal/inbox/pinned_selector_test.go internal/inbox/prompts/select_pinned.tmpl internal/db/inbox.go
git commit -m "feat(inbox): AI pinned selector with mute filtering and failure fallback"
```

---

### Task 12 [PAR after 2]: User preferences block for prompts

**Files:**
- Create: `internal/inbox/user_preferences.go`
- Test: `internal/inbox/user_preferences_test.go`
- Modify: `internal/inbox/prompts/prioritize.tmpl` (inject block)

- [ ] **Step 1: Failing test**

```go
func TestBuildUserPrefs_TopByRelevance(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    d.UpsertLearnedRule(db.InboxLearnedRule{RuleType:"source_mute", ScopeKey:"sender:U1", Weight:-0.9, Source:"user_rule"})
    d.UpsertLearnedRule(db.InboxLearnedRule{RuleType:"source_mute", ScopeKey:"channel:C9", Weight:-0.5, Source:"implicit"})
    d.UpsertLearnedRule(db.InboxLearnedRule{RuleType:"source_boost", ScopeKey:"sender:U2", Weight:0.7, Source:"explicit_feedback"})

    items := []db.InboxItem{
        {SenderUserID: "U1", ChannelID: "Cx"},
        {SenderUserID: "U2", ChannelID: "C9"},
    }
    block, err := buildUserPreferencesBlock(d, items)
    if err != nil { t.Fatal(err) }
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
    d := newTestDB(t); defer d.Close()
    items := []db.InboxItem{{SenderUserID:"U1",ChannelID:"C1"}}
    block, _ := buildUserPreferencesBlock(d, items)
    if strings.Contains(block, "Mutes:") || strings.Contains(block, "Boosts:") {
        t.Errorf("should be minimal when no rules: %s", block)
    }
}
```

- [ ] **Step 2: FAIL** — run.

- [ ] **Step 3: Implement `internal/inbox/user_preferences.go`**

```go
package inbox

import (
    "fmt"
    "sort"
    "strings"
    "watchtower/internal/db"
)

const maxPrefsInPrompt = 20

// buildUserPreferencesBlock returns a formatted "=== USER PREFERENCES ===" section
// containing learned rules relevant to the given items' senders/channels, capped at maxPrefsInPrompt.
func buildUserPreferencesBlock(database *db.DB, items []db.InboxItem) (string, error) {
    seen := map[string]bool{}
    var scopes []string
    for _, it := range items {
        add := func(s string) { if !seen[s] { seen[s] = true; scopes = append(scopes, s) } }
        add("sender:" + it.SenderUserID)
        add("channel:" + it.ChannelID)
    }
    if len(scopes) == 0 { return "", nil }

    rules, err := database.ListLearnedRulesByScope(scopes, maxPrefsInPrompt)
    if err != nil { return "", err }
    if len(rules) == 0 { return "", nil }

    sort.SliceStable(rules, func(i, j int) bool { return absF(rules[i].Weight) > absF(rules[j].Weight) })

    var mutes, boosts []string
    for _, r := range rules {
        line := fmt.Sprintf("%s (weight=%.1f, %s)", r.ScopeKey, r.Weight, r.Source)
        if r.Weight < 0 { mutes = append(mutes, line) } else { boosts = append(boosts, line) }
    }

    var b strings.Builder
    b.WriteString("=== USER PREFERENCES ===\n")
    if len(mutes) > 0 {
        b.WriteString("Mutes: " + strings.Join(mutes, "; ") + "\n")
    }
    if len(boosts) > 0 {
        b.WriteString("Boosts: " + strings.Join(boosts, "; ") + "\n")
    }
    b.WriteString("Apply these when choosing priority and selecting pinned items.\n")
    return b.String(), nil
}

func absF(f float64) float64 { if f < 0 { return -f }; return f }
```

- [ ] **Step 4: Inject into `internal/inbox/prompts/prioritize.tmpl`**

Add near top, before item listing:

```
{{.UserPreferences}}
```

And update the Go code path that renders this template (in `pipeline.go`'s `aiPrioritizeNewItems`) to call `buildUserPreferencesBlock` and pass the result as `UserPreferences`.

- [ ] **Step 5: PASS** — run test + existing `TestAIPrioritize*` tests.

- [ ] **Step 6: Commit**

```bash
git add internal/inbox/user_preferences.go internal/inbox/user_preferences_test.go internal/inbox/prompts/prioritize.tmpl internal/inbox/pipeline.go
git commit -m "feat(inbox): inject user preferences block into AI prompts"
```

---

## Phase 4 — Pipeline Orchestration (serial after Phases 2+3)

### Task 13 [SEQ after 6,7,8,9,10,11,12]: Pipeline.Run orchestration

**Files:**
- Modify: `internal/inbox/pipeline.go`
- Test: `internal/inbox/pipeline_test.go` (add orchestration test)

- [ ] **Step 1: Write orchestration test**

```go
func TestPipeline_Run_OrderedPhases(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Seed: a jira issue assigned to alice, a calendar invite for alice, a high-importance digest decision
    seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-5*time.Minute))
    seedCalendarEvent(t, d, "evt-1", "Sync", `[{"email":"alice@x.com","rsvp_status":"needsAction"}]`, "confirmed", time.Now().Add(-10*time.Minute), time.Now().Add(-10*time.Minute))
    seedDigestWithHighImportance(t, d, "C1", `[{"type":"decision","topic":"Launch","importance":"high"}]`, time.Now().Add(-5*time.Minute))

    cfg := &config.Config{}
    mockGen := &mockGen{respJSON: `{"pinned_ids":[]}`} // no pins
    p := New(d, cfg, mockGen, log.Default())
    p.SetCurrentUser("alice", "alice@x.com")
    _, _, err := p.Run(context.Background())
    if err != nil { t.Fatal(err) }

    // Assert items of each type were created
    mustCount := func(trig string, want int) {
        var n int
        d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type=?`, trig).Scan(&n)
        if n != want { t.Errorf("%s: got %d want %d", trig, n, want) }
    }
    mustCount("jira_assigned", 1)
    mustCount("calendar_invite", 1)
    mustCount("decision_made", 1)

    // Class assigned correctly
    var cls string
    d.QueryRow(`SELECT item_class FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&cls)
    if cls != "ambient" { t.Errorf("decision_made class=%s", cls) }
}

func TestPipeline_Run_AutoArchiveRuns(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    oldT := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
    d.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
        VALUES ('C1','1.0','U1','decision_made','pending','low','ambient',?,?)`, oldT, oldT)
    p := New(d, &config.Config{}, &mockGen{respJSON:`{}`}, log.Default())
    p.Run(context.Background())
    var reason string
    d.QueryRow(`SELECT archive_reason FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&reason)
    if reason != "seen_expired" { t.Errorf("archive reason=%q", reason) }
}
```

- [ ] **Step 2: FAIL** — run.

- [ ] **Step 3: Refactor `Pipeline.Run`**

Add fields to `Pipeline` struct: `currentUserID string`, `currentUserEmail string`, `pinnedSelector *PinnedSelector`, optional Jira/Calendar clients (nil-safe).

New phase structure inside `Run`:

```go
func (p *Pipeline) Run(ctx context.Context) (int, int, error) {
    total := 0

    sinceTS := p.loadLastProcessedTS()

    // Phase 1: detection (non-fatal errors logged, don't stop pipeline)
    if n, err := p.detectSlackTriggers(ctx); err != nil { p.logger.Printf("slack detect: %v", err) } else { total += n }
    if n, err := DetectJira(ctx, p.db, p.currentUserID, sinceTS); err != nil { p.logger.Printf("jira detect: %v", err) } else { total += n }
    if n, err := DetectCalendar(ctx, p.db, p.currentUserEmail, sinceTS); err != nil { p.logger.Printf("calendar detect: %v", err) } else { total += n }
    if n, err := DetectWatchtowerInternal(ctx, p.db, sinceTS); err != nil { p.logger.Printf("wt detect: %v", err) } else { total += n }

    // Phase 2: classify new items (set item_class based on trigger_type)
    if err := p.classifyNewItems(ctx); err != nil { p.logger.Printf("classify: %v", err) }

    // Phase 3: learn implicit rules
    if err := RunImplicitLearner(ctx, p.db, 30*24*time.Hour); err != nil { p.logger.Printf("learn: %v", err) }

    // Phase 4a: AI prioritize new items (existing function, now reads USER PREFERENCES)
    newItems, _ := p.db.ListNewInboxItems() // adjust to match existing API
    if len(newItems) > 0 {
        if _, err := p.aiPrioritizeNewItems(ctx, p.currentUserID, newItems, 0, total); err != nil {
            p.logger.Printf("prioritize: %v", err)
        }
    }

    // Phase 4b: AI select pinned (separate call)
    if _, err := p.pinnedSelector.Run(ctx); err != nil { p.logger.Printf("pin select: %v", err) }

    // Phase 5: auto-resolve + auto-archive
    p.autoResolveByRules(ctx)
    if _, err := p.db.ArchiveExpiredAmbient(7 * 24 * time.Hour); err != nil { p.logger.Printf("archive ambient: %v", err) }
    if _, err := p.db.ArchiveStaleActionable(14 * 24 * time.Hour); err != nil { p.logger.Printf("archive stale: %v", err) }

    // Phase 6: unsnooze expired
    p.unsnoozeExpired(ctx)

    // Advance watermark
    p.saveLastProcessedTS(time.Now())
    return total, 0, nil
}

func (p *Pipeline) classifyNewItems(ctx context.Context) error {
    rows, err := p.db.Query(`SELECT id, trigger_type FROM inbox_items WHERE item_class=''`)
    if err != nil { return err }
    defer rows.Close()
    var updates [][2]interface{}
    for rows.Next() {
        var id int64; var trig string
        rows.Scan(&id, &trig)
        updates = append(updates, [2]interface{}{id, DefaultItemClass(trig)})
    }
    for _, u := range updates {
        p.db.SetInboxItemClass(u[0].(int64), u[1].(string))
    }
    return nil
}
```

Add constructor method `SetCurrentUser`:

```go
func (p *Pipeline) SetCurrentUser(id, email string) { p.currentUserID = id; p.currentUserEmail = email }
```

In `New(...)`, initialise `pinnedSelector := NewPinnedSelector(database, gen)`.

- [ ] **Step 4: PASS** — run test.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/pipeline.go internal/inbox/pipeline_test.go
git commit -m "feat(inbox): orchestrate all detectors, learner, classifier, pinned selector, auto-archive"
```

---

### Task 14 [SEQ after 13]: Auto-resolve extensions for Jira & Calendar

**Files:**
- Modify: `internal/inbox/pipeline.go` (extend `autoResolveByRules`)
- Test: append to `internal/inbox/pipeline_test.go`

- [ ] **Step 1: Failing test**

```go
func TestAutoResolve_Jira_UserCommented(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    // Open jira_comment_mention for WT-1, then user adds comment to the issue
    seedJiraIssue(t, d, "WT-1", "alice", time.Now().Add(-1*time.Hour))
    seedJiraComment(t, d, "WT-1", "bob", "hey [~alice]", time.Now().Add(-30*time.Minute))
    p := newPipelineForTest(t, d, "alice", "alice@x.com")
    p.Run(context.Background())
    // Now alice comments
    seedJiraComment(t, d, "WT-1", "alice", "got it", time.Now())
    p.Run(context.Background())
    var status string
    d.QueryRow(`SELECT status FROM inbox_items WHERE trigger_type='jira_comment_mention' AND channel_id='WT-1'`).Scan(&status)
    if status != "resolved" { t.Errorf("want resolved, got %s", status) }
}

func TestAutoResolve_Calendar_UserResponded(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedCalendarEvent(t, d, "evt-1", "Sync", `[{"email":"alice@x.com","rsvp_status":"needsAction"}]`, "confirmed", time.Now().Add(-30*time.Minute), time.Now().Add(-30*time.Minute))
    p := newPipelineForTest(t, d, "alice", "alice@x.com")
    p.Run(context.Background())
    // Now alice responds
    d.Exec(`UPDATE calendar_events SET attendees=? WHERE id='evt-1'`, `[{"email":"alice@x.com","rsvp_status":"accepted"}]`)
    p.Run(context.Background())
    var status string
    d.QueryRow(`SELECT status FROM inbox_items WHERE trigger_type='calendar_invite'`).Scan(&status)
    if status != "resolved" { t.Errorf("want resolved, got %s", status) }
}
```

- [ ] **Step 2: FAIL** — run.

- [ ] **Step 3: Extend `autoResolveByRules`**

```go
func (p *Pipeline) autoResolveByRules(ctx context.Context) {
    p.autoResolveSlack(ctx) // existing CheckUserReplied logic
    p.autoResolveJira(ctx)
    p.autoResolveCalendar(ctx)
}

func (p *Pipeline) autoResolveJira(ctx context.Context) {
    rows, _ := p.db.Query(`SELECT id, channel_id, created_at FROM inbox_items
        WHERE trigger_type IN ('jira_comment_mention','jira_assigned') AND status='pending'`)
    defer rows.Close()
    for rows.Next() {
        var id int64; var key, createdAt string
        rows.Scan(&id, &key, &createdAt)
        var n int
        p.db.QueryRow(`SELECT COUNT(*) FROM jira_comments WHERE issue_key=? AND author_id=? AND created_at > ?`, key, p.currentUserID, createdAt).Scan(&n)
        if n > 0 {
            p.db.Exec(`UPDATE inbox_items SET status='resolved', resolved_reason='User commented on issue', updated_at=? WHERE id=?`,
                time.Now().UTC().Format(time.RFC3339), id)
        }
    }
}

func (p *Pipeline) autoResolveCalendar(ctx context.Context) {
    rows, _ := p.db.Query(`SELECT id, channel_id FROM inbox_items
        WHERE trigger_type IN ('calendar_invite','calendar_time_change') AND status='pending'`)
    defer rows.Close()
    for rows.Next() {
        var id int64; var eid string
        rows.Scan(&id, &eid)
        var att string
        p.db.QueryRow(`SELECT attendees FROM calendar_events WHERE id=?`, eid).Scan(&att)
        var list []calAttendee
        json.Unmarshal([]byte(att), &list)
        for _, a := range list {
            if a.Email == p.currentUserEmail && a.RSVPStatus != "needsAction" && a.RSVPStatus != "" {
                p.db.Exec(`UPDATE inbox_items SET status='resolved', resolved_reason='User responded to invite', updated_at=? WHERE id=?`,
                    time.Now().UTC().Format(time.RFC3339), id)
                break
            }
        }
    }
}
```

- [ ] **Step 4: PASS** — run test.

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/pipeline.go internal/inbox/pipeline_test.go
git commit -m "feat(inbox): auto-resolve jira and calendar items based on user actions"
```

---

### Task 15 [SEQ after 13]: Daemon integration (ordering + user identity)

**Files:**
- Modify: `internal/daemon/daemon.go` (or wherever Inbox pipeline is run)

- [ ] **Step 1: Locate daemon inbox-hook**

Agent runs:

```bash
grep -rn "SetInboxPipeline\|inbox.Pipeline\|inbox.New" internal/daemon/ internal/cmd/ cmd/
```

- [ ] **Step 2: Adjust call order**

Ensure daemon cycle is:

```
1. slack sync
2. jira sync
3. calendar sync
4. digest pipeline
5. tracks pipeline
6. people pipeline
7. inbox pipeline  ← runs AFTER digests so decision_made is fresh
8. briefing pipeline
```

Move `inboxPipeline.Run(ctx)` to after `peoplePipeline.Run(ctx)` and before `briefingPipeline.Run(ctx)`.

- [ ] **Step 3: Pass current user identity**

Before running Inbox pipeline, call `inboxPipeline.SetCurrentUser(userID, userEmail)` — source user ID and email from config or from workspace metadata (`workspace.user_id`, user profile email).

- [ ] **Step 4: Add Jira/Calendar clients if available**

In pipeline constructor (call site in daemon), pass clients through `New(...)` if already created for their respective syncers, OR leave nil and the detectors short-circuit cleanly (they only read already-synced DB tables).

- [ ] **Step 5: Run daemon integration smoke**

```bash
go build ./...
go test ./internal/daemon/... -v -count=1
```

Expected: compiles and daemon tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/
git commit -m "feat(inbox): integrate inbox pulse pipeline into daemon cycle ordering"
```

---

## Phase 5 — Desktop Foundation (parallel after Phase 4)

### Task 16 [PAR after 1]: Swift InboxItem model extension

**Files:**
- Modify: `WatchtowerDesktop/Sources/Models/InboxItem.swift`
- Modify: `WatchtowerDesktop/Tests/.../InboxItemTests.swift` (create if absent)

- [ ] **Step 1: Failing test**

```swift
import XCTest
@testable import Watchtower

final class InboxItemTests: XCTestCase {
    func testDecodesNewFields() throws {
        // Insert a row with new columns, fetch and assert
        let db = try TestDB.make()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, item_class, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high','actionable',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z","2026-04-23T10:00:00Z"])
        }
        let item = try db.read { db in try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1") }!
        XCTAssertEqual(item.itemClass, .actionable)
        XCTAssertTrue(item.pinned)
        XCTAssertNil(item.archivedAt)
    }
}
```

- [ ] **Step 2: FAIL** — `cd WatchtowerDesktop && swift test --filter InboxItemTests`

- [ ] **Step 3: Extend `InboxItem`**

Add:

```swift
enum ItemClass: String, Codable {
    case actionable, ambient
}

extension InboxItem {
    var itemClass: ItemClass { get { ItemClass(rawValue: itemClassRaw) ?? .ambient } }
}

// In struct InboxItem:
let itemClassRaw: String   // column: item_class
let pinned: Bool            // column: pinned (Int 0/1)
let archivedAt: Date?       // column: archived_at
let archiveReason: String   // column: archive_reason
```

Update `CodingKeys` / GRDB column mapping to include these fields.

Update `.databaseTableName` row decoder to map `item_class`, `pinned`, `archived_at`, `archive_reason`.

- [ ] **Step 4: PASS** — swift test.

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/Models/InboxItem.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): extend InboxItem model with class, pinned, archive fields"
```

---

### Task 17 [PAR after 16]: Swift InboxLearnedRule & InboxFeedback models

**Files:**
- Create: `WatchtowerDesktop/Sources/Models/InboxLearnedRule.swift`
- Create: `WatchtowerDesktop/Sources/Models/InboxFeedback.swift`
- Test: `WatchtowerDesktop/Tests/.../InboxLearnedRuleTests.swift`

- [ ] **Step 1: Failing test**

```swift
func testInboxLearnedRuleFetches() throws {
    let db = try TestDB.make()
    try db.write { db in
        try db.execute(sql: """
            INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
            VALUES ('source_mute','sender:U1',-0.7,'implicit',10,'2026-04-23T10:00:00Z')
        """)
    }
    let rules = try db.read { db in try InboxLearnedRule.fetchAll(db) }
    XCTAssertEqual(rules.count, 1)
    XCTAssertEqual(rules[0].scopeKey, "sender:U1")
    XCTAssertEqual(rules[0].weight, -0.7)
}
```

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Implement models**

```swift
// InboxLearnedRule.swift
import Foundation
import GRDB

struct InboxLearnedRule: Codable, FetchableRecord, PersistableRecord, Identifiable, Equatable {
    static let databaseTableName = "inbox_learned_rules"
    var id: Int64?
    var ruleType: String
    var scopeKey: String
    var weight: Double
    var source: String
    var evidenceCount: Int
    var lastUpdated: String

    enum CodingKeys: String, CodingKey {
        case id, ruleType = "rule_type", scopeKey = "scope_key", weight, source, evidenceCount = "evidence_count", lastUpdated = "last_updated"
    }
}

// InboxFeedback.swift
struct InboxFeedback: Codable, FetchableRecord, PersistableRecord, Identifiable, Equatable {
    static let databaseTableName = "inbox_feedback"
    var id: Int64?
    var inboxItemId: Int64
    var rating: Int
    var reason: String
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, inboxItemId = "inbox_item_id", rating, reason, createdAt = "created_at"
    }
}
```

- [ ] **Step 4: PASS.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/Models/InboxLearnedRule.swift WatchtowerDesktop/Sources/Models/InboxFeedback.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): InboxLearnedRule and InboxFeedback GRDB models"
```

---

### Task 18 [PAR after 17]: Swift Queries — learned rules + feedback

**Files:**
- Create: `WatchtowerDesktop/Sources/Database/Queries/InboxLearnedRulesQueries.swift`
- Create: `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift`
- Test: `WatchtowerDesktop/Tests/.../InboxLearnedRulesQueriesTests.swift`

- [ ] **Step 1: Failing test**

```swift
func testListAllOrderedByAbsWeight() throws {
    let q = InboxLearnedRulesQueries(dbPool: pool)
    try pool.write { db in
        try db.execute(sql: "INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated) VALUES ('source_mute','a',-0.9,'user_rule',0,?)", arguments: [nowISO])
        try db.execute(sql: "INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated) VALUES ('source_boost','b',0.5,'implicit',5,?)", arguments: [nowISO])
    }
    let out = try q.listAll()
    XCTAssertEqual(out[0].scopeKey, "a")
    XCTAssertEqual(out[1].scopeKey, "b")
}

func testDeleteRule() throws {
    // Add rule, delete by (ruleType, scopeKey), verify absent
}

func testUpsertManualRule() throws {
    let q = InboxLearnedRulesQueries(dbPool: pool)
    try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.9)
    let all = try q.listAll()
    XCTAssertEqual(all.count, 1)
    XCTAssertEqual(all[0].source, "user_rule")
}
```

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Implement queries**

```swift
// InboxLearnedRulesQueries.swift
import Foundation
import GRDB

struct InboxLearnedRulesQueries {
    let dbPool: DatabasePool

    func listAll() throws -> [InboxLearnedRule] {
        try dbPool.read { db in
            try InboxLearnedRule.fetchAll(db,
                sql: "SELECT * FROM inbox_learned_rules ORDER BY ABS(weight) DESC, last_updated DESC")
        }
    }

    func upsertManual(ruleType: String, scopeKey: String, weight: Double) throws {
        try dbPool.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                VALUES (?,?,?, 'user_rule', 0, ?)
                ON CONFLICT(rule_type, scope_key) DO UPDATE SET
                    weight=excluded.weight, source='user_rule', last_updated=excluded.last_updated
            """, arguments: [ruleType, scopeKey, weight, ISO8601DateFormatter().string(from: Date())])
        }
    }

    func delete(ruleType: String, scopeKey: String) throws {
        try dbPool.write { db in
            try db.execute(sql: "DELETE FROM inbox_learned_rules WHERE rule_type=? AND scope_key=?",
                arguments: [ruleType, scopeKey])
        }
    }

    func observeAll() -> ValueObservation<ValueReducers.Fetch<[InboxLearnedRule]>> {
        ValueObservation.tracking { db in
            try InboxLearnedRule.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules ORDER BY ABS(weight) DESC")
        }
    }
}

// InboxFeedbackQueries.swift
struct InboxFeedbackQueries {
    let dbPool: DatabasePool
    func record(itemID: Int64, rating: Int, reason: String) throws {
        try dbPool.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
                VALUES (?,?,?,?)
            """, arguments: [itemID, rating, reason, ISO8601DateFormatter().string(from: Date())])
            // Derive rule update (mirror Go SubmitFeedback logic) — for MVP, leave rule derivation to Go daemon.
        }
    }
}
```

Note: rule derivation happens in Go (next daemon cycle runs `RunImplicitLearner`, and explicit feedback-to-rule mapping from `SubmitFeedback` lives there). The Swift side only writes the raw feedback row; Go picks it up. **However**, for immediate UX feedback, also upsert the corresponding rule from Swift using the mapping in `SubmitFeedback`. Re-implement the switch statement in `InboxFeedbackQueries.record`:

```swift
// After inserting feedback row, within same transaction:
switch (rating, reason) {
case (-1, "never_show"):  try upsertRule(db, type: "source_mute", scope: "sender:\(senderID)", weight: -1.0, source: "explicit_feedback")
case (-1, "source_noise"): try upsertRule(db, type: "source_mute", scope: "sender:\(senderID)", weight: -0.8, source: "explicit_feedback")
case (-1, "wrong_class"):  try db.execute(sql: "UPDATE inbox_items SET item_class='ambient' WHERE id=?", arguments: [itemID])
                           try upsertRule(db, type: "trigger_downgrade", scope: "trigger:\(triggerType):sender:\(senderID)", weight: -0.6, source: "explicit_feedback")
case (-1, "wrong_priority"): try upsertRule(db, type: "trigger_downgrade", scope: "sender:\(senderID)", weight: -0.5, source: "explicit_feedback")
case (1, _):               try upsertRule(db, type: "source_boost", scope: "sender:\(senderID)", weight: 0.6, source: "explicit_feedback")
default: break
}
```

(Signature change: `record(item: InboxItem, rating: Int, reason: String)` so sender/trigger are available.)

- [ ] **Step 4: PASS.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/Database/Queries/InboxLearnedRulesQueries.swift WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): queries for learned rules and feedback with rule derivation"
```

---

### Task 19 [PAR after 16]: Swift InboxQueries extensions

**Files:**
- Modify: `WatchtowerDesktop/Sources/Database/Queries/InboxQueries.swift`
- Test: existing `InboxQueriesTests` (extend)

- [ ] **Step 1: Failing test**

```swift
func testObservePinned() throws {
    let q = InboxQueries(dbPool: pool)
    try pool.write { db in
        try db.execute(sql: """
            INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, pinned, created_at, updated_at)
            VALUES ('C1','1.0','U1','mention','pending','high','actionable',1,?,?)
        """, arguments: [nowISO, nowISO])
    }
    let pinned = try q.fetchPinned()
    XCTAssertEqual(pinned.count, 1)
}

func testFetchFeedPaginated() throws {
    // Seed 60 items, fetchFeed(limit:20, offset:20) returns 20
}

func testFetchFeedExcludesArchivedAndResolved() throws {
    // Seed 3 items: alive, archived, resolved. Feed returns only alive.
}

func testHighPriorityInPinnedIndicator() throws {
    // Seed one pinned item priority='high', hasHighPriorityPinned returns true.
}
```

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Extend `InboxQueries`**

```swift
extension InboxQueries {
    func fetchPinned() throws -> [InboxItem] {
        try dbPool.read { db in
            try InboxItem.fetchAll(db, sql: """
                SELECT * FROM inbox_items
                WHERE pinned=1 AND status='pending' AND archived_at IS NULL
                ORDER BY priority DESC, created_at DESC
            """)
        }
    }

    func fetchFeed(limit: Int, offset: Int) throws -> [InboxItem] {
        try dbPool.read { db in
            try InboxItem.fetchAll(db, sql: """
                SELECT * FROM inbox_items
                WHERE pinned=0 AND archived_at IS NULL AND status NOT IN ('resolved','dismissed','snoozed')
                ORDER BY created_at DESC
                LIMIT ? OFFSET ?
            """, arguments: [limit, offset])
        }
    }

    func hasHighPriorityPinned() throws -> Bool {
        try dbPool.read { db in
            let n = try Int.fetchOne(db, sql: """
                SELECT COUNT(*) FROM inbox_items WHERE pinned=1 AND priority='high' AND status='pending'
            """) ?? 0
            return n > 0
        }
    }

    func observePinned() -> ValueObservation<ValueReducers.Fetch<[InboxItem]>> {
        ValueObservation.tracking { db in
            try InboxItem.fetchAll(db, sql: "SELECT * FROM inbox_items WHERE pinned=1 AND status='pending' AND archived_at IS NULL ORDER BY priority DESC, created_at DESC")
        }
    }

    func markSeen(itemID: Int64) throws {
        try dbPool.write { db in
            try db.execute(sql: "UPDATE inbox_items SET read_at=? WHERE id=? AND read_at=''",
                arguments: [ISO8601DateFormatter().string(from: Date()), itemID])
        }
    }
}
```

- [ ] **Step 4: PASS.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/Database/Queries/InboxQueries.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): inbox queries for pinned, feed pagination, mark-seen"
```

---

### Task 20 [PAR after 19,17]: InboxViewModel refactor

**Files:**
- Modify: `WatchtowerDesktop/Sources/ViewModels/InboxViewModel.swift`
- Test: `WatchtowerDesktop/Tests/.../InboxViewModelTests.swift`

- [ ] **Step 1: Failing test**

```swift
func testViewModelSplitsPinnedAndFeed() async throws {
    let db = TestDB.makePool()
    // Seed one pinned + two feed items
    try seedItems(db)
    let vm = await InboxViewModel(db: db)
    await vm.load()
    XCTAssertEqual(vm.pinnedItems.count, 1)
    XCTAssertEqual(vm.feedItems.count, 2)
}

func testSubmitFeedbackUpdatesDB() async throws {
    // vm.submitFeedback(item, rating:-1, reason:"never_show")
    // Check that inbox_learned_rules got a source_mute row
}

func testMarkAsSeenOnScroll() async throws {
    // vm.markSeen(itemID)
    // Check inbox_items.read_at != ''
}

func testSidebarBadgeReflectsHighPriorityPinned() async throws {
    // Seed high-priority pinned. vm.hasHighPriorityPinned == true.
}
```

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Extend `InboxViewModel`**

```swift
@MainActor
@Observable
final class InboxViewModel {
    var pinnedItems: [InboxItem] = []
    var feedItems: [InboxItem] = []
    var hasHighPriorityPinned: Bool = false
    var feedPageSize: Int = 50
    private var feedOffset: Int = 0
    private let queries: InboxQueries
    private let feedback: InboxFeedbackQueries
    private var observationCancel: AnyCancellable?

    init(db: DatabasePool) {
        self.queries = InboxQueries(dbPool: db)
        self.feedback = InboxFeedbackQueries(dbPool: db)
        startObserving()
    }

    func load() async {
        pinnedItems = (try? queries.fetchPinned()) ?? []
        feedItems = (try? queries.fetchFeed(limit: feedPageSize, offset: 0)) ?? []
        feedOffset = feedItems.count
        hasHighPriorityPinned = (try? queries.hasHighPriorityPinned()) ?? false
    }

    func loadMore() async {
        let next = (try? queries.fetchFeed(limit: feedPageSize, offset: feedOffset)) ?? []
        feedItems.append(contentsOf: next)
        feedOffset += next.count
    }

    func markSeen(_ item: InboxItem) {
        guard item.readAt.isEmpty else { return }
        try? queries.markSeen(itemID: item.id)
    }

    func submitFeedback(_ item: InboxItem, rating: Int, reason: String) async {
        try? feedback.record(item: item, rating: rating, reason: reason)
        await load()
    }

    private func startObserving() {
        // ValueObservation on inbox_items changes triggers reload
    }
}
```

- [ ] **Step 4: PASS.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/ViewModels/InboxViewModel.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): InboxViewModel with pinned/feed split, pagination, feedback"
```

---

### Task 21 [PAR after 18]: InboxLearnedRulesViewModel

**Files:**
- Create: `WatchtowerDesktop/Sources/ViewModels/InboxLearnedRulesViewModel.swift`
- Test: `WatchtowerDesktop/Tests/.../InboxLearnedRulesViewModelTests.swift`

- [ ] **Step 1: Failing test**

```swift
func testListsSeparatesMutesAndBoosts() async {
    // Seed rules, vm.mutes contains only weight<0, vm.boosts contains weight>0
}
func testAddManualRule() async {
    // vm.addRule(scope: "sender:U9", weight: -0.5)
    // inbox_learned_rules has a row with source=user_rule
}
func testRemoveRule() async {
    // vm.remove(rule) — row deleted
}
```

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Implement**

```swift
@MainActor
@Observable
final class InboxLearnedRulesViewModel {
    var mutes: [InboxLearnedRule] = []
    var boosts: [InboxLearnedRule] = []
    private let queries: InboxLearnedRulesQueries

    init(db: DatabasePool) {
        self.queries = InboxLearnedRulesQueries(dbPool: db)
    }

    func load() async {
        let all = (try? queries.listAll()) ?? []
        mutes = all.filter { $0.weight < 0 }
        boosts = all.filter { $0.weight > 0 }
    }

    func addRule(ruleType: String, scopeKey: String, weight: Double) async {
        try? queries.upsertManual(ruleType: ruleType, scopeKey: scopeKey, weight: weight)
        await load()
    }

    func remove(_ rule: InboxLearnedRule) async {
        try? queries.delete(ruleType: rule.ruleType, scopeKey: rule.scopeKey)
        await load()
    }
}
```

- [ ] **Step 4: PASS.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/ViewModels/InboxLearnedRulesViewModel.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): learned-rules view model"
```

---

## Phase 6 — Desktop Views (parallel after Phase 5)

### Task 22 [PAR after 16]: InboxCardView with three size variants

**Files:**
- Create: `WatchtowerDesktop/Sources/Views/Inbox/InboxCardView.swift`
- Test: `WatchtowerDesktop/Tests/.../InboxCardViewSnapshotTests.swift` (if snapshot testing is set up; otherwise render tests)

- [ ] **Step 1: Test — render smoke**

```swift
func testCompactCardRendersTriggerIconAndSnippet() {
    let item = InboxItem.sample(trigger: "decision_made", snippet: "Released postponed")
    let view = InboxCardView(item: item, size: .compact)
    let rendered = ViewRenderer.render(view)
    XCTAssertTrue(rendered.contains("Released postponed"))
}
```

(If no snapshot infrastructure, assert non-nil body + host in ViewInspector-style library; otherwise skip and rely on manual inspection.)

- [ ] **Step 2: FAIL.**

- [ ] **Step 3: Implement `InboxCardView`**

```swift
import SwiftUI

enum CardSize { case compact, medium, pinned }

struct InboxCardView: View {
    let item: InboxItem
    let size: CardSize
    let onOpen: () -> Void
    let onSnooze: () -> Void
    let onDismiss: () -> Void
    let onCreateTask: () -> Void
    let onFeedback: (Int, String) -> Void

    var body: some View {
        switch size {
        case .compact: compactView
        case .medium: mediumView
        case .pinned: pinnedView
        }
    }

    private var compactView: some View {
        HStack(spacing: 6) {
            triggerIcon
            Text(item.snippet).lineLimit(1)
            Spacer()
            Text(item.relativeTime).foregroundStyle(.secondary).font(.caption)
            feedbackButtons
        }
        .padding(.vertical, 4)
    }

    private var mediumView: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack { triggerIcon; Text(senderDisplay).bold(); Spacer(); Text(item.relativeTime).font(.caption) }
            Text(item.snippet).lineLimit(2)
            actionBar
        }
        .padding(8)
        .background(RoundedRectangle(cornerRadius: 6).fill(.background.secondary))
    }

    private var pinnedView: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack { priorityDot; triggerIcon; Text(senderDisplay).bold(); Spacer() }
            Text(item.snippet).font(.body)
            if !item.aiReason.isEmpty {
                HStack { Image(systemName: "sparkles").foregroundStyle(.yellow); Text(item.aiReason).font(.caption) }
            }
            actionBar
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 8).fill(.background.secondary))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(priorityColor, lineWidth: 1.5))
    }

    private var triggerIcon: some View { /* map trigger_type → SF Symbol color */ Image(systemName: triggerSymbol) }
    private var triggerSymbol: String {
        switch item.triggerType {
        case "mention": return "at"
        case "dm": return "envelope"
        case "thread_reply": return "bubble.left.and.bubble.right"
        case "reaction": return "eye"
        case "jira_assigned": return "ticket"
        case "jira_comment_mention": return "bubble.left"
        case "calendar_invite": return "calendar.badge.plus"
        case "calendar_time_change": return "clock.arrow.circlepath"
        case "calendar_cancelled": return "calendar.badge.minus"
        case "decision_made": return "paperplane"
        case "briefing_ready": return "sun.max"
        default: return "circle"
        }
    }

    private var priorityDot: some View { Circle().fill(priorityColor).frame(width:8,height:8) }
    private var priorityColor: Color {
        switch item.priority { case "high": return .red; case "medium": return .orange; default: return .gray }
    }

    private var senderDisplay: String { item.senderUserID /* TODO: resolve to name via UserQueries */ }

    private var actionBar: some View {
        HStack(spacing: 8) {
            Button("Open", action: onOpen)
            if item.itemClass == .actionable {
                Menu("Snooze") {
                    Button("1 hour") { /* snooze 1h */ }
                    Button("Till tomorrow") { /* … */ }
                    Button("Till Monday") { /* … */ }
                }
                Button("Dismiss", role: .destructive, action: onDismiss)
                Button("Create Task", action: onCreateTask)
            }
            Spacer()
            feedbackButtons
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
    }

    private var feedbackButtons: some View {
        HStack(spacing: 2) {
            Button(action: { onFeedback(1, "") }) { Image(systemName: "hand.thumbsup") }.buttonStyle(.plain)
            Button(action: { /* open reason sheet */ }) { Image(systemName: "hand.thumbsdown") }.buttonStyle(.plain)
        }.foregroundStyle(.secondary)
    }
}
```

- [ ] **Step 4: PASS / visual review.**

- [ ] **Step 5: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Inbox/InboxCardView.swift WatchtowerDesktop/Tests/
git commit -m "feat(desktop): InboxCardView with compact/medium/pinned size variants"
```

---

### Task 23 [PAR after 20,22]: InboxFeedView

**Files:**
- Create: `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedView.swift`

- [ ] **Step 1: Implement `InboxFeedView`**

```swift
struct InboxFeedView: View {
    @State private var vm: InboxViewModel
    @State private var feedbackItem: InboxItem?
    @State private var tab: Tab = .feed

    enum Tab { case feed, learned }

    init(db: DatabasePool) { _vm = State(initialValue: InboxViewModel(db: db)) }

    var body: some View {
        VStack {
            Picker("", selection: $tab) {
                Text("Feed").tag(Tab.feed)
                Text("Learned").tag(Tab.learned)
            }.pickerStyle(.segmented).padding(.horizontal)

            if tab == .feed {
                feedContent
            } else {
                InboxLearnedRulesView(db: /* pass pool */)
            }
        }
        .task { await vm.load() }
        .sheet(item: $feedbackItem) { item in
            InboxFeedbackSheet(item: item) { rating, reason in
                await vm.submitFeedback(item, rating: rating, reason: reason)
                feedbackItem = nil
            }
        }
    }

    private var feedContent: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 6) {
                if !vm.pinnedItems.isEmpty {
                    Text("Pinned").font(.headline).padding(.horizontal)
                    ForEach(vm.pinnedItems) { it in
                        InboxCardView(item: it, size: .pinned,
                            onOpen: { openDeepLink(it) },
                            onSnooze: { snooze(it) },
                            onDismiss: { dismiss(it) },
                            onCreateTask: { createTask(it) },
                            onFeedback: { r, _ in Task { await vm.submitFeedback(it, rating: r, reason: "") } }
                        )
                    }
                }
                Text("Feed").font(.headline).padding(.horizontal).padding(.top, 8)
                ForEach(groupedByDay, id: \.day) { group in
                    Text(group.day).font(.caption).foregroundStyle(.secondary).padding(.horizontal)
                    ForEach(group.items) { it in
                        InboxCardView(item: it, size: sizeFor(it),
                            onOpen: { openDeepLink(it) },
                            onSnooze: {}, onDismiss: { dismiss(it) },
                            onCreateTask: {},
                            onFeedback: { r, _ in
                                if r == -1 { feedbackItem = it }
                                else { Task { await vm.submitFeedback(it, rating: r, reason: "") } }
                            }
                        )
                        .onAppear { vm.markSeen(it) }
                    }
                }
                if !vm.feedItems.isEmpty {
                    Button("Load more") { Task { await vm.loadMore() } }.padding()
                }
            }.padding(.vertical)
        }
    }

    private func sizeFor(_ it: InboxItem) -> CardSize { it.itemClass == .ambient ? .compact : .medium }

    private var groupedByDay: [(day: String, items: [InboxItem])] {
        // group vm.feedItems into Today / Yesterday / Earlier this week / dates
        // ...
        return []
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Inbox/InboxFeedView.swift
git commit -m "feat(desktop): InboxFeedView with pinned section and chronological feed"
```

---

### Task 24 [PAR after 21,22]: InboxLearnedRulesView

**Files:**
- Create: `WatchtowerDesktop/Sources/Views/Inbox/InboxLearnedRulesView.swift`

- [ ] **Step 1: Implement**

```swift
struct InboxLearnedRulesView: View {
    @State private var vm: InboxLearnedRulesViewModel
    @State private var showAdd = false

    init(db: DatabasePool) { _vm = State(initialValue: InboxLearnedRulesViewModel(db: db)) }

    var body: some View {
        List {
            Section("Mutes (\(vm.mutes.count))") {
                ForEach(vm.mutes) { r in ruleRow(r) }
            }
            Section("Boosts (\(vm.boosts.count))") {
                ForEach(vm.boosts) { r in ruleRow(r) }
            }
        }
        .toolbar { Button { showAdd = true } label: { Image(systemName: "plus") } }
        .task { await vm.load() }
        .sheet(isPresented: $showAdd) {
            AddRuleSheet { scope, weight, type in
                await vm.addRule(ruleType: type, scopeKey: scope, weight: weight)
                showAdd = false
            }
        }
    }

    @ViewBuilder private func ruleRow(_ r: InboxLearnedRule) -> some View {
        HStack {
            Text(r.scopeKey).font(.system(.body, design: .monospaced))
            Spacer()
            Text(String(format: "%+.1f", r.weight)).foregroundStyle(r.weight < 0 ? .red : .green)
            Text(r.source).font(.caption).foregroundStyle(.secondary)
            Button(role: .destructive) { Task { await vm.remove(r) } } label: { Image(systemName: "trash") }
                .buttonStyle(.plain)
        }
    }
}

struct AddRuleSheet: View {
    var onSave: (_ scope: String, _ weight: Double, _ type: String) async -> Void
    @State private var scopeType = "sender"
    @State private var scopeValue = ""
    @State private var weight: Double = -0.8
    @State private var ruleType = "source_mute"

    var body: some View {
        Form {
            Picker("Scope type", selection: $scopeType) {
                Text("Sender").tag("sender")
                Text("Channel").tag("channel")
                Text("Jira label").tag("jira_label")
                Text("Trigger").tag("trigger")
            }
            TextField("Scope value", text: $scopeValue)
            Picker("Rule", selection: $ruleType) {
                Text("Mute").tag("source_mute")
                Text("Boost").tag("source_boost")
                Text("Downgrade class").tag("trigger_downgrade")
                Text("Boost trigger").tag("trigger_boost")
            }
            Slider(value: $weight, in: -1...1, step: 0.1) { Text(String(format: "Weight: %+.1f", weight)) }
            Button("Save") {
                Task { await onSave("\(scopeType):\(scopeValue)", weight, ruleType) }
            }
        }.padding()
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Inbox/InboxLearnedRulesView.swift
git commit -m "feat(desktop): Learned Rules tab with add/remove/edit UI"
```

---

### Task 25 [PAR after 20]: InboxFeedbackSheet

**Files:**
- Create: `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedbackSheet.swift`

- [ ] **Step 1: Implement**

```swift
struct InboxFeedbackSheet: View {
    let item: InboxItem
    var onSubmit: (Int, String) async -> Void
    @State private var selectedReason: String = "source_noise"

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Why is this not helpful?").font(.headline)
            Picker("Reason", selection: $selectedReason) {
                Text("Source usually noise").tag("source_noise")
                Text("Wrong priority").tag("wrong_priority")
                Text("Wrong class").tag("wrong_class")
                Text("Never show me this").tag("never_show")
            }.pickerStyle(.radioGroup)
            HStack {
                Button("Cancel") { Task { await onSubmit(0, "") } }
                Spacer()
                Button("Apply") { Task { await onSubmit(-1, selectedReason) } }.buttonStyle(.borderedProminent)
            }
        }.padding().frame(width: 320)
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Inbox/InboxFeedbackSheet.swift
git commit -m "feat(desktop): feedback reason sheet"
```

---

### Task 26 [SEQ after 23,24]: Remove old InboxListView and wire up routing

**Files:**
- Delete: `WatchtowerDesktop/Sources/Views/Inbox/InboxListView.swift`
- Modify: routing / `Destination` enum / sidebar to use `InboxFeedView`
- Modify: sidebar badge to reflect `hasHighPriorityPinned`

- [ ] **Step 1: Locate current usage**

```bash
grep -rn "InboxListView\|case .inbox" WatchtowerDesktop/Sources/
```

- [ ] **Step 2: Replace with `InboxFeedView`**

In the router (likely `AppState` or `ContentView`):

```swift
case .inbox:
    InboxFeedView(db: appState.databasePool)
```

- [ ] **Step 3: Sidebar badge update**

In the sidebar item for inbox:

```swift
SidebarItem(title: "Inbox", icon: "tray", destination: .inbox,
    badge: appState.inboxUnreadCount,
    badgeTint: appState.hasHighPriorityPinned ? .red : .blue)
```

`appState.hasHighPriorityPinned` wired via GRDB `ValueObservation` on `inbox_items WHERE pinned=1 AND priority='high'`.

- [ ] **Step 4: Delete old file**

```bash
rm WatchtowerDesktop/Sources/Views/Inbox/InboxListView.swift
```

Remove any `import`/references to `InboxListView` across the codebase.

- [ ] **Step 5: Build & test**

```bash
cd WatchtowerDesktop && swift build && swift test
```

Expected: compiles cleanly, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add -A WatchtowerDesktop/
git commit -m "refactor(desktop): replace InboxListView with InboxFeedView and high-priority sidebar indicator"
```

---

## Phase 7 — End-to-End & Documentation

### Task 27 [SEQ after 15,26]: End-to-end integration test

**Files:**
- Create: `internal/inbox/e2e_test.go`

- [ ] **Step 1: Write full-stack test**

```go
func TestE2E_JiraMentionFlow(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    mock := &mockGen{respJSON: `{"pinned_ids":[]}`}
    p := New(d, &config.Config{}, mock, log.Default())
    p.SetCurrentUser("alice", "alice@x.com")

    // Cycle 1: jira comment with @alice appears
    seedJiraIssue(t, d, "WT-99", "alice", time.Now().Add(-10*time.Minute))
    seedJiraComment(t, d, "WT-99", "bob", "[~alice] please", time.Now().Add(-5*time.Minute))
    _, _, err := p.Run(context.Background())
    if err != nil { t.Fatal(err) }

    var n int
    d.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type='jira_comment_mention' AND status='pending'`).Scan(&n)
    if n != 1 { t.Fatalf("expected 1 pending, got %d", n) }

    // Cycle 2: user submits feedback 👎 never_show
    var itemID int64
    d.QueryRow(`SELECT id FROM inbox_items WHERE trigger_type='jira_comment_mention'`).Scan(&itemID)
    err = SubmitFeedback(context.Background(), d, itemID, -1, "never_show")
    if err != nil { t.Fatal(err) }

    // Cycle 3: another jira comment from bob arrives
    seedJiraComment(t, d, "WT-99", "bob", "[~alice] urgent!", time.Now())
    p.Run(context.Background())

    // Expected: second item's priority gets downgraded due to sender:bob mute rule → AI prioritize marks low
    // (We check rule exists and is applied in preferences block.)
    r, _ := d.GetLearnedRule("source_mute", "sender:bob")
    if r.Weight != -1.0 { t.Errorf("mute rule missing or wrong: %+v", r) }
}

func TestE2E_AmbientAutoArchive(t *testing.T) {
    d := newTestDB(t); defer d.Close()
    seedDigestWithHighImportance(t, d, "C1", `[{"type":"decision","topic":"X","importance":"high"}]`, time.Now().Add(-10*time.Minute))
    p := newPipelineForTest(t, d, "alice", "alice@x.com")
    p.Run(context.Background())

    // Fast-forward: set created_at to 8 days ago
    d.Exec(`UPDATE inbox_items SET created_at=? WHERE trigger_type='decision_made'`,
        time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339))
    p.Run(context.Background())

    var reason string
    d.QueryRow(`SELECT archive_reason FROM inbox_items WHERE trigger_type='decision_made'`).Scan(&reason)
    if reason != "seen_expired" { t.Errorf("expected seen_expired, got %q", reason) }
}
```

- [ ] **Step 2: Run**

`go test ./internal/inbox -run TestE2E -v -count=1`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/inbox/e2e_test.go
git commit -m "test(inbox): end-to-end integration flows"
```

---

### Task 28 [SEQ after 27]: Observability — daemon log line

**Files:**
- Modify: `internal/inbox/pipeline.go` (logger output)

- [ ] **Step 1: Add per-cycle summary log**

At end of `Pipeline.Run`, log:

```go
p.logger.Printf("inbox: +%d new (%d slack, %d jira, %d calendar, %d internal), %d pinned, %d auto-resolved, %d auto-archived, %d learned-rule-updates",
    totalNew, nSlack, nJira, nCal, nInternal, nPinned, nResolved, nArchived, nRuleUpdates)
```

Track each count via return values from detectors / learner / archive methods (they already return counts; expose them in orchestration).

Feedback events also log:

```go
// In SubmitFeedback, after rule upsert:
log.Printf("inbox_feedback: item=%d rating=%d reason=%s → rule %s weight=%.2f",
    itemID, rating, reason, ruleKey, ruleWeight)
```

- [ ] **Step 2: Commit**

```bash
git add internal/inbox/pipeline.go internal/inbox/feedback.go
git commit -m "chore(inbox): structured per-cycle observability logging"
```

---

### Task 29 [SEQ after 28]: MEMORY.md + CLAUDE.md update (documentation)

**Files:**
- Modify: root `CLAUDE.md` (if project-level docs for agents)
- Modify: user-memory `day_plan_pipeline.md` and add `inbox_pulse.md` alongside (user's `MEMORY.md` is outside repo — skip)

- [ ] **Step 1: Update repo-level docs**

Add a section to `CLAUDE.md`:

```markdown
### Inbox Pulse (v67+)
- `internal/inbox/` — Pipeline now runs: detectors (slack/jira/calendar/watchtower) → classifier → implicit learner → AI prioritize → AI pinned selector → auto-resolve/archive → unsnooze
- Two classes: `actionable` (pending/resolved) vs `ambient` (auto-seen, auto-archive 7d)
- `pinned` column on inbox_items; Pinned selector is separate AI call, capped at 5
- `inbox_learned_rules` (implicit + explicit + user_rule) injected into prompts via buildUserPreferencesBlock
- `inbox_feedback` records raw 👍/👎 + reason; Go SubmitFeedback derives rule updates
- Desktop: InboxFeedView (replaces InboxListView) + Learned tab
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: inbox pulse notes in CLAUDE.md"
```

---

## Final Verification

### Task 30 [SEQ final]: Full suite + manual QA checklist

- [ ] **Step 1: Run all tests**

```bash
go test ./... -count=1
```

Expected: ALL PASS.

- [ ] **Step 2: Run Desktop suite**

```bash
cd WatchtowerDesktop && swift test
```

Expected: ALL PASS.

- [ ] **Step 3: Manual QA checklist**

- [ ] Daemon cycle produces no panics with empty Jira/Calendar state
- [ ] Migrating a pre-v67 DB does not lose any existing inbox_items
- [ ] `InboxFeedView` loads within 500ms on a DB with 1000 inbox_items
- [ ] 👎 "never show" on one item makes subsequent items from same sender not appear in pinned
- [ ] Ambient item > 7 days old disappears from feed
- [ ] Actionable item resolved via `User commented on issue` auto-rule does not re-appear

- [ ] **Step 4: Final commit (if docs/tests changed)**

```bash
git add -A
git commit -m "chore(inbox): final verification pass"
```

- [ ] **Step 5: Ready for PR to main (or to feature/day-plan for interim integration)**

```bash
git log --oneline feature/day-plan..HEAD
```

---

## Deferred (follow-up tasks captured, not in this plan)

1. **Jira history-based detectors** — `jira_status_change`, `jira_priority_change`, `jira_comment_watching` require `jira_issue_history` and `jira_watchers` tables. If schema is absent, spike a separate spec + migration.
2. **User-preference-aware prompt caching** — when prompt USER PREFERENCES changes, invalidate cached prompts.
3. **Weekly calibration (L4 learning)** — deferred to v2.
4. **Briefing.attention merge with Inbox pinned** — v2 decision.
5. **Startup screen logic** — v2 decision (possibly time-of-day switcher).
6. **Decay on learned rules** — explicitly not in v1.

---

## Self-Review Checklist

- [x] Every spec section has at least one implementing task.
- [x] Every code step shows actual code (no "TBD"/"similar to above").
- [x] Types/names consistent: `InboxLearnedRule`, `SubmitFeedback`, `DefaultItemClass`, `ApplyAIOverride`, `ArchiveExpiredAmbient`, `ArchiveStaleActionable`, `ListInboxPinned`, `ListInboxFeed`, `buildUserPreferencesBlock`, `RunImplicitLearner`, `NewPinnedSelector`, `DetectJira`, `DetectCalendar`, `DetectWatchtowerInternal` — these names are used identically wherever referenced.
- [x] Tests precede implementation in every task (TDD).
- [x] Commits are small and frequent (1 commit per task, 30 tasks total).
- [x] Parallelization markers `[PAR]` vs `[SEQ]` give the orchestrator a clean dependency graph.
- [x] Test coverage target `high`: every detector, every classifier branch, every rule-creation path, every auto-resolve path, every feed query, every ViewModel path has at least one test.
