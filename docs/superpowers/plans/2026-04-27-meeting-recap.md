# Meeting Recap (AI) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an AI-powered post-meeting recap flow: user pastes raw notes/transcript into a sheet attached to a calendar event, AI returns a structured summary (summary + decisions + action_items + open_questions), persisted in a new `meeting_recaps` table and rendered above the existing meeting notes.

**Architecture:** Go pipeline `Pipeline.GenerateRecap` (mirrors existing `Pipeline.ExtractDiscussionTopics`) → exposed via new `meeting-prep recap` CLI subcommand → Swift `MeetingRecapService` shells out via `ProcessCLIRunner` → `GenerateRecapSheet` UI in `MeetingNotesView`. CLI is the sole writer to `meeting_recaps`; Desktop reads only.

**Tech Stack:** Go 1.25 (cobra, modernc.org/sqlite), Swift 5.10 (SwiftUI, GRDB.swift), macOS 14+.

**Spec:** `docs/superpowers/specs/2026-04-27-meeting-recap-design.md`.

---

## Out of Scope

- Automatic recap generation after event end (manual trigger only).
- Per-attendee takeaways or owner-assignment for action items.
- Recap history / versioning (re-run overwrites).
- Inclusion of related tracks, people cards, or Slack context.
- Direct "Create task" button on action items (we route through the existing `meeting_note → Create task` UI).

## File Structure

**Go — create:**
- `internal/db/meeting_recaps.go` — `MeetingRecap` struct + `UpsertMeetingRecap`, `GetMeetingRecap`, `GetMeetingNotesForEvent`.
- `internal/db/meeting_recaps_test.go` — table tests for the three helpers.
- `internal/meeting/recap.go` — `RecapResult` + `Pipeline.GenerateRecap` + prompt loader fallback.
- `internal/meeting/recap_test.go` — mock-generator tests for happy path + 4 error modes.

**Go — modify:**
- `internal/db/schema.sql` — append `meeting_recaps` table + index near other meeting_* tables.
- `internal/db/db.go` — bump fresh-install `PRAGMA user_version` to 70 and add `version < 70` migration block.
- `internal/prompts/store.go` — register `MeetingRecap = "meeting.recap"` constant.
- `internal/prompts/defaults.go` — add default template under `MeetingRecap` key.
- `cmd/meeting.go` — new `meetingRecapCmd` subcommand of `meetingPrepCmd`.
- `cmd/meeting_test.go` — integration test for CLI envelope + DB row written.

**Swift — create:**
- `WatchtowerDesktop/Sources/Models/MeetingRecap.swift` — model + nested `Content` decoder for `recap_json`.
- `WatchtowerDesktop/Sources/Database/Queries/MeetingRecapQueries.swift` — read-only `fetch(_:eventID:)`.
- `WatchtowerDesktop/Sources/Services/MeetingRecapService.swift` — `CLIRunnerProtocol` wrapper.
- `WatchtowerDesktop/Sources/Views/Calendar/GenerateRecapSheet.swift` — paste-textarea sheet.
- `WatchtowerDesktop/Tests/MeetingRecapTests.swift` — model parsing.
- `WatchtowerDesktop/Tests/MeetingRecapServiceTests.swift` — service happy/error paths.

**Swift — modify:**
- `WatchtowerDesktop/Sources/Views/Calendar/MeetingNotesView.swift` — add `recap` state, `recapSection`, "Recap from text" button in `notesSection` header, `+ to notes` action item handler, sheet binding.

---

## Task 1: Schema + migration v70

**Files:**
- Modify: `internal/db/schema.sql:881-895` (insert meeting_recaps next to meeting_notes)
- Modify: `internal/db/db.go:86` (fresh-install PRAGMA), append new `version < 70` block before the function returns
- Test: `internal/db/db_test.go` (or whichever existing test exercises migration; if absent, create `internal/db/migrations_test.go`)

- [ ] **Step 1.1: Append schema.sql section**

In `internal/db/schema.sql`, after the `meeting_notes` index (line ~894), add:

```sql
-- Meeting recaps (AI-generated post-meeting summary; one row per event)
CREATE TABLE IF NOT EXISTS meeting_recaps (
    event_id    TEXT PRIMARY KEY REFERENCES calendar_events(id) ON DELETE CASCADE,
    source_text TEXT NOT NULL,
    recap_json  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
```

- [ ] **Step 1.2: Bump fresh-install user_version to 70**

In `internal/db/db.go`, line 86, change:

```go
if _, err := tx.Exec("PRAGMA user_version = 69"); err != nil {
```

to:

```go
if _, err := tx.Exec("PRAGMA user_version = 70"); err != nil {
```

- [ ] **Step 1.3: Add migration block for upgrade path (existing DBs at v69 → v70)**

In `internal/db/db.go`, scroll to the end of the migration cascade (just before `return nil`), append a new branch following the existing pattern. The exact line will depend on where the v69 block ends — search for `PRAGMA user_version = 69` (the upgrade variant, not the fresh-install one) and add immediately after the `version = 69` reassignment:

```go
if version < 70 {
    tx, err := db.Begin()
    if err != nil {
        return fmt.Errorf("beginning migration v70 tx: %w", err)
    }
    defer tx.Rollback()
    if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS meeting_recaps (
        event_id    TEXT PRIMARY KEY REFERENCES calendar_events(id) ON DELETE CASCADE,
        source_text TEXT NOT NULL,
        recap_json  TEXT NOT NULL,
        created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    )`); err != nil {
        return fmt.Errorf("creating meeting_recaps: %w", err)
    }
    if _, err := tx.Exec("PRAGMA user_version = 70"); err != nil {
        return fmt.Errorf("setting schema version: %w", err)
    }
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("committing migration v70: %w", err)
    }
    version = 70
}
```

- [ ] **Step 1.4: Write a migration test**

Open `internal/db/db_test.go` (or create `internal/db/migrations_test.go` if no migration test exists). Add:

```go
func TestMigrationV70CreatesMeetingRecaps(t *testing.T) {
    tmp := t.TempDir() + "/test.db"
    database, err := Open(tmp)
    if err != nil {
        t.Fatal(err)
    }
    defer database.Close()

    var version int
    if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
        t.Fatalf("reading user_version: %v", err)
    }
    if version < 70 {
        t.Errorf("expected user_version >= 70, got %d", version)
    }

    // Table exists
    var name string
    err = database.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='meeting_recaps'`).Scan(&name)
    if err != nil {
        t.Fatalf("meeting_recaps table missing: %v", err)
    }
    if name != "meeting_recaps" {
        t.Errorf("expected table 'meeting_recaps', got %q", name)
    }
}
```

- [ ] **Step 1.5: Run test, verify pass**

```bash
go test ./internal/db/ -run TestMigrationV70CreatesMeetingRecaps -v
```

Expected: PASS.

- [ ] **Step 1.6: Run full DB test suite to confirm no regressions**

```bash
go test ./internal/db/ -v
```

