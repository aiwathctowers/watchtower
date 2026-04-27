# Killer Features Inventory — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the killer-features inventory infrastructure (`docs/inventory/`, AI session protocol in `CLAUDE.md`) and pilot it on Inbox Pulse with 8 contracts (INBOX-01..08), each with guard tests following the `TestInbox<NN>_*` naming convention and inline `// KILLER FEATURE INBOX-<NN>` comment markers.

**Architecture:** Pure documentation + selective test renames + comment markers. No new tooling, no hooks, no CI gates. Each contract gets one entry in `docs/inventory/inbox-pulse.md` with a status field (`Enforced`, `Partial`, or `Aspirational`). Existing tests are renamed to fit the convention; gaps are documented as `Tracked gap:` lines for follow-up plans.

**Tech Stack:** Markdown, Go `testing`, Swift XCTest. Standard `git` for atomic commits.

**Spec reference:** `docs/superpowers/specs/2026-04-27-killer-features-inventory-design.md`.

---

## File Structure

Files created or modified by this plan:

- **Create:** `docs/inventory/README.md` — index + protocol blurb.
- **Create:** `docs/inventory/inbox-pulse.md` — 8 contracts + changelog.
- **Modify:** `CLAUDE.md` — append `## Killer Features Inventory` section.
- **Modify (rename + add comment marker):**
  - `internal/inbox/classifier_test.go` — rename 2 tests for INBOX-01.
  - `internal/inbox/pipeline_test.go` — rename 3 tests for INBOX-02.
  - `internal/inbox/pinned_selector_test.go` — rename 1 test for INBOX-03 + 2 tests for INBOX-07.
  - `internal/inbox/user_preferences_test.go` — rename 1 test for INBOX-03.
  - `internal/inbox/learner_test.go` — rename 2 tests for INBOX-04 + 1 test for INBOX-06.
  - `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift` — rename 2 tests for INBOX-05.
  - `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift` — rename 1 test for INBOX-05 + 1 test reused for INBOX-06.
- **Touch (no behaviour change):** none. INBOX-08 is `Aspirational`; no code or test today.

The renames preserve original test bodies — only function names and a 2-line comment marker change. Behaviour is preserved; `make test` continues to pass after every commit.

---

## Task 1: Foundation — directory, README, skeleton, CLAUDE.md hook

**Files:**
- Create: `docs/inventory/README.md`
- Create: `docs/inventory/inbox-pulse.md` (skeleton, contracts added in later tasks)
- Modify: `CLAUDE.md` — append new section at the bottom

- [ ] **Step 1: Create `docs/inventory/README.md` with the protocol blurb**

```markdown
# Killer Features Inventory

This directory catalogs the **behavioral contracts** of each business module — the user-observable invariants that must not change without explicit owner approval.

Each entry is a guard against silent regression. Modifying any contract or its guard test requires explicit approval from @Vadym.

## Module → file mapping

| Module | Inventory file | Code paths |
|---|---|---|
| Inbox Pulse | [inbox-pulse.md](inbox-pulse.md) | `internal/inbox/`, `WatchtowerDesktop/Sources/Views/Inbox/`, `WatchtowerDesktop/Sources/ViewModels/Inbox*.swift` |

(Other modules will be added as their inventories are written.)

## Protocol

1. Before changing code under any listed path, **read the corresponding inventory file**.
2. Identify whether the change touches any `<MODULE>-NN` contract.
3. If yes, **stop and ask the owner** before proceeding. Quote the affected ID.
4. If approved, change code + guard test + inventory entry + changelog **in one atomic commit**.

There are no pre-commit hooks, CI gates, or codeowner enforcement. Protection rests on four soft layers:

- Guard tests fail at `make test`.
- Test name prefix (`TestInbox01_…`, `TestDigest03_…`) is greppable.
- `// KILLER FEATURE …` comment markers show up in diff.
- AI assistant reads inventory before touching covered code.
```

- [ ] **Step 2: Create `docs/inventory/inbox-pulse.md` skeleton**

```markdown
# Killer Features — Inbox Pulse

