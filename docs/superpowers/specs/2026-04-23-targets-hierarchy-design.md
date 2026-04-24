# Targets — Hierarchical Goal System with AI-Driven Extraction and Linking

**Date:** 2026-04-23
**Status:** Approved (brainstorm complete, pending implementation plan)
**Branch:** feature/day-plan (base for new feature branch `feature/targets`)

## Context

Watchtower currently has a flat `tasks` table (see `internal/db/tasks.go`, `cmd/tasks.go`, `WatchtowerDesktop/Sources/Views/Tasks/`). It serves as a per-user action-item store wired into briefing (`gatherTasks`), inbox (`task_id`), day-plan, and chat.

The user's pain points:

1. **No granularity.** Tasks are flat; there's no way to express "this is a quarterly goal, this is a weekly step toward it, this is today's action." Strategic planning and tactical execution live in the same pile.
2. **No relationships.** Tasks are independent — no parent/child, no "contributes to," no "blocks." The user wants an LLM to *placeholder-fill* those relationships, because maintaining them by hand is impossible.
3. **URL context is lost.** Messages often contain Slack permalinks or Jira issue URLs, which are auth-protected and can't be fetched from a naive web-crawl. The relevant data is already in the local DB (`messages`, `jira_issues`) or accessible via MCP.
4. **One message, many actions.** A Slack message or email frequently packs 3–10 discrete action items. The current flow (one task per create) forces manual splitting.

This spec replaces `tasks` with `targets` — a hierarchical, AI-linked goal system that treats each of those four pain points as a first-class concern.

## Goals

- Replace `tasks` with `targets` across the entire codebase (Go + Swift + CLI + DB).
- Hierarchy with **flexible periods** (OKR-style): each target has `period_start` / `period_end` as the source of truth, plus a `level` tag (`quarter`/`month`/`week`/`day`/`custom`) for grouping and UI.
- **Tree + secondary links** hierarchy: one primary `parent_id` for tree rendering and progress rollup; up to 3 secondary `target_links` (typed `contributes_to`/`blocks`/`related`/`duplicates`) for cross-cutting relationships.
- **Synchronous AI linking at creation**: the same LLM call that extracts a target also proposes `parent_id` and secondary links in one JSON response — no separate linking pipeline, no async delay.
- **URL enrichment** of Slack and Jira links in the source message: local-DB first, MCP Atlassian fallback for Jira issues not in local cache, no persistent cache.
- **Multi-target extraction from one message** with an editable preview dialog before persistence.
- Full `tasks` → `targets` rename, drop all existing tasks data (user chose clean-slate migration).

## Non-Goals (V1)