Expected: PASS, no other tests broken.

- [ ] **Step 1.7: Commit**

```bash
git add internal/db/schema.sql internal/db/db.go internal/db/db_test.go
git commit -m "feat(db): meeting_recaps table + migration v70"
```

---

## Task 2: Go DB layer for meeting_recaps

**Files:**
- Create: `internal/db/meeting_recaps.go`
- Create: `internal/db/meeting_recaps_test.go`

- [ ] **Step 2.1: Write failing test for Upsert + Get**

Create `internal/db/meeting_recaps_test.go`:

```go
package db

import (
    "testing"
)

func TestMeetingRecapUpsertAndGet(t *testing.T) {
    database := openTestDB(t)
    defer database.Close()

    // Need a calendar event to satisfy FK
    if _, err := database.Exec(`INSERT INTO calendar_events (id, calendar_id, title, start_time, end_time)
        VALUES ('evt-1', 'cal-1', 'Test event', '2026-04-27T10:00:00Z', '2026-04-27T11:00:00Z')`); err != nil {
        t.Fatalf("seeding event: %v", err)
    }

    if err := database.UpsertMeetingRecap("evt-1", "raw notes here", `{"summary":"x"}`); err != nil {
        t.Fatalf("first upsert: %v", err)
    }

    got, err := database.GetMeetingRecap("evt-1")
    if err != nil {
        t.Fatalf("get after upsert: %v", err)
    }
    if got == nil {
        t.Fatal("expected recap, got nil")
    }
    if got.SourceText != "raw notes here" {
        t.Errorf("source_text = %q, want %q", got.SourceText, "raw notes here")
    }
    if got.RecapJSON != `{"summary":"x"}` {
        t.Errorf("recap_json = %q, want %q", got.RecapJSON, `{"summary":"x"}`)
    }
    if got.CreatedAt == "" || got.UpdatedAt == "" {
        t.Error("timestamps must be set")
    }

    // Idempotent re-upsert overrides
    if err := database.UpsertMeetingRecap("evt-1", "edited", `{"summary":"y"}`); err != nil {
        t.Fatalf("re-upsert: %v", err)
    }
    got2, _ := database.GetMeetingRecap("evt-1")
    if got2.SourceText != "edited" || got2.RecapJSON != `{"summary":"y"}` {
        t.Errorf("re-upsert failed: %+v", got2)
    }
}

func TestMeetingRecapGetMissing(t *testing.T) {
    database := openTestDB(t)
    defer database.Close()

    got, err := database.GetMeetingRecap("nope")
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    if got != nil {
        t.Errorf("expected nil for missing event, got %+v", got)
    }
}

func TestMeetingRecapCascadeDelete(t *testing.T) {
    database := openTestDB(t)
    defer database.Close()

    if _, err := database.Exec(`INSERT INTO calendar_events (id, calendar_id, title, start_time, end_time)
        VALUES ('evt-2', 'cal-1', 't', '2026-04-27T10:00:00Z', '2026-04-27T11:00:00Z')`); err != nil {
        t.Fatal(err)
    }
    if err := database.UpsertMeetingRecap("evt-2", "x", "{}"); err != nil {
        t.Fatal(err)
    }
    if _, err := database.Exec(`DELETE FROM calendar_events WHERE id='evt-2'`); err != nil {
        t.Fatal(err)
    }

    got, _ := database.GetMeetingRecap("evt-2")
    if got != nil {
        t.Errorf("expected recap to be cascade-deleted, got %+v", got)
    }
}
```

If `openTestDB` doesn't exist in the package, search for how other DB tests get a connection (e.g., `internal/db/db_test.go`); reuse the same helper or define one:

```go
// openTestDB returns an in-memory DB with all migrations applied.
func openTestDB(t *testing.T) *DB {
    t.Helper()
    tmp := t.TempDir() + "/test.db"
    d, err := Open(tmp)
    if err != nil {
        t.Fatal(err)
    }
    return d
}
```

- [ ] **Step 2.2: Run, verify fail**

```bash
go test ./internal/db/ -run TestMeetingRecap -v
```

Expected: FAIL — `UpsertMeetingRecap`/`GetMeetingRecap` undefined.

- [ ] **Step 2.3: Create the helpers**

Create `internal/db/meeting_recaps.go`:

```go
package db

import (
    "database/sql"
    "fmt"
)

// MeetingRecap is an AI-generated post-meeting summary attached to a calendar
// event. One row per event_id; re-running the recap CLI overwrites the row.
type MeetingRecap struct {
    EventID    string
    SourceText string
    RecapJSON  string
    CreatedAt  string
    UpdatedAt  string
}

// UpsertMeetingRecap inserts a new recap or updates an existing row for the
// given event_id. updated_at is bumped to "now" on every call.
func (db *DB) UpsertMeetingRecap(eventID, sourceText, recapJSON string) error {
    _, err := db.Exec(`
        INSERT INTO meeting_recaps (event_id, source_text, recap_json)
        VALUES (?, ?, ?)
        ON CONFLICT(event_id) DO UPDATE SET
            source_text = excluded.source_text,
            recap_json  = excluded.recap_json,
            updated_at  = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
    `, eventID, sourceText, recapJSON)
    if err != nil {
        return fmt.Errorf("upserting meeting recap for %s: %w", eventID, err)
    }
    return nil
}

// GetMeetingRecap returns the recap for the given event, or (nil, nil) if none.
func (db *DB) GetMeetingRecap(eventID string) (*MeetingRecap, error) {
    var r MeetingRecap
    err := db.QueryRow(`
        SELECT event_id, source_text, recap_json, created_at, updated_at
        FROM meeting_recaps WHERE event_id = ?
    `, eventID).Scan(&r.EventID, &r.SourceText, &r.RecapJSON, &r.CreatedAt, &r.UpdatedAt)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("loading meeting recap for %s: %w", eventID, err)
    }
    return &r, nil
}
```

- [ ] **Step 2.4: Run tests, verify pass**

```bash
go test ./internal/db/ -run TestMeetingRecap -v
```

Expected: PASS for `TestMeetingRecapUpsertAndGet`, `TestMeetingRecapGetMissing`, `TestMeetingRecapCascadeDelete`.

- [ ] **Step 2.5: Add `GetMeetingNotesForEvent` test**

Append to `internal/db/meeting_recaps_test.go`:

