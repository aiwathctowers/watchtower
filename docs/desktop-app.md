# Watchtower Desktop — macOS App Design Document

## Overview

Native macOS application (SwiftUI) providing a visual interface to the Watchtower Slack intelligence platform. The app reads the same SQLite database as the CLI, integrates with Claude CLI for AI conversations, and delivers macOS-native notifications for key decisions.

**Key principle:** the app is a **read-only UI layer** over the existing Go backend. Sync, digest generation, and Slack API interaction remain in the Go CLI daemon. The Swift app focuses exclusively on presentation, AI chat, and notifications.

### Why Native Swift

- First-class macOS integration: native notifications with action buttons, menu bar presence, Spotlight
- Minimal resource footprint vs Electron (~15MB vs ~150MB)
- Full access to UserNotifications, AppKit, and system APIs
- SwiftUI + GRDB observation gives real-time UI updates when the daemon writes to SQLite

### What the App Does NOT Do

- Does not sync from Slack (Go daemon does this)
- Does not generate digests (Go daemon does this)
- Does not manage Slack tokens or OAuth (CLI `auth login` does this)
- Does not write to the database except for: watch list edits, user checkpoint updates

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                    SwiftUI macOS App                           │
│                                                               │
│  ┌───────────┐  ┌───────────┐  ┌──────────┐  ┌────────────┐ │
│  │ Dashboard  │  │  AI Chat  │  │ Digests  │  │  Channels  │ │
│  └─────┬─────┘  └─────┬─────┘  └────┬─────┘  └─────┬──────┘ │
│        │               │             │              │         │
│  ┌─────▼───────────────▼─────────────▼──────────────▼──────┐ │
│  │              GRDB.swift (read + observe)                 │ │
│  │      ~/.local/share/watchtower/{ws}/watchtower.db       │ │
│  └─────────────────────┬───────────────────────────────────┘ │
│                        │                                      │
│  ┌──────────┐  ┌───────▼──────┐  ┌──────────────────────┐   │
│  │ Daemon   │  │  claude CLI  │  │  UserNotifications   │   │
│  │ Manager  │  │  subprocess  │  │  (macOS native)      │   │
│  └────┬─────┘  └───────┬──────┘  └──────────┬───────────┘   │
└───────┼────────────────┼────────────────────┼────────────────┘
        │                │                    │
   ┌────▼──────┐   ┌─────▼─────┐    OS notification center
   │ watchtower │   │  claude   │
   │ sync       │   │  binary   │
   │ --detach   │   └───────────┘
   └────────────┘
