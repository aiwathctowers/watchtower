# Google Calendar Integration: Architecture Design

## Overview

Интеграция Google Calendar позволяет Watchtower учитывать расписание пользователя при генерации брифингов, расстановке приоритетов в inbox и coaching-рекомендациях. Календарь дополняет Slack-данные контекстом: "у тебя 1:1 с Петром через час — подготовь пункты из tracks".

Ключевые принципы:
- **Read-only** — Watchtower только читает события, не создаёт/изменяет
- **Privacy-first** — хранятся только title, time, attendees, response status (не body/attachments/conference links)
- **Offline-first** — события кешируются в SQLite, AI работает с локальными данными
- **Graceful degradation** — без Calendar всё работает как раньше, Calendar лишь обогащает

---

## Go Backend

### 1. Новый пакет `internal/calendar/`

#### `auth.go` — Google OAuth2:
```go
// GoogleOAuthConfig holds credentials for Google Calendar API.
type GoogleOAuthConfig struct {
    ClientID     string
    ClientSecret string
}

// TokenStore persists and loads OAuth2 refresh/access tokens.
type TokenStore struct {
    path string // ~/.local/share/watchtower/{workspace}/google_token.json
}

func NewTokenStore(workspaceDir string) *TokenStore
func (s *TokenStore) Load() (*oauth2.Token, error)
func (s *TokenStore) Save(token *oauth2.Token) error
func (s *TokenStore) Delete() error

// Login performs Google OAuth2 flow (browser-based, like Slack OAuth).
// Uses localhost HTTPS callback on port 18501-18510 (separate range from Slack 18491-18500).
// Scopes: https://www.googleapis.com/auth/calendar.readonly
func Login(ctx context.Context, cfg GoogleOAuthConfig, out io.Writer, opts ...LoginOptions) (*oauth2.Token, error)

// Prepare generates an authorization URL for desktop app flow.
func Prepare(cfg GoogleOAuthConfig, customRedirectURI string) (*PrepareResult, error)

// Complete exchanges authorization code for tokens.
func Complete(ctx context.Context, cfg GoogleOAuthConfig, code, redirectURI string) (*oauth2.Token, error)
```

Ключевое:
- Те же паттерны что `internal/auth/oauth.go` — browser open, self-signed TLS, state validation
- Separate port range: 18501-18510 (Slack uses 18491-18500)
- Token хранится в `{workspaceDir}/google_token.json` (per-workspace)
- `DefaultGoogleClientID` / `DefaultGoogleClientSecret` injected via `-ldflags`
- Scope: `calendar.readonly` (events.list + calendars.list)

#### `client.go` — Google Calendar API client:
```go
type Client struct {
    service *googlecalendar.Service // google.golang.org/api/calendar/v3
    userID  string                  // "primary" or specific calendar ID
}

func NewClient(ctx context.Context, token *oauth2.Token, cfg GoogleOAuthConfig) (*Client, error)

// FetchEvents fetches events from primary calendar within a time range.
// Returns only events where user is organizer or accepted/tentative attendee.
func (c *Client) FetchEvents(ctx context.Context, timeMin, timeMax time.Time) ([]CalendarEvent, error)

// FetchCalendars lists visible calendars (for settings UI).
func (c *Client) FetchCalendars(ctx context.Context) ([]CalendarInfo, error)
```

#### `models.go` — data models:
```go
type CalendarEvent struct {
    ID             string    // Google event ID
    Title          string    // event summary (sanitized, no body)
    StartTime      time.Time // UTC
    EndTime        time.Time // UTC
    IsAllDay       bool
    Location       string    // optional
    Organizer      string    // email
    Attendees      []Attendee
    ResponseStatus string    // "accepted", "tentative", "declined", "needsAction"
    Recurring      bool
    CalendarID     string
}

type Attendee struct {
    Email          string `json:"email"`
    DisplayName    string `json:"display_name"`
    ResponseStatus string `json:"response_status"` // accepted/declined/tentative/needsAction
    SlackUserID    string `json:"slack_user_id"`    // resolved via email→users.email lookup
}

type CalendarInfo struct {
    ID       string
    Summary  string
    Primary  bool
    Selected bool // user-configured in settings
}
```

#### `sync.go` — calendar sync pipeline:
```go
type Syncer struct {
    client     *Client
    db         *db.DB
    logger     *log.Logger
}

func NewSyncer(client *Client, database *db.DB, logger *log.Logger) *Syncer

// Sync fetches events for the next 7 days + past 1 day and upserts to DB.
// Returns count of new/updated events.
func (s *Syncer) Sync(ctx context.Context) (int, error)

// ResolveAttendees matches attendee emails to Slack user_ids via users.email column.
func (s *Syncer) ResolveAttendees(events []CalendarEvent) []CalendarEvent
```