- Confluence, Google Docs, GitHub, or generic-web URL resolvers (interface is pluggable; concrete resolvers are V2).
- Dedup of extracted targets against existing targets (accepted risk: duplicates are created, user prunes manually).
- Embeddings-based similarity.
- Weighted OKR progress (progress rollup is a simple average).
- Gantt chart / calendar-overlay view.
- Automatic period-rollover (e.g., new week cloning last week's children from parent).
- Soft caps or retry for extraction (hard cap 10 targets per AI call).
- Back-compat alias `watchtower tasks` — command prints a renamed-to-targets notice and exits with code 2.
- Backfill of legacy tasks into targets (tasks are dropped).

## Terminology

- **Target** — a goal at any level. Replaces "task."
- **Level** — one of `quarter`/`month`/`week`/`day`/`custom`. Tag for grouping/UI.
- **Period** — `period_start` / `period_end` date range. Source of truth for "when."
- **Primary parent** — the single `parent_id` used for tree rendering and progress rollup.
- **Secondary link** — a typed edge in `target_links` to another target OR to an external reference (Jira key, Slack permalink).
- **Extraction** — the AI pipeline that turns raw text + resolved URL enrichments into a list of targets with proposed links.

## High-Level Architecture

```
create_sheet / inbox / chat / cli-extract
   │
   └──► targets.Pipeline.Extract(request)
          │
          ├── resolver.Resolve(urls) ──► local-DB (messages, jira_issues) ──► [fallback] MCP Atlassian
          │
          ├── snapshot := loadActiveTargets() // top 100 by updated_at + priority
          │
          ├── Generator.Query(prompt = raw_text + ENRICHMENTS + snapshot + instructions)
          │     ↳ returns JSON {extracted[], omitted_count, notes}
          │
          └── preview_sheet (Desktop only) ──► user edits/confirms ──► batch INSERT targets + target_links
```

Manual creation (no paste, no AI) uses a lighter path: direct INSERT, then optional async `Linker.LinkExisting(target_id)` via a "Suggest links" button.

## Data Model (Migration v66)

### New tables

```sql
CREATE TABLE targets (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    text                TEXT NOT NULL,
    intent              TEXT NOT NULL DEFAULT '',
    level               TEXT NOT NULL DEFAULT 'day'
                        CHECK(level IN ('quarter','month','week','day','custom')),
    custom_label        TEXT NOT NULL DEFAULT '',       -- free text only when level='custom'
    period_start        TEXT NOT NULL,                   -- YYYY-MM-DD, source of truth
    period_end          TEXT NOT NULL,                   -- YYYY-MM-DD, source of truth
    parent_id           INTEGER REFERENCES targets(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'todo'
                        CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
    priority            TEXT NOT NULL DEFAULT 'medium'
                        CHECK(priority IN ('high','medium','low')),
    ownership           TEXT NOT NULL DEFAULT 'mine'
                        CHECK(ownership IN ('mine','delegated','watching')),
    ball_on             TEXT NOT NULL DEFAULT '',
    due_date            TEXT NOT NULL DEFAULT '',         -- YYYY-MM-DDTHH:MM or ''
    snooze_until        TEXT NOT NULL DEFAULT '',
    blocking            TEXT NOT NULL DEFAULT '',
    tags                TEXT NOT NULL DEFAULT '[]',
    sub_items           TEXT NOT NULL DEFAULT '[]',
    notes               TEXT NOT NULL DEFAULT '[]',
    progress            REAL NOT NULL DEFAULT 0.0,        -- 0.0..1.0, auto-rollup when children exist
    source_type         TEXT NOT NULL DEFAULT 'manual'
                        CHECK(source_type IN ('extract','briefing','manual','chat','inbox','jira','slack')),
    source_id           TEXT NOT NULL DEFAULT '',
    ai_level_confidence REAL DEFAULT NULL,                -- AI-guess confidence for level, NULL for manual
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE INDEX idx_targets_level       ON targets(level);
CREATE INDEX idx_targets_parent      ON targets(parent_id);
CREATE INDEX idx_targets_period      ON targets(period_start, period_end);
CREATE INDEX idx_targets_status      ON targets(status);
CREATE INDEX idx_targets_priority    ON targets(priority);
CREATE INDEX idx_targets_due         ON targets(due_date);
CREATE INDEX idx_targets_source      ON targets(source_type, source_id);
CREATE INDEX idx_targets_updated     ON targets(updated_at DESC);

CREATE TABLE target_links (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    source_target_id    INTEGER NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
    target_target_id    INTEGER REFERENCES targets(id) ON DELETE CASCADE,   -- nullable
    external_ref        TEXT NOT NULL DEFAULT '',                             -- e.g. 'jira:PROJ-123', 'slack:C123:1714567890.123456'
    relation            TEXT NOT NULL
                        CHECK(relation IN ('contributes_to','blocks','related','duplicates')),
    confidence          REAL DEFAULT NULL,                                    -- AI-assigned, NULL if user-created
    created_by          TEXT NOT NULL DEFAULT 'ai'
                        CHECK(created_by IN ('ai','user')),
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    CHECK (target_target_id IS NOT NULL OR external_ref != ''),
    UNIQUE(source_target_id, target_target_id, external_ref, relation)
);

CREATE INDEX idx_target_links_source   ON target_links(source_target_id);
CREATE INDEX idx_target_links_target   ON target_links(target_target_id);
CREATE INDEX idx_target_links_external ON target_links(external_ref);
```

### Drop and cascade-rename

```sql
-- Clean slate: existing tasks are discarded per product decision.
DROP TABLE IF EXISTS tasks;

-- References elsewhere
ALTER TABLE inbox_items RENAME COLUMN task_id TO target_id;
-- CHECK constraints with 'task' literal must be rewritten via table recreate:
--   feedback.entity_type         : add 'target', remove 'task'
--   (briefings / pipeline_runs do not contain hardcoded 'task' literals — verify during implementation)

DELETE FROM feedback WHERE entity_type = 'task';
```

### Progress rollup

- On INSERT/UPDATE/DELETE of a target, recompute `parent.progress = AVG(children.progress)` where children are non-dismissed. If a target has no children, its `progress` is derived from status (`done`=1.0, `in_progress`=0.5, else 0.0). Implemented as application-level logic in `internal/targets/store.go`; no SQL triggers (keep portable).
- Manual override is out of scope for V1.

### `tasks` → `targets` rename breadcrumbs

The migration log (stored in `db.Migrations`) records the v66 rename for operator traceability. Code-level references touched:

- `internal/db/tasks.go` → `internal/db/targets.go` (struct `Task` → `Target`, `TaskFilter` → `TargetFilter`, all queries)
- `internal/db/tasks_test.go` → `internal/db/targets_test.go`
- `cmd/tasks.go` → `cmd/targets.go` + a stub `cmd/tasks_deprecated.go` that prints a rename notice.
- `WatchtowerDesktop/Sources/Models/Task*.swift` → `Target*.swift`
- `WatchtowerDesktop/Sources/Database/Queries/TaskQueries.swift` → `TargetQueries.swift`
- `WatchtowerDesktop/Sources/ViewModels/TasksViewModel.swift` → `TargetsViewModel.swift`
- `WatchtowerDesktop/Sources/Views/Tasks/` → `WatchtowerDesktop/Sources/Views/Targets/` (CreateTaskSheet → CreateTargetSheet, TaskDetailView → TargetDetailView, TasksListView → TargetsListView)
- `Destination.tasks` → `Destination.targets` in Desktop navigation enum
- Briefing `gatherTasks` → `gatherTargets`; briefing JSON field `suggest_task` → `suggest_target`, `task_id` → `target_id`
- Chat system prompt references in `internal/repl/` updated to describe `targets` + `target_links`

## Extraction Pipeline

### Package layout

```
internal/targets/
├── pipeline.go      // Pipeline struct, Extract()/LinkExisting() entry points
├── extractor.go     // AI prompt assembly, JSON parsing, preview dispatch
├── linker.go        // Lighter AI call for post-hoc linking of a single manual target
├── resolver.go      // URL detection + local/MCP enrichment (Slack, Jira)
├── store.go         // targets + target_links CRUD, progress rollup
└── prompts.go       // embedded prompt templates (targets.extract, targets.link)
```

### `ExtractRequest`

```go
type ExtractRequest struct {
    RawText    string  // paste / message body / form input
    EntryPoint string  // 'create_sheet' | 'inbox' | 'chat' | 'cli'
    SourceRef  string  // 'inbox:42' | 'slack:C123:1714...' | '' (manual paste)
    UserLevel  string  // optional hint; if set, AI is told to prefer this level
}
```

### Prompt input

- `RawText` (verbatim)
- `=== ENRICHMENTS ===` block produced by `resolver.Resolve` (see URL Enrichment section)
- `=== ACTIVE TARGETS ===` snapshot — all targets where `status NOT IN ('done','dismissed')`, top 100 by `updated_at DESC` then priority. Format:
  `[id=12 level=week period=2026-04-20..26 priority=high status=in_progress] text`
- `=== CURRENT DATE ===` — today's date, so AI can resolve relative dates.
- `=== USER HINT ===` — present only if `UserLevel != ""`.
- Instructions enforcing JSON schema, cap 10 extracted targets, level guidance (e.g., "if timeframe > 1 month → quarter; 1–4 weeks → month or week; within this week → week; today-only → day; none of the above → custom").

### AI JSON response schema

```json
{
  "extracted": [
    {
      "text": "string (required, <=280 chars)",
      "intent": "string (optional)",
      "level": "quarter|month|week|day|custom",
      "custom_label": "string (required iff level=custom)",
      "level_confidence": 0.0,
      "period_start": "YYYY-MM-DD",
      "period_end": "YYYY-MM-DD",
      "priority": "high|medium|low",
      "due_date": "YYYY-MM-DDTHH:MM or ''",
      "parent_id": 123,
      "secondary_links": [
        {"target_id": 7, "relation": "contributes_to", "confidence": 0.72},
        {"external_ref": "jira:PROJ-123", "relation": "contributes_to"}
      ]
    }
  ],
  "omitted_count": 0,
  "notes": "optional free-form message shown to user in preview footer"
}
```

Constraints enforced by `extractor.go` on the returned JSON:
- `len(extracted) <= 10` (truncate and count as `omitted_count` if AI over-returns).
- `secondary_links` per target: cap 3 (truncate rest).
- `parent_id` must exist in snapshot; unknown IDs → `SET NULL` with warning.
- Unknown `target_id` in a secondary link → drop the link with warning.
- `period_end >= period_start`; if AI violates, swap.

### Preview dialog UX (Desktop)

`ExtractPreviewSheet`:

- One editable card per extracted target.
- Fields: text (textarea), level (picker — AI's choice highlighted with "AI guess, confidence X%"), period_start/end (date pickers, pre-filled), priority, parent (picker, pre-filled from AI), secondary links (chip list, each editable/removable).
- Per-card checkbox (default: checked).
- Footer: "AI omitted N more items" if `omitted_count > 0`; notes verbatim.
- Buttons: **Create selected** (batch INSERT in one transaction), **Cancel** (discard).

### Manual creation — no AI

- `CreateTargetSheet` opened without paste: user fills fields by hand. On Save, direct INSERT. No AI call.
- After save, `TargetDetailView` offers **Suggest links** button which calls `linker.LinkExisting(id)` (AI with a smaller prompt: just this target + active-targets snapshot), returning a proposed `parent_id` + secondary links. User confirms via a mini-dialog.

### CLI

- `watchtower targets create --text ...` — direct INSERT, no AI.
- `watchtower targets extract --text ... [--source-ref ...]` — runs AI extraction, prints a preview list, prompts `[y/N]` per target for confirmation (CLI doesn't have Desktop preview, so terminal confirm).
- `watchtower targets extract --from-inbox <id>` — extract with RawText loaded from inbox_items.raw_text.

### Graceful degradation

- AI timeout (default 30s) → preview dialog shows warning, falls back to manual creation pre-filled with RawText in the text field.
- Malformed JSON → one retry; on second failure, same manual fallback.
- Parent_id referencing a missing target → `NULL`, logged warning.
- MCP unavailable → resolver degrades to local-only, URLs that need MCP get `[not in local DB]` annotation in the enrichment block.

## URL Enrichment

### Detection

Regex in `resolver.Extract(text) []URLMatch`:

- Slack: `https://[a-z0-9-]+\.slack\.com/archives/([A-Z0-9]+)/p(\d+)(?:\?thread_ts=([\d.]+))?`
- Jira: `https://[a-z0-9-]+\.atlassian\.net/browse/([A-Z][A-Z0-9]+-\d+)`

All other URLs in text are ignored in V1.

### `URLResolver` interface

```go
type URLResolver interface {
    CanResolve(url string) bool
    Resolve(ctx context.Context, match URLMatch) (*Enrichment, error)
}

type Enrichment struct {
    Ref       string   // 'jira:PROJ-123' or 'slack:C123:1714567890.123456'
    Title     string
    Body      string   // formatted text for prompt injection
    Source    string   // 'local' or 'mcp'
    Error     string   // non-empty if resolution failed (pipeline still proceeds)
}
```

V1 registers `SlackResolver` and `JiraResolver`. V2 stubs (`ConfluenceResolver`, `GithubResolver`, `GdocsResolver`) are not registered.

### `SlackResolver`

1. Parse `channel_id` + `ts` (converting `p1714567890123456` → `1714567890.123456`).
2. `SELECT text, user_id, channel_id, thread_ts FROM messages WHERE channel_id=? AND ts=?` (index-driven).
3. If hit: format as
   ```
   #<channel-name> by @<user-display-name> at <YYYY-MM-DD HH:MM>
   <text-first-500-chars>
   ```
4. If miss: annotate `[slack url not in local DB]`. MCP Slack is not configured for fetch-by-permalink in V1 — skip.

### `JiraResolver`

1. Extract `KEY-NUM`.
2. `SELECT key, summary, status, priority, assignee_display_name, sprint_name, due_date, epic_key, status_category FROM jira_issues WHERE key=?`.
3. If hit: reuse `jira.BuildIssueContext([]db.JiraIssue{...})`.
4. If miss: call MCP `mcp__claude_ai_Atlassian__getJiraIssue` with a 10s timeout. Format result with the same `jira.BuildIssueContext` shape. Mark `Source='mcp'`.
5. On MCP error/timeout: annotate `[jira fetch failed: <reason>]`.

### Prompt injection format

```
=== ENRICHMENTS ===
[jira:PROJ-123]
- [PROJ-123 status=In Progress priority=High sprint="Sprint 42" due=2026-05-01]
  summary: "Redesign onboarding flow"
  assignee: John Doe

[slack:C12345:1714567890.123456]
#product-design by @jane at 2026-04-22 14:30
"hey team, we need to wrap up the Figma mockups before sprint review"

[jira:PROJ-456] (via MCP)
- [PROJ-456 status=To Do priority=Medium]
  summary: "Draft API spec for v2 endpoints"
=== /ENRICHMENTS ===
```

### Automatic external link creation

When an extracted target's RawText came from or references a resolved URL, the AI is instructed to add a secondary link with `external_ref` set to the URL's `Ref`. Example:

```json
"secondary_links": [
  {"external_ref": "slack:C12345:1714567890.123456", "relation": "contributes_to"}
]
```

The pipeline writes these to `target_links` at create time (same transaction as the target INSERT).

## LLM Linking Details

- Context sent to AI: full snapshot of active targets (top 100 by recency + priority; see Prompt input). Compressed one-line-per-target format to minimize tokens.
- `parent_id`: AI picks one from snapshot or returns `null`. Unknown → SET NULL.
- `secondary_links`: cap 3 per target. Relation types fixed: `contributes_to`/`blocks`/`related`/`duplicates`. Unknown relations → drop.
- `confidence` is optional; persisted when supplied (useful for future filtering/UI hints).
- For `linker.LinkExisting(id)` (manual-creation path): smaller prompt with only the single target + snapshot. Same JSON schema, but only `parent_id` and `secondary_links` fields matter.

## UI (Desktop)

### Targets tab (replaces Tasks)

- Sidebar entry "Targets" with an icon (suggest `scope` or `target`). Replaces the Tasks entry; `Destination.tasks` → `Destination.targets`.
- Badge: count of active targets with due today or overdue.

### Outline view (`TargetsListView`)

- Top filter bar: level (multi-select), status (default hides done/dismissed), period preset (This week / This month / This quarter / All / Custom), priority, free-text search.
- Main list: one flat list, sorted by semantic level order `quarter → month → week → day → custom` (implemented via `CASE level WHEN 'quarter' THEN 0 WHEN 'month' THEN 1 WHEN 'week' THEN 2 WHEN 'day' THEN 3 ELSE 4 END`), then `period_start ASC`, then semantic priority order `high → medium → low` (via analogous CASE). Indentation reflects depth from root (up to 4 levels; capped in rendering).
- Row inline actions: status toggle, priority change, snooze.
- Right pane: `TargetDetailView` with tabs **Details**, **Links**, **Activity**.

### `TargetDetailView`

- **Details** tab: text, intent, level+custom_label picker, period_start/end, parent picker (tree selector scoped to valid parents), tags, sub-items (checklist), notes, progress bar (greyed if auto-rollup from children).
- **Links** tab: two sections, *Inbound* (links where `target_target_id=this.id`) and *Outbound* (links where `source_target_id=this.id`). Each row: relation, peer target link or external ref (clickable Slack permalink or Jira URL), AI confidence badge, remove button. Footer: **Suggest links** button (calls `linker.LinkExisting`).
- **Activity** tab: sub-items progress, feedback buttons, created/updated, source_ref badge linking back to originator (inbox, chat, etc.).

### `CreateTargetSheet`

- Text field.
- **Paste and extract** button — triggers the AI pipeline (blocking dialog with spinner).
- Autodetect: if pasted content > 200 chars, highlight the extract button.
- Manual pickers for level/period/priority/parent remain visible; filling them bypasses AI and goes through direct INSERT.

### `ExtractPreviewSheet`

- Modal over CreateTargetSheet.
- List of editable cards (see Preview dialog UX above).
- Footer: omitted count, AI notes.
- Buttons: Create selected, Cancel.

## CLI

```
watchtower targets                               # list active
watchtower targets --all                         # include done/dismissed
watchtower targets --level week
watchtower targets --period this-week
watchtower targets show <id>
watchtower targets create --text "..." [--level week] [--parent <id>] \
                         [--period-start YYYY-MM-DD] [--period-end YYYY-MM-DD]
watchtower targets extract --text "..." [--source-ref ...]
watchtower targets extract --from-inbox <id>
watchtower targets link <id> --parent <pid>
watchtower targets link <id> --to <tid> --relation contributes_to
watchtower targets link <id> --external jira:PROJ-123 --relation contributes_to
watchtower targets unlink <link-id>
watchtower targets suggest-links <id>            # async AI linker for manual target
watchtower targets done <id>
watchtower targets dismiss <id>
watchtower targets snooze <id> <date>
watchtower targets update <id> [--text ...] [--priority ...] [--level ...] [--period-start ...] ...
```

`watchtower tasks` is retained as a **stub** that prints:

> `tasks` has been renamed to `targets`. See `watchtower targets --help`.

and exits with code 2. No alias — the rename is intentional and user-facing.

## Integrations

### Briefing

- `internal/briefing/gatherTasks` → `gatherTargets`. Pulls active targets grouped by level, formats hierarchy into the prompt block (e.g., Quarter goals → their Month/Week children). AI can reason about alignment.
- Prompt `briefing.daily` updated to mention levels and secondary links.
- Briefing JSON fields: `your_day[].task_id` → `target_id`; `attention[].suggest_task` → `suggest_target`.

### Inbox

- Column `inbox_items.task_id` → `target_id` (rename).
- Desktop `InboxDetailView`: "Create task" button → "Create targets" (plural, launches `ExtractPreviewSheet` with inbox item text as RawText; a single inbox item may spawn multiple targets, per requirement 4).
- CLI `watchtower inbox task <id>` → `watchtower inbox target <id>`; triggers CLI extract flow.

### Day Plan (`feature/day-plan` branch)

- Day-plan blocks remain independent of targets (per decision 9b = B).
- Optional UI-level link: `day_plan_blocks.target_id` nullable column added so a block can reference the target it implements (displayed as badge in `TargetDetailView` and a link icon in `DayPlanTimelineView`).
- Day-plan pipeline is unchanged; new column is strictly informational.
- "Promote to target" action on a day-plan block creates a `level='day'` target with `period_start=period_end=block.date` and writes `target_id` back to the block.

### Chat (MCP `read_query`)

- `schema.sql` (embedded in system prompt) is updated so the AI chat agent sees `targets` + `target_links`.
- System prompt text: all mentions of "tasks" replaced with "targets"; a short paragraph describes the hierarchy and link semantics so the agent can query and propose linkings correctly.

### Feedback

- `feedback.entity_type` CHECK: remove `'task'`, add `'target'`.
- Existing feedback with `entity_type='task'` is deleted in the same migration (the underlying tasks are dropped anyway).

### Pipeline runs / token tracking

- New pipeline type identifiers recorded in `pipeline_runs`: `targets.extract`, `targets.link`. Token usage is already captured via commit `c20b7d6`; just register the new names.

## Configuration

```yaml
targets:
  extract:
    enabled: true
    max_per_call: 10
    timeout_seconds: 30
    model: ""                   # empty → provider default
  resolver:
    slack_enabled: true
    jira_enabled: true
    mcp_timeout_seconds: 10
    active_snapshot_limit: 100
```

Loaded via existing `internal/config` patterns. Feature-flag the whole pipeline with `targets.extract.enabled=false` if someone wants direct-INSERT-only behavior.

## Testing

### Go

- `internal/targets/pipeline_test.go` — mock `Generator`, verify JSON → (targets, links) write path; verify cap enforcement; verify malformed-JSON retry + fallback.
- `internal/targets/resolver_test.go` — Slack local hit, Slack miss, Jira local hit, Jira miss with MCP-mock hit, Jira MCP timeout.
- `internal/targets/linker_test.go` — manual-target `LinkExisting` flow, parent + secondary link proposal shape.
- `internal/db/targets_test.go` — CRUD, cascade on delete of parent, progress rollup over 2-level hierarchy, `target_links` UNIQUE constraint.
- Migration test: seed DB at schema v65 with sample tasks/inbox/feedback → apply v66 → verify targets table exists empty, inbox `target_id` column exists (old values nulled), feedback cleaned.

### Swift

- `WatchtowerDesktop/Tests/TargetTests.swift` — `TargetQueries` CRUD, filter by level/period, ValueObservation for list updates.
- `TargetsViewModel` tests — outline sort order, filter toggling.
- Integration check: `ExtractPreviewSheet` renders N cards, respects per-card checkbox on Create selected.

### Manual QA checklist

- Paste a Slack message with 3 action items + a Jira link → preview shows 3 targets with correct levels and an `external_ref: jira:...` link on the relevant one.
- Create a target manually (no paste) → no AI call (verify via pipeline_runs table); Suggest links produces a reasonable parent+secondaries.
- Mark a child done → parent's progress increments.
- Delete a parent → children's `parent_id` becomes NULL, they remain in list as orphans.

## Out of Scope (V2 candidates)

- Confluence / Google Docs / GitHub / generic web URL resolvers (plugin slots exist).
- Duplicate detection against existing targets (AI already sees snapshot; V2 can enable stricter duplicate logic).
- Embeddings-based similarity for dedup/linking.
- OKR-style weighted progress rollup.
- Gantt / calendar-overlay visualization.
- Automatic period-rollover templates.
- Soft cap on extraction size with multi-pass AI.
- Bulk operations UI (mass snooze, mass reassign).
- Chat tool-call `extract_targets_from_chat` (user-initiated only in V1; explicit copy-paste).

## Open Questions

None at spec time. Any ambiguity discovered during implementation should be flagged in the implementation plan.