```

### Data Flow

1. **Go daemon** syncs Slack → writes to SQLite (WAL mode)
2. **GRDB ValueObservation** detects DB changes → updates SwiftUI views automatically
3. **AI Chat** invokes `claude` CLI as subprocess with streaming JSON output
4. **DigestWatcher** polls for new digests with decisions → fires macOS notifications
5. **DaemonManager** controls Go daemon lifecycle via `watchtower sync --detach` / `watchtower sync stop`

### Concurrency Model

- GRDB database pool for concurrent reads (WAL mode allows this)
- `Process` (Foundation) for Claude CLI subprocess on background thread
- `AsyncStream` bridges subprocess stdout → SwiftUI
- Main actor for all UI updates
- `DigestWatcher` runs as background `Task` with periodic checks

---

## Tech Stack

| Component | Technology | Version | Why |
|-----------|-----------|---------|-----|
| UI | SwiftUI | macOS 14+ | Modern declarative UI, native look |
| SQLite | [GRDB.swift](https://github.com/groue/GRDB.swift) | 7.x | Best Swift SQLite lib, ValueObservation for live updates, FTS5 support |
| Markdown | [swift-markdown](https://github.com/apple/swift-markdown) | 0.5+ | Apple's own Markdown parser → AttributedString |
| YAML | [Yams](https://github.com/jpsim/Yams) | 5.x | Read existing config.yaml |
| Notifications | UserNotifications | macOS 14+ | Native notification center |
| Architecture | MVVM + @Observable | Swift 5.9+ | Clean separation, testable |
| Package Manager | SPM | — | No CocoaPods/Carthage needed |

### SPM Dependencies

```swift
// Package.swift
dependencies: [
    .package(url: "https://github.com/groue/GRDB.swift", from: "7.0.0"),
    .package(url: "https://github.com/apple/swift-markdown", from: "0.5.0"),
    .package(url: "https://github.com/jpsim/Yams", from: "5.0.0"),
]
```

---

## SQLite Schema Reference

The app reads the existing database created by the Go CLI. Schema version 4, WAL mode enabled.

### Tables

#### `workspace`
| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Slack team_id |
| name | TEXT | Workspace name |
| domain | TEXT | Workspace domain |
| synced_at | TEXT | ISO8601 last sync time |
| search_last_date | TEXT | YYYY-MM-DD of last search sync |

#### `channels`
| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Slack channel ID |
| name | TEXT | Channel name |
| type | TEXT | `public`, `private`, `dm`, `group_dm` |
| topic | TEXT | Channel topic |
| purpose | TEXT | Channel purpose |
| is_archived | INTEGER | 0/1 |
| is_member | INTEGER | 0/1 |
| dm_user_id | TEXT | Nullable, for DM channels |
| num_members | INTEGER | Member count |
| updated_at | TEXT | ISO8601 |

#### `messages`
| Column | Type | Notes |
|--------|------|-------|
| channel_id | TEXT | PK part 1 |
| ts | TEXT | PK part 2, Slack timestamp |
| user_id | TEXT | Author |
| text | TEXT | Message content |
| thread_ts | TEXT | Nullable, parent thread timestamp |
| reply_count | INTEGER | Number of replies |
| is_edited | INTEGER | 0/1 |
| is_deleted | INTEGER | 0/1 |
| subtype | TEXT | Slack message subtype |
| permalink | TEXT | Slack permalink |
| ts_unix | REAL | **GENERATED** column: unix timestamp from ts |
| raw_json | TEXT | Original Slack JSON |

**Important:** `ts_unix` is a `GENERATED ALWAYS AS (...) STORED` column. GRDB handles this fine for reads but must never try to write it.

#### `messages_fts` (FTS5 Virtual Table)
```sql
CREATE VIRTUAL TABLE messages_fts USING fts5(
    text,
    channel_id UNINDEXED,
    ts UNINDEXED,
    user_id UNINDEXED,
    tokenize='porter unicode61'
);
```
Kept in sync via triggers on `messages` table. App can query directly:
```sql
SELECT m.* FROM messages m
JOIN messages_fts fts ON fts.channel_id = m.channel_id AND fts.ts = m.ts
WHERE messages_fts MATCH 'search query'
ORDER BY rank
```

#### `users`
| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Slack user ID |
| name | TEXT | Username |
| display_name | TEXT | Display name |
| real_name | TEXT | Full name |
| email | TEXT | Email |
| is_bot | INTEGER | 0/1 |
| is_deleted | INTEGER | 0/1 |
| profile_json | TEXT | JSON blob |
| updated_at | TEXT | ISO8601 |

#### `digests`
| Column | Type | Notes |
|--------|------|-------|
| id | INTEGER PK | Auto-increment |
| channel_id | TEXT | Empty string for cross-channel digests |
| period_from | REAL | Unix timestamp start |
| period_to | REAL | Unix timestamp end |
| type | TEXT | `channel`, `daily`, `weekly` |
| summary | TEXT | Plain text summary |
| topics | TEXT | JSON array of strings |
| decisions | TEXT | JSON array: `[{"text":"...", "by":"...", "message_ts":"..."}]` |
| action_items | TEXT | JSON array: `[{"text":"...", "assignee":"...", "status":"..."}]` |
| message_count | INTEGER | Messages analyzed |
| model | TEXT | AI model used |
| input_tokens | INTEGER | Tokens consumed |
| output_tokens | INTEGER | Tokens produced |
| cost_usd | REAL | Cost in USD |
| created_at | TEXT | ISO8601 |

#### `watch_list`
| Column | Type | Notes |
|--------|------|-------|
| entity_type | TEXT | PK part 1: `channel` or `user` |
| entity_id | TEXT | PK part 2 |
| entity_name | TEXT | Display name |
| priority | TEXT | `high`, `normal`, `low` |
| created_at | TEXT | ISO8601 |

#### Other Tables
- `reactions` — PK(channel_id, message_ts, user_id, emoji)
- `files` — PK(id), linked to messages via (message_channel_id, message_ts)
- `sync_state` — PK(channel_id), tracks pagination cursors and sync progress
- `user_checkpoints` — singleton (id=1), tracks last catchup time

---

## Claude CLI Integration

The app calls `claude` as a subprocess, same approach as the Go code in `internal/ai/client.go`.

### Streaming Query

```
claude \
  -p "user question here" \
  --output-format stream-json \
  --model claude-sonnet-4-6 \
  --system-prompt "You are Watchtower..." \
  --allowedTools "mcp__sqlite__*,Bash(sqlite3*)" \
  --disallowedTools "Edit,Write,NotebookEdit" \
  --mcp-config '{"mcpServers":{"sqlite":{"command":"npx","args":["-y","@anthropic-ai/mcp-server-sqlite","/path/to/watchtower.db"]}}}'
```

### Session Resumption (Multi-turn)

First message uses `--system-prompt`. Subsequent messages use `--resume <session_id>`:
```
claude -p "follow-up question" --output-format stream-json --resume "session-abc123"
```

### Stream JSON Format

Each line of stdout is a JSON object. Relevant event types:

```json
{"type":"assistant","message":{"content":[{"type":"text","text":"Hello..."}]}}
```
```json
{"type":"result","session_id":"session-abc123","result":"Full response text"}
```

**Parsing rules:**
- Read stdout line-by-line (JSONL format)
- For `type == "assistant"`: extract `message.content[*].text` → stream to UI
- For `type == "result"`: capture `session_id` for multi-turn
- Ignore other event types
- stderr is diagnostic only, cap at 64KB

### Error Handling

- `claude` not in PATH → show "Install Claude Code" message with link
- Exit code non-zero → show error from stderr
- Context cancelled → send SIGINT, force SIGKILL after 5s

### MCP SQLite Server

When the DB path is provided, Claude gets direct read access to the SQLite database via MCP. This allows Claude to run its own SQL queries for better answers.

---

## Feature Specifications

### 1. Dashboard

**Purpose:** At-a-glance workspace health and recent activity.

**Components:**
- **Stats cards:** Total channels, total users, total messages, total digests, DB file size
- **Sync status banner:** Last sync time (relative), daemon running/stopped indicator, start/stop button
- **Recent activity feed:** Latest messages from watched channels (last 24h), grouped by channel
- **Quick actions:** "Ask Claude", "Catchup", "Generate Digest" buttons

**Data sources:**
```sql
-- Stats
SELECT COUNT(*) FROM channels;
SELECT COUNT(*) FROM users WHERE is_deleted = 0;
SELECT COUNT(*) FROM messages;
SELECT COUNT(*) FROM digests;

-- Last sync
SELECT synced_at FROM workspace LIMIT 1;

