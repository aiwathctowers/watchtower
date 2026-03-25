package briefing

// This file documents the prompt template format for briefing.daily.
//
// The prompt template in prompts/defaults.go uses 10 format verbs (%s):
//   1. userName     — display name of the current user
//   2. date         — YYYY-MM-DD
//   3. role         — user's role (or empty)
//   4. langDirective — "Respond in <language>"
//   5. tracksCtx    — active tracks with status, participants, priority
//   6. digestsCtx   — channel digests from last 24h
//   7. dailyDigestCtx — latest daily rollup
//   8. peopleCardsCtx — latest people cards
//   9. peopleSummaryCtx — latest team summary
//  10. profileCtx   — user profile (role, team, reports, etc.)
