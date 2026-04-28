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

## INBOX-01 — Two tones: actionable vs ambient

**Status:** Enforced

**Observable:** Inbox shows two kinds of signals. **Actionable** items demand a response and persist until handled. **Ambient** items are awareness-only and fade on their own. The UI distinguishes them visually. AI may only **downgrade** a class (actionable → ambient); upgrades require explicit user action.

**Why locked:** Without this split, Inbox collapses into a single noisy feed and the "no inbox-zero pressure" promise dies.

**Test guards:**
- `internal/inbox/classifier_test.go::TestInbox01_DefaultClassByTrigger`
- `internal/inbox/classifier_test.go::TestInbox01_AINeverUpgrades`

**Locked since:** 2026-04-27

## INBOX-02 — Inbox understands what I've already answered

**Status:** Enforced

**Observable:** I reply in Slack/DM/thread, comment on a Jira issue, or RSVP a calendar invite — the corresponding inbox item disappears **without my click**. Inbox follows the conversation; I never close the same thing twice.

**Why locked:** This is the basic promise that makes Inbox lower-friction than native Slack/Jira/Calendar notifications. Break it and users stop trusting the feed and revert to the original sources.

**Test guards:**
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveSlackOnUserReply`
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveJiraOnUserComment`
- `internal/inbox/pipeline_test.go::TestInbox02_AutoResolveCalendarOnUserRSVP`

**Locked since:** 2026-04-27

## INBOX-03 — Surfaces signals that would have been buried in noise

**Status:** Partial

**Observable:** If 200 messages flow past me in a day and one needed a reaction, Inbox surfaces it. Not "all mentions" — specifically the ones that look like signal in the surrounding volume. Noisy sources (deploy channels, dependabot, chatty Jira projects) do not crowd out high-signal ones.

**Why locked:** Without this, Inbox is just an alias for `@mentions` and adds nothing over native Slack notifications.

**Test guards (partial):**
- `internal/inbox/pinned_selector_test.go::TestInbox03_MutedSourcesNotPinned`
- `internal/inbox/user_preferences_test.go::TestInbox03_UserPrefsRankedByRelevance`

**Tracked gap:** Today's pipeline relies on user-curated mutes/boosts plus per-trigger default class. There is no learned signal-vs-noise scoring across activity volume. Closing this gap is a separate feature plan; see `docs/superpowers/specs/2026-04-23-inbox-pulse-design.md` (open questions).

**Locked since:** 2026-04-27

## INBOX-04 — Inbox learns gradually, not by single click

**Status:** Partial

**Observable:** A single 👎 does not silence a source forever — it is one signal in a pool. Muting / boosting decisions emerge from accumulated evidence (explicit feedback **plus** implicit dismissals, response times, recency). Behavior shifts smoothly over time, like Spotify recommendations, not like a toggle.

**Why locked:** A single-click kill switch makes users either afraid to give feedback ("I might over-mute") or distrustful when feedback doesn't bite ("I clicked once and nothing changed"). Gradual accumulation is the only model that earns trust at both ends.

**Test guards (partial — implicit side):**
- `internal/inbox/learner_test.go::TestInbox04_GradualMuteFromAccumulatedDismissals`
- `internal/inbox/learner_test.go::TestInbox04_NoRuleBelowEvidenceThreshold`

**Tracked gap:** Explicit feedback (`internal/inbox/feedback.go`) currently maps `(-1, never_show)` to weight `-1.0` instantly — a single-click kill switch contradicting this contract. Closing this gap requires reworking `SubmitFeedback` so explicit votes accumulate as evidence rather than setting final weight directly. Follow-up design doc to be authored.

**Locked since:** 2026-04-27

## INBOX-05 — I can see and edit what Inbox has learned about me

**Status:** Enforced

**Observable:** The "Learned" tab inside Inbox shows the system's current model of me — mutes, boosts, manual rules — with weight, source ("learned from 12 dismissals" / "I added this manually"), and an inline remove/edit. I can add a rule, remove a rule, change a weight; changes persist and reflect in subsequent pinned/feed cycles.

**Why locked:** Without visibility, the learning system is a black box and trust collapses. Without editability, users cannot recover from misclassifications — feedback becomes a one-way street.

**Test guards:**
- `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift::test_INBOX_05_add_manual_rule`
- `WatchtowerDesktop/Tests/InboxLearnedRulesViewModelTests.swift::test_INBOX_05_remove_rule`
- `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift::test_INBOX_05_list_rules_ordered_by_weight`

**Locked since:** 2026-04-27

## INBOX-06 — Manual rules outrank statistics

**Status:** Enforced

**Observable:** Any rule I author by hand in the "Learned" tab (`source='user_rule'`) is never overwritten by the automatic implicit learner. If I say "mute @bob," statistics across the next month do not silently undo me.

**Why locked:** Without this, the "Learned" tab is theatre — the user edits a rule, walks away, and the aggregator overrides them. Explicit user intent must beat statistical aggregates.

**Test guards:**
- `internal/inbox/learner_test.go::TestInbox06_UserRuleProtectedFromImplicitOverwrite`
- `WatchtowerDesktop/Tests/InboxLearnedRulesQueriesTests.swift::test_INBOX_06_manual_rule_overrides_implicit`

**Locked since:** 2026-04-27

## INBOX-07 — AI failure does not lose state

**Status:** Enforced

**Observable:** When the pinned-selection AI call errors out or returns unparseable JSON, the existing pinned items are preserved untouched until a future cycle succeeds. The feed does not blank out, items do not reshuffle, the user can keep working on whatever they were focused on.

**Why locked:** Inbox is a "pulse" surface. A flapping AI call that periodically blanks pinned would teach the user to distrust the screen. Stability beats freshness when the alternative is chaos.

**Test guards:**
- `internal/inbox/pinned_selector_test.go::TestInbox07_PinnedKeepsStateOnAIError`
- `internal/inbox/pinned_selector_test.go::TestInbox07_PinnedKeepsStateOnInvalidJSON`

**Locked since:** 2026-04-27

## INBOX-08 — Inbox does not re-spam the same item

**Status:** Aspirational

**Observable:** When an item was shown and I did not engage (no click, no feedback, no resolve), it does not climb back to the top of the feed each cycle. Once it has had its chance, it backs off — visibility decays even if the underlying signal repeats.

**Why locked:** Without this, repeated high-priority items I am intentionally ignoring train me to stop looking at the feed at all (the iOS-Notifications-fatigue effect).

**Test guards:** none yet — see Tracked gap.

**Tracked gap:** Today's pinned-selector rewrites the set from scratch each cycle with no penalty for previously-shown-and-ignored items. Closing this gap requires (a) tracking a "last surfaced and ignored" timestamp on `inbox_items`, (b) feeding it to the pinned-selector AI prompt as a signal, and (c) a Go-side post-filter or weight decay. Implementation is a separate feature plan; for now this contract is documented intent.

**Locked since:** 2026-04-27 (intent recorded; enforcement pending)


## Changelog

- 2026-04-27: file created.