-- Recent watched activity
SELECT m.*, c.name as channel_name, u.display_name as user_name
FROM messages m
JOIN channels c ON c.id = m.channel_id
JOIN users u ON u.id = m.user_id
JOIN watch_list w ON w.entity_type = 'channel' AND w.entity_id = m.channel_id
WHERE m.ts_unix > unixepoch('now', '-1 day')
ORDER BY m.ts_unix DESC
LIMIT 50;
```

### 2. AI Chat

**Purpose:** Conversational AI interface for querying workspace data. Replaces CLI `ask` and `catchup` commands.

**Components:**
- **Message list:** ScrollView + LazyVStack, auto-scroll to bottom
- **User bubble:** Right-aligned, accent color
- **AI bubble:** Left-aligned, with Markdown rendering (code blocks, lists, bold, links)
- **Streaming indicator:** Animated dots while Claude responds, partial text appears in real-time
- **Input area:** Multi-line TextEditor, Cmd+Enter to send, Shift+Enter for newline
- **Quick prompts:** "What happened today?", "Any decisions?", "Summarize #channel"

**AI Context Building:**
The app builds a system prompt with workspace context, similar to Go's `context_builder.go`:
```
You are Watchtower, an AI assistant analyzing Slack workspace "{workspace_name}".
Current time: {now}. Domain: {domain}.slack.com

Workspace: {channel_count} channels, {user_count} users, {message_count} messages.
Last sync: {synced_at}.

{digest_summaries if available}

Answer concisely. Include Slack permalinks where relevant.
```

**Session management:**
- New chat = new session (no `--resume`)
- Follow-up in same chat = `--resume <session_id>`
- Chat history persisted locally (UserDefaults or separate SQLite table)
- Multiple chat sessions via tabs or sidebar list

### 3. Digest Viewer

**Purpose:** Browse AI-generated summaries, decisions, and trends.

**Components:**
- **Filter bar:** Type (channel/daily/weekly), date range picker, channel filter
- **Digest list:** Cards with type icon, date range, channel name (or "Cross-channel"), preview of summary
- **Digest detail:** Full summary, topics as tags, decisions as highlighted cards, action items as checklist
- **Decision cards:** Decision text, "by" attribution, link to original message (permalink)
- **Stats footer:** Model used, tokens consumed, cost

**Data:**
```sql
-- Latest digests
SELECT d.*, c.name as channel_name
FROM digests d
LEFT JOIN channels c ON c.id = d.channel_id
ORDER BY d.created_at DESC
LIMIT 50;

-- Decisions only (parsed from JSON in Swift)
-- Filter digests where decisions != '[]'
```

### 4. Channel Browser

**Purpose:** Explore synced channels and read messages.

**Components:**
- **Channel list:** Sortable by name, message count, last activity. Filter by type (public/private/dm). Watched badge
- **Channel detail:** Header with name, topic, member count, watched toggle
- **Message list:** Chronological, paginated (load-more). User avatar placeholder, name, timestamp, text
- **Slack mrkdwn rendering:** Bold (`*text*`), italic (`_text_`), strikethrough (`~text~`), code (`` `code` ``), code blocks (` ```code``` `), links (`<url|text>`), user mentions (`<@U123>` → resolved name), channel mentions (`<#C123|name>`)
- **Thread view:** Slide-over or nested view showing thread replies
- **Watch toggle:** Star icon to add/remove channel from watch list

**Write operations (only place app writes to DB):**
```sql
-- Add to watch list
INSERT OR REPLACE INTO watch_list (entity_type, entity_id, entity_name, priority)
VALUES ('channel', ?, ?, 'normal');

-- Remove from watch list
DELETE FROM watch_list WHERE entity_type = ? AND entity_id = ?;
```

### 5. Full-Text Search

**Purpose:** Search across all messages using FTS5.

**Components:**
- **Search bar:** In toolbar (Cmd+K shortcut), debounced (300ms)
- **Results:** Grouped by channel, snippet with match highlighting, timestamp, author
- **Filters:** Channel, user, date range
- **Click action:** Navigate to message in Channel Browser

**Query:**
```sql
SELECT m.*, c.name as channel_name, u.display_name as user_name,
       snippet(messages_fts, 0, '<mark>', '</mark>', '...', 64) as snippet
FROM messages m
JOIN messages_fts fts ON fts.channel_id = m.channel_id AND fts.ts = m.ts
JOIN channels c ON c.id = m.channel_id
LEFT JOIN users u ON u.id = m.user_id
WHERE messages_fts MATCH ?
ORDER BY rank
LIMIT 100;
```

### 6. Notifications

**Purpose:** Alert user about important decisions detected in digests.

**Trigger:** `DigestWatcher` service polls `digests` table every 60 seconds for new rows where `decisions != '[]'`.

**Notification content:**
```
Title: "New decision in #engineering"
Body: "We're switching to PostgreSQL for the analytics service — @alice"
Action: "Open in Watchtower" → navigates to digest detail
```

**Configuration (in app Settings):**
- Enable/disable notifications globally
- Enable/disable per notification type: decisions, action items, daily summaries
- Quiet hours: don't notify between HH:MM and HH:MM

**Implementation:**
```swift
UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])

let content = UNMutableNotificationContent()
content.title = "New decision in #\(channelName)"
content.body = decision.text
content.sound = .default
content.userInfo = ["digestId": digest.id, "type": "decision"]

let request = UNNotificationRequest(identifier: "decision-\(digest.id)", content: content, trigger: nil)
UNUserNotificationCenter.current().add(request)
```

### 7. Settings

**Components:**
- **Workspace info:** Name, domain, DB path, DB size
- **Daemon control:** Start/stop, poll interval display, last sync time
- **AI model:** Display current model from config (read-only, configured via CLI)
- **Notifications:** Toggle types, quiet hours
- **About:** App version, links to docs