> Each item below is a **behavioral contract** that must be preserved.
> Modifying or weakening the protecting test requires explicit approval
> from @Vadym.
>
> AI assistant: when working in `internal/inbox/` or
> `WatchtowerDesktop/Sources/Views/Inbox/`, read this file first. Any
> proposed change that would break a guard test or remove a contract
> must be raised as a question before touching code.

**Module:** `internal/inbox/` + `WatchtowerDesktop/Sources/Views/Inbox/`
**Last full audit:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->

## Changelog

- 2026-04-27: file created.
```

- [ ] **Step 3: Append "Killer Features Inventory" section to `CLAUDE.md`**

Read `CLAUDE.md` first to find the end. Append:

```markdown

## Killer Features Inventory

Behavioral contracts that must not be modified without explicit owner approval are catalogued in `docs/inventory/`. Before touching code in any module covered by inventory, read the corresponding file and treat each entry as load-bearing.

Module → file mapping is in [docs/inventory/README.md](docs/inventory/README.md).

If a proposed change would weaken or break a guard test, **stop and ask the owner** before proceeding. Do not "improve" a guard test by relaxing its assertions, renaming it out of the `Test<Module>NN_` convention, or splitting it into multiple weaker tests.
```

- [ ] **Step 4: Verify nothing broke**

Run: `go build ./... && go vet ./...`
Expected: success, no errors.

- [ ] **Step 5: Commit**

```bash
git add docs/inventory/README.md docs/inventory/inbox-pulse.md CLAUDE.md
git commit -m "$(cat <<'EOF'
docs(inventory): scaffold killer features inventory + protocol

Adds docs/inventory/ with README protocol, empty inbox-pulse.md
skeleton, and a CLAUDE.md pointer so AI sessions discover the
inventory before touching covered modules.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: INBOX-01 — Two tones (actionable vs ambient)

**Files:**
- Modify: `internal/inbox/classifier_test.go` (rename 2 functions, add markers)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-01 entry)

**Existing tests to rename:**
- `TestClassifier_DefaultForTriggerType` → `TestInbox01_DefaultClassByTrigger`
- `TestClassifier_ApplyAIOverride_DowngradeOnly` → `TestInbox01_AINeverUpgrades`

- [ ] **Step 1: Rename `TestClassifier_DefaultForTriggerType` and add marker**

Open `internal/inbox/classifier_test.go`. Replace the function header:

```go
func TestClassifier_DefaultForTriggerType(t *testing.T) {
```

with:

```go
func TestInbox01_DefaultClassByTrigger(t *testing.T) {
	// KILLER FEATURE INBOX-01 — see docs/inventory/inbox-pulse.md
	// Default class assignment per trigger_type. Do not weaken or remove
	// without explicit owner approval.
```

- [ ] **Step 2: Rename `TestClassifier_ApplyAIOverride_DowngradeOnly` and add marker**

Replace:

```go
func TestClassifier_ApplyAIOverride_DowngradeOnly(t *testing.T) {
```

with:

```go
func TestInbox01_AINeverUpgrades(t *testing.T) {
	// KILLER FEATURE INBOX-01 — see docs/inventory/inbox-pulse.md
	// AI may downgrade actionable→ambient but never the reverse.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Run renamed tests, confirm pass**

Run: `go test -run 'TestInbox01_' ./internal/inbox/ -v`
Expected: 2 tests PASS.

- [ ] **Step 4: Insert INBOX-01 entry into `docs/inventory/inbox-pulse.md`**

Replace the line `<!-- Contracts will be inserted here in subsequent commits. -->` with:

```markdown
## INBOX-01 — Two tones: actionable vs ambient

**Status:** Enforced

**Observable:** Inbox shows two kinds of signals. **Actionable** items demand a response and persist until handled. **Ambient** items are awareness-only and fade on their own. The UI distinguishes them visually. AI may only **downgrade** a class (actionable → ambient); upgrades require explicit user action.

