package briefing

// This file documents the prompt template format for briefing.daily.
//
// The prompt template in prompts/defaults.go uses 11 format verbs (%s):
//   1. userName     — display name of the current user
//   2. date         — YYYY-MM-DD
//   3. role         — user's role (or empty)
//   4. langDirective — "Respond in <language>"
//   5. tracksCtx    — active/inbox tracks formatted
//   6. chainsCtx    — active chains from last 14 days
//   7. digestsCtx   — channel digests from last 24h
//   8. dailyDigestCtx — latest daily rollup
//   9. peopleCardsCtx — latest people cards
//  10. peopleSummaryCtx — latest team summary
//  11. profileCtx   — user profile (role, team, reports, etc.)