Ключевое:
- Window: yesterday → +7 days (configurable via `calendar.lookahead_days`)
- Upsert by Google event ID (idempotent)
- Attendee resolution: `SELECT id FROM users WHERE email = ?` — best effort, no API call
- Rate limit: Google Calendar API has generous limits (1M queries/day), no special handling needed
- Sync triggered by daemon after Slack sync, before pipelines

### 2. Database Schema (migration v55)

```sql
-- Synced Google Calendars (multi-calendar support)
CREATE TABLE IF NOT EXISTS calendar_calendars (
    id          TEXT PRIMARY KEY,     -- Google calendar ID (e.g. "primary", "user@gmail.com")
    summary     TEXT NOT NULL,        -- calendar display name
    is_primary  INTEGER NOT NULL DEFAULT 0,
    is_selected INTEGER NOT NULL DEFAULT 1, -- user toggle in settings
    color       TEXT NOT NULL DEFAULT '',    -- hex color from Google
    synced_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Calendar events (synced from Google Calendar)
CREATE TABLE IF NOT EXISTS calendar_events (
    id              TEXT PRIMARY KEY,     -- Google event ID
    calendar_id     TEXT NOT NULL DEFAULT 'primary',
    title           TEXT NOT NULL,
    start_time      REAL NOT NULL,        -- Unix timestamp
    end_time        REAL NOT NULL,        -- Unix timestamp
    is_all_day      INTEGER NOT NULL DEFAULT 0,
    location        TEXT NOT NULL DEFAULT '',
    organizer       TEXT NOT NULL DEFAULT '',  -- email
    attendees       TEXT NOT NULL DEFAULT '[]', -- JSON array of Attendee
    response_status TEXT NOT NULL DEFAULT 'accepted',
    recurring       INTEGER NOT NULL DEFAULT 0,
    synced_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    FOREIGN KEY (calendar_id) REFERENCES calendar_calendars(id)
);
CREATE INDEX IF NOT EXISTS idx_calendar_start ON calendar_events(start_time);
CREATE INDEX IF NOT EXISTS idx_calendar_end ON calendar_events(end_time);
CREATE INDEX IF NOT EXISTS idx_calendar_cal_id ON calendar_events(calendar_id);
```

DB queries for calendars (`internal/db/calendar.go`):
```go
func (db *DB) UpsertCalendarCalendar(c CalendarCalendar) error
func (db *DB) GetCalendarCalendars() ([]CalendarCalendar, error)
func (db *DB) GetSelectedCalendarIDs() ([]string, error)          // where is_selected=1
func (db *DB) SetCalendarSelected(calendarID string, selected bool) error
func (db *DB) ClearCalendarCalendars() error                      // for disconnect
```

DB model (`internal/db/models.go`):
```go
type CalendarCalendar struct {
    ID         string
    Summary    string
    IsPrimary  bool
    IsSelected bool
    Color      string
    SyncedAt   string
}
```

DB queries (`internal/db/calendar.go`):
```go
func (db *DB) UpsertCalendarEvent(e CalendarEvent) error
func (db *DB) GetCalendarEvents(from, to float64) ([]CalendarEvent, error)  // by time range
func (db *DB) GetTodayCalendarEvents() ([]CalendarEvent, error)             // convenience
func (db *DB) GetUpcomingEvents(hours int) ([]CalendarEvent, error)         // next N hours
func (db *DB) DeleteOldCalendarEvents(before float64) error                 // cleanup >30 days old
func (db *DB) ClearCalendarEvents() error                                   // for disconnect
```

DB model (`internal/db/models.go`):
```go
type CalendarEvent struct {
    ID             string
    CalendarID     string
    Title          string
    StartTime      float64 // Unix timestamp
    EndTime        float64 // Unix timestamp
    IsAllDay       bool
    Location       string
    Organizer      string
    Attendees      string  // JSON
    ResponseStatus string
    Recurring      bool
    SyncedAt       string
}
```

### 3. Config

`internal/config/config.go` — добавить:
```go
// CalendarConfig holds Google Calendar integration settings.
type CalendarConfig struct {
    Enabled       bool   `mapstructure:"enabled"`        // enable calendar sync (default: false)
    LookaheadDays int    `mapstructure:"lookahead_days"`  // days ahead to fetch (default: 7)
    CalendarIDs   []string `mapstructure:"calendar_ids"`  // specific calendar IDs (default: ["primary"])
}

// В Config:
Calendar CalendarConfig `mapstructure:"calendar"`
```

