# Behavior Inventory — Design

**Date:** 2026-04-27
**Status:** Design approved, ready for implementation plan
**Owner:** Vadym
**Pilot module:** Inbox Pulse (`internal/inbox/` + `WatchtowerDesktop/Sources/Views/Inbox/`)

## Problem

Watchtower has many business modules (Inbox Pulse, Digest, Tracks, People, Briefing, Day Plan, Tasks, Calendar, Meeting, Targets). Each carries a small set of behaviors that are the "soul" of the module — the things that, if changed silently, would break the product promise even though the code still compiles and tests pass elsewhere. Today nothing prevents quiet drift: a test gets relaxed, a weight gets re-tuned, a fallback path gets removed, and the module degrades without anyone noticing.

The owner needs an explicit, auditable list of these contracts per module, with guard tests and a discovery protocol that the AI assistant respects in every session.

## Goals

- Identify and document the small set of **behavioral contracts** (user-observable invariants) per business module.
- Protect each contract with at least one explicit guard test that fails loudly if the behavior breaks.
- Make the inventory discoverable by the AI assistant so it stops before silently weakening a contract.
- Keep the system lightweight: no pre-commit hooks, no CI gates, no special tooling. Defense in depth via documentation visibility, test naming, and AI session protocol.
- Pilot the format on Inbox Pulse, then roll out to remaining modules using the same template.

## Non-Goals

- Not a substitute for design specs in `docs/superpowers/specs/` or feature specs in `docs/specs/`. Inventory captures *what must not change*, not *how the module works*.
- Not a CI / governance enforcement system. The barrier is intentionally soft; rigor comes from visibility and the AI session protocol.
- Not a comprehensive test plan. Inventory tests are guards, not coverage.
- Not a replacement for `CLAUDE.md` project instructions. Inventory is referenced from `CLAUDE.md` but lives separately.
- No automatic generation of inventory entries from code. Curation is a human (and assisted-by-AI) judgement call about what is essential vs incidental.

## Concept

Each business module gets one markdown file in `docs/inventory/<module>.md` listing 5–10 numbered behavioral contracts. Each contract has a stable ID (`INBOX-01`, `DIGEST-03`, etc.), names what the user observes, and points to one or more guard tests in `internal/<module>/` or `WatchtowerDesktop/Sources/.../<Module>Tests/`.

The AI assistant is instructed (via `CLAUDE.md`) to read the relevant inventory file before touching code in a covered module and to stop and ask before any change that would weaken a guard test.

## File Layout

```
docs/inventory/
├── README.md                  # index + protocol overview
├── inbox-pulse.md             # pilot
├── digest.md                  # rolled out next
├── tracks.md
├── briefing.md
├── people.md
├── day-plan.md
├── tasks.md
├── calendar.md
├── meeting.md
└── targets.md
```

`README.md` contains a single table mapping module → inventory file → primary code paths the file covers, plus a one-paragraph statement of the protocol so a fresh reader (human or AI) understands the system without context.

## Per-Module File Format

Each `<module>.md` has three parts: header preamble, feature list, changelog.

### Header preamble

```markdown
# Behavior Inventory — Inbox Pulse

> Each item below is a **behavioral contract** that must be preserved.
> Modifying or weakening the protecting test requires explicit approval
> from @Vadym.
>
> AI assistant: when working in `internal/inbox/` or
> `WatchtowerDesktop/Sources/Views/Inbox/`, read this file first. Any
> proposed change that would break a guard test or remove a contract
> must be raised as a question before touching code.

**Module:** internal/inbox + WatchtowerDesktop/Sources/Views/Inbox
**Last full audit:** 2026-04-27
```

### Feature entry schema

Each contract is one section, ~6–10 lines:

```markdown
## INBOX-01 — Two tones: actionable vs ambient

**Observable:** Inbox shows two kinds of signals. Actionable items demand
a response and persist until handled. Ambient items are awareness-only
and fade on their own. The UI distinguishes them visually. AI may only
downgrade a class (actionable → ambient); upgrades require explicit
user action.

**Why locked:** Without this split, Inbox collapses into a single noisy
feed and the "no inbox-zero pressure" promise dies.

**Test guards:**
- `internal/inbox/classifier_test.go::TestInbox01_DefaultClassByTrigger`
- `internal/inbox/classifier_test.go::TestInbox01_AINeverUpgrades`

**Locked since:** 2026-04-27
```

Fields:

- **Observable** — what the user (or calling code) sees. 1–3 sentences in product language, not implementation.
- **Why locked** — one sentence on why this contract matters. Helps future reviewers judge whether the contract is still load-bearing.
- **Test guards** — file path + test function name. At least one. The test must fail if the behavior breaks.
- **Locked since** — date the contract was added or last reformulated.

### Changelog

Append-only at the bottom of the file:

```markdown
## Changelog

- 2026-04-27: file created with 8 features (INBOX-01..08).
```

When a contract is reformulated or removed, add a line:

```markdown
- 2026-05-15: INBOX-04 reformulated — gradual learning instead of instant mute (owner approved).
- 2026-05-15: INBOX-08 removed — anti-spam rule replaced by daily-quota config (owner approved).
```

## Test Markers

Guard tests are made discoverable by **two complementary markers**:

### 1. Name prefix

Test functions follow the pattern `Test<Module><NN>_<ShortDescription>`:

```go
func TestInbox01_DefaultClassByTrigger(t *testing.T) { ... }
func TestInbox04_GradualLearningNotInstantMute(t *testing.T) { ... }
func TestInbox05_LearnedTabExposesAllRules(t *testing.T) { ... }
```

The `Inbox01_` prefix makes the behavior anchor greppable across the codebase:

```bash
grep -rn "TestInbox0" internal/inbox/
```

Existing tests that already cover a contract are renamed to fit the pattern (no duplication). The old name is preserved as a comment if discoverability through history matters.

### 2. Comment-block marker

The first lines inside each guard test:

```go
func TestInbox01_DefaultClassByTrigger(t *testing.T) {
    // BEHAVIOR INBOX-01 — see docs/inventory/inbox-pulse.md
    // Do not weaken or remove without explicit owner approval.
    ...
}
```

The marker shows up in diffs whenever the test is touched, drawing reviewer attention.

### Swift tests

For Desktop-side guards, the same pattern applies:

```swift
func test_INBOX_01_two_tones_distinguished_in_feed() {
    // BEHAVIOR INBOX-01 — see docs/inventory/inbox-pulse.md
    ...
}
```

## AI Session Protocol

Three layered pointers ensure the AI assistant reaches the inventory regardless of how it enters the session.

### 1. `CLAUDE.md` section

A new top-level section in `CLAUDE.md`:

```markdown
## Behavior Inventory

Behavioral contracts that must not be modified without explicit owner
approval are catalogued in `docs/inventory/`. Before touching code in
any module covered by inventory, read the corresponding file and treat
each entry as load-bearing.

Module → file mapping is in `docs/inventory/README.md`.

If a proposed change would weaken or break a guard test, stop and ask
the user before proceeding. Do not "improve" a guard test by relaxing
its assertions, renaming it out of the `Test<Module>NN_` convention,
or splitting it into multiple weaker tests.
```

This section is loaded into every session via the `claudeMd` context block.

### 2. `docs/inventory/README.md`

Acts as the lookup table:

```markdown
# Behavior Inventory

This directory catalogs the behavioral contracts of each business module.
Each entry is a guard against silent regression. Modifying any contract
or its guard test requires explicit owner approval.

| Module | Inventory file | Code paths |
|---|---|---|
| Inbox Pulse | [inbox-pulse.md](inbox-pulse.md) | `internal/inbox/`, `WatchtowerDesktop/Sources/Views/Inbox/` |
| Digest | [digest.md](digest.md) | `internal/digest/`, `WatchtowerDesktop/Sources/Views/Digest/` |
| ... | ... | ... |

## Protocol

1. Before changing code under any listed path, read the corresponding inventory file.
2. Identify whether the change touches any `INBOX-NN` / `DIGEST-NN` / etc. contract.
3. If yes, stop and ask the owner before proceeding. Quote the affected ID.
4. If approved, change code + guard test + inventory entry + changelog in one commit.
```

### 3. Per-module file header

Each `<module>.md` repeats the protocol in its header preamble (see "Header preamble" above). Redundancy is intentional — the AI may read these files in isolation, without `CLAUDE.md` context.

## Governance

When a legitimate change to a protected contract is needed:

1. **Identification** — the proposer (owner or AI) names the affected ID. Example: "INBOX-04 needs to change because explicit hard-mute proved valuable for power users; we want to keep gradual learning AND offer a per-rule override."
2. **Explicit approval** — owner says "yes, change it" with the new formulation.
3. **Atomic commit** — one commit changes:
   - Production code
   - Guard test (rewritten or removed with justification)
   - `<module>.md` entry (rewritten or deleted)
   - `<module>.md` changelog (line added with date + reason)
4. **`Locked since:`** date is reset to the commit date. The previous formulation is captured in the changelog, not in the entry itself (entry stays single-current-truth).

