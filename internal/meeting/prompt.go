// Package meeting provides meeting preparation using AI analysis of attendee
// context from tracks, inbox, tasks, and people cards.
package meeting

// Documents the prompt template format for meeting.prep.
// The prompt template uses 7 format verbs (%s):
//
//	1. userName       — current user's display name
//	2. meetingTitle   — event title
//	3. meetingTime    — "10:00-11:00"
//	4. langDirective  — "Respond in <language>"
//	5. attendeesCtx   — attendee details with people cards + open items
//	6. sharedCtx      — shared tracks, digest highlights, recent decisions
//	7. profileCtx     — user profile context