---

## Project Structure

```
WatchtowerDesktop/
├── WatchtowerDesktop.xcodeproj
├── Package.swift
│
├── Sources/
│   ├── App/
│   │   ├── WatchtowerApp.swift           # @main entry point, app lifecycle
│   │   ├── AppState.swift                # @Observable global state
│   │   └── Navigation.swift              # NavigationSplitView routing
│   │
│   ├── Models/
│   │   ├── Workspace.swift               # GRDB FetchableRecord
│   │   ├── Channel.swift
│   │   ├── Message.swift
│   │   ├── User.swift
│   │   ├── Digest.swift
│   │   ├── WatchItem.swift
│   │   ├── Decision.swift                # Decoded from Digest.decisions JSON
│   │   └── ActionItem.swift              # Decoded from Digest.action_items JSON
│   │
│   ├── Database/
│   │   ├── DatabaseManager.swift         # GRDB DatabasePool, WAL config
│   │   ├── DatabaseObserver.swift        # ValueObservation wrappers
│   │   └── Queries/
│   │       ├── WorkspaceQueries.swift
│   │       ├── ChannelQueries.swift
│   │       ├── MessageQueries.swift
│   │       ├── UserQueries.swift
│   │       ├── DigestQueries.swift
│   │       ├── WatchQueries.swift
│   │       └── SearchQueries.swift       # FTS5 queries
│   │
│   ├── Services/
│   │   ├── ClaudeService.swift           # Process + streaming JSON
│   │   ├── DaemonManager.swift           # watchtower CLI lifecycle
│   │   ├── NotificationService.swift     # UNUserNotificationCenter
│   │   ├── DigestWatcher.swift           # Poll digests → trigger notifs
│   │   └── ConfigService.swift           # Read config.yaml via Yams
│   │
│   ├── ViewModels/
│   │   ├── DashboardViewModel.swift
│   │   ├── ChatViewModel.swift
│   │   ├── DigestViewModel.swift
│   │   ├── ChannelViewModel.swift
│   │   └── SearchViewModel.swift
│   │
│   ├── Views/
│   │   ├── Sidebar/
│   │   │   └── SidebarView.swift
│   │   ├── Dashboard/
│   │   │   ├── DashboardView.swift
│   │   │   ├── StatsCard.swift
│   │   │   ├── ActivityFeed.swift
│   │   │   └── SyncStatusBanner.swift
│   │   ├── Chat/
│   │   │   ├── ChatView.swift
│   │   │   ├── MessageBubble.swift
│   │   │   ├── MarkdownText.swift
│   │   │   ├── ChatInput.swift
│   │   │   └── StreamingIndicator.swift
│   │   ├── Digests/
│   │   │   ├── DigestListView.swift
│   │   │   ├── DigestDetailView.swift
│   │   │   └── DecisionCard.swift
│   │   ├── Channels/
│   │   │   ├── ChannelListView.swift
│   │   │   ├── ChannelDetailView.swift
│   │   │   ├── MessageRow.swift
│   │   │   └── ThreadView.swift
│   │   ├── Search/
│   │   │   ├── SearchView.swift
│   │   │   └── SearchResultRow.swift
│   │   └── Settings/
│   │       ├── SettingsView.swift
│   │       ├── NotificationSettings.swift
│   │       └── DaemonSettings.swift
│   │
│   └── Utilities/
│       ├── SlackTextParser.swift         # Slack mrkdwn → AttributedString
│       ├── TimeFormatting.swift          # Relative timestamps
│       └── Constants.swift               # Paths, defaults
│
├── Tests/
│   ├── DatabaseTests/
│   │   ├── DatabaseManagerTests.swift
│   │   └── QueryTests.swift
│   ├── ServiceTests/
│   │   ├── ClaudeServiceTests.swift
│   │   └── DigestWatcherTests.swift
│   └── ViewModelTests/
│       ├── DashboardViewModelTests.swift
│       └── ChatViewModelTests.swift
│
└── Resources/
    └── Assets.xcassets                   # App icon, colors
```

---

## Progress

### Task 1: Xcode project scaffolding and SPM dependencies
- [x] Create macOS App project `WatchtowerDesktop` with SwiftUI lifecycle, deployment target macOS 14.0
- [x] Add SPM dependencies: GRDB.swift 7.x, ~~swift-markdown 0.5+~~, Yams 5.x *(swift-markdown dropped — using `AttributedString(markdown:)` instead)*
- [x] Create `Sources/` directory structure: App/, Models/, Database/, Services/, ViewModels/, Views/, Utilities/
- [ ] Configure build settings: App Sandbox OFF (needs filesystem + process access), Hardened Runtime ON
- [x] Verify project builds and launches empty window

**Files:** `Package.swift` *(SPM-only, no .xcodeproj)*

### Task 2: Database connection and model layer
- [x] Create `DatabaseManager.swift`: open existing watchtower.db via `DatabasePool` in read-only mode, WAL journal
- [x] Auto-detect DB path: read `~/.config/watchtower/config.yaml` → extract `active_workspace` → resolve `~/.local/share/watchtower/{workspace}/watchtower.db`
- [x] Create `Workspace.swift`: `FetchableRecord, Decodable` with columns id, name, domain, synced_at, search_last_date
- [x] Create `Channel.swift`: `FetchableRecord, Decodable` with all columns including dm_user_id as optional
- [x] Create `Message.swift`: `FetchableRecord, Decodable` with all columns; ts_unix as REAL, thread_ts as optional
- [x] Create `User.swift`: `FetchableRecord, Decodable` with all columns; is_bot/is_deleted as Bool
- [x] Create `Digest.swift`: `FetchableRecord, Decodable` with all columns; period_from/period_to as Double
- [x] Create `WatchItem.swift`: `FetchableRecord, Decodable` with entity_type, entity_id, entity_name, priority, created_at
- [x] Create `Decision.swift`: `Codable` struct (text, by, messageTS) for JSON decoding from digest.decisions field
- [x] Create `ActionItem.swift`: `Codable` struct (text, assignee, status) for JSON decoding from digest.action_items field
- [ ] Smoke test: open real DB, read workspace name, count channels/messages/users

