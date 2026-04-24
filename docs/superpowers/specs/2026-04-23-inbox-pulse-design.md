# Inbox Pulse — Unified Live Feed Design

**Date:** 2026-04-23
**Status:** Design approved, ready for implementation plan
**Owner:** Vadym
**DB version bump:** 66 → 67

## Problem

The current Inbox (Slack @mentions, DMs, thread replies, reactions) functions as a noisy mention-aggregator and little more. It has no view of Jira comments, calendar changes, or internal Watchtower signals (decisions, briefings). Users need a single place to "keep a finger on the pulse" throughout the day — distinct from Day Plan (morning planning) and Briefing (daily reflection). It must surface action-required items without losing context-changing events, and it must learn from the user so signal-to-noise improves over time.

## Goals

- Unify Slack + Jira + Calendar + internal Watchtower events into one live feed.
- Separate items that require action from items that only need awareness.
- Feed-style UX (social-media-like) rather than inbox-zero pressure.
- Pinned high-priority items on top, reverse-chronological ambient items below.
- Auto-resolve aggressively so nothing lingers as noise.
- Learn from user behavior and explicit feedback; show what the system has learned.
- Keep AI cost bounded: ~2 calls per daemon cycle.

## Non-Goals (v1)

- Not replacing Briefing or Day Plan — they keep running in parallel. Merge decision is deferred to v2 after real-world use.
- Not the startup screen yet — Inbox remains a sidebar tab; merge-with-Briefing decision comes later.
- No silent signals (PR-stale, vacation-aware), hot-thread detection, AI reply drafts, pre-meeting context drops, or cross-system correlations (Jira ↔ Slack by ID). These are candidates for v2/v3.
- No GitHub/GitLab signals — integration does not exist yet.
- No weekly calibration session (L4 learning) in v1.
- Learning system is scoped to Inbox only — it does not influence Digest/Tracks/Briefing/People prompts.
- No rule decay — last-write-wins on `inbox_learned_rules`.

## Concept

Inbox becomes an AI-curated personal newsfeed with two classes of items:

**Actionable** — requires user action. Lives in `pending/resolved` lifecycle. Stays visible until resolved (explicitly or via auto-resolve rule). Candidate for the pinned section.

**Ambient** — awareness-only. Auto-seen on scroll, auto-archived after 7 days. Lives in chronological feed, never pinned.

A rule-based classifier assigns a default class per `trigger_type`. The AI prioritizer may downgrade `actionable → ambient` based on learned user preferences. The user can manually downgrade via a "not important" action, which records as a learning signal.

## Feed Layout

Layout variant D: pinned + chronological.

- **Pinned section (max 5)** — AI-selected actionable items requiring attention in the next ~2 hours. Full-size cards with prominent actions.
- **Feed** — reverse-chronological list of all non-pinned items. Ambient items render as compact cards (one line + metadata); actionable non-pinned as medium cards.
- **Day dividers** — "Today", "Yesterday", "Earlier this week", etc.
- **Sidebar badge** — unread count, with a red dot indicator if any pinned item is high-priority.

The old `InboxListView` (sender-grouped) is removed. Feed UI ships as the only Inbox view.

## Sources and Trigger Types (v1)

### Slack (existing, extended)