`internal/config/defaults.go`:
```go
const (
    DefaultCalendarEnabled       = false
    DefaultCalendarLookaheadDays = 7
)
```

YAML формат:
```yaml
calendar:
  enabled: true
  lookahead_days: 7
  calendar_ids: ["primary"]  # optional, defaults to primary
```

### 4. Daemon Integration

`internal/daemon/daemon.go`:
```go
// Add to Daemon struct:
calendarSyncer *calendar.Syncer

// New setter:
func (d *Daemon) SetCalendarSyncer(s *calendar.Syncer)

// In runSync(), after Slack sync, before pipelines:
if d.calendarSyncer != nil {
    n, err := d.calendarSyncer.Sync(ctx)
    if err != nil {
        d.logger.Printf("calendar sync error: %v", err)
    } else if n > 0 {
        d.logger.Printf("calendar: %d events synced", n)
    }
}
```

### 5. Briefing Integration

`internal/briefing/pipeline.go` — новый gather:
```go
func (p *Pipeline) gatherCalendar() string {
    events, err := p.db.GetTodayCalendarEvents()
    if err != nil || len(events) == 0 {
        return ""
    }
    // Format: === CALENDAR ===
    // Each event: time range, title, attendees (with Slack names resolved)
    // Flag meetings with people who have open tracks/inbox items
    var buf strings.Builder
    buf.WriteString("=== CALENDAR (today's meetings) ===\n")
    for _, e := range events {
        // Format each event with time, title, attendees
        buf.WriteString(formatCalendarEvent(e, p.db))
    }
    return buf.String()
}

func formatCalendarEvent(e db.CalendarEvent, database *db.DB) string {
    // Resolve attendee slack_user_ids to names
    // Mark attendees who have open tracks (ball_on=them) or pending inbox
    // Example output:
    // - 10:00-11:00 "Sprint Planning" — @john (has 2 open tracks), @alice, @bob
}
```

Prompt injection (в `briefing.daily`):
```
=== CALENDAR (today's meetings) ===
- 10:00-11:00 "Sprint Planning" — @john (has 2 open tracks), @alice, @bob
- 14:00-14:30 "1:1 with Alice" — @alice (3 pending inbox items from her)
- 16:00-17:00 "Design Review" — @charlie (new red flag: volume_drop)
```

AI uses this to:
- Suggest preparation items in `your_day` ("before 1:1 with Alice, review her pending items")
- Cross-reference `attention` with upcoming meetings ("you meet John in 2h — discuss track #42")
- Flag scheduling conflicts with task deadlines

### 6. AI Chat Context Integration

`internal/ai/context_builder.go` — add calendar tier:
```go
// In Build(), add after existing tiers:
if calendarCtx := cb.buildCalendarContext(query); calendarCtx != "" {
    sections = append(sections, calendarCtx)
}

func (cb *ContextBuilder) buildCalendarContext(query ParsedQuery) string {
    events, err := cb.db.GetUpcomingEvents(48) // next 48 hours
    if err != nil || len(events) == 0 {
        return ""
    }
    // Budget: ~500 tokens from broad tier
    var buf strings.Builder
    buf.WriteString("=== UPCOMING CALENDAR ===\n")
    for _, e := range events {
        buf.WriteString(formatEventForChat(e, cb.db))
    }
    return buf.String()
}
```

### 7. CLI Commands

`cmd/calendar.go`:
```go
// watchtower calendar — show today's events
var calendarCmd = &cobra.Command{
    Use:   "calendar",
    Short: "Show upcoming calendar events",
    RunE:  runCalendar,
}

// watchtower calendar login — Google OAuth login
var calendarLoginCmd = &cobra.Command{
    Use:   "login",
    Short: "Connect Google Calendar",
    RunE:  runCalendarLogin,
}

// watchtower calendar logout — disconnect
var calendarLogoutCmd = &cobra.Command{
    Use:   "logout",
    Short: "Disconnect Google Calendar",
    RunE:  runCalendarLogout,
}

// watchtower calendar sync — manual sync
var calendarSyncCmd = &cobra.Command{
    Use:   "sync",
    Short: "Sync calendar events",
    RunE:  runCalendarSync,
}
```

Flags: `--days N` (override lookahead), `--json` (JSON output).

### 8. Meeting Prep Pipeline

Новый пакет `internal/meeting/` + промпт `meeting.prep` + CLI + Swift view.