There are deliberately no pre-commit hooks, CI gates, or codeowner enforcement. Protection rests on four soft layers:

- **Guard tests** — fail at `make test`.
- **Name prefix** — visible in test output and grep.
- **Comment marker** — visible in diff.
- **AI protocol** — assistant reads inventory before touching covered code.

If any of these is bypassed by accident, the others remain. If all four are bypassed, the change was deliberate and the owner's review of the diff is the final guard.

## Pilot — Inbox Pulse (initial 8 contracts)

The pilot file `docs/inventory/inbox-pulse.md` ships with these eight features:

- **INBOX-01** — Two tones: actionable vs ambient. AI may only downgrade.
- **INBOX-02** — Inbox understands what the user has answered (Slack/DM/thread/Jira-comment/Calendar-RSVP) and clears the item without a click.
- **INBOX-03** — Inbox surfaces signals that would have been buried in noise. The product promise: "you won't miss what matters because of volume."
- **INBOX-04** — Learning is gradual, not single-click. One 👎 is a signal in a pool of signals; muting requires accumulated evidence (explicit + implicit).
- **INBOX-05** — The "Learned" tab exposes the system's current model of the user (mutes, boosts, manual rules, weights, sources). Editable.
- **INBOX-06** — Manual rules (`source='user_rule'`) outrank statistical aggregates. The implicit learner never overwrites a user-authored rule.
- **INBOX-07** — AI failure or invalid response keeps the existing pinned/feed state. No flapping, no blanking.
- **INBOX-08** — Inbox does not re-spam the same item. An item shown once and ignored does not climb back to the top each cycle.

For each, the pilot delivery captures: a final user-language `Observable` and `Why locked` line, at least one guard test in `internal/inbox/` (renamed or newly-written), Desktop-side guards where applicable for INBOX-01 / INBOX-05.

The detailed test mapping is produced by the implementation plan, not this design doc — some guards already exist (`TestPinnedSelector_AIFailureKeepsState`, `TestFeedback_*`) and need rename + comment-marker, others (e.g. INBOX-08 anti-spam, INBOX-03 noise-filtering) need to be written.

## Roll-Out Plan

After pilot:

1. **Inbox Pulse** (this design) — full pilot through implementation plan.
2. **Validate format** with the pilot file and 1–2 weeks of real use. Adjust the schema or AI protocol if the pilot reveals gaps.
3. **Roll out** to remaining modules in this order, by stability and hot-path frequency:
   - Digest (mature, high-traffic)
   - Tracks (mature)
   - Briefing (mature, depends on Digest/Tracks/Tasks)
   - People (mature)
   - Tasks (mature)
   - Day Plan (newer)
   - Calendar (mature, mostly integration)
   - Meeting (newer)
   - Targets (WIP — defer until stable)

Each module gets its own micro-design exercise (~30 min): owner + AI walk through the spec, pick 5–10 contracts, write the file and rename/author guard tests. Same template, same protocol.

## Open Questions

- **Should `CLAUDE.md` reference the inventory pointer be at the top or bundled with other project notes?** Prefer top-level visibility, but defer to where the existing `CLAUDE.md` structure puts it most naturally during implementation.
- **Should Swift behavior tests live in a separate target or alongside existing tests?** Default: alongside, with the comment marker. Revisit if Swift test counts grow disproportionately.
- **Targets module** — currently WIP on `feature/targets-ai-ui`. Inventory is added only after the feature stabilizes; otherwise the contracts churn.

## File / Module Touchpoints

- `docs/inventory/README.md` — new
- `docs/inventory/inbox-pulse.md` — new (pilot)
- `CLAUDE.md` — extended with "Behavior Inventory" section
- `internal/inbox/*_test.go` — selective renames + comment markers; new tests for INBOX-03 (noise filtering) and INBOX-08 (anti-spam)
- `WatchtowerDesktop/Sources/.../InboxTests/` — new tests for INBOX-01 and INBOX-05 if not already covered

## Acceptance Criteria

- `docs/inventory/README.md` exists with the protocol blurb and the module table (initially with one row: Inbox Pulse).
- `docs/inventory/inbox-pulse.md` exists with all 8 contracts in the schema, each pointing to at least one passing guard test.
- Every guard test follows the `Test<Module>NN_` naming convention and contains the comment-block marker.
- `CLAUDE.md` contains the "Behavior Inventory" section.
- `make test` passes; renamed tests still cover their original assertions.
- A grep for `BEHAVIOR INBOX-` returns one match per active contract.
