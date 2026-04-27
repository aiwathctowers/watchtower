# Meeting Recap (AI) — Design

**Status:** approved (brainstorming → spec)
**Date:** 2026-04-27
**Owner:** Vadym
**Related code:** `internal/meeting/`, `cmd/meeting.go`, `WatchtowerDesktop/Sources/Views/Calendar/`

## Goal

After a meeting, the user pastes a raw recap (transcript fragment, hand-written notes, scratchpad) into a sheet and Watchtower's AI returns a structured summary that is persisted per-event and rendered above the existing meeting notes.

This is **post-meeting** complement to the existing **pre-meeting** "Paste and extract → Discussion Topics" flow.

## Non-goals (MVP)

- No automatic trigger after the calendar event ends. MVP is **manual button only**.
- No per-attendee takeaways. AI does not assign action items to specific people.
- No history/versioning of recaps — re-running overwrites.
- No related-tracks / people-cards / Slack-context inclusion. AI sees only event metadata + existing meeting notes + the pasted text.
- No automatic task creation from action items. User goes through the existing `meeting_note → Create task` flow.

## User flow

1. User opens an event in Calendar tab → sees `MeetingNotesView`.
2. In the **Meeting Notes** section header, alongside its title, a button **"Recap from text"** (icon `sparkles`).
3. Click → `GenerateRecapSheet` opens — large `TextEditor` (~580×580) prefilled with the previously stored `source_text` if a recap already exists for this event (otherwise empty).
4. User pastes / edits text → clicks **Generate** (or **Re-generate**).
5. CLI runs `watchtower meeting-prep recap --event-id <id> --text <body> --json`. Spinner in footer.
6. On success: sheet closes, `MeetingNotesView` re-renders with a new **AI Recap** section above the existing **Discussion Topics** section.

## Recap section UI

Renders only when `meeting_recaps` row exists for `event_id`.

```
┌── AI Recap ────────────────────────── [Re-generate] ──┐
│ <summary — 1–2 sentences>                              │
│                                                        │
│ Decisions                                              │
│   • <text>                                             │
│                                                        │
│ Action items                                           │
│   • <text>           [+ to notes]                      │
│                                                        │
│ Open questions                                         │
│   • <text>                                             │
│                                                        │
│ Generated <relative time>                              │
└────────────────────────────────────────────────────────┘
```

- Sections with empty arrays are **hidden**.
- `[Re-generate]` reopens `GenerateRecapSheet` with current `source_text` prefilled.
- `[+ to notes]` (per action item) creates a `meeting_notes(type='note')` row via existing `MeetingNoteQueries.create`. The button is replaced with a static "Added" label after creation; the action item itself stays in the recap (recap is read-only history).

## Architecture overview

| Layer | New | Changed |
|---|---|---|
| DB schema | `meeting_recaps` table (migration v70) | none |
| Go DB | `internal/db/meeting_recaps.go` (Upsert + Get) | `internal/db/db.go` (migration runner) |
| Go pipeline | `internal/meeting/recap.go` (`Pipeline.GenerateRecap`) | none |
| Go prompts | new key `prompts.MeetingRecap` in `internal/prompts/defaults.go` | `internal/prompts/store.go` (key registration) |
| Go CLI | `cmd/meeting.go`: subcommand `meeting-prep recap` | `cmd/meeting.go` flags + `init()` |
| Swift model | `MeetingRecap.swift` | none |
| Swift query | `MeetingRecapQueries.swift` (read-only) | none |
| Swift service | `MeetingRecapService.swift` (CLI bridge) | none |
| Swift view | `GenerateRecapSheet.swift` | `MeetingNotesView.swift` (recap section + button) |

No changes to daemon, sync, briefing pipelines, or other consumers.

## Schema

```sql
-- Migration v70: meeting_recaps  (current latest version is v69)
CREATE TABLE IF NOT EXISTS meeting_recaps (
    event_id    TEXT PRIMARY KEY REFERENCES calendar_events(id) ON DELETE CASCADE,
    source_text TEXT NOT NULL,
    recap_json  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
```

- `event_id` PK → one recap per event. Re-run is `INSERT … ON CONFLICT(event_id) DO UPDATE SET source_text=…, recap_json=…, updated_at=…`.
- `recap_json` is the raw AI output stringified — schema can evolve without `ALTER TABLE`.
- `ON DELETE CASCADE` removes the recap when its calendar event is deleted (Google sync prunes vanished events).
- No extra index — PK suffices.

## AI contract

### Input (system prompt template — `prompts.MeetingRecap`)

```
You produce a structured recap of a meeting based on raw notes the user pasted.

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

%s   (← optional language directive, e.g. "Respond in Russian")

Return ONLY a JSON object matching:
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
- Strip markdown (**bold**, numbered lists, emojis) from output strings.
```

User message: `"Generate a recap from the meeting notes."`

### Output (parsed Go struct)

```go
type RecapResult struct {
    Summary       string   `json:"summary"`
    KeyDecisions  []string `json:"key_decisions"`
    ActionItems   []string `json:"action_items"`
    OpenQuestions []string `json:"open_questions"`
}
```

