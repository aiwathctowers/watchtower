# TRACKS-06 — Track State History

**Date:** 2026-04-28
**Status:** Design approved, ready for implementation plan
**Owner:** Vadym
**Closes gap on:** `docs/inventory/tracks.md` TRACKS-06 (Partial → Enforced) — Aspirational sub-contract "track state history"
**DB version bump:** 72 → 73

## Problem

Inventory contract `TRACKS-06` says re-extraction never narrows history. The channel/digest-origins half is enforced (`UpdateTrackFromExtraction` merges `channel_ids` and `related_digest_ids`). The track-state half is not: every call to `internal/db/tracks.go::UpdateTrackFromExtraction` overwrites `text`, `context`, `category`, `ownership`, `ball_on`, `owner_user_id`, `requester_name`, `requester_user_id`, `blocking`, `decision_summary`, `decision_options`, `sub_items`, `participants`, `tags`, `priority`, `due_date`, `fingerprint` in place. The prior values disappear.

Result:

- A user who read "Review API PR" yesterday and sees "Review API PR + reconcile with auth team" today has no way to confirm the AI rewrote it; they can't tell whether they misremember or the system silently reframed the work.
- A bad re-extraction (LLM hallucination, prompt regression) silently destroys the prior text — there's no undo, no audit, no diff.
- The next extraction prompt only sees the latest snapshot — updates feel amnesic; the LLM cannot compose on its prior reading because that reading is gone.

A `track_history` table existed pre-v43. It was dropped during the chains→tracks v3 refactor (`internal/db/db.go:1905`) along with chains, and never reinstated. This spec re-introduces it under a new name (`track_states`) shaped for the current track schema.

## Goals

- Persist a snapshot of every narrative-field state of a track, immutably, with provenance (`source='extraction'|'manual'`, `model`, `prompt_version`, `created_at`).
- Snapshot is captured **before** the new state is written — a failed write doesn't poison history; a successful write always has its predecessor in `track_states`.
- Skip noise snapshots: if no narrative field actually changed (e.g. an extraction that re-confirms the same `text`/`context`/`priority`/`ownership`/`category`), do not write a row.
- Bound storage: cap at 30 most recent states per track. Older snapshots are dropped at insert time. Empirical: typical track gets 5–15 updates over its life, 30 is comfortably above the long tail.
- Cover manual edits the same way: `UpdateTrackPriority`, `UpdateTrackOwnership`, `UpdateTrackSubItems`, and the `t.ID > 0` branch of `UpsertTrack` all snapshot with `source='manual'` so the UI history surface is uniform across causes of change.
- Expose history in `TrackDetailView` as a read-only timeline. Read-only is enough for v1 — revert is a separate, later decision.
- Promote `TRACKS-06` from Partial to Enforced.

## Non-Goals

- **No revert action in v1.** The history surface is read-only. Revert can be added later if usage shows demand; specifying it now risks scope creep on a foundation that needs to land first.
- **No prior-state injection into the extraction prompt in v1.** The TRACKS-06 inventory entry mentions this as "optionally"; making the LLM compose on prior state requires careful prompt-engineering and evaluation that is out of scope here. The schema makes this trivially addable later (one new SELECT in `pipeline.go::generateBatchTracks`).
- **No diff-store optimisation.** Each row is a full snapshot of narrative fields, not a diff. A track with 30 states ≈ 30 × ~2 KB = ~60 KB; for 10 000 tracks that's 600 MB worst-case, but the realistic per-user track count is in the low thousands and most rows are well under 2 KB. Diff-store complexity is not justified.
- **No retention by date.** Hard cap by count (30 most recent). Date-based retention adds a clock dependency and a background job; count is simpler and bounded.
- **No history for `read_at`/`has_updates`/`dismissed_at` transitions.** Those are lifecycle flags, not narrative content. They are implicitly visible: dismiss removes the track from the active list, and read-state is per-cycle. Mixing them into `track_states` would conflate "what does this track *say*" with "have I interacted with it" — different surfaces.
- **No history of `channel_ids`/`related_digest_ids`/`source_refs` arrays.** Those already accumulate in the live track row (TRACKS-06 channel/digest part). Snapshotting them would duplicate.

## Concept