#### `internal/meeting/pipeline.go`:
```go
type Pipeline struct {
    db          *db.DB
    cfg         *config.Config
    generator   digest.Generator
    logger      *log.Logger
    promptStore *prompts.Store
}

func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline
func (p *Pipeline) SetPromptStore(store *prompts.Store)

// MeetingPrepResult is the AI output for a single meeting.
type MeetingPrepResult struct {
    EventID       string          `json:"event_id"`
    Title         string          `json:"title"`
    StartTime     string          `json:"start_time"`     // ISO8601
    TalkingPoints []TalkingPoint  `json:"talking_points"`
    OpenItems     []OpenItem      `json:"open_items"`     // tracks/inbox/tasks involving attendees
    PeopleNotes   []PersonNote    `json:"people_notes"`   // communication tips per attendee
    SuggestedPrep []string        `json:"suggested_prep"` // what to read/review before the meeting
}

type TalkingPoint struct {
    Text       string `json:"text"`
    SourceType string `json:"source_type"` // track, digest, inbox, task
    SourceID   string `json:"source_id"`
    Priority   string `json:"priority"`    // high, medium, low
}

type OpenItem struct {
    Text       string `json:"text"`
    Type       string `json:"type"`    // track, inbox, task
    ID         string `json:"id"`
    PersonName string `json:"person_name"`
    PersonID   string `json:"person_id"`
}

type PersonNote struct {
    UserID            string `json:"user_id"`
    Name              string `json:"name"`
    CommunicationTip  string `json:"communication_tip"`  // from people card
    RecentContext     string `json:"recent_context"`      // what they've been working on
}

// PrepareForEvent generates meeting prep for a single calendar event.
func (p *Pipeline) PrepareForEvent(ctx context.Context, eventID string) (*MeetingPrepResult, error)

// PrepareForNext generates meeting prep for the next upcoming event.
func (p *Pipeline) PrepareForNext(ctx context.Context) (*MeetingPrepResult, error)
```

`PrepareForEvent` flow:
1. Load event from `calendar_events` by ID
2. Resolve attendees → Slack user_ids (already in attendees JSON)
3. Gather per-attendee context:
   - Open tracks where attendee is participant (`tracks.participants` JSON contains their user_id)
   - Pending inbox items from attendee (`inbox_items.sender_user_id`)
   - Active tasks related to attendee (`tasks.ball_on`)
   - Latest people card for attendee (communication_guide, decision_style)
4. Gather cross-attendee context:
   - Recent channel digests from shared channels
   - Active tracks involving multiple attendees
5. Build prompt with all context → AI generates MeetingPrepResult

#### `internal/meeting/prompt.go`:
```go
// Documents the prompt template format for meeting.prep.
// The prompt template uses 7 format verbs (%s):
//   1. userName       — current user's display name
//   2. meetingTitle   — event title
//   3. meetingTime    — "10:00-11:00"
//   4. langDirective  — "Respond in <language>"
//   5. attendeesCtx   — attendee details with people cards + open items
//   6. sharedCtx      — shared tracks, digest highlights, recent decisions
//   7. profileCtx     — user profile context
```

#### Prompt `meeting.prep` (new prompt ID in `internal/prompts/`):

```go
const MeetingPrep = "meeting.prep"
```

Default prompt template:
```
You are preparing a meeting brief for %s ahead of "%s" at %s.

Your job is to help the user walk into this meeting fully prepared. Synthesize available data about attendees, shared work, and open items into actionable prep.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "event_id": "google-event-id",
  "title": "Meeting title",
  "start_time": "ISO8601",
  "talking_points": [
    {"text": "Topic to raise or discuss", "source_type": "track|digest|inbox|task", "source_id": "123", "priority": "high|medium|low"}
  ],
  "open_items": [
    {"text": "Unresolved item involving an attendee", "type": "track|inbox|task", "id": "456", "person_name": "@alice", "person_id": "U123"}
  ],
  "people_notes": [
    {"user_id": "U123", "name": "@alice", "communication_tip": "Prefers data-driven arguments", "recent_context": "Leading the migration project, under deadline pressure"}
  ],
  "suggested_prep": [
    "Review track #42 (blocked, involves @alice and @bob)",
    "Check digest from #engineering — decision about API versioning"
  ]
}

Rules:
- talking_points: max 7. Prioritize: blocked items > decisions needed > FYI updates. Include source references.
- open_items: items specifically involving meeting attendees. Include tracks where attendee is ball_on, pending inbox from them, shared tasks.
- people_notes: only for attendees the user interacts with regularly. Use people card data if available. Skip if no useful context.
- suggested_prep: max 5. Specific links/references to read before the meeting. Not generic advice.
- Be concrete: name tracks by title, reference specific decisions, cite channel digests.
- If meeting has >8 attendees, focus people_notes on the user's direct reports and key collaborators.
- If no relevant data exists for an attendee, omit them from people_notes — don't fabricate.
- %s
- Return valid JSON only.

=== MEETING ATTENDEES ===
%s

=== SHARED CONTEXT (tracks, digests, decisions involving attendees) ===
%s

=== USER PROFILE ===
%s
```