```go
func TestGetMeetingNotesForEvent(t *testing.T) {
    database := openTestDB(t)
    defer database.Close()

    if _, err := database.Exec(`INSERT INTO calendar_events (id, calendar_id, title, start_time, end_time)
        VALUES ('evt-3', 'cal-1', 't', '2026-04-27T10:00:00Z', '2026-04-27T11:00:00Z')`); err != nil {
        t.Fatal(err)
    }
    inserts := []struct{ typ, text string; ord int }{
        {"question", "topic A", 0},
        {"note", "freeform B", 0},
        {"question", "topic C", 1},
    }
    for _, ins := range inserts {
        if _, err := database.Exec(`INSERT INTO meeting_notes (event_id, type, text, sort_order)
            VALUES ('evt-3', ?, ?, ?)`, ins.typ, ins.text, ins.ord); err != nil {
            t.Fatal(err)
        }
    }

    notes, err := database.GetMeetingNotesForEvent("evt-3")
    if err != nil {
        t.Fatalf("get notes: %v", err)
    }
    if len(notes) != 3 {
        t.Fatalf("expected 3 rows, got %d", len(notes))
    }
    // Ordered by type then sort_order — exact contract: see implementation.
    // We assert all texts present.
    seen := map[string]bool{}
    for _, n := range notes {
        seen[n.Text] = true
    }
    for _, want := range []string{"topic A", "topic C", "freeform B"} {
        if !seen[want] {
            t.Errorf("missing note %q", want)
        }
    }
}
```

- [ ] **Step 2.6: Run, verify fail**

```bash
go test ./internal/db/ -run TestGetMeetingNotesForEvent -v
```

Expected: FAIL — function undefined.

- [ ] **Step 2.7: Implement `GetMeetingNotesForEvent`**

Append to `internal/db/meeting_recaps.go` (or add a new file `internal/db/meeting_notes.go` if it makes more sense in your conventions — both are acceptable):

```go
// MeetingNote is a single row in meeting_notes (questions or freeform notes
// attached to a calendar event).
type MeetingNote struct {
    ID        int64
    EventID   string
    Type      string // 'question' | 'note'
    Text      string
    IsChecked bool
    SortOrder int
    TaskID    sql.NullInt64
    CreatedAt string
    UpdatedAt string
}

// GetMeetingNotesForEvent returns all meeting_notes for the event, ordered
// first by type (questions before notes) then by sort_order.
func (db *DB) GetMeetingNotesForEvent(eventID string) ([]MeetingNote, error) {
    rows, err := db.Query(`
        SELECT id, event_id, type, text, is_checked, sort_order, task_id, created_at, updated_at
        FROM meeting_notes WHERE event_id = ?
        ORDER BY type DESC, sort_order ASC
    `, eventID)
    if err != nil {
        return nil, fmt.Errorf("loading meeting notes for %s: %w", eventID, err)
    }
    defer rows.Close()

    var out []MeetingNote
    for rows.Next() {
        var n MeetingNote
        var checked int
        if err := rows.Scan(&n.ID, &n.EventID, &n.Type, &n.Text, &checked, &n.SortOrder, &n.TaskID, &n.CreatedAt, &n.UpdatedAt); err != nil {
            return nil, fmt.Errorf("scanning meeting note: %w", err)
        }
        n.IsChecked = checked != 0
        out = append(out, n)
    }
    return out, rows.Err()
}
```

(Note: `ORDER BY type DESC` puts `'question'` (q) before `'note'` (n) lexicographically — both DESC and ASC happen to put `question` first because `q > n` in ASCII. We use DESC to make it explicit. Verify in the test.)

- [ ] **Step 2.8: Run, verify pass**

```bash
go test ./internal/db/ -run TestGetMeetingNotesForEvent -v
```

Expected: PASS.

- [ ] **Step 2.9: Run all DB tests**

```bash
go test ./internal/db/ -v
```

Expected: PASS, no regressions.

- [ ] **Step 2.10: Commit**

```bash
git add internal/db/meeting_recaps.go internal/db/meeting_recaps_test.go
git commit -m "feat(db): UpsertMeetingRecap, GetMeetingRecap, GetMeetingNotesForEvent"
```

---

## Task 3: Prompts — register `meeting.recap` key

**Files:**
- Modify: `internal/prompts/store.go:34` (add constant near `MeetingExtractTopics`)
- Modify: `internal/prompts/defaults.go` (add map entry)

- [ ] **Step 3.1: Add constant in `prompts/store.go`**

Find the line:
```go
MeetingExtractTopics = "meeting.extract_topics"
```

Add immediately after:
```go
MeetingRecap         = "meeting.recap"
```

- [ ] **Step 3.2: Add default template in `prompts/defaults.go`**

Open `internal/prompts/defaults.go`. Look at how `MeetingExtractTopics` entry is defined (likely a map literal `Defaults = map[string]string{...}`). Add a new entry following the same style. The template:

```go
MeetingRecap: `You produce a structured recap of a meeting based on raw notes the user pasted.

=== EVENT ===
Title: %s
Time:  %s — %s
Attendees: %s
Description: %s

=== EXISTING DISCUSSION TOPICS (pre-meeting) ===
%s

=== EXISTING FREEFORM NOTES ===
%s

=== USER'S RAW RECAP TEXT ===
%s

%s

Return ONLY a JSON object (no markdown fences, no commentary) matching:

{
  "summary": "string (1-2 sentences, what the meeting was about and outcome)",
  "key_decisions": ["string", ...],
  "action_items": ["string (imperative; if a person is named in the text, include them)", ...],
  "open_questions": ["string", ...]
}

Rules:
- Be concise; merge near-duplicates.
- Decisions: things explicitly resolved.
- Action items: only items with implied owner or commitment ("X will do Y" / "we'll send Y").
- Open questions: things flagged as unresolved or "to discuss later".
- Use empty arrays if a category has nothing.
- Strip markdown (**bold**, numbered lists, emojis) from output strings.`,
```

The 9 `%s` placeholders are filled by `fmt.Sprintf` in `recap.go`: title, start_time, end_time, attendees, description, existing_topics_block, existing_notes_block, raw_recap_text, lang_directive.

- [ ] **Step 3.3: Verify build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3.4: Commit**

```bash
git add internal/prompts/store.go internal/prompts/defaults.go
git commit -m "feat(prompts): register meeting.recap default template"
```

---

## Task 4: Recap pipeline (`internal/meeting/recap.go`)

**Files:**
- Create: `internal/meeting/recap.go`
- Create: `internal/meeting/recap_test.go`
- Read for reference: `internal/meeting/extract.go`

- [ ] **Step 4.1: Write the failing test scaffold**

Create `internal/meeting/recap_test.go`. Look at `internal/meeting/extract_test.go` to mimic the mock generator pattern. Skeleton:

```go
package meeting

import (
    "context"
    "strings"
    "testing"
)

func TestGenerateRecap_EmptyTextReturnsError(t *testing.T) {
    p := newTestPipelineForRecap(t, nil) // returns Pipeline with mock generator
    _, err := p.GenerateRecap(context.Background(), "evt-1", "")
    if err == nil {
        t.Fatal("expected error for empty source text, got nil")
    }
}

func TestGenerateRecap_HappyPath(t *testing.T) {
    aiResponse := `{
      "summary": "Talked about the launch.",
      "key_decisions": ["Ship Friday"],
      "action_items": ["Vadym to draft launch post"],
      "open_questions": ["Pricing tier?"]
    }`
    p := newTestPipelineForRecap(t, mockGen{response: aiResponse})

    res, err := p.GenerateRecap(context.Background(), "evt-1", "raw notes")
    if err != nil {
        t.Fatalf("unexpected: %v", err)
    }
    if res.Summary != "Talked about the launch." {
        t.Errorf("summary = %q", res.Summary)
    }
    if len(res.KeyDecisions) != 1 || res.KeyDecisions[0] != "Ship Friday" {
        t.Errorf("decisions = %v", res.KeyDecisions)
    }
    if len(res.ActionItems) != 1 {
        t.Errorf("action_items = %v", res.ActionItems)
    }
}

func TestGenerateRecap_StripsMarkdownFences(t *testing.T) {
    aiResponse := "```json\n" + `{"summary":"x","key_decisions":[],"action_items":[],"open_questions":[]}` + "\n```"
    p := newTestPipelineForRecap(t, mockGen{response: aiResponse})

    res, err := p.GenerateRecap(context.Background(), "evt-1", "raw")
    if err != nil {
        t.Fatalf("unexpected: %v", err)
    }
    if res.Summary != "x" {
        t.Errorf("summary = %q", res.Summary)
    }
}

func TestGenerateRecap_MalformedJSONErrorsWithSnippet(t *testing.T) {
    aiResponse := "not json at all, full of garbage and other text"
    p := newTestPipelineForRecap(t, mockGen{response: aiResponse})

    _, err := p.GenerateRecap(context.Background(), "evt-1", "raw")
    if err == nil {
        t.Fatal("expected parse error")
    }
    if !strings.Contains(err.Error(), "garbage") {
        t.Errorf("error should include raw snippet, got %v", err)
    }
}

func TestGenerateRecap_TrimsAndDropsEmptyArrayEntries(t *testing.T) {
    aiResponse := `{
      "summary": "  hello  ",
      "key_decisions": ["", "  decision  ", " "],
      "action_items": [],
      "open_questions": []
    }`
    p := newTestPipelineForRecap(t, mockGen{response: aiResponse})

    res, err := p.GenerateRecap(context.Background(), "evt-1", "raw")
    if err != nil {
        t.Fatalf("unexpected: %v", err)
    }
    if res.Summary != "hello" {
        t.Errorf("summary not trimmed: %q", res.Summary)
    }
    if len(res.KeyDecisions) != 1 || res.KeyDecisions[0] != "decision" {
        t.Errorf("expected 1 cleaned decision, got %v", res.KeyDecisions)
    }
}
```

For `newTestPipelineForRecap` and `mockGen`, copy the pattern from `extract_test.go`. If `extract_test.go` already has `newTestPipeline`/`mockGen`, **reuse** them — don't duplicate. If they're unexported and live in the same package, you can just call them directly.

If `extract_test.go` does not have such helpers, add minimum scaffold:

```go
type mockGen struct {
    response string
    err      error
}

func (m mockGen) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
    return m.response, nil, "", m.err
}

// Match the actual digest.Generator interface — adjust if different.
```

(Run `go doc watchtower/internal/digest Generator` to confirm the exact interface signature.)

- [ ] **Step 4.2: Run, verify fail**

```bash
go test ./internal/meeting/ -run TestGenerateRecap -v
```

Expected: FAIL — `GenerateRecap` undefined.

- [ ] **Step 4.3: Implement `GenerateRecap`**

Create `internal/meeting/recap.go`:

```go
package meeting

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "watchtower/internal/db"
    "watchtower/internal/prompts"
)

// RecapResult is the AI output for a meeting recap.
type RecapResult struct {
    Summary       string   `json:"summary"`
    KeyDecisions  []string `json:"key_decisions"`
    ActionItems   []string `json:"action_items"`
    OpenQuestions []string `json:"open_questions"`
}

// GenerateRecap takes the raw text the user pasted and returns a structured
// recap. The pipeline does NOT persist — the CLI caller writes the result
// (this keeps the pipeline mockable without DB writes).
func (p *Pipeline) GenerateRecap(
    ctx context.Context,
    eventID, sourceText string,
) (*RecapResult, error) {
    trimmed := strings.TrimSpace(sourceText)
    if trimmed == "" {
        return nil, fmt.Errorf("source text is required")
    }

    // Event metadata (non-fatal if missing — CLI may pass an event ID for an
    // event that hasn't synced yet; we still produce a recap with placeholders).
    title, startTime, endTime, attendees, description := "(no event)", "", "", "", ""
    if p.db != nil {
        if ev, err := p.db.GetCalendarEventByID(eventID); err == nil && ev != nil {
            title = ev.Title
            startTime = ev.StartTime
            endTime = ev.EndTime
            attendees = ev.Attendees // assumed JSON or comma-list — pass through verbatim
            description = ev.Description
        }
    }

    // Existing meeting_notes (pre-meeting topics + freeform notes) for context.
    var topicsBlock, notesBlock string
    topicsBlock, notesBlock = "(none)", "(none)"
    if p.db != nil {
        if notes, err := p.db.GetMeetingNotesForEvent(eventID); err == nil {
            var qs, ns []string
            for _, n := range notes {
                line := "- " + strings.TrimSpace(n.Text)
                if n.Type == "question" {
                    qs = append(qs, line)
                } else if n.Type == "note" {
                    ns = append(ns, line)
                }
            }
            if len(qs) > 0 {
                topicsBlock = strings.Join(qs, "\n")
            }
            if len(ns) > 0 {
                notesBlock = strings.Join(ns, "\n")
            }
        }
    }

    langDirective := ""
    if p.cfg != nil && p.cfg.Digest.Language != "" {
        langDirective = fmt.Sprintf("Respond in %s.", p.cfg.Digest.Language)
    }

    tmpl := p.loadRecapPrompt()
    systemPrompt := fmt.Sprintf(
        tmpl,
        title, startTime, endTime, attendees, description,
        topicsBlock, notesBlock, trimmed, langDirective,
    )
    userMessage := "Generate a recap from the meeting notes."

    aiResponse, _, _, err := p.generator.Generate(ctx, systemPrompt, userMessage, "")
    if err != nil {
        return nil, fmt.Errorf("AI generation: %w", err)
    }

    cleaned := cleanJSON(aiResponse) // reuse helper from extract.go (same package)
    var raw RecapResult
    if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
        return nil, fmt.Errorf("parsing AI response: %w (raw: %.300s)", err, aiResponse)
    }

    raw.Summary = strings.TrimSpace(raw.Summary)
    raw.KeyDecisions = trimNonEmpty(raw.KeyDecisions)
    raw.ActionItems = trimNonEmpty(raw.ActionItems)
    raw.OpenQuestions = trimNonEmpty(raw.OpenQuestions)

    return &raw, nil
}

func trimNonEmpty(in []string) []string {
    out := make([]string, 0, len(in))
    for _, s := range in {
        s = strings.TrimSpace(s)
        if s != "" {
            out = append(out, s)
        }
    }
    return out
}

func (p *Pipeline) loadRecapPrompt() string {
    if p.promptStore != nil {
        if tmpl, _, err := p.promptStore.Get(prompts.MeetingRecap); err == nil && tmpl != "" {
            return tmpl
        }
    }
    if tmpl, ok := prompts.Defaults[prompts.MeetingRecap]; ok && tmpl != "" {
        return tmpl
    }
    return defaultRecapPromptFallback
}

const defaultRecapPromptFallback = `Recap the meeting. Event: %s (%s—%s, %s, %s). Topics: %s. Notes: %s. Raw: %s. %s
Return JSON: {"summary":"","key_decisions":[],"action_items":[],"open_questions":[]}`

var _ = db.MeetingRecap{} // keep import if MeetingRecap struct ever needed here
```