```
                      track edit happens
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
UpdateTrackFromExtraction  UpdateTrackPriority    UpsertTrack(ID>0)
   (source='extraction')   UpdateTrackOwnership   (source='manual')
                           UpdateTrackSubItems
                              │
                              ▼
                  ┌───────────────────────────┐
                  │ snapshotPriorTrackState() │
                  │ 1. Load current track row │
                  │ 2. Compare narrative      │
                  │    fields to incoming     │
                  │ 3. If unchanged → return  │
                  │ 4. Else INSERT into       │
                  │    track_states           │
                  │ 5. Trim to 30 newest      │
                  └───────────────────────────┘
                              │
                              ▼
                       [original UPDATE
                        writes the new
                        state to tracks]
```

Snapshot before update. If write fails, history is untouched and no orphan row is left (the snapshot represents the *predecessor* of a state that was about to be written; pairing with a future write is implicit by `created_at` ordering, not by foreign-key linkage to a specific update).

## Schema — `track_states` (migration v73)

```sql
CREATE TABLE IF NOT EXISTS track_states (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id           INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,

    -- narrative snapshot (copied from tracks at the moment-just-before the update)
    text               TEXT NOT NULL,
    context            TEXT NOT NULL DEFAULT '',
    category           TEXT NOT NULL,
    ownership          TEXT NOT NULL,
    ball_on            TEXT NOT NULL DEFAULT '',
    owner_user_id      TEXT NOT NULL DEFAULT '',
    requester_name     TEXT NOT NULL DEFAULT '',
    requester_user_id  TEXT NOT NULL DEFAULT '',
    blocking           TEXT NOT NULL DEFAULT '',
    decision_summary   TEXT NOT NULL DEFAULT '',
    decision_options   TEXT NOT NULL DEFAULT '[]',
    sub_items          TEXT NOT NULL DEFAULT '[]',
    participants       TEXT NOT NULL DEFAULT '[]',
    tags               TEXT NOT NULL DEFAULT '[]',
    priority           TEXT NOT NULL,
    due_date           REAL,

    -- provenance
    source             TEXT NOT NULL CHECK(source IN ('extraction','manual')),
    model              TEXT NOT NULL DEFAULT '',
    prompt_version     INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_track_states_track
    ON track_states(track_id, created_at DESC);
```

Migration body is a single `CREATE TABLE … IF NOT EXISTS` plus the index plus `PRAGMA user_version = 73`. No backfill — pre-existing tracks start with empty history. The table grows organically from the next daemon cycle and the next user edit forward; this is a deliberate "history starts now" choice (backfilling fake states from a single live row would be misleading).

## Behavior — `internal/db/tracks.go`

New helper:

```go
// snapshotTrackState writes a snapshot of the current track row into
// track_states if any narrative field differs from the incoming Track.
// Returns nil if the snapshot is skipped (no narrative change) or written.
// Errors do NOT abort the caller's update — they are logged and the
// caller proceeds with its UPDATE.
func (db *DB) snapshotTrackState(id int, incoming Track, source string) error
```

Narrative-change detection: snapshot is written iff at least one of the following differs between the loaded row and `incoming`:

```
text, context, category, ownership, ball_on, owner_user_id,
requester_name, requester_user_id, blocking, decision_summary,
decision_options, sub_items, participants, tags, priority, due_date
```

Other fields (`fingerprint`, `model`, `input_tokens`, `output_tokens`, `cost_usd`, `prompt_version`, `read_at`, `has_updates`, `channel_ids`, `related_digest_ids`, `dismissed_at`) are not part of the narrative-equality check — they are bookkeeping metadata or covered by other contracts.

After the INSERT, trim:

```sql
DELETE FROM track_states
 WHERE track_id = ?
   AND id NOT IN (
       SELECT id FROM track_states
        WHERE track_id = ?
        ORDER BY created_at DESC, id DESC
        LIMIT 30
   );
```

(Using both `created_at` and `id` in the ordering disambiguates same-second writes.)

Call sites:

| Function | Source value | Skip if |
|---|---|---|
| `UpdateTrackFromExtraction(id, t Track)` | `'extraction'` | narrative fields all unchanged |
| `UpsertTrack(t Track)` when `t.ID > 0` | `'manual'` | narrative fields all unchanged |
| `UpdateTrackPriority(id, priority)` | `'manual'` | priority unchanged |
| `UpdateTrackOwnership(id, ownership)` | `'manual'` | ownership unchanged |
| `UpdateTrackSubItems(id, subItems)` | `'manual'` | sub_items unchanged |

`MarkTrackRead`, `DismissTrack`, `RestoreTrack`, `SetTrackHasUpdates`, `UpsertTrack` (insert path with `t.ID == 0`) do **not** call `snapshotTrackState` — they don't change narrative content (insert has no predecessor; the others change lifecycle flags only).

Read API:

```go
// GetTrackStates returns the history of a track ordered by created_at DESC,
// most recent first. Empty slice if no history.
func (db *DB) GetTrackStates(trackID int) ([]TrackState, error)
```

`TrackState` struct mirrors the table columns. Lives in `internal/db/tracks.go`.

## Behavior — Desktop

`WatchtowerDesktop/Sources/Models/TrackState.swift` (new): GRDB `FetchableRecord, TableRecord` mirroring the schema.

`WatchtowerDesktop/Sources/Queries/TrackStateQueries.swift` (new): one method `fetchByTrackID(_ id: Int64) -> [TrackState]`.

`WatchtowerDesktop/Sources/ViewModels/TracksViewModel.swift`: extend with a per-track lazy load. Either via a method `loadHistory(for trackID: Int64)` returning `[TrackState]`, or via a GRDB ValueObservation if the detail view is open. Single-shot load on detail open is enough for v1 — re-fetching on each daemon cycle is unnecessary; the detail view re-opens often enough.

`WatchtowerDesktop/Sources/Views/Tracks/TrackDetailView.swift`: new collapsible `historySection` placed below `decisionSection`/`subItemsSection`, above `sourceRefsSection`. Disclosure-style group with per-state rows showing:

- Timestamp (relative + absolute on hover).
- Source badge: "Extraction (model)" / "Manual".
- Field that changed most prominently (heuristic: prefer `text` if changed, else `priority`+`ownership`, else "Updated"). Body collapsed by default.
- Tap to expand — full snapshot fields rendered as the current detail body (so users see what the track *looked like* at that moment).

No revert button. No edit. Read-only.

Empty state: "No history yet — track was created in this state." (i.e. zero `track_states` rows).

## Touchpoints

- `internal/db/db.go` — add migration block `if version < 73 { … }` with the schema above, mirroring v72's structure (transaction, `PRAGMA user_version = 73`, commit).
- `internal/db/tracks.go` — add `TrackState` struct, `snapshotTrackState` helper, `GetTrackStates`. Modify `UpdateTrackFromExtraction`, `UpsertTrack` (update branch), `UpdateTrackPriority`, `UpdateTrackOwnership`, `UpdateTrackSubItems` to call `snapshotTrackState` before the UPDATE.
- `internal/db/tracks_test.go` — add tests (see Testing).
- `internal/tracks/pipeline.go` — no change (pipeline calls `UpdateTrackFromExtraction`/`UpsertTrack`, history is handled at the DB layer).
- `WatchtowerDesktop/Sources/Models/TrackState.swift` — new file.
- `WatchtowerDesktop/Sources/Queries/TrackStateQueries.swift` — new file.
- `WatchtowerDesktop/Sources/ViewModels/TracksViewModel.swift` — extend with `loadHistory(for:)`.
- `WatchtowerDesktop/Sources/Views/Tracks/TrackDetailView.swift` — add `historySection`.
- `WatchtowerDesktop/Tests/TrackStateQueriesTests.swift` — new file.
- `docs/inventory/tracks.md` — TRACKS-06: Status `Partial → Enforced`; remove the "Tracked gap" subsection; expand "Test guards" with the new tests; bump `Locked since` to the implementation date; append Changelog line.
- `internal/db/migration_test.go` — extend `TestMigrationsRunForwardOnFresh` (or local equivalent) to assert `track_states` exists and has the expected columns after migration.

## Testing

New Go guard tests in `internal/db/tracks_test.go`:

- `TestTracks06_StateSnapshotOnExtractionUpdate` — create track, mark read, call `UpdateTrackFromExtraction` with changed `text` → assert one `track_states` row exists with the **previous** text, `source='extraction'`, correct `model`/`prompt_version`.
- `TestTracks06_NoSnapshotWhenNarrativeUnchanged` — call `UpdateTrackFromExtraction` with identical narrative fields (only `model` and tokens differ) → assert zero `track_states` rows.
- `TestTracks06_StateSnapshotOnManualPriorityChange` — `UpdateTrackPriority(id, "high")` on a `medium` track → one row with `source='manual'`, snapshot has `priority='medium'`.
- `TestTracks06_StateSnapshotOnManualOwnershipChange` — symmetric for ownership.
- `TestTracks06_StateSnapshotOnManualSubItemsChange` — symmetric for sub_items.
- `TestTracks06_StateSnapshotOnUpsertWithID` — `UpsertTrack(Track{ID: id, Text: "new"})` with predecessor present → one row with `source='manual'`, snapshot has the predecessor's text.
- `TestTracks06_NoSnapshotOnInsert` — `UpsertTrack(Track{Text: "fresh"})` (ID=0) → zero `track_states` rows for that new track.
- `TestTracks06_HistoryCapAt30` — drive 35 updates, assert exactly 30 rows remain, and the rows are the 30 most recent (by `created_at` DESC).
- `TestTracks06_GetTrackStatesOrdersDescByCreatedAt` — insert 3 states with explicit `created_at`, assert returned order.
- `TestTracks06_HistoryCascadesOnTrackDelete` — delete the parent track → `track_states` rows for that track gone (FK `ON DELETE CASCADE`).
- `TestTracks06_SnapshotErrorDoesNotBlockUpdate` — force the snapshot path to fail (e.g. by passing a sentinel that triggers a controlled error) → assert the parent UPDATE still applied. (Implementation detail: the helper logs and returns nil/error; callers proceed regardless.)
- `TestMigrationV73_CreatesTrackStatesTable` — apply migrations on a fresh DB at version 72, run v73, assert table + index exist with the expected schema.

Existing tests to update:

- `TestUpdateTrackFromExtraction` — assert exactly one `track_states` row is written as a side effect (the prior state).
- `TestUpsertTrack_Update` — same.

Swift tests in `WatchtowerDesktop/Tests/TrackStateQueriesTests.swift`:

- `test_TRACKS_06_fetchByTrackID_returnsDescendingOrder`
- `test_TRACKS_06_fetchByTrackID_emptyForNewTrack`
- `test_TRACKS_06_fetchByTrackID_decodesAllFields`

UI test (manual, smoke): open a track that has at least 2 historical states → expand history section → confirm timestamps + source badges render and the expanded body shows the historical narrative.

## Error Handling

- Migration v73 is `CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS` — safe to re-run, idempotent. If the table already exists from a prior partial run, the migration succeeds and bumps `user_version` to 73.
- `snapshotTrackState` failures: log via `db.logger` (or stderr if no logger) and return nil. Do not abort the parent `UpdateTrack…` call. Rationale: history is observability; losing one history entry is recoverable, losing the actual update is not.
- A track with `track_id` not present at write time would fail the FK constraint — but `snapshotTrackState` always loads the track first by ID; if the load fails (track was just deleted), the helper returns early with no INSERT.
- The 30-row trim DELETE runs unconditionally after each successful INSERT. If the INSERT skipped (no narrative change), no DELETE runs — the cap is enforced incrementally.
- Concurrent writes to the same track: SQLite single-writer serialises. The snapshot+insert+trim sequence is not transactionally atomic with the parent UPDATE; this is acceptable because (a) the snapshot reflects state as of its own load, and (b) the TRACKS-06 contract is "history exists", not "history is exactly aligned per-write".

## Performance

- `snapshotTrackState` adds one SELECT (loading the track) and one INSERT per narrative-changing update. Cost ~2 ms locally on `:memory:`. Daemon cycles already do hundreds of `UpdateTrackFromExtraction` calls; the additional load is bounded by the number of *changed* tracks per cycle (typically < 50). Negligible.
- Trim DELETE is bounded by 30 rows per track — O(1) per insert.
- The index `idx_track_states_track(track_id, created_at DESC)` covers the read path and the trim subquery's ORDER BY. No table scan.
- No additional memory pressure: the helper does not cache anything between calls.