**Also created:** `SyncState.swift` model (FetchableRecord, tracks per-channel sync progress)

**Files:** `Database/DatabaseManager.swift`, `Models/*.swift`

### Task 3: Query layer
- [x] Create `WorkspaceQueries.swift`: `fetchWorkspace()`, `fetchStats()` (channel/user/message/digest counts + DB file size)
- [x] Create `ChannelQueries.swift`: `fetchAll(filter:sort:)` with type/archived/member filters and name/messages/recent sort; `fetchByID(_:)`, `fetchByName(_:)`, `fetchWatched()` joining watch_list
- [x] Create `MessageQueries.swift`: `fetchByChannel(_:limit:offset:)` paginated; `fetchByTimeRange(channelID:from:to:)`; `fetchThreadReplies(channelID:threadTS:)`; `fetchRecentWatched()` with MessageWithContext
- [x] Create `UserQueries.swift`: `fetchAll(activeOnly:)`, `fetchByID(_:)`, `fetchDisplayName(forID:)`
- [x] Create `DigestQueries.swift`: `fetchAll(type:channelID:from:to:limit:)` filtered; `fetchByID(_:)`, `fetchLatest(type:)`, `fetchWithDecisions()`, `fetchNewSince()`, `maxID()`
- [x] Create `WatchQueries.swift`: `fetchAll()`, `isWatched()`, `add()` INSERT OR REPLACE, `remove()` DELETE
- [x] Create `SearchQueries.swift`: `search(query:limit:)` via FTS5 MATCH with `snippet()`, FTS5 query sanitization (H5 fix)
- [ ] Unit tests: create test fixture DB, verify each query returns correct types and handles empty results

**Also created:** `StatsQueries.swift` (fetchDashboardStats, fetchTopChannels, fetchMessageVolume, fetchSyncSummary)

**Files:** `Database/Queries/*.swift`

### Task 4: App shell and sidebar navigation
- [x] Create `WatchtowerApp.swift`: @main, WindowGroup, initialize DatabaseManager on launch, handle DB-not-found error state
- [x] Create `AppState.swift`: @Observable class holding DatabaseManager, selected sidebar item enum, error state
- [x] Create `Navigation.swift`: enum `SidebarDestination` with cases: dashboard, chat, digests, channels, search
- [x] Create `SidebarView.swift`: NavigationSplitView with List of sidebar items (SF Symbols icons), selection binding to AppState
- [x] Create placeholder views for each destination (DashboardView, ChatView, etc.) with title text
- [x] Handle error state: if DB not found, show onboarding view "Run `watchtower auth login && watchtower sync` to get started"
- [ ] Set default window size (1200x800), minimum size (800x600)

**Also created:** `NavigationRoot` and `MainNavigationView` for top-level routing, `OnboardingView` for first-launch state

**Files:** `App/WatchtowerApp.swift`, `App/AppState.swift`, `App/Navigation.swift`, `Views/Sidebar/SidebarView.swift`

### Task 5: Dashboard — stats and sync status
- [x] Create `DashboardViewModel.swift`: @Observable, loads stats (channel/user/message/digest counts, DB size), last sync time, daemon status
- [x] Create `StatsCard.swift`: reusable view component — SF Symbol icon, label, formatted value (humanized numbers)
- [x] Create `DashboardView.swift`: LazyVGrid of StatsCards (2 columns), sync status section below
- [x] Create `SyncStatusBanner.swift`: green/red circle indicator, "Last sync: X minutes ago" relative text, "Daemon running/stopped"
- [x] Wire DashboardViewModel to DatabaseManager queries
- [ ] Add pull-to-refresh or manual refresh button

**Files:** `ViewModels/DashboardViewModel.swift`, `Views/Dashboard/*.swift`

### Task 6: Dashboard — activity feed
- [x] Create `ActivityFeed.swift`: list of recent messages from watched channels (last 24h)
- [x] Query: JOIN messages + channels + watch_list, filter ts_unix > now-24h, ORDER BY ts_unix DESC, LIMIT 50
- [x] Group messages by channel name with section headers
- [x] Each row: user display name, relative timestamp ("2h ago"), truncated text preview (2 lines max)
- [ ] Tap message row → navigate to channel detail at that message
- [ ] Empty state: "No watched channels. Add channels to your watch list to see activity here."
- [ ] Add "Quick actions" row: buttons for "Ask Claude", "Catchup", "View Digests"

**Files:** `Views/Dashboard/ActivityFeed.swift`

### Task 7: Channel list view
- [x] Create `ChannelViewModel.swift`: @Observable, fetches channels with stats (message count, last activity), manages filters/sort
- [x] Create `ChannelListView.swift`: List with search field (local filter by name)
- [x] Add type filter: segmented control or menu (All / Public / Private / DMs)
- [x] Each channel row: # icon (colored by type), name, star icon if watched
- [x] Archived channels shown dimmed (opacity 0.6)
- [x] Tap → navigate to ChannelDetailView
- [ ] Sort picker (name/messages/recent)
- [ ] Show total channel count in section header
- [ ] Message count badge, last activity relative time per row