Notes for the implementer:
- `cleanJSON` is already defined in `internal/meeting/extract.go` — same package, just call it.
- `p.db`, `p.cfg`, `p.generator`, `p.promptStore` are existing fields on `Pipeline` (see `extract.go` and `meeting.New` constructor). Don't change the constructor.
- `db.GetCalendarEventByID` already exists (used in `cmd/meeting.go:207`). If it returns a different field name than `Attendees`/`Description`, adjust accordingly.

- [ ] **Step 4.4: Run tests, verify pass**

```bash
go test ./internal/meeting/ -run TestGenerateRecap -v
```

Expected: PASS for all 5 sub-tests.

- [ ] **Step 4.5: Run full meeting package tests**

```bash
go test ./internal/meeting/ -v
```

Expected: no regressions in `TestExtractDiscussionTopics*` etc.

- [ ] **Step 4.6: Commit**

```bash
git add internal/meeting/recap.go internal/meeting/recap_test.go
git commit -m "feat(meeting): GenerateRecap pipeline with structured output"
```

---

## Task 5: CLI command — `meeting-prep recap`

**Files:**
- Modify: `cmd/meeting.go` (add subcommand + flags + runner)
- Modify: `cmd/meeting_test.go` (add CLI test)

- [ ] **Step 5.1: Write the failing CLI test**

In `cmd/meeting_test.go`, add (mirroring how `runMeetingExtractTopics` is tested):

```go
func TestMeetingRecapCmdHasRequiredFlags(t *testing.T) {
    if meetingRecapCmd.Flags().Lookup("event-id") == nil {
        t.Error("meeting-prep recap should have --event-id flag")
    }
    if meetingRecapCmd.Flags().Lookup("text") == nil {
        t.Error("meeting-prep recap should have --text flag")
    }
}

func TestMeetingRecapCmdRequiresEventID(t *testing.T) {
    // Reset flags to default and run with only --text
    meetingRecapFlagText = "some text"
    meetingRecapFlagEventID = ""
    err := runMeetingRecap(meetingRecapCmd, nil)
    if err == nil {
        t.Fatal("expected error when --event-id is missing")
    }
}

func TestMeetingRecapCmdRequiresText(t *testing.T) {
    meetingRecapFlagText = ""
    meetingRecapFlagEventID = "evt-1"
    err := runMeetingRecap(meetingRecapCmd, nil)
    if err == nil {
        t.Fatal("expected error when --text is missing")
    }
}
```

(End-to-end test with mock generator + real DB writing is harder because `cliGenerator(cfg)` is hard to inject. Skip it at this layer — the pipeline test covers logic; the CLI test covers wiring.)

- [ ] **Step 5.2: Run, verify fail**

```bash
go test ./cmd/ -run TestMeetingRecap -v
```

Expected: FAIL — symbols undefined.

- [ ] **Step 5.3: Add the subcommand**

In `cmd/meeting.go`, add new vars near the existing `meetingExtractTopicsFlag*` block:

```go
var (
    meetingRecapFlagEventID string
    meetingRecapFlagText    string
    meetingRecapFlagJSON    bool
)
```

Add the cobra command:

```go
var meetingRecapCmd = &cobra.Command{
    Use:   "recap",
    Short: "Generate AI-structured recap from raw meeting notes",
    Long:  "Reads pasted text plus event metadata and existing meeting_notes, produces a JSON summary (summary, decisions, action items, open questions), and persists it in meeting_recaps. Re-running overwrites.",
    RunE:  runMeetingRecap,
}
```

Register it in `init()` next to the existing `extract-topics` registration:

```go
meetingPrepCmd.AddCommand(meetingRecapCmd)
meetingRecapCmd.Flags().StringVar(&meetingRecapFlagEventID, "event-id", "", "calendar event id (required)")
meetingRecapCmd.Flags().StringVar(&meetingRecapFlagText, "text", "", "raw recap text (required)")
meetingRecapCmd.Flags().BoolVar(&meetingRecapFlagJSON, "json", true, "output as JSON (default true)")
```

Add the runner (anywhere after `runMeetingExtractTopics`):

```go
func runMeetingRecap(cmd *cobra.Command, _ []string) error {
    if meetingRecapFlagEventID == "" {
        return fmt.Errorf("--event-id is required")
    }
    if meetingRecapFlagText == "" {
        return fmt.Errorf("--text is required")
    }

    cfg, err := config.Load(flagConfig)
    if err != nil {
        return fmt.Errorf("loading config: %w", err)
    }
    if flagWorkspace != "" {
        cfg.ActiveWorkspace = flagWorkspace
    }
    if err := cfg.ValidateWorkspace(); err != nil {
        return err
    }

    database, err := db.Open(cfg.DBPath())
    if err != nil {
        return fmt.Errorf("opening database: %w", err)
    }
    defer database.Close()

    gen := cliGenerator(cfg)
    pipe := meeting.New(database, cfg, gen, nil)

    result, err := pipe.GenerateRecap(cmd.Context(), meetingRecapFlagEventID, meetingRecapFlagText)
    if err != nil {
        return err
    }

    recapBytes, err := json.Marshal(result)
    if err != nil {
        return fmt.Errorf("marshalling recap: %w", err)
    }
    if err := database.UpsertMeetingRecap(meetingRecapFlagEventID, meetingRecapFlagText, string(recapBytes)); err != nil {
        return fmt.Errorf("persisting recap: %w", err)
    }

    // Refetch for authoritative timestamps in the envelope.
    saved, err := database.GetMeetingRecap(meetingRecapFlagEventID)
    if err != nil || saved == nil {
        return fmt.Errorf("re-loading saved recap: %w", err)
    }

    envelope := map[string]any{
        "event_id":       saved.EventID,
        "summary":        result.Summary,
        "key_decisions":  result.KeyDecisions,
        "action_items":   result.ActionItems,
        "open_questions": result.OpenQuestions,
        "created_at":     saved.CreatedAt,
        "updated_at":     saved.UpdatedAt,
    }
    enc := json.NewEncoder(cmd.OutOrStdout())
    enc.SetIndent("", "  ")
    return enc.Encode(envelope)
}
```

- [ ] **Step 5.4: Run, verify pass**