### Defensive parsing
- `cleanJSON()` strips markdown fences (reuse helper from `internal/meeting/extract.go` if present, otherwise add).
- Trim each string in arrays; drop empty entries.
- On JSON parse failure → return error wrapping first 300 chars of raw output (matches `extract.go` style).

## Go pipeline

`internal/meeting/recap.go`:

```go
func (p *Pipeline) GenerateRecap(
    ctx context.Context,
    eventID, sourceText string,
) (*RecapResult, error)
```

Steps:
1. Validate `sourceText` non-empty (return `RecapResult` with empty arrays + summary "" — let CLI / Desktop choose to surface).
   *Decision:* return error `"--text is required"`-equivalent so CLI errors loudly. UI guards on empty input via the disabled button.
2. `event, _ := p.db.GetCalendarEventByID(eventID)` — non-fatal if missing (use `(no event)` placeholder); we still want to record the recap for an event_id passed by Desktop.
3. `notes, _ := p.db.GetMeetingNotesForEvent(eventID)` — split into `questions` (`type=question`) and `freeformNotes` (`type=note`); render each as `- {text}\n`. If a list is empty, write `(none)`.
4. Format prompt with event metadata + notes lists + raw text + optional `langDirective` (read from `cfg.Digest.Language`).
5. Call `p.generator.Generate(ctx, systemPrompt, "Generate a recap from the meeting notes.", "")`.
6. `cleanJSON` → `json.Unmarshal` → trim → return `*RecapResult`.

Persistence is the **caller's** job — `cmd/meeting.go` calls `GenerateRecap`, then `database.UpsertMeetingRecap(eventID, sourceText, recapJSON)` itself. This keeps `meeting.Pipeline` pure (mockable in tests without DB writes).

## Go DB layer

`internal/db/meeting_recaps.go`:

```go
type MeetingRecap struct {
    EventID    string
    SourceText string
    RecapJSON  string
    CreatedAt  string
    UpdatedAt  string
}

func (db *DB) UpsertMeetingRecap(eventID, sourceText, recapJSON string) error
func (db *DB) GetMeetingRecap(eventID string) (*MeetingRecap, error)  // nil, nil if missing
func (db *DB) GetMeetingNotesForEvent(eventID string) ([]MeetingNote, error)  // if not already present
```

Plus `migrate070MeetingRecaps()` (or whatever next-step convention `db.go` uses — current latest is v69) registered in `internal/db/db.go`'s migration list and `PRAGMA user_version` bumped to 70.