**Why locked:** Without this split, Inbox collapses into a single noisy feed and the "no inbox-zero pressure" promise dies.

**Test guards:**
- `internal/inbox/classifier_test.go::TestInbox01_DefaultClassByTrigger`
- `internal/inbox/classifier_test.go::TestInbox01_AINeverUpgrades`

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/classifier_test.go docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-01 — two tones (actionable vs ambient)

Renames 2 classifier tests to TestInbox01_* and adds KILLER FEATURE
markers. Adds INBOX-01 entry to inventory.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: INBOX-02 — Inbox understands what user has answered

**Files:**
- Modify: `internal/inbox/pipeline_test.go` (rename 3 functions, add markers)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-02 entry)

**Existing tests to rename:**
- `TestPipeline_Run_AutoResolveWithoutAI` → `TestInbox02_AutoResolveSlackOnUserReply`
- `TestAutoResolve_Jira_UserCommented` → `TestInbox02_AutoResolveJiraOnUserComment`
- `TestAutoResolve_Calendar_UserResponded` → `TestInbox02_AutoResolveCalendarOnUserRSVP`

- [ ] **Step 1: Rename all three tests and add markers**

In `internal/inbox/pipeline_test.go`, perform three renames:

Replace `func TestPipeline_Run_AutoResolveWithoutAI(t *testing.T) {` with:

```go
func TestInbox02_AutoResolveSlackOnUserReply(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User replies in Slack → mention/dm/thread_reply auto-resolves.
	// Do not weaken or remove without explicit owner approval.
```

Replace `func TestAutoResolve_Jira_UserCommented(t *testing.T) {` with:

```go
func TestInbox02_AutoResolveJiraOnUserComment(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User comments on a Jira issue → jira_comment_mention auto-resolves.
	// Do not weaken or remove without explicit owner approval.
```

Replace `func TestAutoResolve_Calendar_UserResponded(t *testing.T) {` with:

```go
func TestInbox02_AutoResolveCalendarOnUserRSVP(t *testing.T) {
	// KILLER FEATURE INBOX-02 — see docs/inventory/inbox-pulse.md
	// User responds to a calendar invite → calendar_invite auto-resolves.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Run renamed tests, confirm pass**

Run: `go test -run 'TestInbox02_' ./internal/inbox/ -v`
Expected: 3 tests PASS.

- [ ] **Step 3: Insert INBOX-02 entry**

Replace the placeholder `<!-- Contracts will be inserted here in subsequent commits. -->` (which now sits after INBOX-01) with the INBOX-02 entry followed by the placeholder:

```markdown
## INBOX-02 — Inbox understands what I've already answered

**Status:** Enforced

**Observable:** I reply in Slack/DM/thread, comment on a Jira issue, or RSVP a calendar invite — the corresponding inbox item disappears **without my click**. Inbox follows the conversation; I never close the same thing twice.

**Why locked:** This is the basic promise that makes Inbox lower-friction than nativeSlack/Jira/Calendar notifications. Break it and users stop trusting the feed and revert to the original sources.

**Test guards:**
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveSlackOnUserReply`
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveJiraOnUserComment`
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveCalendarOnUserRSVP`

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 4: Commit**

```bash
git add internal/inbox/pipeline_test.go docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-02 — auto-resolve on user reply

Renames 3 pipeline tests to TestInbox02_* and adds KILLER FEATURE
markers. Slack/Jira/Calendar auto-resolve guards are now anchored
to the inventory entry.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: INBOX-03 — Surfaces signals from noise (Partial)

**Files:**
- Modify: `internal/inbox/pinned_selector_test.go` (rename 1 function, add marker)
- Modify: `internal/inbox/user_preferences_test.go` (rename 1 function, add marker)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-03 entry)

**Existing tests to rename:**
- `TestPinnedSelector_RespectsMuteRules` → `TestInbox03_MutedSourcesNotPinned`
- `TestBuildUserPrefs_TopByRelevance` → `TestInbox03_UserPrefsRankedByRelevance`