| trigger_type | default class | auto-resolve rule |
|---|---|---|
| `mention` | actionable | user replied in channel/thread |
| `dm` | actionable | user replied in DM |
| `thread_reply` | actionable | user replied in the thread |
| `reaction` (emoji reaction signalling pending — `👀`/`❓` on user's message) | ambient | auto-archive after 24h |

### Jira (new)

| trigger_type | default class | auto-resolve rule |
|---|---|---|
| `jira_assigned` | actionable | user changed status or reassigned |
| `jira_comment_mention` (@me in comment) | actionable | user added a comment to the issue |
| `jira_comment_watching` (comment on issue I watch) | ambient | auto-archive after 48h |
| `jira_status_change` (someone else's status change on my issue) | ambient | auto-archive after 24h |
| `jira_priority_change` (priority changed on my issue) | ambient | auto-archive after 24h |

### Calendar (new)

| trigger_type | default class | auto-resolve rule |
|---|---|---|
| `calendar_invite` | actionable | user responded (accept/decline/maybe) |
| `calendar_time_change` | actionable | user re-confirmed, or after 12h |
| `calendar_cancelled` | ambient | auto-archive after 24h |

### Watchtower-internal (new, cheap — read existing generated data)

| trigger_type | default class | auto-resolve rule |
|---|---|---|
| `decision_made` (new high-importance decision from digest `situations`) | ambient | auto-archive after 7d |
| `briefing_ready` (new briefing generated today) | ambient | auto-archive after 12h |

### Global archive rules

- Actionable with no activity > 14 days → auto-archive with reason `stale`.
- Ambient > 7 days → auto-archive with reason `seen_expired` (regardless of whether user scrolled past).

## Lifecycle

```
actionable: new → seen (auto on render) → resolved (explicit or auto-rule)
                                        → dismissed (user: "not important")
                                        → snoozed → back to new (after snooze expires)

ambient:    new → seen (auto on render) → archived (auto after 7d)
                                        → dismissed (user)
```

`status` and `archived_at` interact as follows:
- `status` transitions stay as-is (`pending → resolved | dismissed | snoozed`). Archival does not change `status`.
- `archived_at` is set ONLY by auto-archive rules (ambient-expired or actionable-stale). When set, `archive_reason` records why.
- Feed query excludes archived rows: `WHERE archived_at IS NULL AND status IN ('pending')` for actionable pinned/feed; ambient uses `WHERE archived_at IS NULL`.
- A `resolved` item disappears from the feed purely via `status`, without touching `archived_at`.
- `dismissed` is treated the same as `archived_at IS NOT NULL` for visibility purposes — both hide the item from the feed.

No `in_progress/blocked` states — Inbox items are signals, not tasks. If the user needs task-like tracking, the "Create Task" action spawns a `tasks` row with `source_type='inbox'`.

## Classifier

Two-step:

1. **Rule-based default** — a static table maps `trigger_type` → default `item_class`. Set at detection time.
2. **AI override (during batch prioritization)** — the `inbox.prioritize` prompt may downgrade `actionable → ambient` if `USER PREFERENCES` indicate the source is typically unimportant for this user. AI cannot upgrade `ambient → actionable` — that requires an explicit user action.

## Pinned Selection

Separate AI call per daemon cycle: `inbox.select_pinned`.

- Input: all currently-open actionable items (status=pending, not snoozed).
- Output: JSON list of item IDs to pin, max 5.
- Prompt context: user preferences from learned rules + task deadlines + calendar awareness (for the "next ~2 hours" heuristic).
- Fallback on AI failure: keep current pinned state unchanged, log warning.

Pinned set is rewritten each cycle (no manual pin by user in v1).

## Card Actions

### Actionable card
- `[open]` — deep link (Slack URL / Jira issue / Google Calendar event)
- `[snooze]` — dropdown: 1h / till tomorrow morning / till Monday
- `[dismiss]` — marks dismissed, records as negative signal for learning
- `[create task]` — opens the existing task-creation sheet prefilled with inbox context
- `👍` / `👎` — optional explicit feedback (see Learning)

### Ambient card
- `[open]` — deep link
- `[dismiss]` — marks dismissed, negative signal
- `👍` / `👎` — optional feedback

## Learning System

Three layers: implicit patterns (L1), explicit feedback (L2), editable rules page (L3). Scoped to Inbox only in v1.

### DB tables

```sql
-- L1 + L2: auto-derived + explicit feedback
CREATE TABLE inbox_learned_rules (
    id INTEGER PRIMARY KEY,
    rule_type TEXT NOT NULL CHECK (rule_type IN (
        'source_mute', 'source_boost',
        'trigger_downgrade', 'trigger_boost'
    )),
    scope_key TEXT NOT NULL,        -- serialized: "sender:U123", "channel:C456", "jira_label:infra", "trigger:jira_comment_watching"
    weight REAL NOT NULL,            -- -1.0 (mute) .. +1.0 (boost)
    source TEXT NOT NULL CHECK (source IN ('implicit', 'explicit_feedback', 'user_rule')),
    evidence_count INTEGER NOT NULL DEFAULT 0,
    last_updated TEXT NOT NULL,
    UNIQUE(rule_type, scope_key)
);

-- L2: raw feedback events for audit and re-learning
CREATE TABLE inbox_feedback (
    id INTEGER PRIMARY KEY,
    inbox_item_id INTEGER NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
    rating INTEGER NOT NULL CHECK (rating IN (-1, 1)),
    reason TEXT CHECK (reason IN ('', 'source_noise', 'wrong_priority', 'wrong_class', 'never_show')),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_inbox_feedback_item ON inbox_feedback(inbox_item_id);
```

### L1 — Implicit pattern learning

Runs at the end of each daemon cycle in `learnImplicitRules()`, pure SQL aggregates:

- For each `sender_id` over the last 30 days: if `dismiss_rate > 80%` with `evidence_count ≥ 5` → insert/update `source_mute` with weight `-0.7`.
- For each `sender_id` over 30d: if `responded_within_10min_rate > 80%` with `evidence_count ≥ 5` → `source_boost` weight `+0.5`.
- Channel-level: `dismiss_rate > 70%` with ≥5 events → `source_mute` weight `-0.5`.
- Jira-label-level: analogous.

All upserts use last-write-wins (no decay).

### L2 — Explicit feedback

On each card the user can tap 👍 or 👎. A 👎 opens a dropdown with four reasons:

- **"Source is usually noise"** → `source_mute` weight `-0.8` on `sender_id` or `channel_id`.
- **"Wrong priority"** → downgrade class on future items from this source (actionable → ambient for this `trigger_type` + `scope_key`).
- **"Wrong class"** → direct reclassification for this exact item + learning signal for future.
- **"Never show me this"** → `source_mute` weight `-1.0` (hard filter — item is never surfaced again from this source).

Each feedback event inserts into `inbox_feedback` and updates `inbox_learned_rules`.

### L3 — Rules page (inside Inbox as "Learned" tab)

A tab within the Inbox view showing three sections:

- **Mutes** — rules with negative weight (auto-derived and manual), with remove button.
- **Boosts** — rules with positive weight, with remove button.
- **Manual rules** — user-added rules, with `[+ Add]` button to create a new rule (scope + weight).

Each row shows `scope_key`, `weight`, `source`, and a remove button. Users can edit weight inline.

**Adding a manual rule** (via `[+ Add]`):
- Scope type picker: sender / channel / jira_label / trigger_type
- Scope value (searchable dropdown pulled from known senders/channels, or free text for labels)
- Rule type: mute / boost / downgrade class / upgrade class
- Weight slider: -1.0 to +1.0 (defaults to ±0.8 based on rule type sign)
- Saves with `source = 'user_rule'`. Manual rules always win over implicit ones with the same `scope_key` (last-write-wins; manual write stamps `source='user_rule'` and implicit learner won't overwrite it — the learner skips scopes already marked `user_rule`).

### How it flows into AI prompts

`inbox.prioritize` and `inbox.select_pinned` receive a `=== USER PREFERENCES ===` block generated at prompt-assembly time:

```
=== USER PREFERENCES ===
Mutes: dependabot bot (strong), #deploy channel (moderate), jira-label:infra (moderate)
Boosts: @alice messages (strong), jira-label:blocker (strong)
Rules: max 5 pinned; prefer actionable class for boss messages

Apply these when assigning priority and selecting pinned items.
```

The prompt includes the top 20 rules ranked by relevance to the current batch (by matching scope against items' senders/channels/labels), preventing prompt bloat.

## Pipeline (internal/inbox/pipeline.go)

The existing `Pipeline` is extended with new detectors and phases.

```go
type Pipeline struct {
    db        *sql.DB
    generator digest.Generator
    jira      *jira.Client       // NEW (optional — nil if Jira disabled)
    calendar  *calendar.Client   // NEW (optional — nil if Calendar disabled)
    // ...existing fields
}

func (p *Pipeline) Run(ctx context.Context) error {
    // Phase 1: detection (SQL-only, no AI)
    p.detectSlackTriggers(ctx)        // existing
    p.detectJiraTriggers(ctx)         // NEW
    p.detectCalendarTriggers(ctx)     // NEW
    p.detectWatchtowerTriggers(ctx)   // NEW

    // Phase 2: classification
    p.classifyNewItems(ctx)           // rule-based default class per trigger_type

    // Phase 3: learning
    p.learnImplicitRules(ctx)         // NEW — SQL aggregates → inbox_learned_rules

    // Phase 4: AI — separate calls
    p.aiPrioritizeNewItems(ctx)       // extended: includes user preferences block
    p.aiSelectPinned(ctx)             // NEW — separate call, max 5 pinned

    // Phase 5: auto-resolve + auto-archive
    p.autoResolveByRules(ctx)         // existing CheckUserReplied + per-source rules
    p.autoArchiveExpired(ctx)         // NEW — ambient >7d, actionable stale >14d

    // Phase 6: unsnooze expired
    p.unsnoozeExpired(ctx)            // existing
    return nil
}
```

### Detectors (all read already-synced data — no extra polling)

- **Jira detector** — reads `jira_issues` and `jira_comments` where `updated_at > inbox_last_processed_ts`. Matches `assignee == me`, `watchers contains me`, or `comment_text contains @me`. Emits the corresponding trigger_type.
- **Calendar detector** — reads `calendar_events` where `created_at > inbox_last_processed_ts` or `updated_at > last_processed_ts`. Classifies into invite / time_change / cancelled based on RSVP status and field deltas.
- **Watchtower detector** — reads `digests` with `created_at > last_processed_ts` and extracts situations marked high-importance as `decision_made` items. Reads `briefings` for today → emits single `briefing_ready` item when new.

Detectors return non-fatal errors (logged, pipeline continues). Missing optional clients (Jira, Calendar) short-circuit cleanly.

### Daemon integration

Order within the daemon cycle:

1. Slack sync
2. Jira sync
3. Calendar sync
4. Digest pipeline
5. Tracks pipeline
6. People pipeline
7. **Inbox pipeline** ← runs here, so Watchtower detector sees fresh digests
8. Briefing pipeline (can read Inbox state in v2 merge)

### AI call budget

Per daemon cycle: ~2 calls (`inbox.prioritize` + `inbox.select_pinned`). `learnImplicitRules` and all detectors are pure SQL.

## DB Schema Migration (v66 → v67)

```sql
-- Extend inbox_items
ALTER TABLE inbox_items ADD COLUMN item_class TEXT NOT NULL DEFAULT 'actionable'
    CHECK (item_class IN ('actionable', 'ambient'));
ALTER TABLE inbox_items ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbox_items ADD COLUMN archived_at TEXT;
ALTER TABLE inbox_items ADD COLUMN archive_reason TEXT
    CHECK (archive_reason IN ('', 'resolved', 'seen_expired', 'stale', 'dismissed'));

-- Expand trigger_type CHECK to include new sources
-- SQLite limitation: CHECK constraints can't be altered directly.
-- Strategy: drop-and-recreate inbox_items in migration (preserve data), OR
-- relax the CHECK to no whitelist (since validation now lives in Go code).
-- Chosen: relax CHECK — rely on Go-side validation in Pipeline.

-- Backfill existing items
UPDATE inbox_items SET item_class = 'actionable'
    WHERE trigger_type IN ('mention', 'dm', 'thread_reply');
UPDATE inbox_items SET item_class = 'ambient'
    WHERE trigger_type = 'reaction';

-- New tables
CREATE TABLE inbox_learned_rules (...);  -- see Learning section
CREATE TABLE inbox_feedback (...);        -- see Learning section

-- Indexes
CREATE INDEX idx_inbox_items_class_status ON inbox_items(item_class, status);
CREATE INDEX idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1;
CREATE INDEX idx_inbox_items_archived ON inbox_items(archived_at);
CREATE INDEX idx_inbox_learned_rules_scope ON inbox_learned_rules(rule_type, scope_key);
```

Migration is idempotent and preserves all existing inbox data.

## Desktop UI (WatchtowerDesktop/)

### Views

- **`InboxFeedView`** — replaces `InboxListView`. Sections: `PinnedSection` (up to 5 cards), `FeedSection` (infinite-scroll paginated). Day dividers inside the feed.
- **`InboxCardView`** — single card renderer with three size variants: `.compact` (ambient), `.medium` (actionable non-pinned), `.pinned` (full).
- **`InboxLearnedRulesView`** — "Learned" tab inside Inbox. Lists mutes/boosts/manual rules, edit/remove inline, "Add rule" sheet.
- **`InboxFeedbackSheet`** — dropdown-style sheet for 👎 reason selection.

### Models / Queries

- `InboxItem` model extended with `itemClass: ItemClass` (enum: `.actionable`, `.ambient`), `pinned: Bool`, `archivedAt: Date?`, `archiveReason: String`.
- `InboxLearnedRule` model and `InboxLearnedRulesQueries` for CRUD.
- `InboxFeedbackQueries` for writing feedback + upserting rules atomically.

### ViewModel

`InboxViewModel` (existing, extended):
- `pinnedItems: [InboxItem]` — `WHERE pinned = 1 AND status = 'pending'`
- `feedItems: [InboxItem]` — paginated, `WHERE pinned = 0 AND archived_at IS NULL`
- `markAsSeen(item)` — sets `read_at` when card becomes visible
- `submitFeedback(item, rating, reason)` — writes to DB, no UI re-fetch needed (reactive via `ValueObservation`)

### Sidebar badge

- Badge count = `unread_pending`
- Red dot if any pinned item has `priority = 'high'`

### Removal

`InboxListView` (sender-grouped) is deleted. No toggle, no legacy path.

## Error Handling

- **Jira/Calendar API down:** detector logs warning, returns nil; pipeline continues. Next cycle catches up.
- **AI call fail in `aiSelectPinned`:** keep current pinned state, log warning.
- **AI returns invalid JSON:** log parse error, priorities stay at rule-based defaults, pipeline does not fail.
- **Corrupted `inbox_learned_rules` row** (unparseable `scope_key`): rule ignored with warning, not fatal.
- **Dirty DB on v67 migration:** if an existing `trigger_type` is unrecognized, it is preserved as-is (CHECK relaxed); Go-side classifier treats unknown types as `ambient` with low priority.
- **Missing optional integrations** (Jira or Calendar not configured): corresponding detector short-circuits on nil client; no items of those trigger_types are produced.

## Performance

- Max new items detected per cycle: 100 (existing limit, retained).
- Feed pagination: `LIMIT 50 OFFSET N` on `WHERE pinned = 0 AND archived_at IS NULL AND status NOT IN ('resolved', 'dismissed', 'snoozed') ORDER BY created_at DESC`.
- Pinned query: `WHERE pinned = 1 AND status = 'pending'` uses partial index, always O(≤5).
- Learned rules passed to prompt: top 20 rules whose `scope_key` matches any sender / channel / jira-label / trigger_type in the current batch. If fewer than 20 match, include highest-weight global rules to pad.
- Auto-archive: single SQL per cycle, typically affects < 20 rows.

## Observability

Per daemon cycle log line:

```
inbox: +12 new (6 slack, 3 jira, 2 calendar, 1 internal), 3 pinned, 5 auto-resolved, 2 auto-archived, 18 learned-rule-updates
```

Feedback events log:

```
inbox_feedback: item=42 rating=-1 reason=never_show → rule source_mute sender:U789 weight=-1.0
```

## Testing

### Go backend

- `TestSlackDetector` (existing coverage retained)
- `TestJiraDetector` — mock `jira.Client`, assert correct `inbox_items` rows for each trigger_type
- `TestCalendarDetector` — mock calendar events, assert invite/time_change/cancelled classification
- `TestWatchtowerDetector` — seed `digests` with high-importance situations → expect `decision_made` items; seed fresh `briefings` → expect `briefing_ready`
- `TestClassifier_DefaultRules` — every trigger_type maps to expected default class
- `TestClassifier_AIDowngrade` — mock generator returns downgrade decision → item becomes ambient; AI upgrade attempt is ignored
- `TestLearnImplicitRules_MuteThreshold` — seed 10 dismiss events from sender X → `source_mute` rule created with `weight=-0.7`
- `TestLearnImplicitRules_BoostThreshold` — seed 8 fast-reply events → `source_boost` created
- `TestLearnImplicitRules_InsufficientEvidence` — 3 events < 5 threshold → no rule
- `TestAutoArchive_AmbientExpiry` — ambient 8d old → `archived_at` set, reason `seen_expired`
- `TestAutoArchive_ActionableStale` — actionable 15d no activity → archived with reason `stale`
- `TestAISelectPinned_MaxFive` — seed 20 high-priority actionable → AI called with all, output capped at 5
- `TestAISelectPinned_RespectLearnedRules` — muted source in learned rules → no items from that source appear in pinned, even if AI suggests them
- `TestAISelectPinned_Fallback` — AI call errors → existing `pinned=1` items retained, no changes
- `TestFeedbackFlow_NeverShow` — 👎 with reason `never_show` → creates `source_mute` weight `-1.0` → next item from same sender is filtered out
- `TestFeedbackFlow_WrongPriority` — 👎 with reason `wrong_priority` → downgrades class on future items from that source
- `TestAutoResolve_Jira` — user adds comment in Jira → `jira_comment_mention` item for that issue auto-resolves
- `TestAutoResolve_Calendar` — user responds to invite → `calendar_invite` auto-resolves
- `TestMigration_v67_Backfill` — existing items get correct default `item_class`; `pinned=0`, `archived_at=NULL`
- `TestMigration_v67_UnknownTriggerType` — pre-existing row with unexpected trigger_type survives migration, classified as ambient at runtime

### Desktop (Swift)

- `InboxViewModelTests` — pinned vs feed split, sort order, pagination
- `InboxFeedbackTests` — 👍/👎 persists to DB, triggers `ValueObservation` refresh
- `InboxLearnedRulesViewModelTests` — CRUD for manual rules, inline edit weight
- `InboxCardViewSnapshotTests` — snapshot each card size (compact / medium / pinned)
- `InboxSidebarBadgeTests` — unread count + red dot logic
- `InboxAutoSeenTests` — scrolling past an ambient item sets `read_at`

Test coverage target: high — every new detector, every classifier rule, every auto-resolve path, every learning path has a test.

## Migration from Current Inbox

Users currently with active `inbox_items`:

1. Migration runs on first daemon start with v67 code.
2. All existing items backfilled with `item_class` per rule table; `pinned=0`; `archived_at=NULL`.
3. Next daemon cycle runs AI prioritization + pinned selection on open items, populates pinned naturally.
4. Old `InboxListView` removed; Feed UI is the only view.
5. No data loss; no user action required.

## Open Questions Deferred to v2

- Merge Briefing.attention with Inbox pinned once real-world usage shows whether they duplicate.
- Decide whether Inbox becomes the startup screen, or introduce a time-of-day switcher (Briefing morning → Inbox rest of day).
- Weekly calibration session (L4 learning).
- Silent signals (PR stale, my-question-no-answer, ignored ping).
- Blocker-cleared positive signals.
- Smart batching of multi-event Jira/Slack spans into a single card.
- Cross-system correlations (Jira WT-123 ↔ Slack thread by URL).
- Extending learning system beyond Inbox.

## File/Module Touchpoints

- `internal/inbox/pipeline.go` — extended with new detectors, classifier, learner, pinned selector
- `internal/inbox/jira_detector.go` — new
- `internal/inbox/calendar_detector.go` — new
- `internal/inbox/watchtower_detector.go` — new
- `internal/inbox/classifier.go` — new, rule-based default class
- `internal/inbox/learner.go` — new, implicit rule aggregation
- `internal/inbox/feedback.go` — new, feedback write + rule upsert
- `internal/inbox/prompts/prioritize.tmpl` — extended with USER PREFERENCES block
- `internal/inbox/prompts/select_pinned.tmpl` — new
- `internal/db/schema.sql` — v67 tables + columns
- `internal/db/migrations/067_inbox_pulse.sql` — new migration
- `internal/db/inbox.go` — extended with new queries (learned rules, feedback, archive)
- `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedView.swift` — new, replaces InboxListView
- `WatchtowerDesktop/Sources/Views/Inbox/InboxCardView.swift` — new
- `WatchtowerDesktop/Sources/Views/Inbox/InboxLearnedRulesView.swift` — new
- `WatchtowerDesktop/Sources/Views/Inbox/InboxFeedbackSheet.swift` — new
- `WatchtowerDesktop/Sources/ViewModels/InboxViewModel.swift` — extended
- `WatchtowerDesktop/Sources/ViewModels/InboxLearnedRulesViewModel.swift` — new
- `WatchtowerDesktop/Sources/Database/Queries/InboxQueries.swift` — extended
- `WatchtowerDesktop/Sources/Database/Queries/InboxLearnedRulesQueries.swift` — new
- `WatchtowerDesktop/Sources/Database/Queries/InboxFeedbackQueries.swift` — new
- `WatchtowerDesktop/Sources/Models/InboxItem.swift` — extended
- `WatchtowerDesktop/Sources/Models/InboxLearnedRule.swift` — new
- Removal: `WatchtowerDesktop/Sources/Views/Inbox/InboxListView.swift`