Note: `GetMeetingNotesForEvent` does not yet exist on the Go side (verified — only `internal/meeting/extract.go` references the table, but doesn't fetch). Implementation must add the helper.

## CLI command

In `cmd/meeting.go`, alongside the existing `meetingExtractTopicsCmd`:

```
watchtower meeting-prep recap --event-id <id> --text <body> [--json]
```

- Both flags required; `--text` may come from a heredoc/`--text-file` later (out of scope MVP).
- Always emits JSON (the `--json` flag is accepted for symmetry with sibling commands; default is also JSON).
- Output structure:
  ```json
  {
    "event_id": "...",
    "summary": "...",
    "key_decisions": [...],
    "action_items": [...],
    "open_questions": [...],
    "created_at": "...",
    "updated_at": "..."
  }
  ```
- On error: non-zero exit, stderr message.

Implementation:
1. Load config + DB (mirrors `runMeetingExtractTopics`).
2. `pipe := meeting.New(database, cfg, gen, nil)`.
3. `result, err := pipe.GenerateRecap(ctx, eventID, text)`.
4. `recapJSON, _ := json.Marshal(result)` → `database.UpsertMeetingRecap(eventID, text, string(recapJSON))`.
5. Re-fetch via `GetMeetingRecap` to get authoritative timestamps → emit envelope JSON.

## Tests (Go)

- `internal/meeting/recap_test.go`: mock `digest.Generator`. Cases:
  - Empty source text → error.
  - Happy path: AI returns clean JSON → fields parsed and trimmed.
  - AI returns markdown-fenced JSON → still parses.
  - AI returns malformed JSON → error wrapping first 300 chars.
  - Empty meeting notes / missing event → still works (`(none)` / `(no event)`).
  - Prompt includes event title and raw text.
- `cmd/meeting_test.go` (extend): integration test invokes CLI with mock generator, asserts JSON envelope and DB row written.
- `internal/db/meeting_recaps_test.go`: table tests for upsert idempotency (insert then re-insert with same `event_id`), get-missing returns `nil, nil`, cascade delete via FK.

## Swift surface

### Model (`Sources/Models/MeetingRecap.swift`)
```swift
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

### Queries (`Sources/Database/Queries/MeetingRecapQueries.swift`)
```swift
enum MeetingRecapQueries {
    static func fetch(_ db: Database, eventID: String) throws -> MeetingRecap? {
        try MeetingRecap.filter(Column("event_id") == eventID).fetchOne(db)
    }
}
```
No write helpers — Desktop never writes to this table; only the Go CLI does (single source of truth).

### Service (`Sources/Services/MeetingRecapService.swift`)
Mirrors `MeetingTopicsExtractService`:
```swift
struct MeetingRecapService {
    let runner: CLIRunnerProtocol
    func generate(eventID: String, text: String) async throws -> Void
    // Returns Void: caller refetches from DB to get authoritative content + timestamps.
}
```
- Args: `["meeting-prep", "recap", "--event-id", eventID, "--text", text, "--json"]`.
- Throws on non-zero exit (delegated to `CLIRunnerError`).
- On success, the row is already upserted by the CLI.

### Sheet (`Sources/Views/Calendar/GenerateRecapSheet.swift`)
- `eventID: String`, `prefilledText: String` (passed by parent based on existing recap), `onCompleted: () -> Void`.
- State: `text`, `isGenerating`, `errorMessage`.
- Body: header → big `TextEditor` (font `.callout`, lineLimit unlimited) → footer with `[Generate]` / `[Re-generate]` (label depends on whether `prefilledText` is non-empty) and ProgressView.
- On submit:
  ```swift
  guard let runner = ProcessCLIRunner.makeDefault() else { ... }
  let svc = MeetingRecapService(runner: runner)
  try await svc.generate(eventID: eventID, text: text)
  onCompleted()  // parent refetches and re-renders
  dismiss()
  ```
- Frame: `.frame(width: 580, height: 580)`.

### MeetingNotesView changes
1. New `@State var recap: MeetingRecap?`.
2. `loadNotes()` also calls `MeetingRecapQueries.fetch(...)` and assigns.
3. New `recapSection: some View` rendered above `questionsSection` only when `recap?.parsed != nil`.
4. New button "Recap from text" in `notesSection` header (matches placement of "Paste and extract" in `questionsSection`).
5. New `@State var showRecapSheet = false` + `.sheet(isPresented: $showRecapSheet) { GenerateRecapSheet(...) }`.
6. `[+ to notes]` per action item: calls `MeetingNoteQueries.create(... type: .note, text: actionItem, sortOrder: nextSortOrder)` then `loadNotes()`. Persistent local state `addedItems: Set<Int>` (index in array) so the button switches to a static "Added" indicator within the same view session.

## Tests (Swift)

- `Tests/MeetingRecapServiceTests.swift`:
  - Happy path (FakeCLIRunner returns success exit code) → no throw.
  - Non-zero exit → throws `CLIRunnerError.nonZeroExit`.
  - Binary-not-found path covered by reusing existing `FakeCLIRunner` fixture.
- `Tests/MeetingRecapTests.swift`:
  - `parsed` returns non-nil for valid JSON.
  - `parsed` returns nil for malformed JSON (no crash).
  - `Content` decodes snake_case keys correctly.

UI tests for `GenerateRecapSheet` are out of scope (existing project doesn't UI-test sheets).

## Edge cases

| Case | Behaviour |
|---|---|
| User opens sheet, hits Generate with empty text | Button is disabled (existing pattern). |
| Calendar event deleted while sheet is open | `meeting-prep recap` still upserts (FK violation rejected by SQLite → CLI returns error → sheet shows error). Desktop refetches and recap is gone via cascade. |
| AI returns empty `summary` and all empty arrays | Recap section in UI is rendered but only "Generated X ago" footer is visible. Acceptable (still signals AI ran). |
| AI provider unavailable / CLI crashes | `CLIRunnerError.nonZeroExit` → sheet shows stderr; user can retry. |
| User re-runs Generate within seconds | Upsert overwrites; UI re-renders. No race condition (single user, single sheet). |
| Migration runs on existing DB | `CREATE TABLE IF NOT EXISTS` is safe; no data loss. |
| Two events with same ID across calendars | Out of scope — `calendar_events.id` is already PK and de-duped by sync. |

## Open items deferred to v2 (NOT in MVP)

- Automatic trigger after `event.end_time` via daemon.
- Per-attendee takeaways.
- Recap history / versioning.
- Inclusion of related tracks / people cards / Slack context.
- Inline "Create task" button on action items (current path: "+ to notes" → existing "Create task" in notes section).
- Editing the recap content directly (currently regenerate-only).
- Recap surfaced in daily briefing or rollups.

## Acceptance checklist

- [ ] `meeting_recaps` table created via migration v70; idempotent on re-run.
- [ ] `watchtower meeting-prep recap --event-id … --text …` prints JSON envelope and writes a row.
- [ ] Re-running the CLI for the same event_id overwrites the row (PK conflict path).
- [ ] Go pipeline mocked-generator tests cover happy path + 3 error modes.
- [ ] Desktop sheet appears via "Recap from text" button in `MeetingNotesView`.
- [ ] On Generate success, recap section renders summary + non-empty subsections.
- [ ] `[+ to notes]` creates a `meeting_notes(type=note)` row.
- [ ] `[Re-generate]` opens sheet with `source_text` prefilled.
- [ ] No TCC prompts attributable to this feature (subprocess respects existing `cmd.Dir = os.TempDir()`).
- [ ] No regressions in existing Discussion-Topics extract flow.