```bash
go test ./cmd/ -run TestMeetingRecap -v
```

Expected: PASS.

- [ ] **Step 5.5: Build the binary, smoke-test**

```bash
go build -o /tmp/wt-recap-test .
/tmp/wt-recap-test meeting-prep recap --event-id missing --text "test" 2>&1 | head -20
```

Expected: error about missing event or AI provider (depending on local config). The point is the command is wired and reaches the runner.

- [ ] **Step 5.6: Commit**

```bash
git add cmd/meeting.go cmd/meeting_test.go
git commit -m "feat(cmd): meeting-prep recap subcommand"
```

---

## Task 6: Swift model + queries

**Files:**
- Create: `WatchtowerDesktop/Sources/Models/MeetingRecap.swift`
- Create: `WatchtowerDesktop/Sources/Database/Queries/MeetingRecapQueries.swift`
- Create: `WatchtowerDesktop/Tests/MeetingRecapTests.swift`

- [ ] **Step 6.1: Write failing test for model parsing**

Create `WatchtowerDesktop/Tests/MeetingRecapTests.swift`:

```swift
import XCTest
@testable import WatchtowerDesktop

final class MeetingRecapTests: XCTestCase {
    func test_parsedReturnsContentForValidJSON() throws {
        let recap = MeetingRecap(
            eventID: "evt-1",
            sourceText: "raw",
            recapJSON: """
            {
              "summary": "Talked about Q3.",
              "key_decisions": ["ship friday"],
              "action_items": ["draft post"],
              "open_questions": ["pricing?"]
            }
            """,
            createdAt: "2026-04-27T10:00:00Z",
            updatedAt: "2026-04-27T10:00:00Z"
        )
        let parsed = try XCTUnwrap(recap.parsed)
        XCTAssertEqual(parsed.summary, "Talked about Q3.")
        XCTAssertEqual(parsed.keyDecisions, ["ship friday"])
        XCTAssertEqual(parsed.actionItems, ["draft post"])
        XCTAssertEqual(parsed.openQuestions, ["pricing?"])
    }

    func test_parsedReturnsNilForMalformedJSON() {
        let recap = MeetingRecap(
            eventID: "x", sourceText: "", recapJSON: "not json",
            createdAt: "", updatedAt: ""
        )
        XCTAssertNil(recap.parsed)
    }

    func test_parsedDecodesSnakeCaseKeys() throws {
        let json = """
        {"summary":"s","key_decisions":[],"action_items":[],"open_questions":[]}
        """
        let recap = MeetingRecap(eventID: "x", sourceText: "", recapJSON: json,
                                 createdAt: "", updatedAt: "")
        let parsed = try XCTUnwrap(recap.parsed)
        XCTAssertEqual(parsed.summary, "s")
        XCTAssertTrue(parsed.keyDecisions.isEmpty)
    }
}
```

- [ ] **Step 6.2: Run, verify fail**

```bash
cd WatchtowerDesktop && swift test --filter MeetingRecapTests
```

Expected: FAIL — `MeetingRecap` undefined.

- [ ] **Step 6.3: Create the model**

Create `WatchtowerDesktop/Sources/Models/MeetingRecap.swift`:

```swift
import Foundation
import GRDB

struct MeetingRecap: Codable, FetchableRecord, PersistableRecord {
    static let databaseTableName = "meeting_recaps"

    let eventID: String
    let sourceText: String
    let recapJSON: String
    let createdAt: String
    let updatedAt: String

    struct Content: Decodable, Equatable {
        let summary: String
        let keyDecisions: [String]
        let actionItems: [String]
        let openQuestions: [String]

        enum CodingKeys: String, CodingKey {
            case summary
            case keyDecisions = "key_decisions"
            case actionItems = "action_items"
            case openQuestions = "open_questions"
        }
    }

    var parsed: Content? {
        guard let data = recapJSON.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(Content.self, from: data)
    }

    enum CodingKeys: String, CodingKey {
        case eventID = "event_id"
        case sourceText = "source_text"
        case recapJSON = "recap_json"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}
```

- [ ] **Step 6.4: Create queries**

Create `WatchtowerDesktop/Sources/Database/Queries/MeetingRecapQueries.swift`:

```swift
import Foundation
import GRDB

enum MeetingRecapQueries {
    static func fetch(_ db: Database, eventID: String) throws -> MeetingRecap? {
        try MeetingRecap
            .filter(Column("event_id") == eventID)
            .fetchOne(db)
    }
}
```

- [ ] **Step 6.5: Run tests, verify pass**

```bash
cd WatchtowerDesktop && swift test --filter MeetingRecapTests
```

Expected: PASS for all 3 model tests.

- [ ] **Step 6.6: Commit**

```bash
git add WatchtowerDesktop/Sources/Models/MeetingRecap.swift \
        WatchtowerDesktop/Sources/Database/Queries/MeetingRecapQueries.swift \
        WatchtowerDesktop/Tests/MeetingRecapTests.swift
git commit -m "feat(desktop): MeetingRecap model + read-only queries"
```

---

## Task 7: Swift service — CLI bridge

**Files:**
- Create: `WatchtowerDesktop/Sources/Services/MeetingRecapService.swift`
- Create: `WatchtowerDesktop/Tests/MeetingRecapServiceTests.swift`

- [ ] **Step 7.1: Write failing test**

Create `WatchtowerDesktop/Tests/MeetingRecapServiceTests.swift`. Look at `MeetingTopicsExtractServiceTests.swift` as the reference — reuse `FakeCLIRunner` from `Tests/Helpers/FakeCLIRunner.swift`:

```swift
import XCTest
@testable import WatchtowerDesktop

final class MeetingRecapServiceTests: XCTestCase {
    func test_generateInvokesCLIWithRightArgs() async throws {
        let fake = FakeCLIRunner(payload: """
        {"event_id":"evt-1","summary":"x","key_decisions":[],"action_items":[],"open_questions":[],"created_at":"","updated_at":""}
        """.data(using: .utf8)!)
        let svc = MeetingRecapService(runner: fake)

        try await svc.generate(eventID: "evt-1", text: "raw")

        let captured = try XCTUnwrap(fake.lastArgs)
        XCTAssertEqual(captured.first, "meeting-prep")
        XCTAssertEqual(captured[safe: 1], "recap")
        XCTAssertTrue(captured.contains("--event-id"))
        XCTAssertTrue(captured.contains("evt-1"))
        XCTAssertTrue(captured.contains("--text"))
        XCTAssertTrue(captured.contains("raw"))
    }

    func test_generatePropagatesNonZeroExit() async {
        let fake = FakeCLIRunner.failing(error: .nonZeroExit(code: 1, stderr: "boom"))
        let svc = MeetingRecapService(runner: fake)
        do {
            try await svc.generate(eventID: "evt-1", text: "raw")
            XCTFail("expected throw")
        } catch CLIRunnerError.nonZeroExit(let code, _) {
            XCTAssertEqual(code, 1)
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }
}
```