**Files:** `ViewModels/ChannelViewModel.swift`, `Views/Channels/ChannelListView.swift`

### Task 8: Channel detail — message list and header
- [x] Create `ChannelDetailView.swift`: header + scrollable message list
- [x] Watch toggle: star button in header, calls WatchQueries.add/remove, updates UI immediately
- [x] Message list: load 50 messages initially, "Load more" button at top for pagination
- [x] Create `MessageRow.swift`: user display name (resolved from users table), relative timestamp, message text, reply count badge ("N replies")
- [x] Tap reply count → open ThreadView (sheet)
- [ ] Channel header: type badge, topic (if set), member count, purpose (collapsible)
- [ ] Messages sorted by ts_unix ascending (oldest first, natural chat order)
- [ ] Auto-scroll to bottom on first load

**Files:** `Views/Channels/ChannelDetailView.swift`, `Views/Channels/MessageRow.swift`

### Task 9: Slack mrkdwn parser
- [x] Create `SlackTextParser.swift`: converts Slack mrkdwn text → plain text *(not AttributedString)*
- [x] Parse links: `<https://url|display text>` → display text; `<https://url>` → url as text
- [x] Parse user mentions: `<@U123ABC>` → "@U123ABC" *(no DB resolution, plain text only)*
- [x] Parse channel mentions: `<#C123ABC|channel-name>` → "#channel-name"
- [ ] Parse bold: `*text*` → bold weight
- [ ] Parse italic: `_text_` → italic
- [ ] Parse strikethrough: `~text~` → strikethrough
- [ ] Parse inline code: `` `code` `` → monospace font, background tint
- [ ] Parse code blocks: ` ```code``` ` → monospace block with background
- [ ] Handle emoji shortcodes: `:emoji_name:` → show as text (or map common ones to Unicode)
- [ ] Unit tests: table-driven tests for each pattern

**Note:** Current implementation is plain text conversion only. Full AttributedString rendering not yet implemented.

**Files:** `Utilities/SlackTextParser.swift`

### Task 10: Thread view
- [x] Create `ThreadView.swift`: sheet panel showing thread replies
- [x] Header: channel name, thread info
- [x] Reply list: same MessageRow component, sorted ascending by ts
- [x] Query: fetchThreadReplies(channelID:threadTS:) from MessageQueries
- [x] Close button (sheet dismiss)
- [ ] Show "No replies" empty state if reply_count > 0 but replies not synced yet

**Files:** `Views/Channels/ThreadView.swift`

### Task 11: Claude CLI service — subprocess management
- [x] Create `ClaudeService.swift` with protocol `ClaudeServiceProtocol` for testability
- [x] Implement `stream(prompt:systemPrompt:sessionID:) -> AsyncThrowingStream<StreamEvent, Error>`
- [x] `StreamEvent` enum: `.text(String)`, `.sessionID(String)`, `.done`
- [x] Build CLI args: `-p`, `--output-format stream-json`, `--model`, `--system-prompt` or `--resume`, `--allowedTools`, `--disallowedTools`, `--mcp-config`
- [x] Launch `Process` with stdout Pipe, stderr redirected to /dev/null (H4 fix)
- [x] Read stdout line-by-line, parse JSONL
- [x] Parse each line as JSON: extract text from `assistant` events, session_id from `result` events
- [x] Implement graceful shutdown: ProcessHandle with NSLock for thread-safe termination (H3 fix)
- [x] Error classification: `claude` not found → user-friendly message; exit code → parse stderr
- [x] Build MCP config JSON via JSONSerialization to prevent injection (C1 fix)
- [x] Find `claude` binary in PATH or known locations
- [ ] Allow 1MB max line length for large responses
- [ ] Unit tests: mock Process via protocol, test JSONL parsing, test error classification

**Files:** `Services/ClaudeService.swift`

### Task 12: AI Chat — view model and context building
- [x] Create `ChatViewModel.swift`: @Observable, manages conversation state
- [x] Define `ChatMessage` struct: id (UUID), role (user/assistant), text (String), timestamp (Date), isStreaming (Bool)
- [x] `send(text:)`: append user message, start async task calling ClaudeService.stream(), append assistant message with progressive text updates
- [x] Track sessionID: nil on first message, captured from first `result` event, passed to subsequent messages via `--resume`
- [x] Build system prompt from DB: workspace name, domain, channel/user/message counts, last sync time, latest digest summaries (nonisolated static method, H2 fix)
- [x] Handle cancel: user can stop streaming mid-response
- [x] Handle errors: show error as system message in chat
- [x] `newChat()`: clear messages, reset sessionID
- [ ] Persist chat sessions: store in UserDefaults or local JSON file

**Files:** `ViewModels/ChatViewModel.swift`

### Task 13: AI Chat — UI
- [x] Create `ChatView.swift`: VStack with message list (ScrollView) + input area at bottom
- [x] Auto-scroll to latest message on new message or streaming update
- [x] Create `MessageBubble.swift`: user messages right-aligned (blue bg), assistant messages left-aligned (gray bg)
- [x] Assistant bubbles render content via `MarkdownText` component
- [x] Create `MarkdownText.swift`: `AttributedString(markdown:)` for rendering *(uses Foundation, not swift-markdown)*
- [x] Create `ChatInput.swift`: TextEditor with placeholder, Cmd+Return sends, disabled while streaming
- [x] Create `StreamingIndicator.swift`: animated three dots shown while waiting for first token
- [x] Add quick prompt chips above input: "What happened today?", "Any decisions?", "Summarize activity" — shown only when chat is empty
- [x] New chat button in toolbar

**Files:** `Views/Chat/*.swift`