#### CLI: `cmd/meeting.go`:
```go
// watchtower meeting-prep [event-id|next] — generate meeting preparation
var meetingPrepCmd = &cobra.Command{
    Use:   "meeting-prep [event-id|next]",
    Short: "Generate AI-powered meeting preparation",
    Long:  "Analyzes attendees, shared tracks, open items, and people cards to create a meeting brief.",
    Args:  cobra.MaximumNArgs(1),
    RunE:  runMeetingPrep,
}
```

`runMeetingPrep` logic:
- No args or "next" → `PrepareForNext(ctx)` (next upcoming event with >1 attendee)
- Event ID → `PrepareForEvent(ctx, eventID)`
- Output: formatted markdown (default) or JSON (`--json`)

Flags: `--json` (JSON output).

### 9. Briefing Prompt: Calendar Instructions

The `briefing.daily` prompt template gains a 13th `%s` verb for calendar context.

Current format verbs (12): userName, date, role, langDirective, tasksCtx, inboxCtx, tracksCtx, digestsCtx, dailyDigestCtx, peopleCardsCtx, peopleSummaryCtx, profileCtx.

New format verbs (13): userName, date, role, langDirective, tasksCtx, inboxCtx, **calendarCtx**, tracksCtx, digestsCtx, dailyDigestCtx, peopleCardsCtx, peopleSummaryCtx, profileCtx.

Add after `=== INBOX ===` section in `defaultBriefingDaily`:
```
=== CALENDAR (today's meetings) ===
%s
```

Add to Rules section:
```
- CALENDAR INTEGRATION: When calendar events are present, cross-reference attendees with tracks, inbox, and people data.
  - In "attention": flag meetings in the next 2 hours with unresolved items involving attendees ("You meet @alice at 14:00 — 3 inbox items pending from her")
  - In "your_day": interleave meetings with tasks/tracks, ordered chronologically. Add prep suggestions before important meetings.
  - In "coaching": suggest conversation points based on people cards of attendees ("@bob prefers data-driven arguments — bring metrics to the 15:00 review")
  - If a meeting attendee has a people card with red_flags, mention it in team_pulse.
  - Do NOT list meetings as standalone items — always cross-reference with work data.
  - If CALENDAR section is empty, ignore calendar instructions entirely.
```

`internal/briefing/pipeline.go` changes:
```go
// Add gatherCalendar to the gather block:
calendarCtx := p.gatherCalendar()

// Update Sprintf call (13 verbs):
systemPrompt := fmt.Sprintf(promptTmpl,
    userName, date, role,
    langDirective,
    tasksCtx,
    inboxCtx,
    calendarCtx,   // NEW — position 7
    tracksCtx,
    digestsCtx,
    dailyDigestCtx,
    peopleCardsCtx,
    peopleSummaryCtx,
    profileCtx,
)
```

`internal/briefing/prompt.go` — update comment:
```go
// The prompt template in prompts/defaults.go uses 13 format verbs (%s):
//   1. userName       — display name of the current user
//   2. date           — YYYY-MM-DD
//   3. role           — user's role (or empty)
//   4. langDirective  — "Respond in <language>"
//   5. tasksCtx       — active tasks with priority and due dates
//   6. inboxCtx       — pending inbox items awaiting response
//   7. calendarCtx    — today's calendar events with attendee context (NEW)
//   8. tracksCtx      — active tracks with status, participants, priority
//   9. digestsCtx     — channel digests from last 24h
//  10. dailyDigestCtx — latest daily rollup
//  11. peopleCardsCtx — latest people cards
//  12. peopleSummaryCtx — latest team summary
//  13. profileCtx     — user profile (role, team, reports, etc.)
```

Prompt version bump: `BriefingDaily: 4` (v4: calendar integration).

### 10. Go файлы (scope для Go Dev):

**Новые:**
1. `internal/calendar/auth.go` — Google OAuth2 flow (browser + token store)
2. `internal/calendar/client.go` — Google Calendar API client
3. `internal/calendar/models.go` — CalendarEvent, Attendee, CalendarInfo
4. `internal/calendar/sync.go` — calendar sync pipeline
5. `internal/db/calendar.go` — DB queries for calendar_events + calendar_calendars
6. `internal/meeting/pipeline.go` — Meeting Prep pipeline
7. `internal/meeting/prompt.go` — prompt format documentation
8. `cmd/calendar.go` — CLI commands (calendar, login, logout, sync)
9. `cmd/meeting.go` — CLI command (meeting-prep)