If `FakeCLIRunner` doesn't have a `failing` static or `payload` initializer, look at how `MeetingTopicsExtractServiceTests.swift` constructs it and follow that pattern; copy/adjust as needed. Don't reinvent — reuse what's there. The `[safe:]` collection extension is widely used in the test target; if absent, replace with `captured.indices.contains(1) ? captured[1] : nil`.

- [ ] **Step 7.2: Run, verify fail**

```bash
cd WatchtowerDesktop && swift test --filter MeetingRecapServiceTests
```

Expected: FAIL — `MeetingRecapService` undefined.

- [ ] **Step 7.3: Implement the service**

Create `WatchtowerDesktop/Sources/Services/MeetingRecapService.swift`:

```swift
import Foundation

/// Bridges the Desktop app to `watchtower meeting-prep recap --json`.
/// CLI is the sole writer to `meeting_recaps`; on success the row is upserted
/// before returning. Caller refetches via `MeetingRecapQueries.fetch` to get
/// authoritative content + timestamps.
struct MeetingRecapService {
    let runner: CLIRunnerProtocol

    func generate(eventID: String, text: String) async throws {
        let args = [
            "meeting-prep", "recap",
            "--event-id", eventID,
            "--text", text,
            "--json"
        ]
        // Discard stdout — caller refetches from DB.
        _ = try await runner.run(args: args)
    }
}
```

- [ ] **Step 7.4: Run tests, verify pass**

```bash
cd WatchtowerDesktop && swift test --filter MeetingRecapServiceTests
```

Expected: PASS.

- [ ] **Step 7.5: Commit**

```bash
git add WatchtowerDesktop/Sources/Services/MeetingRecapService.swift \
        WatchtowerDesktop/Tests/MeetingRecapServiceTests.swift
git commit -m "feat(desktop): MeetingRecapService CLI bridge"
```

---

## Task 8: `GenerateRecapSheet` — paste-and-process UI

**Files:**
- Create: `WatchtowerDesktop/Sources/Views/Calendar/GenerateRecapSheet.swift`
- Read for reference: `WatchtowerDesktop/Sources/Views/Calendar/ExtractMeetingTopicsSheet.swift`

(SwiftUI views are not unit-tested in this repo by convention — manual verification via `make app-dev` is enough. Still, the view itself must compile cleanly and run without runtime crashes.)

- [ ] **Step 8.1: Implement the sheet**

Create `WatchtowerDesktop/Sources/Views/Calendar/GenerateRecapSheet.swift`:

```swift
import SwiftUI

/// Paste-and-process sheet for AI-generated meeting recap. The user pastes
/// raw notes (transcript fragment, hand-written summary, scratchpad), Watchtower's
/// AI returns a structured summary which is persisted by the CLI directly.
struct GenerateRecapSheet: View {
    @Environment(\.dismiss) private var dismiss

    let eventID: String
    var prefilledText: String = ""
    var onCompleted: () -> Void = {}

    @State private var text: String = ""
    @State private var isGenerating = false
    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            editor
            Divider()
            footer
        }
        .frame(width: 580, height: 580)
        .onAppear {
            if text.isEmpty {
                text = prefilledText
            }
        }
    }

    private var header: some View {
        HStack {
            Label("AI Recap", systemImage: "sparkles")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    private var editor: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Paste a recap, transcript fragment, or rough notes. The AI will produce a structured summary (decisions, action items, open questions).")
                .font(.callout)
                .foregroundStyle(.secondary)

            TextEditor(text: $text)
                .font(.callout)
                .padding(6)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.secondary.opacity(0.2), lineWidth: 1)
                )

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .padding()
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button {
                Task { await runGenerate() }
            } label: {
                if isGenerating {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.small)
                        Text("Generating…")
                    }
                } else {
                    Label(prefilledText.isEmpty ? "Generate" : "Re-generate",
                          systemImage: "sparkles")
                }
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
            .disabled(isGenerating || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }

    private func runGenerate() async {
        guard let runner = ProcessCLIRunner.makeDefault() else {
            errorMessage = "watchtower CLI not found in PATH"
            return
        }
        isGenerating = true
        errorMessage = nil
        defer { isGenerating = false }

        let svc = MeetingRecapService(runner: runner)
        do {
            try await svc.generate(eventID: eventID, text: text)
            onCompleted()
            dismiss()
        } catch {
            errorMessage = "Generation failed: \(error.localizedDescription)"
        }
    }
}
```

- [ ] **Step 8.2: Verify build**

```bash
cd WatchtowerDesktop && swift build
```

Expected: clean build.

- [ ] **Step 8.3: Run all tests**

```bash
cd WatchtowerDesktop && swift test
```

Expected: PASS, no regressions.

- [ ] **Step 8.4: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Calendar/GenerateRecapSheet.swift
git commit -m "feat(desktop): GenerateRecapSheet for paste-and-process recap"
```

---

## Task 9: Wire `MeetingNotesView` — recap section + button + action items

**Files:**
- Modify: `WatchtowerDesktop/Sources/Views/Calendar/MeetingNotesView.swift`

- [ ] **Step 9.1: Add `recap` state and load it in `loadNotes`**

Open `MeetingNotesView.swift`. Find the `@State private var notes: [MeetingNote] = []` block and add:

```swift
@State private var recap: MeetingRecap?
@State private var showRecapSheet = false
@State private var addedActionItems: Set<Int> = []  // indices already added to notes
```

In `loadNotes()`, after the existing `MeetingNoteQueries.fetchForEvent` call, add a parallel fetch for recap:

```swift
recap = try? db.dbPool.read { dbConn in
    try MeetingRecapQueries.fetch(dbConn, eventID: eventID)
}
```

Reset `addedActionItems = []` in `loadNotes()` so re-runs reset the row state.

- [ ] **Step 9.2: Add recap section view**

Add a new computed property and place it above `questionsSection`:

```swift
@ViewBuilder
private var recapSection: some View {
    if let recap, let content = recap.parsed {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "sparkles")
                    .foregroundStyle(.purple)
                Text("AI Recap")
                    .font(.headline)
                Spacer()
                Button {
                    showRecapSheet = true
                } label: {
                    Label("Re-generate", systemImage: "arrow.triangle.2.circlepath")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }

            if !content.summary.isEmpty {
                Text(content.summary)
                    .font(.callout)
                    .textSelection(.enabled)
            }

            if !content.keyDecisions.isEmpty {
                recapSubsection(title: "Decisions", items: content.keyDecisions)
            }

            if !content.actionItems.isEmpty {
                actionItemsSubsection(items: content.actionItems)
            }

            if !content.openQuestions.isEmpty {
                recapSubsection(title: "Open questions", items: content.openQuestions)
            }

            Text("Generated \(formattedTime(recap.updatedAt))")
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(10)
        .background(Color.purple.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
    }
}

