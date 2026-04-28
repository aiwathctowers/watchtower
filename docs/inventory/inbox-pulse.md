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

<!-- Contracts will be inserted here in subsequent commits. -->

## Changelog

- 2026-04-27: file created.