**Изменяемые:**
10. `internal/db/db.go` — migration v55 (calendar_calendars + calendar_events tables)
11. `internal/db/models.go` — добавить CalendarEvent, CalendarCalendar structs
12. `internal/db/schema.sql` — добавить calendar_calendars + calendar_events tables
13. `internal/config/config.go` — добавить CalendarConfig
14. `internal/config/defaults.go` — добавить default constants
15. `internal/daemon/daemon.go` — добавить calendarSyncer, sync integration
16. `internal/briefing/pipeline.go` — gatherCalendar(), inject in prompt (13th %s verb)
17. `internal/briefing/prompt.go` — update format verb documentation
18. `internal/prompts/defaults.go` — добавить MeetingPrep prompt + update defaultBriefingDaily (v4: calendar)
19. `internal/prompts/store.go` — добавить MeetingPrep const
20. `internal/ai/context_builder.go` — buildCalendarContext()
21. `cmd/watch.go` — wire calendar syncer in daemon setup
22. `go.mod` / `go.sum` — add `google.golang.org/api` + `golang.org/x/oauth2`

---

## Swift Desktop

### 1. CalendarService.swift — новый сервис

```swift
@MainActor
@Observable
final class CalendarService {
    var isConnected: Bool = false
    var events: [CalendarEventItem] = []
    var calendars: [CalendarInfoItem] = []
    var syncError: String?

    private let dbPool: DatabasePool

    init(dbPool: DatabasePool) {
        self.dbPool = dbPool
        checkConnection()
    }

    /// Check if Google Calendar is connected (token file exists).
    func checkConnection()

    /// Fetch today's events from local DB.
    func loadTodayEvents()

    /// Fetch upcoming events for N hours from local DB.
    func loadUpcomingEvents(hours: Int = 48)

    /// Trigger calendar login via CLI subprocess.
    func login() async throws

    /// Disconnect: remove token, clear events.
    func logout() async throws
}
```

### 2. CalendarEventItem model (Swift):
```swift
struct CalendarEventItem: FetchableRecord, Identifiable {
    var id: String              // Google event ID
    var calendarID: String
    var title: String
    var startTime: Double       // Unix timestamp
    var endTime: Double         // Unix timestamp
    var isAllDay: Bool
    var location: String
    var organizer: String
    var attendees: String       // JSON
    var responseStatus: String
    var recurring: Bool
    var syncedAt: String

    // Computed:
    var startDate: Date { Date(timeIntervalSince1970: startTime) }
    var endDate: Date { Date(timeIntervalSince1970: endTime) }
    var duration: TimeInterval { endTime - startTime }
    var decodedAttendees: [CalendarAttendee] { ... }
    var isHappeningNow: Bool { ... }
    var isUpcoming: Bool { ... } // starts within 1 hour
}

struct CalendarAttendee: Codable {
    let email: String
    let displayName: String
    let responseStatus: String
    let slackUserID: String
}
```

### 3. CalendarCalendarItem model (Swift):
```swift
struct CalendarCalendarItem: FetchableRecord, Identifiable {
    var id: String              // Google calendar ID
    var summary: String
    var isPrimary: Bool
    var isSelected: Bool
    var color: String
    var syncedAt: String
}
```

### 4. CalendarQueries (Swift):
```swift
struct CalendarQueries {
    static func fetchToday(db: Database) throws -> [CalendarEventItem]
    static func fetchUpcoming(db: Database, hours: Int) throws -> [CalendarEventItem]
    static func fetchByDateRange(db: Database, from: Double, to: Double) throws -> [CalendarEventItem]
    static func eventCount(db: Database) throws -> Int
    static func fetchCalendars(db: Database) throws -> [CalendarCalendarItem]
    static func setCalendarSelected(db: Database, calendarID: String, selected: Bool) throws
}
```

### 4. BriefingDetailView integration:

В `BriefingDetailView.swift` — добавить секцию "Today's Schedule" перед Attention:
```swift
if !calendarEvents.isEmpty {
    Section("Today's Schedule") {
        ForEach(calendarEvents) { event in
            CalendarEventRow(event: event)
        }
    }
}
```

`CalendarEventRow.swift`:
```swift
struct CalendarEventRow: View {
    let event: CalendarEventItem

    var body: some View {
        HStack {
            VStack(alignment: .leading) {
                Text(event.title).font(.body)
                Text(timeRange).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if !event.decodedAttendees.isEmpty {
                Text("\(event.decodedAttendees.count) attendees")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }
}
```

### 5. Meeting Prep View (Swift):