## Decisions

Locked-in choices for this plan (no further sign-off needed):

1. **Cap = 30 states per track.** Sufficient for the typical 10–15-update lifetime; trim runs at insert time.
2. **Manual edits are in scope.** `UpdateTrackPriority`, `UpdateTrackOwnership`, `UpdateTrackSubItems`, and `UpsertTrack(ID>0)` all snapshot with `source='manual'`. Uniform surface; future revert will benefit.
3. **Read-only history in v1.** No revert button. Viewing-only is enough to pin TRACKS-06 Enforced.
4. **No prompt injection in v1.** Prior states are not fed back into the extraction prompt. Schema enables it later as a one-line SELECT in `pipeline.go::generateBatchTracks`.
5. **History starts now — no backfill.** Pre-migration tracks begin with empty history; "no history yet" is honest, a synthetic snapshot of the live row would not be.
6. **`UpsertTrack(ID>0)` is covered.** Even though rare, snapshotting it keeps the audit surface uniform across all narrative-mutating paths.

## Acceptance Criteria

- All `TestTracks06_*` tests (≥10 new + 2 updated) pass.
- `make test` clean.
- Fresh DB migrated to v73 contains `track_states` table with the documented schema and index.
- `internal/db/tracks.go::UpdateTrackFromExtraction` writes exactly one `track_states` row per call when narrative changes; zero when narrative unchanged.
- `WatchtowerDesktop/Sources/Views/Tracks/TrackDetailView.swift` renders a History section showing prior states for a track that has been updated at least once.
- `docs/inventory/tracks.md` TRACKS-06 entry has Status `Enforced`, no `Tracked gap` subsection, expanded `Test guards`, and a Changelog entry referencing this design.
- `git grep -n 'BEHAVIOR TRACKS-06'` returns at least one match per new guard test (i.e. each test contains a `// BEHAVIOR TRACKS-06: …` marker comment per the soft-protection convention from the inventory README).

## TRACKS-06 Inventory Update

After implementation, the entry becomes:

```markdown
## TRACKS-06 — Re-extraction never narrows history

**Status:** Enforced

**Observable — channel/digest origins:** [unchanged]

**Observable — track state history:** A track preserves a record of its
own past states. Every change to a narrative field — text, context,
priority, ownership, category, decision_summary, sub_items,
participants, tags, etc. — writes a snapshot of the prior state into
`track_states` with provenance (`source='extraction'|'manual'`, model,
prompt_version, created_at). The user sees a timeline in
TrackDetailView; a bad re-extraction is recoverable in inspection
even though revert is not yet a UI action. Up to 30 most recent
states per track are retained.

**Why locked:** [unchanged]

**Test guards:**
- internal/db/tracks_test.go::TestUpdateTrackFromExtraction
- internal/db/tracks_test.go::TestMergeJSONArrays
- internal/db/tracks_test.go::TestTracks06_StateSnapshotOnExtractionUpdate
- internal/db/tracks_test.go::TestTracks06_NoSnapshotWhenNarrativeUnchanged
- internal/db/tracks_test.go::TestTracks06_StateSnapshotOnManualPriorityChange
- internal/db/tracks_test.go::TestTracks06_StateSnapshotOnManualOwnershipChange
- internal/db/tracks_test.go::TestTracks06_StateSnapshotOnManualSubItemsChange
- internal/db/tracks_test.go::TestTracks06_StateSnapshotOnUpsertWithID
- internal/db/tracks_test.go::TestTracks06_NoSnapshotOnInsert
- internal/db/tracks_test.go::TestTracks06_HistoryCapAt30
- internal/db/tracks_test.go::TestTracks06_GetTrackStatesOrdersDescByCreatedAt
- internal/db/tracks_test.go::TestTracks06_HistoryCascadesOnTrackDelete
- internal/db/migration_test.go::TestMigrationV73_CreatesTrackStatesTable

**Locked since:** <implementation date>
```

The "Tracked gap (Aspirational part)" subsection is removed entirely. Changelog line: `<date>: TRACKS-06 closed gap — track_states table introduced (migration v73); narrative-field history captured for both extraction and manual edits; TrackDetailView surfaces timeline.`
