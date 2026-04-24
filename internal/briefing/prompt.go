package briefing

// This file documents the prompt template format for briefing.daily.
//
// The prompt template in prompts/defaults.go uses 14 format verbs (%s):
//   1. userName       — display name of the current user
//   2. date           — YYYY-MM-DD
//   3. role           — user's role (or empty)
//   4. langDirective  — "Respond in <language>"
//   5. targetsCtx     — active targets with level, priority and due dates
//   6. inboxCtx       — pending inbox items awaiting response
//   7. calendarCtx    — today's calendar events with attendee context
//   8. tracksCtx      — active tracks with status, participants, priority
//   9. digestsCtx     — channel digests from last 24h
//  10. dailyDigestCtx — latest daily rollup
//  11. peopleCardsCtx — latest people cards
//  12. peopleSummaryCtx — latest team summary
//  13. profileCtx     — user profile (role, team, reports, etc.)
//  14. jiraCtx        — Jira issues, sprint progress, stale/overdue signals