`MeetingPrepView.swift` — generates and displays meeting prep for an event:
```swift
struct MeetingPrepView: View {
    let event: CalendarEventItem
    @State private var prep: MeetingPrepResult?
    @State private var isLoading = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            if isLoading {
                ProgressView("Preparing meeting brief...")
            } else if let prep = prep {
                VStack(alignment: .leading, spacing: 16) {
                    meetingHeader(prep)
                    talkingPointsSection(prep.talkingPoints)
                    openItemsSection(prep.openItems)
                    peopleNotesSection(prep.peopleNotes)
                    suggestedPrepSection(prep.suggestedPrep)
                }
                .padding()
            }
        }
        .task { await generatePrep() }
    }

    private func generatePrep() async {
        // Runs: watchtower meeting-prep <event-id> --json
        // Parses MeetingPrepResult from JSON output
    }
}

struct MeetingPrepResult: Codable {
    let eventID: String
    let title: String
    let startTime: String
    let talkingPoints: [TalkingPoint]
    let openItems: [OpenItem]
    let peopleNotes: [PersonNote]
    let suggestedPrep: [String]
}

struct TalkingPoint: Codable, Identifiable {
    var id: String { text }
    let text: String
    let sourceType: String
    let sourceID: String
    let priority: String
}

struct OpenItem: Codable, Identifiable {
    var id: String { "\(type)-\(itemID)" }
    let text: String
    let type: String
    let itemID: String
    let personName: String
    let personID: String

    enum CodingKeys: String, CodingKey {
        case text, type, personName = "person_name", personID = "person_id"
        case itemID = "id"
    }
}

struct PersonNote: Codable, Identifiable {
    var id: String { userID }
    let userID: String
    let name: String
    let communicationTip: String
    let recentContext: String
}
```

Entry points:
- `CalendarEventRow` context menu → "Prepare for Meeting" → opens `MeetingPrepView`
- `CalendarDayView` toolbar button → "Prep Next Meeting" → runs for next event
- `BriefingDetailView` — meetings with >2 attendees show "Prep" button

### 6. Settings UI — Calendar section:

`SettingsView.swift` — добавить `calendarSection`:
```swift
private var calendarSection: some View {
    Section("Google Calendar") {
        if calendarService.isConnected {
            HStack {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text("Connected")
                Spacer()
                Button("Disconnect") {
                    Task { try? await calendarService.logout() }
                }
            }

            Picker("Lookahead Days", selection: $config.calendarLookaheadDays) {
                ForEach([3, 5, 7, 14], id: \.self) { days in
                    Text("\(days) days").tag(days)
                }
            }

            // Multi-calendar picker (from calendar_calendars table)
            if !calendarService.calendars.isEmpty {
                DisclosureGroup("Calendars") {
                    ForEach(calendarService.calendars) { cal in
                        Toggle(cal.summary, isOn: Binding(
                            get: { cal.isSelected },
                            set: { calendarService.setCalendarSelected(cal.id, selected: $0) }
                        ))
                    }
                }
            }
        } else {
            HStack {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.secondary)
                Text("Not connected")
                Spacer()
                Button("Connect Google Calendar") {
                    Task { try? await calendarService.login() }
                }
                .buttonStyle(.borderedProminent)
            }
        }
    }
}
```

### 6. Sidebar — Calendar widget (optional):

В `SidebarView.swift` — внизу под sync status:
```swift
if calendarService.isConnected, let nextEvent = calendarService.nextEvent {
    HStack(spacing: 4) {
        Image(systemName: "calendar")
            .foregroundStyle(.secondary)
        VStack(alignment: .leading) {
            Text(nextEvent.title).font(.caption).lineLimit(1)
            Text(nextEvent.startDate, style: .relative).font(.caption2).foregroundStyle(.secondary)
        }
    }
    .padding(.horizontal, 12)
    .padding(.vertical, 4)
}
```

### 7. ConfigService — calendar config:

```swift
// Add to ConfigService:
var calendarEnabled: Bool = false
var calendarLookaheadDays: Int = 7

// In reload():
if let calendar = yaml["calendar"] as? [String: Any] {
    calendarEnabled = (calendar["enabled"] as? Bool) ?? false
    calendarLookaheadDays = (calendar["lookahead_days"] as? Int) ?? 7
}

// In save():
calendarDict["enabled"] = calendarEnabled
calendarDict["lookahead_days"] = calendarLookaheadDays
```

### 8. Swift файлы (scope для Swift Dev):