This contract is **Partial** — its full enforcement (a learned-noise-vs-signal scoring layer) is aspirational. The two tests above guard the strongest currently-true sub-properties: muted sources are filtered from pinned, and the prefs block reaching AI is relevance-ranked rather than alphabetical. The remaining gap is documented in the entry.

- [ ] **Step 1: Rename `TestPinnedSelector_RespectsMuteRules`**

In `internal/inbox/pinned_selector_test.go`, replace:

```go
func TestPinnedSelector_RespectsMuteRules(t *testing.T) {
```

with:

```go
func TestInbox03_MutedSourcesNotPinned(t *testing.T) {
	// KILLER FEATURE INBOX-03 — see docs/inventory/inbox-pulse.md
	// Muted sources are filtered from pinned regardless of AI suggestion.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Rename `TestBuildUserPrefs_TopByRelevance`**

In `internal/inbox/user_preferences_test.go`, replace:

```go
func TestBuildUserPrefs_TopByRelevance(t *testing.T) {
```

with:

```go
func TestInbox03_UserPrefsRankedByRelevance(t *testing.T) {
	// KILLER FEATURE INBOX-03 — see docs/inventory/inbox-pulse.md
	// USER PREFERENCES block reaching AI prioritizes relevant rules.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Run renamed tests, confirm pass**

Run: `go test -run 'TestInbox03_' ./internal/inbox/ -v`
Expected: 2 tests PASS.

- [ ] **Step 4: Insert INBOX-03 entry**

Replace the placeholder with:

```markdown
## INBOX-03 — Surfaces signals that would have been buried in noise

**Status:** Partial

**Observable:** If 200 messages flow past me in a day and one needed a reaction, Inbox surfaces it. Not "all mentions" — specifically the ones that look like signal in the surrounding volume. Noisy sources (deploy channels, dependabot, chatty Jira projects) do not crowd out high-signal ones.

**Why locked:** Without this, Inbox is just an alias for `@mentions` and adds nothing over native Slack notifications.

**Test guards (partial):**
- `internal/inbox/pinned_selector_test.go::TestInbox03_MutedSourcesNotPinned`
- `internal/inbox/user_preferences_test.go::TestInbox03_UserPrefsRankedByRelevance`

**Tracked gap:** Today's pipeline relies on user-curated mutes/boosts plus per-trigger default class. There is no learned signal-vs-noise scoring across activity volume. Closing this gap is a separate feature plan; see `docs/superpowers/specs/2026-04-23-inbox-pulse-design.md` (open questions).

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/pinned_selector_test.go internal/inbox/user_preferences_test.go docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-03 — surface signals from noise (Partial)

Renames 2 partial-guard tests to TestInbox03_* and documents the
remaining gap (learned signal-vs-noise scoring) in the inventory
entry.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: INBOX-04 — Gradual learning, not single-click (Partial)

**Files:**
- Modify: `internal/inbox/learner_test.go` (rename 2 functions, add markers)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-04 entry)

**Existing tests to rename:**
- `TestLearner_MuteOnHighDismissRate` → `TestInbox04_GradualMuteFromAccumulatedDismissals`
- `TestLearner_BelowThresholdNoRule` → `TestInbox04_NoRuleBelowEvidenceThreshold`

This contract is **Partial**. The implicit learner today *is* gradual (requires `evidence_count ≥ 5` over 30 days plus a dismiss-rate threshold). Explicit feedback is **not** gradual: a single `(-1, never_show)` immediately writes weight `-1.0`. The two tests above guard the implicit-side gradualness; the explicit-side gap is documented.

- [ ] **Step 1: Rename `TestLearner_MuteOnHighDismissRate`**

In `internal/inbox/learner_test.go`, replace:

```go
func TestLearner_MuteOnHighDismissRate(t *testing.T) {
```

with:

```go
func TestInbox04_GradualMuteFromAccumulatedDismissals(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// Implicit mute requires accumulated dismiss evidence, not a single click.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Rename `TestLearner_BelowThresholdNoRule`**

Replace:

```go
func TestLearner_BelowThresholdNoRule(t *testing.T) {
```

with:

```go
func TestInbox04_NoRuleBelowEvidenceThreshold(t *testing.T) {
	// KILLER FEATURE INBOX-04 — see docs/inventory/inbox-pulse.md
	// Below the evidence threshold no rule is created — preserves gradual
	// learning. Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Run renamed tests, confirm pass**

Run: `go test -run 'TestInbox04_' ./internal/inbox/ -v`
Expected: 2 tests PASS.

- [ ] **Step 4: Insert INBOX-04 entry**

Replace the placeholder with:

```markdown
## INBOX-04 — Inbox learns gradually, not by single click

**Status:** Partial

**Observable:** A single 👎 does not silence a source forever — it is one signal in a pool. Muting / boosting decisions emerge from accumulated evidence (explicit feedback **plus** implicit dismissals, response times, recency). Behavior shifts smoothly over time, like Spotify recommendations, not like a toggle.

**Why locked:** A single-click kill switch makes users either afraid to give feedback ("I might over-mute") or distrustful when feedback doesn't bite ("I clicked once and nothing changed"). Gradual accumulation is the only model that earns trust at both ends.

**Test guards (partial — implicit side):**
- `internal/inbox/learner_test.go::TestInbox04_GradualMuteFromAccumulatedDismissals`
- `internal/inbox/learner_test.go::TestInbox04_NoRuleBelowEvidenceThreshold`

**Tracked gap:** Explicit feedback (`internal/inbox/feedback.go`) currently maps `(-1, never_show)` to weight `-1.0` instantly — a single-click kill switch contradicting this contract. Closing this gap requires reworking `SubmitFeedback` so explicit votes accumulate as evidence rather than setting final weight directly. See follow-up: `docs/superpowers/specs/<date>-inbox-gradual-explicit-learning-design.md` (to be authored).

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/learner_test.go docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-04 — gradual learning (Partial)

Renames 2 implicit-learner tests to TestInbox04_* and documents the
explicit-feedback gap (instant weight=-1.0 on never_show) for a
follow-up plan.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: INBOX-05 — Learned tab exposes & lets me edit the model

**Files:**
- Modify: `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift` (rename 2 funcs, add markers)
- Modify: `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift` (rename 1 func, add marker)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-05 entry)

**Existing tests to rename:**
- `InboxLearnedRulesViewModelTests.testAddManualRule` → `test_INBOX_05_add_manual_rule`
- `InboxLearnedRulesViewModelTests.testRemoveRule` → `test_INBOX_05_remove_rule`
- `InboxLearnedRulesQueriesTests.testListAllOrderedByAbsWeight` → `test_INBOX_05_list_rules_ordered_by_weight`

Swift naming convention diverges from Go: dot-separated `module_NN`. Both are unique-greppable.

- [ ] **Step 1: Rename `testAddManualRule`**

In `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift`, replace:

```swift
    func testAddManualRule() async throws {
```

with:

```swift
    func test_INBOX_05_add_manual_rule() async throws {
        // KILLER FEATURE INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab adds a manual rule that surfaces immediately.
        // Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Rename `testRemoveRule`**

Replace:

```swift
    func testRemoveRule() async throws {
```

with:

```swift
    func test_INBOX_05_remove_rule() async throws {
        // KILLER FEATURE INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab removes a rule, persisted to DB.
        // Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Rename `testListAllOrderedByAbsWeight`**

In `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift`, replace:

```swift
    func testListAllOrderedByAbsWeight() throws {
```

with:

```swift
    func test_INBOX_05_list_rules_ordered_by_weight() throws {
        // KILLER FEATURE INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab lists rules ordered so the most impactful are visible first.
        // Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 4: Run Swift tests**

Run: `cd WatchtowerDesktop && swift test --filter 'test_INBOX_05_'`
Expected: 3 tests PASS.

- [ ] **Step 5: Insert INBOX-05 entry**

Replace the placeholder with:

```markdown
## INBOX-05 — I can see and edit what Inbox has learned about me

**Status:** Enforced

**Observable:** The "Learned" tab inside Inbox shows the system's current model of me — mutes, boosts, manual rules — with weight, source ("learned from 12 dismissals" / "I added this manually"), and an inline remove/edit. I can add a rule, remove a rule, change a weight; changes persist and reflect in subsequent pinned/feed cycles.

**Why locked:** Without visibility, the learning system is a black box and trust collapses. Without editability, users cannot recover from misclassifications — feedback becomes a one-way street.

**Test guards:**
- `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift::test_INBOX_05_add_manual_rule`
- `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift::test_INBOX_05_remove_rule`
- `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift::test_INBOX_05_list_rules_ordered_by_weight`

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 6: Commit**

```bash
git add WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-05 — Learned tab visibility & editability

Renames 3 Swift tests covering the Learned tab's CRUD and ordering
to test_INBOX_05_* with KILLER FEATURE markers.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: INBOX-06 — Manual rules outrank statistics

**Files:**
- Modify: `internal/inbox/learner_test.go` (rename 1 function, add marker)
- Modify: `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift` (rename 1 function, add marker)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-06 entry)

**Existing tests to rename:**
- `TestLearner_DoesNotOverwriteUserRule` → `TestInbox06_UserRuleProtectedFromImplicitOverwrite`
- `InboxLearnedRulesQueriesTests.testUpsertManualOverridesImplicitSource` → `test_INBOX_06_manual_rule_overrides_implicit`

- [ ] **Step 1: Rename Go test**

In `internal/inbox/learner_test.go`, replace:

```go
func TestLearner_DoesNotOverwriteUserRule(t *testing.T) {
```

with:

```go
func TestInbox06_UserRuleProtectedFromImplicitOverwrite(t *testing.T) {
	// KILLER FEATURE INBOX-06 — see docs/inventory/inbox-pulse.md
	// source='user_rule' is never overwritten by the implicit learner.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Rename Swift test**

In `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift`, replace:

```swift
    func testUpsertManualOverridesImplicitSource() throws {
```

with:

```swift
    func test_INBOX_06_manual_rule_overrides_implicit() throws {
        // KILLER FEATURE INBOX-06 — see docs/inventory/inbox-pulse.md
        // Manual rule upsert overrides an existing implicit rule on the same scope.
        // Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Run renamed tests, confirm pass**

Run: `go test -run 'TestInbox06_' ./internal/inbox/ -v`
Expected: 1 test PASS.

Run: `cd WatchtowerDesktop && swift test --filter 'test_INBOX_06_'`
Expected: 1 test PASS.

- [ ] **Step 4: Insert INBOX-06 entry**

Replace the placeholder with:

```markdown
## INBOX-06 — Manual rules outrank statistics

**Status:** Enforced

**Observable:** Any rule I author by hand in the "Learned" tab (`source='user_rule'`) is never overwritten by the automatic implicit learner. If I say "mute @bob," statistics across the next month do not silently undo me.

**Why locked:** Without this, the "Learned" tab is theatre — the user edits a rule, walks away, and the aggregator overrides them. Explicit user intent must beat statistical aggregates.

**Test guards:**
- `internal/inbox/learner_test.go::TestInbox06_UserRuleProtectedFromImplicitOverwrite`
- `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift::test_INBOX_06_manual_rule_overrides_implicit`

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/learner_test.go WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-06 — manual rules outrank statistics

Renames 1 Go test + 1 Swift test guarding the user_rule precedence
contract to the INBOX-06 naming convention.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: INBOX-07 — AI failure ≠ state loss

**Files:**
- Modify: `internal/inbox/pinned_selector_test.go` (rename 2 functions, add markers)
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-07 entry)

**Existing tests to rename:**
- `TestPinnedSelector_AIFailureKeepsState` → `TestInbox07_PinnedKeepsStateOnAIError`
- `TestPinnedSelector_InvalidJSONFallback` → `TestInbox07_PinnedKeepsStateOnInvalidJSON`

- [ ] **Step 1: Rename `TestPinnedSelector_AIFailureKeepsState`**

In `internal/inbox/pinned_selector_test.go`, replace:

```go
func TestPinnedSelector_AIFailureKeepsState(t *testing.T) {
```

with:

```go
func TestInbox07_PinnedKeepsStateOnAIError(t *testing.T) {
	// KILLER FEATURE INBOX-07 — see docs/inventory/inbox-pulse.md
	// AI error during pinned selection preserves the previous pinned set.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 2: Rename `TestPinnedSelector_InvalidJSONFallback`**

Replace:

```go
func TestPinnedSelector_InvalidJSONFallback(t *testing.T) {
```

with:

```go
func TestInbox07_PinnedKeepsStateOnInvalidJSON(t *testing.T) {
	// KILLER FEATURE INBOX-07 — see docs/inventory/inbox-pulse.md
	// Invalid JSON from AI preserves the previous pinned set.
	// Do not weaken or remove without explicit owner approval.
```

- [ ] **Step 3: Run renamed tests**

Run: `go test -run 'TestInbox07_' ./internal/inbox/ -v`
Expected: 2 tests PASS.

- [ ] **Step 4: Insert INBOX-07 entry**

Replace the placeholder with:

```markdown
## INBOX-07 — AI failure does not lose state

**Status:** Enforced

**Observable:** When the pinned-selection AI call errors out or returns unparseable JSON, the existing pinned items are preserved untouched until a future cycle succeeds. The feed does not blank out, items do not reshuffle, the user can keep working on whatever they were focused on.

**Why locked:** Inbox is a "pulse" surface. A flapping AI call that periodically blanks pinned would teach the user to distrust the screen. Stability beats freshness when the alternative is chaos.

**Test guards:**
- `internal/inbox/pinned_selector_test.go::TestInbox07_PinnedKeepsStateOnAIError`
- `internal/inbox/pinned_selector_test.go::TestInbox07_PinnedKeepsStateOnInvalidJSON`

**Locked since:** 2026-04-27

<!-- Contracts will be inserted here in subsequent commits. -->
```

- [ ] **Step 5: Commit**

```bash
git add internal/inbox/pinned_selector_test.go docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-07 — pinned state survives AI failure

Renames 2 fallback tests to TestInbox07_* and adds the inventory entry.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: INBOX-08 — Inbox does not re-spam (Aspirational)

**Files:**
- Modify: `docs/inventory/inbox-pulse.md` (insert INBOX-08 entry)

There is no current implementation or test for anti-respam behaviour. The pinned set is rewritten each cycle from scratch; an item that climbed once and was ignored has no penalty applied for the next cycle. INBOX-08 is therefore documented as **Aspirational** with a tracked gap. No code changes in this task.

- [ ] **Step 1: Insert INBOX-08 entry**

Replace the placeholder with:

```markdown
## INBOX-08 — Inbox does not re-spam the same item

**Status:** Aspirational

**Observable:** When an item was shown and I did not engage (no click, no feedback, no resolve), it does not climb back to the top of the feed each cycle. Once it has had its chance, it backs off — visibility decays even if the underlying signal repeats.

**Why locked:** Without this, repeated high-priority items I am intentionally ignoring train me to stop looking at the feed at all (the iOS-Notifications-fatigue effect).

**Test guards:** none yet — see Tracked gap.

**Tracked gap:** Today's pinned-selector rewrites the set from scratch each cycle with no penalty for previously-shown-and-ignored items. Closing this gap requires (a) tracking a "last surfaced and ignored" timestamp on `inbox_items`, (b) feeding it to the pinned-selector AI prompt as a signal, and (c) a Go-side post-filter or weight decay. Implementation is a separate feature plan; for now this contract is documented intent.

**Locked since:** 2026-04-27 (intent recorded; enforcement pending)
```

- [ ] **Step 2: Commit**

```bash
git add docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): INBOX-08 — anti re-spam (Aspirational)

Records the contract that an ignored pinned item should not climb
back each cycle. No implementation or test exists yet; entry stands
as documented intent for a follow-up feature plan.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Final verification + changelog

**Files:**
- Modify: `docs/inventory/inbox-pulse.md` (update changelog summary)

- [ ] **Step 1: Run full Go test suite to confirm nothing regressed**

Run: `go test ./internal/inbox/ -v -run 'TestInbox'`
Expected: All `TestInbox<NN>_*` tests PASS (count should match: 2 + 3 + 2 + 2 + 1 + 2 = 12 Go tests).

- [ ] **Step 2: Run full Swift test suite for renamed tests**

Run: `cd WatchtowerDesktop && swift test --filter 'test_INBOX_'`
Expected: 4 Swift tests PASS (3 for INBOX-05, 1 for INBOX-06).

- [ ] **Step 3: Grep for KILLER FEATURE markers and verify count**

Run: `grep -rn 'KILLER FEATURE INBOX-' internal/inbox/ WatchtowerDesktop/Tests/`
Expected: at least 16 marker lines (one per renamed test). Each `INBOX-NN` from 01..07 should appear at least once. INBOX-08 has no marker (aspirational).

- [ ] **Step 4: Confirm `go build` and `go vet` clean**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 5: Update changelog summary line in `docs/inventory/inbox-pulse.md`**

Replace the existing changelog block:

```markdown
## Changelog

- 2026-04-27: file created.
```

with:

```markdown
## Changelog

- 2026-04-27: file created with 8 contracts (INBOX-01..08). Five are Enforced (01, 02, 05, 06, 07), two are Partial (03, 04), one is Aspirational (08). Tracked gaps recorded inline on Partial/Aspirational entries.
```

- [ ] **Step 6: Final commit**

```bash
git add docs/inventory/inbox-pulse.md
git commit -m "$(cat <<'EOF'
docs(inventory): finalize Inbox Pulse pilot — 8 contracts

INBOX-01..08 documented. Enforced: 01, 02, 05, 06, 07. Partial: 03,
04. Aspirational: 08. Pilot is now ready for owner review and the
roll-out template can be applied to other modules.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 7: Report**

Print to the user:

```
Inbox Pulse killer-feature inventory pilot complete.
- File: docs/inventory/inbox-pulse.md
- 8 contracts (5 Enforced, 2 Partial, 1 Aspirational)
- 12 Go guard tests + 4 Swift guard tests renamed and marked
- CLAUDE.md updated with inventory pointer
- README.md scaffolded for future module rows

Next: review the pilot file and decide which module to roll out next
(suggested order: Digest → Tracks → Briefing → People → Tasks → Day
Plan → Calendar → Meeting → Targets[when stable]).
```

---

## Self-Review

**Spec coverage:**
- Directory layout (`docs/inventory/<module>.md`) — Task 1 ✓
- Per-module file format (header, schema, changelog) — Tasks 1 + 2..9 + 10 ✓
- Test markers (name prefix + comment block) — Tasks 2..8 ✓
- AI session protocol (`CLAUDE.md` section + `README.md` + per-file header) — Task 1 ✓
- Governance (atomic commits with code+test+entry) — applied throughout ✓
- 8 INBOX contracts — Tasks 2..9 ✓
- Acceptance criteria (grep returns one match per contract; `make test` passes) — Task 10 ✓

**Placeholder scan:** Each step contains the exact code or commands to run. The single intentional "later" reference is the `<date>-inbox-gradual-explicit-learning-design.md` filename in INBOX-04's tracked gap, which is a deliberate forward pointer — not a placeholder for this plan.

**Type / signature consistency:** All renamed function names referenced in commit steps and inventory entries match the names declared in the rename steps (`TestInbox01_DefaultClassByTrigger`, `TestInbox01_AINeverUpgrades`, ...). Spot-checked 01..08; consistent.

No issues to fix.