### Task 14: Digest list view
- [x] Create `DigestViewModel.swift`: @Observable, fetches digests with filters, parses JSON fields; caches channel names (M9 fix)
- [x] Create `DigestListView.swift`: list of digest cards
- [x] Digest card: type icon (number=channel, calendar=daily, chart=weekly), channel name or "Cross-channel", decision count badge (orange)
- [x] Tap card → navigate to DigestDetailView
- [x] Sort by created_at DESC (newest first)
- [x] Type filter picker (all/channel/daily/weekly)
- [ ] DatePicker for date range filter
- [ ] Channel filter
- [ ] Empty state message

**Files:** `ViewModels/DigestViewModel.swift`, `Views/Digests/DigestListView.swift`

### Task 15: Digest detail view
- [x] Create `DigestDetailView.swift`: full digest content in scrollable view
- [x] Header: type badge, date range, channel name, created_at
- [x] Summary section: full summary text (GroupBox)
- [x] Topics section: FlowLayout with capsule-styled tag chips
- [x] Create `DecisionCard.swift`: highlighted card — decision text, orange left border, "by" attribution
- [x] Action items section: list with text, assignee, status (checkmark if done)
- [x] Stats footer: model used, input/output tokens, cost in USD
- [ ] Navigate from decision/action item → ChannelDetailView at specific message

**Files:** `Views/Digests/DigestDetailView.swift`, `Views/Digests/DecisionCard.swift`

### Task 16: Full-text search
- [x] Create `SearchViewModel.swift`: @Observable, debounced query input (300ms via Task.sleep), FTS5 search
- [x] Create `SearchView.swift`: search field, results below
- [x] Create `SearchResultRow.swift`: channel name, user name, snippet (HTML tags stripped), relative timestamp
- [x] Show "No results for ..." / "Type to search" empty states
- [x] Limit results to 100, show footer note if truncated
- [x] FTS5 query sanitization to prevent injection (H5 fix)
- [ ] Wire Cmd+K keyboard shortcut to focus search field
- [ ] Snippet match highlighting (AttributedString from `<mark>` tags)
- [ ] Results grouped by channel with section headers
- [ ] Tap result → navigate to ChannelDetailView scrolled to that message
- [ ] Filter chips: channel picker, user picker, date range

**Files:** `ViewModels/SearchViewModel.swift`, `Views/Search/*.swift`

### Task 17: Daemon management service
- [x] Create `DaemonManager.swift`: @Observable, manages Go daemon lifecycle
- [x] `isDaemonRunning() -> Bool`: read PID file, parse PID, check process existence
- [x] `startDaemon()`: async, run Process off main thread (C4 fix)
- [x] `stopDaemon()`: async, run Process off main thread (C4 fix)
- [x] Background polling: check daemon status every 10 seconds
- [x] Detect `watchtower` binary: resolvePathIfNeeded() deferred to avoid blocking init
- [x] Error handling: binary not found, already running, not running
- [ ] `lastSyncTime: Date?`: read from workspace.synced_at, updated via DB observation

**Files:** `Services/DaemonManager.swift`

### Task 18: Sync status integration in UI
- [x] Update `SyncStatusBanner.swift`: bind to DaemonManager — status indicator when running/stopped
- [x] Add start/stop toggle button in banner
- [x] Show last sync time as relative
- [x] Create `DaemonSettings.swift`: daemon control panel in Settings — start/stop, binary path, error messages
- [x] Show "watchtower not found" error state if binary missing
- [ ] Add "Sync Now" button: runs `watchtower sync` (one-shot, not daemon) and shows progress

**Files:** `Views/Dashboard/SyncStatusBanner.swift`, `Views/Settings/DaemonSettings.swift`

### Task 19: Live database observation
- [x] Create `DatabaseObserver.swift`: centralized GRDB ValueObservation manager
- [x] `observeWorkspace()` → Publisher<Workspace?, Error>
- [x] `observeMessages(channelID:limit:)` → Publisher<[Message], Error>
- [x] `observeDigests(type:limit:)` → Publisher<[Digest], Error>
- [x] `observeStats()` → Publisher<WorkspaceStats, Error>
- [x] `observeWatchList()` → Publisher<[WatchItem], Error>
- [x] Use GRDB `ValueObservation.tracking { db in ... }`
- [ ] Add `.removeDuplicates()` to avoid redundant updates
- [ ] Add debounce (0.5s) for high-frequency writes during sync
- [ ] Wire into ViewModels for automatic refresh
- [ ] Verify: start daemon → new messages appear without manual refresh

**Note:** Implemented as Combine Publishers, not AsyncStreams. Observation infrastructure is built but not yet wired into ViewModels.

**Files:** `Database/DatabaseObserver.swift`

### Task 20: Notification service
- [x] Create `NotificationService.swift`: wraps `UNUserNotificationCenter`
- [x] `requestPermission()`: call on first launch, handle denied gracefully
- [x] `sendDecisionNotification(decision:channelName:digestID:)`: stable identifier using digestID + text hash (M7 fix)
- [x] `sendDailySummaryNotification(digestSummary:)`: "Daily summary ready" with preview
- [ ] Handle notification tap: implement `UNUserNotificationCenterDelegate`, deep-link to DigestDetailView
- [ ] Badge: show unread decision count on app icon

**Files:** `Services/NotificationService.swift`

### Task 21: Digest watcher — notification trigger
- [x] Create `DigestWatcher.swift`: background service polling for new digests with decisions
- [x] Store `lastCheckedDigestID` in UserDefaults, initialized to max(digests.id) on first run
- [x] Poll every 60 seconds
- [x] For each new digest: parse decisions JSON, call NotificationService for each decision
- [x] Update `lastCheckedDigestID` after processing
- [x] Start watcher on app launch
- [x] Rate limit: max 5 notifications per poll cycle
- [x] Log poll errors instead of silently swallowing (M18 fix)
- [ ] Unit tests: mock DB returns new digests → verify notification calls