**Новые:**
1. `WatchtowerDesktop/Sources/Services/CalendarService.swift` — sync/auth service
2. `WatchtowerDesktop/Sources/Models/CalendarEventItem.swift` — CalendarEventItem + CalendarAttendee
3. `WatchtowerDesktop/Sources/Models/CalendarCalendarItem.swift` — CalendarCalendarItem
4. `WatchtowerDesktop/Sources/Database/CalendarQueries.swift` — DB queries (events + calendars)
5. `WatchtowerDesktop/Sources/Views/Calendar/CalendarEventRow.swift` — event row component
6. `WatchtowerDesktop/Sources/Views/Calendar/CalendarDayView.swift` — daily schedule view
7. `WatchtowerDesktop/Sources/Views/Calendar/MeetingPrepView.swift` — AI meeting prep view
8. `WatchtowerDesktop/Sources/Models/MeetingPrepResult.swift` — MeetingPrepResult + sub-models

**Изменяемые:**
9. `WatchtowerDesktop/Sources/Views/Settings/SettingsView.swift` — calendar section + calendar picker
10. `WatchtowerDesktop/Sources/Services/ConfigService.swift` — calendarEnabled, calendarLookaheadDays
11. `WatchtowerDesktop/Sources/Views/Briefings/BriefingDetailView.swift` — calendar section + "Prep" button
12. `WatchtowerDesktop/Sources/Views/Sidebar/SidebarView.swift` — next event widget
13. `WatchtowerDesktop/Sources/App/AppState.swift` — CalendarService initialization
14. `WatchtowerDesktop/Sources/Database/TestDatabase.swift` — calendar_calendars + calendar_events in test schema

---

## Config YAML формат (Go <-> Swift контракт)

```yaml
calendar:
  enabled: true                # bool, default false
  lookahead_days: 7            # int, default 7
  calendar_ids: ["primary"]    # string array, optional
```

Token storage: `~/.local/share/watchtower/{workspace}/google_token.json`
```json
{
  "access_token": "ya29...",
  "refresh_token": "1//...",
  "token_type": "Bearer",
  "expiry": "2026-04-02T08:00:00Z"
}
```

## Data Flow

```
Google Calendar API
       |
       v
calendar.Client.FetchEvents()    ← triggered by daemon or CLI
       |
       v
calendar.Syncer.Sync()           ← resolves attendee emails → Slack user_ids
       |
       v
db.UpsertCalendarEvent()         ← stored in calendar_events table
       |
       v
  ┌────┼──────────────────┐
  |    |                  |
  v    v                  v
briefing.gatherCalendar()   ai.buildCalendarContext()   Swift CalendarQueries.fetchToday()
  |                           |                              |
  v                           v                              v
AI prompt enrichment        Chat context                  Desktop UI
```

## Security & Privacy

1. **Minimal scopes**: `calendar.readonly` only — no write access
2. **No body/description stored**: Only title, time, attendees, location
3. **Token per-workspace**: Each workspace has its own Google token
4. **Token encryption**: Stored as plain JSON in workspace dir (same security model as Slack token in config.yaml)
5. **Disconnect = full cleanup**: `calendar logout` deletes token + clears all calendar_events rows
6. **No conference links**: We strip Google Meet/Zoom links from event data

## Dependencies

Go:
- `google.golang.org/api v0.xxx` (calendar/v3)
- `golang.org/x/oauth2` (Google OAuth2 flow)

Swift:
- No new dependencies — calendar data comes from SQLite (same GRDB)

## OAuth Flow (CLI)

```
User runs: watchtower calendar login
    |
    v
1. Start HTTPS server on 127.0.0.1:18501-18510 (self-signed TLS, same pattern as Slack)
2. Open browser → Google consent screen (calendar.readonly scope)
3. User grants access → Google redirects to localhost callback
4. Exchange code → access_token + refresh_token
5. Save tokens to {workspaceDir}/google_token.json
6. Test: fetch 1 event → confirm working
```

## OAuth Flow (Desktop)

```
1. CalendarService.login() → watchtower calendar login --skip-browser-open
2. Display URL to user OR use ASWebAuthenticationSession
3. CLI handles callback, stores token
4. CalendarService detects token file → isConnected = true
5. Trigger initial sync
```

## Daemon Lifecycle

```
daemon.Run():
  1. Slack sync (existing)
  2. Calendar sync (NEW — if calendar.enabled && token exists)
  3. Inbox pipeline (existing, async)
  4. Channel digests (existing)
  5. Tracks (existing)
  6. People cards (existing)
  7. Briefing (existing — now includes calendar context)
```

Calendar sync runs:
- On every daemon tick (same as Slack sync)
- Lightweight: typically 5-20 events, single API call
- Automatic token refresh via `golang.org/x/oauth2` transport
- On failure: log warning, continue with stale data (graceful degradation)