private func recapSubsection(title: String, items: [String]) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text(title)
            .font(.subheadline)
            .fontWeight(.medium)
        ForEach(Array(items.enumerated()), id: \.offset) { _, text in
            HStack(alignment: .top, spacing: 6) {
                Text("•").foregroundStyle(.secondary)
                Text(text)
                    .font(.callout)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

private func actionItemsSubsection(items: [String]) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text("Action items")
            .font(.subheadline)
            .fontWeight(.medium)
        ForEach(Array(items.enumerated()), id: \.offset) { idx, text in
            HStack(alignment: .top, spacing: 6) {
                Text("•").foregroundStyle(.secondary)
                Text(text)
                    .font(.callout)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)

                if addedActionItems.contains(idx) {
                    Label("Added", systemImage: "checkmark")
                        .labelStyle(.titleAndIcon)
                        .font(.caption2)
                        .foregroundStyle(.green)
                } else {
                    Button {
                        addActionItemToNotes(idx: idx, text: text)
                    } label: {
                        Label("+ to notes", systemImage: "plus")
                            .font(.caption2)
                    }
                    .buttonStyle(.borderless)
                }
            }
        }
    }
}

private func formattedTime(_ iso: String) -> String {
    let f = ISO8601DateFormatter()
    guard let date = f.date(from: iso) else { return iso }
    let rel = RelativeDateTimeFormatter()
    rel.unitsStyle = .abbreviated
    return rel.localizedString(for: date, relativeTo: Date())
}

private func addActionItemToNotes(idx: Int, text: String) {
    guard let db = appState.databaseManager else { return }
    do {
        let nextSort = (freeformNotes.last?.sortOrder ?? -1) + 1
        _ = try db.dbPool.write { dbConn in
            try MeetingNoteQueries.create(
                dbConn,
                eventID: eventID,
                type: .note,
                text: text,
                sortOrder: nextSort
            )
        }
        addedActionItems.insert(idx)
        loadNotes()
    } catch {
        errorMessage = error.localizedDescription
    }
}
```

- [ ] **Step 9.3: Add `recapSection` to the body**

In `body`'s top-level `VStack`, insert `recapSection` BEFORE `questionsSection`:

```swift
var body: some View {
    VStack(alignment: .leading, spacing: 20) {
        if let error = errorMessage {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
                .padding(.horizontal)
        }
        recapSection            // NEW
        questionsSection
        notesSection
    }
    .onAppear { loadNotes() }
    .sheet(isPresented: $showExtractSheet) {
        ExtractMeetingTopicsSheet(
            eventID: eventID,
            existingTopicSortOrderCeiling: questions.last?.sortOrder ?? -1,
            onCreated: { loadNotes() }
        )
    }
    .sheet(isPresented: $showRecapSheet) {       // NEW
        GenerateRecapSheet(
            eventID: eventID,
            prefilledText: recap?.sourceText ?? "",
            onCompleted: { loadNotes() }
        )
    }
}
```

- [ ] **Step 9.4: Add "Recap from text" button to `notesSection` header**

Find the `notesSection` `var` (the `Meeting Notes` header). Modify its `HStack`:

```swift
HStack(spacing: 6) {
    Image(systemName: "note.text")
        .foregroundStyle(.blue)
    Text("Meeting Notes")
        .font(.headline)
    Spacer()
    Button {
        showRecapSheet = true
    } label: {
        Label("Recap from text", systemImage: "sparkles")
            .font(.caption)
    }
    .buttonStyle(.bordered)
    .controlSize(.small)
}
.padding(.top, 4)
```

- [ ] **Step 9.5: Build & swift-test the desktop project**

```bash
cd WatchtowerDesktop && swift build && swift test
```

Expected: clean build, no test regressions.

- [ ] **Step 9.6: Manual smoke test in dev mode**

```bash
cd /Users/user/PhpstormProjects/watchtower
make app-dev
open build/Watchtower.app
```

Manually verify:
1. Calendar tab → click on an event with `meeting_notes` → `MeetingNotesView` shows.
2. Click **"Recap from text"** in Meeting Notes header. Sheet opens with empty textarea.
3. Paste a few sentences ("Alice and Bob met. Decided to ship Friday. Bob will write the post. Pricing is still open.") → click **Generate**.
4. Sheet closes, **AI Recap** section appears at top with summary + decisions + action items + open questions.
5. Click **"+ to notes"** on an action item → "Added" label appears, item shows up in **Meeting Notes** section below. Existing "Create task" button on the new note works.
6. Click **"Re-generate"** in the recap header → sheet opens with the previous source_text prefilled. Edit text, click Re-generate → recap is overwritten.
7. Close & reopen the event → recap is still there (persisted).

If anything fails: capture the specific failure and fix in the relevant task before committing.

- [ ] **Step 9.7: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Calendar/MeetingNotesView.swift
git commit -m "feat(desktop): wire AI Recap section + sheet in MeetingNotesView"
```

---

## Task 10: Final integration checks

- [ ] **Step 10.1: Run full Go test suite**

```bash
cd /Users/user/PhpstormProjects/watchtower
go test ./...
```

Expected: PASS, no regressions in any package.

- [ ] **Step 10.2: Run full Swift test suite**

```bash
cd WatchtowerDesktop && swift test
```

Expected: PASS.

- [ ] **Step 10.3: Lint Go code**

```bash
cd /Users/user/PhpstormProjects/watchtower
golangci-lint run ./...
```

Expected: no new findings in `internal/db/meeting_recaps.go`, `internal/meeting/recap.go`, `cmd/meeting.go`.

- [ ] **Step 10.4: Verify acceptance checklist from spec**

Walk through each item in `docs/superpowers/specs/2026-04-27-meeting-recap-design.md` "Acceptance checklist". Tick each, fix any gap.

- [ ] **Step 10.5: (No commit needed if no changes; if lint fixes — commit "chore: linter fixes for meeting recap")**

---

## Self-Review Notes

- Spec section coverage: schema (Task 1), Go DB (Task 2), prompts (Task 3), pipeline (Task 4), CLI (Task 5), Swift model+queries (Task 6), Swift service (Task 7), Swift sheet (Task 8), MeetingNotesView wiring (Task 9), final QA (Task 10). All design sections mapped.
- Type consistency: `MeetingRecap`/`Content` field names identical between Swift model (Task 6) and what `recap.go` produces in JSON (Task 4). `RecapResult` Go struct uses snake_case JSON tags matching Swift `CodingKeys`. CLI envelope (Task 5) emits same keys as `Content` plus `event_id`/timestamps which Swift refetches separately, so no conflict.
- Migration version: locked to **70** everywhere (Task 1.1, 1.2, 1.3, 1.4 all consistent).
- `cleanJSON` is reused from `extract.go` (same package), no duplication.
- All test code is concrete — no "add tests for the above" placeholders.
- `+ to notes` flow uses existing `MeetingNoteQueries.create`, no new write helper needed.
- Manual smoke test (Task 9.6) covers all spec acceptance criteria except automated tests.
