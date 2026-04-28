# INBOX-04 — Gradual Explicit Feedback Learning

**Date:** 2026-04-28
**Status:** Design approved, ready for implementation plan
**Owner:** Vadym
**Closes gap on:** `docs/inventory/inbox-pulse.md` INBOX-04 (Partial → Enforced)
**DB version bump:** 67 → 68

## Problem

Inventory contract `INBOX-04` says Inbox learns gradually — a single 👎 is one signal in a pool, not an instant kill switch. Today `feedback.go::SubmitFeedback` violates that for explicit feedback: a single `(-1, never_show)` immediately upserts `source_mute` weight `-1.0`; a single `(-1, source_noise)` immediately upserts `-0.8`; etc. The implicit learner (dismissals) is gradual, but explicit feedback bypasses it.

Result: the user gets two contradictory experiences — clicking 👎 once silences a source forever, while ignoring messages takes weeks of repeated dismissals to do the same.

## Goals

- Make `(-1, source_noise)`, `(-1, wrong_priority)`, `(-1, wrong_class)`, `(+1, "")` accumulate as evidence in a unified pool with implicit dismissals/fast-replies.
- Keep `(-1, never_show)` as a deliberate one-click escape hatch (creates `source='user_rule'` weight `-1.0` immediately) — distinct UI affordance for "I really mean it".
- Keep per-item correction on `(-1, wrong_class)` (sets THIS item's class to `ambient` immediately) — that part is per-item correction, not learning.
- Single source of truth for automatic rules: `learner` becomes the only writer of `source='implicit'` rules. `feedback.go` writes only raw rows to `inbox_feedback` and the one-shot `user_rule` for `never_show`.
- Migrate cleanly: existing `source='explicit_feedback'` rules in DB are removed (they were derived under the old instant logic and no longer reflect the contract); new rules emerge organically on subsequent daemon cycles.
- Promote `INBOX-04` from Partial to Enforced: close the inventory tracked gap.

## Non-Goals

- No decay over time. Last-write-wins on `inbox_learned_rules` stays.
- No re-tuning of the implicit learner's behaviour for dismiss-only signal — only the pool is broadened.
- No change to UI/UX wording on the feedback sheet (cards still show 👍/👎 + 4 reasons). Only behaviour shifts.
- No retroactive re-derivation of `source='implicit'` rules over historical `inbox_feedback`. New rules emerge only from the next daemon cycle's 30-day window.
- INBOX-08 (anti re-spam) is a separate plan and is not addressed here.

## Concept

The model has two paths into `inbox_learned_rules`:

```
                 explicit feedback                  implicit signals
                 (👍/👎 from card)                  (dismissals, fast replies)
                       │                                   │
                       ▼                                   │
                 ┌─────────────────┐                       │
                 │  inbox_feedback │  ◄── append-only      │
                 └─────────────────┘                       │
                       │                                   │
       ┌───────────────┼───────────────┐                   │
       │               │               │                   │
   never_show     wrong_class      others                  │
   (instant)     (per-item only)  (no rule write)         │
       │               │               │                   │
       │               ▼               │                   │
       │      inbox_items.item_class   │                   │
       │      = 'ambient'              │                   │
       │                               │                   │
       │                               └───────┬───────────┘
       │                                       │
       │                                       ▼
       │                              ┌──────────────────┐
       │                              │ learnImplicit    │
       │                              │ Rules (extended) │
       │                              └──────────────────┘
       │                                       │
       ▼                                       ▼
inbox_learned_rules               inbox_learned_rules
source='user_rule'                source='implicit'
weight=-1.0                       weight=±0.7 (sender) / ±0.5 (channel)
```

The learner aggregates negative and positive events across both `inbox_items` (dismiss / fast-reply markers) and `inbox_feedback` (explicit ratings) per-sender, per-channel, per-jira-label. Thresholds (`evidence ≥ 5`, `rate > 70%`) decide when to materialise a rule. `source='user_rule'` rows are skipped (INBOX-06).

## Behavior — `feedback.go::SubmitFeedback`

Refactored mapping:

| `(rating, reason)` | Effect |
|---|---|
| `(-1, never_show)` | (1) `inbox_feedback` row appended. (2) Upsert `inbox_learned_rules`: `rule_type=source_mute`, `scope_key='sender:'+sender_id`, `weight=-1.0`, **`source='user_rule'`**. **No item mutation.** |
| `(-1, source_noise)` | `inbox_feedback` row appended. **No rule write.** |
| `(-1, wrong_priority)` | `inbox_feedback` row appended. **No rule write.** |
| `(-1, wrong_class)` | (1) `inbox_feedback` row appended. (2) If item's current `item_class='actionable'` → set to `'ambient'`. **No rule write.** |
| `(+1, "")` | `inbox_feedback` row appended. **No rule write.** |

Key invariants:

- The raw `inbox_feedback` row is always written **before** any other side effect, so audit trail is intact even if downstream operations fail.
- The only writer of `inbox_learned_rules.source='user_rule'` from this code path is `(-1, never_show)`. Manual rule edits in the Learned tab also write `source='user_rule'`; they coexist.
- For `wrong_class` only the per-item class flip remains. The previous `trigger_downgrade` rule write is removed.

## Behavior — `learner.go::RunImplicitLearner`

Extended aggregation. Window stays 30 days.

**Per-sender negative pool** (combined for `source_mute`):

```sql
WITH events AS (
  SELECT sender_user_id AS sender, 1 AS is_negative
    FROM inbox_items
   WHERE status='dismissed' AND updated_at >= :since
  UNION ALL
  SELECT i.sender_user_id AS sender, 1 AS is_negative
    FROM inbox_feedback f
    JOIN inbox_items i ON i.id = f.inbox_item_id
   WHERE f.rating = -1 AND f.reason != 'never_show' AND f.created_at >= :since
  UNION ALL
  -- positive counter-events (resolved-without-dismiss + 👍) for rate calc
  SELECT sender_user_id AS sender, 0 AS is_negative
    FROM inbox_items
   WHERE status='resolved' AND updated_at >= :since
  UNION ALL
  SELECT i.sender_user_id AS sender, 0 AS is_negative
    FROM inbox_feedback f
    JOIN inbox_items i ON i.id = f.inbox_item_id
   WHERE f.rating = +1 AND f.created_at >= :since
)
SELECT sender,
       COUNT(*) AS total,
       SUM(is_negative) AS negatives
  FROM events
 GROUP BY sender
HAVING total >= 5 AND negatives * 1.0 / total > 0.70
```

For each row: upsert `inbox_learned_rules` with `rule_type='source_mute'`, `scope_key='sender:'+sender`, `weight=-0.7`, `source='implicit'`, `evidence_count=total`.

**Per-sender positive pool** (combined for `source_boost`):

Symmetric. Positives = fast replies (`responded_within_10min` from existing logic) ∪ `(+1)` ratings. Negatives = dismissals ∪ `(-1)` ratings (excluding `never_show`). Threshold: `total ≥ 5 ∧ positives/total > 0.70`. Weight: `+0.7` (raised from current `+0.5` for symmetry with mute).

**Channel-level and Jira-label-level pools:** analogous. Existing logic for channels (`dismiss_rate > 70%` over channel events → `source_mute -0.5`) is extended by JOINing `inbox_feedback` to capture explicit ratings on items belonging to the channel. Same for `jira_label:*`.

**INBOX-06 protection:** scope_keys whose existing rule has `source='user_rule'` are skipped (current behaviour, retained).

**Edge case:** if a scope has both `source='implicit'` (from prior cycles) and qualifies for an updated rule this cycle, the upsert overwrites with the latest weight and `evidence_count`. No user_rule is touched.

## Migration v68

```sql
-- Drop legacy explicit_feedback rules — they were derived under instant logic.
-- user_rule and implicit rows are preserved.
DELETE FROM inbox_learned_rules WHERE source = 'explicit_feedback';

-- Schema check: source CHECK constraint already allows ('implicit','explicit_feedback','user_rule').
-- We do NOT drop 'explicit_feedback' from the enum (historical inbox_feedback rows reference no
-- foreign-keyed source, so this is safe to leave; new code never writes it).
```

The migration is idempotent (a no-op if already applied). It does not touch `inbox_feedback` rows — the audit log is preserved and is exactly the data the new learner reads.

After migration, the next daemon cycle's `RunImplicitLearner` re-derives `source='implicit'` rules from the unified pool. Users with sparse activity get fewer rules than before — that is correct, because the old instant rules were not justified by gradual evidence.

## Touchpoints

- `internal/inbox/feedback.go` — rewrite the `switch` mapping. Remove `UpsertLearnedRule` calls except for `never_show`. Remove `trigger_downgrade` write in `wrong_class`. Keep `RecordInboxFeedback` and the per-item `SetInboxItemClass` for `wrong_class`.
- `internal/inbox/learner.go` — extend SQL queries to UNION `inbox_feedback` events into the negative/positive pools. Adjust threshold from `> 0.80` to `> 0.70`. Raise sender boost weight from `+0.5` to `+0.7`. Channel and jira-label paths get the same UNION extension.
- `internal/db/migrations/068_inbox_drop_legacy_explicit_feedback_rules.sql` — new migration file (or inline SQL in the existing migration runner, matching project convention).
- `internal/db/db.go` — bump schema version constant from 67 to 68 if such a constant exists; otherwise the migration runner picks it up by filename.
- `docs/inventory/inbox-pulse.md` — INBOX-04 entry: Status `Partial → Enforced`; remove "Tracked gap" section; expand Test guards; bump `Locked since` to 2026-04-28; append Changelog line.
- `internal/inbox/feedback_test.go` — keep `TestFeedback_NeverShow_CreatesHardMute` (rename to follow convention if needed; semantics unchanged), DELETE the assertions on rule creation in `TestFeedback_SourceNoise_*`, `TestFeedback_WrongClass_*`, `TestFeedback_WrongPriority_*`, `TestFeedback_PositiveBoost_*` (since rules are no longer written there). Replace those assertions with checks that `inbox_feedback` row is written and no `inbox_learned_rules` row appears.
- `internal/inbox/learner_test.go` — add tests for the unified pool (see Testing section).
- `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift` — Swift `record(rating:reason:...)` mirror logic must follow the same refactor: existing tests `testRecordSourceNoiseCreatesMuteRuleWeight08`, `testRecordWrongClassSetsAmbientAndCreatesDowngradeRule`, `testRecordWrongPriorityCreatesDowngradeRuleOnSender`, `testRecordPositiveRatingCreatesBoostRule` must be updated to assert the new behaviour (no rule created for non-never_show ratings; per-item ambient flip kept for wrong_class). `testRecordNeverShowCreatesMuteRuleWeight1` keeps semantics but now the rule has `source='user_rule'` instead of `source='explicit_feedback'`.

## Testing

New Go guard tests for INBOX-04 (extending the existing `TestInbox04_*` set):

- `TestInbox04_NeverShowStillInstantHardMute` — `(-1, never_show)` creates `source_mute -1.0` with **`source='user_rule'`** in one call. Locks the escape hatch.
- `TestInbox04_SourceNoiseDoesNotCreateRule` — `(-1, source_noise)` writes one `inbox_feedback` row, zero `inbox_learned_rules` rows.
- `TestInbox04_WrongClassChangesItemButNotRule` — `(-1, wrong_class)` flips item to `ambient`, writes one `inbox_feedback` row, zero rule rows.
- `TestInbox04_WrongPriorityDoesNotCreateRule` — symmetric.
- `TestInbox04_PositiveFeedbackDoesNotCreateRule` — `(+1, "")` writes one `inbox_feedback` row, zero rule rows.
- `TestInbox04_LearnerAggregatesExplicitWithImplicit` — seed 3 dismissed items + 2 `(-1, source_noise)` feedback rows for sender X → learner produces `source_mute -0.7` `source='implicit'` `evidence_count=5` (or matching the chosen pool size).
- `TestInbox04_LearnerNoRuleBelowCombinedThreshold` — 2 dismissals + 1 feedback (total 3) → no rule.
- `TestInbox04_LearnerPositiveBoostFromExplicit` — 5 `(+1, "")` rows over 30d, no negative events → `source_boost +0.7` `source='implicit'`.
- `TestInbox04_LearnerSkipsUserRuleScope` — pre-existing `source='user_rule'` for sender X; even with 10 dismissals + 5 negative feedback, no overwrite. (Re-asserts INBOX-06; intentional belt-and-braces.)
- `TestInbox04_MigrationDropsExplicitFeedbackRules` — pre-seed 3 rules with `source='explicit_feedback'`, 1 with `source='implicit'`, 1 with `source='user_rule'`. Run migration v68. Assert: 2 rules remain, both non-explicit_feedback.

Existing tests to update:

- `TestInbox04_GradualMuteFromAccumulatedDismissals` — keep, threshold updated to 70%; weight stays `-0.7`.
- `TestInbox04_NoRuleBelowEvidenceThreshold` — keep, semantics unchanged.

Swift tests to update (in `InboxLearnedRulesQueriesTests.swift`):

- `test_INBOX_06_manual_rule_overrides_implicit` — unchanged (still passes).
- `testRecordNeverShowCreatesMuteRuleWeight1` — assert `source='user_rule'` (was `explicit_feedback`).
- `testRecordSourceNoiseCreatesMuteRuleWeight08` — invert: assert NO rule created, only feedback row.
- `testRecordWrongClassSetsAmbientAndCreatesDowngradeRule` — split into two: `…SetsAmbient` (kept) and `…CreatesDowngradeRule` deleted.
- `testRecordWrongPriorityCreatesDowngradeRuleOnSender` — invert: no rule.
- `testRecordPositiveRatingCreatesBoostRule` — invert: no rule.

These updates are part of this plan, not a follow-up.

## Error Handling

- Migration v68 runs once; if `inbox_learned_rules` is empty or has no `explicit_feedback` rows, the DELETE is a no-op (safe to re-run).
- If `inbox_feedback` is empty, the learner's UNIONs reduce to the existing dismiss-only pool — backward-compatible behaviour.
- If a sender has 4 dismissals + 1 `(-1, never_show)` row, the learner counts 4 (excludes never_show explicitly via `WHERE reason != 'never_show'`) — no new rule, but the user_rule from never_show stands. Verified by `TestInbox04_LearnerNoRuleBelowCombinedThreshold` plus `TestInbox04_NeverShowStillInstantHardMute` together.
- If `feedback.go` returns an error before the rule write (e.g. DB busy on `inbox_feedback` insert), no rule is created — the contract holds.
- Concurrency: `feedback.go` is invoked from CLI/Desktop on user action; learner runs in daemon cycle. Race between feedback write and learner read is acceptable — learner uses a 30-day window snapshot per cycle, drift is fine.

## Performance

- Learner queries grow by one UNION ALL each — explicit events are small (typically <50 per user per 30d). No expected impact.
- The existing index on `inbox_feedback(inbox_item_id)` is sufficient for the JOIN. No new index required.
- Migration v68 is one DELETE; bounded by current `inbox_learned_rules` row count (typically <100). O(rows).

## INBOX-04 Inventory Update

After implementation lands, the inventory entry becomes:

```markdown
## INBOX-04 — Inbox learns gradually, not by single click

**Status:** Enforced

**Observable:** [unchanged — same product description]

**Why locked:** [unchanged]

**Test guards:**
- `internal/inbox/learner_test.go::TestInbox04_GradualMuteFromAccumulatedDismissals`
- `internal/inbox/learner_test.go::TestInbox04_NoRuleBelowEvidenceThreshold`
- `internal/inbox/learner_test.go::TestInbox04_LearnerAggregatesExplicitWithImplicit`
- `internal/inbox/learner_test.go::TestInbox04_LearnerNoRuleBelowCombinedThreshold`
- `internal/inbox/learner_test.go::TestInbox04_LearnerPositiveBoostFromExplicit`
- `internal/inbox/feedback_test.go::TestInbox04_NeverShowStillInstantHardMute`
- `internal/inbox/feedback_test.go::TestInbox04_SourceNoiseDoesNotCreateRule`
- `internal/inbox/feedback_test.go::TestInbox04_WrongClassChangesItemButNotRule`
- `internal/inbox/feedback_test.go::TestInbox04_WrongPriorityDoesNotCreateRule`
- `internal/inbox/feedback_test.go::TestInbox04_PositiveFeedbackDoesNotCreateRule`

**Locked since:** 2026-04-28
```

Tracked gap line is removed entirely. Changelog entry added: `2026-04-28: INBOX-04 closed gap — explicit feedback now accumulates via learner; never_show stays as one-click escape hatch (source='user_rule').`

## Acceptance Criteria

- All `TestInbox04_*` tests (existing + new, ≥7 Go tests) pass.
- All updated Swift tests pass; the Swift mirror logic in `InboxFeedbackQueries.record(...)` matches Go.
- After migration v68 on a fresh-from-master database, no rows in `inbox_learned_rules` have `source='explicit_feedback'`.
- `feedback.go::SubmitFeedback` calls `UpsertLearnedRule` only in the `never_show` branch.
- `docs/inventory/inbox-pulse.md` INBOX-04 entry has Status `Enforced`, no `Tracked gap` section, and a Changelog entry for the closure.
- `make test` clean.
- `git grep -n 'BEHAVIOR INBOX-04'` returns at least 5 matches (one per new + existing guard test).

## Open Questions

None. All design choices were nailed during brainstorm:

- One-click escape hatch: keep, via `source='user_rule'`.
- Aggregation model: unified pool through learner.
- Channel and jira-label paths: extended symmetrically.
- Existing `explicit_feedback` rules: deleted on migration; not re-derived from history.
- Thresholds: 70% rate, 5 evidence; weights ±0.7 sender, ±0.5 channel.
- `wrong_class` per-item flip: kept.
- Decay over time: not in scope.
