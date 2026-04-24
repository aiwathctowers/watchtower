# Watchtower — Developer Notes

**Project:** `watchtower` (Go module: `watchtower`)
**Backend:** Go 1.25, SQLite via `modernc.org/sqlite` (`database/sql`), see `go.mod`
**Desktop:** SwiftUI macOS app (Swift 5.10, macOS 14+), GRDB.swift, see `WatchtowerDesktop/Package.swift`

---

## Feature Notes

### Inbox Pulse (v67+)
- `internal/inbox/` — Pipeline now runs phases in order: detectors (slack / jira / calendar / watchtower) → classifier → implicit learner → AI prioritize → AI pinned selector (separate call, max 5) → auto-resolve/archive → unsnooze
- Two item classes: `actionable` (pending/resolved lifecycle) vs `ambient` (auto-seen, auto-archive after 7 days; actionable stale after 14 days)
- `inbox_items.pinned` column; pinned selection is a dedicated AI call that respects learned mute rules (weight ≤ -0.8 filtered out)
- `inbox_learned_rules` table (implicit + explicit_feedback + user_rule sources) — `source='user_rule'` is protected from implicit overwrite. Rules are injected into AI prompts via `buildUserPreferencesBlock`.
- `inbox_feedback` table records raw 👍/👎 + reason; `inbox.SubmitFeedback` in Go maps (rating, reason) → rule upsert or class downgrade.
- Desktop: `InboxFeedView` (replaces the removed `InboxListView`) with pinned section + chronological feed + "Learned" tab for rules management.
- Desktop feedback path: Swift `InboxFeedbackQueries.record(...)` mirrors the Go rule derivation logic so UI is immediately consistent.