**Files:** `Services/DigestWatcher.swift`

### Task 22: Settings view
- [x] Create `SettingsView.swift`: macOS Settings scene with TabView (General, Notifications, Daemon)
- [x] Create `ConfigService.swift`: read `~/.config/watchtower/config.yaml` via Yams, expose workspace name, AI model, sync interval, digest config
- [x] Surface YAML parse errors instead of silently swallowing (M13 fix)
- [x] General tab: workspace name, digest model, enabled status
- [x] Create `NotificationSettings.swift`: toggles for decision notifications, daily summary notifications, quiet hours
- [x] Store notification preferences in @AppStorage (UserDefaults)
- [ ] Watch config file for changes via DispatchSource
- [ ] About tab: app version, CLI version, links to docs/GitHub

**Files:** `Services/ConfigService.swift`, `Views/Settings/*.swift`

### Task 23: Time formatting and constants
- [x] Create `TimeFormatting.swift`: `relativeTime(from:)` → "just now", "5m ago", "2h ago", etc.
- [x] `formatTimestamp(_ isoString: String) -> String` for ISO8601 → display format
- [x] `formatUnixTimestamp(_ ts: Double) -> String` for ts_unix → display format
- [x] Static DateFormatter reuse (M4 optimization)
- [x] Create `Constants.swift`: default config path, default DB base path, notification category identifiers

**Files:** `Utilities/TimeFormatting.swift`, `Utilities/Constants.swift`

### Task 24: Keyboard shortcuts and window management
- [ ] Register `Cmd+K` → focus search
- [ ] Register `Cmd+N` → new chat session
- [ ] Register `Cmd+1` through `Cmd+5` → switch sidebar sections
- [ ] Register `Cmd+R` → refresh current view
- [ ] Remember window frame
- [ ] Set minimum window size 800x600, default 1200x800
- [ ] Support multiple windows

**Status:** Not started

**Files:** updates to `App/WatchtowerApp.swift`, `Views/Sidebar/SidebarView.swift`

### Task 25: Empty states and error handling
- [x] First-launch state: DB not found → OnboardingView with setup instructions
- [x] Reusable `ErrorView(title:message:action:)` component
- [ ] No messages state: channel has 0 messages → "No messages synced for this channel"
- [ ] No digests state: "No digests generated yet" with button
- [x] Daemon not running: SyncStatusBanner shows status with Start button
- [ ] Claude not found: chat shows "Claude CLI not installed" with link
- [ ] DB locked error: "Database is busy..." with retry

**Files:** `Views/Common/ErrorView.swift`, various views

### Task 26: Dark mode, app icon, and visual polish
- [x] SwiftUI handles light/dark mode automatically
- [x] App icon: Assets.xcassets bundle created
- [x] Message bubbles: rounded corners, role-based styling (user blue, assistant gray)
- [x] Decision cards: orange left border, background highlight
- [x] Streaming indicator: animated three dots
- [ ] Custom accent color: watchtower blue/teal
- [ ] Stats cards: SF Symbol colors, subtle gradients or tints
- [ ] Code blocks in Markdown: monospace font, dark background, rounded corners
- [ ] Loading states: ProgressView spinners
- [ ] Animations: sidebar selection, sheet presentation

**Files:** `Resources/Assets.xcassets`, visual updates across `Views/`

### Task 27: Performance testing and optimization
- [ ] Test with large DB: 100K+ messages, 500+ channels, 1000+ users
- [ ] Profile message list scrolling
- [ ] Profile FTS5 search
- [ ] Verify concurrent DB access
- [ ] Memory profiling
- [ ] Optimize query patterns
- [ ] Add index hints if needed

**Status:** Not started

**Files:** `Tests/` performance tests

---

## Testing Strategy

### Unit Tests
- **Database queries:** Use a copy of real watchtower.db (or fixture DB) to verify all GRDB queries return expected types and handle edge cases
- **SlackTextParser:** Table-driven tests for each mrkdwn pattern
- **ClaudeService:** Mock Process via protocol, verify JSONL parsing, session ID extraction, error handling
- **DigestWatcher:** Mock DatabaseManager, verify it detects new digests and fires notifications
- **ViewModels:** Test with mock services, verify state transitions

### Integration Tests
- **DB observation:** Write to test DB → verify ValueObservation fires → ViewModel updates
- **Full chat flow:** Mock claude process → send message → verify streaming updates ViewModel

### Manual Testing
- Large DB performance (100K+ messages)
- Concurrent access: CLI daemon writing while app reads
- Notification delivery and tap handling
- Dark/light mode transitions

---

## Open Questions

1. **Chat history persistence:** Store in UserDefaults, separate SQLite, or add a table to watchtower.db?
   - Recommendation: separate SQLite in app container to avoid polluting the shared DB
2. **Menu bar icon:** Essential for MVP or defer to v2?
   - Recommendation: defer, focus on main window
3. **App distribution:** Direct download, Homebrew cask, or Mac App Store?
   - Recommendation: start with direct download + Homebrew cask, App Store later (sandboxing constraints)
4. **Minimum macOS version:** 14 (Sonoma) drops support for older machines.
   - Recommendation: 14+ is fine, @Observable requires it

---

## Prerequisites

Before the app can be used, the user must have:
1. `watchtower` Go binary installed and in PATH
2. `claude` CLI installed and authenticated
3. Initial sync completed (`watchtower auth login && watchtower sync`)
4. macOS 14.0 or later
