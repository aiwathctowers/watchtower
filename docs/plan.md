# Watchtower — Implementation Plan

## 1. Project Overview

Watchtower is an open-source Go CLI tool that syncs a Slack workspace into a local SQLite database and provides an AI-powered interface for analysis via the Claude API.

**Gap it fills:** No existing tool combines local Slack DB + AI analysis + CLI interface + read-only mode. Current alternatives are either pure dumpers (Slackdump), enterprise SaaS (Glean), or in-Slack bots.

### High-Level Architecture

```
[Slack API] --(sync)--> [SQLite DB] --(query)--> [Claude API] --> [Terminal Output]
                              ^                        |
                              |                        v
                         [FTS5 Search]          [Links to Slack]
```

### Tech Stack

| Component       | Library                          | Reason                                |
|-----------------|----------------------------------|---------------------------------------|
| Language        | Go 1.25                          | Single binary, cross-platform         |
| CLI framework   | cobra + viper                    | Industry standard for Go CLIs         |
| SQLite          | modernc.org/sqlite               | Pure Go, zero CGO dependency          |
| Slack client    | github.com/slack-go/slack        | Mature, well-maintained               |
| Claude API      | github.com/anthropics/anthropic-sdk-go | Official SDK                    |
| TUI / REPL      | bubbletea + lipgloss + glamour   | Rich terminal UI                      |
| Rate limiting   | golang.org/x/time/rate           | Token bucket implementation           |

---

## 2. CLI Commands — Full Specification

### 2.1 `watchtower` (no subcommand) — REPL Mode

Launches an interactive session powered by bubbletea. User can type natural-language queries, and the system responds with AI-generated analysis. Special commands inside REPL:

- `/sync` — trigger incremental sync
- `/status` — show DB status
- `/catchup` — equivalent to `watchtower catchup`
- `/quit` or `/exit` — exit REPL
- `/help` — list available commands

### 2.2 `watchtower ask "<question>"`

One-shot AI query. Runs the full AI pipeline (parse → context → prompt → Claude → render) and exits.

**Flags:**
- `--model <model-id>` — override AI model
- `--no-stream` — disable streaming output
- `--channel <name>` — scope to specific channel(s)
- `--since <duration>` — time range filter

### 2.3 `watchtower catchup`

"What happened since my last check?" Uses `user_checkpoints` table to determine the time window.

**Flags:**
- `--since <duration>` — override: use last N hours/days instead of checkpoint (e.g., `8h`, `2d`)
- `--watched-only` — only summarize watch list channels/users
- `--channel <name>` — scope to specific channel

**Behavior:**
1. Reads `user_checkpoints.last_checked_at`
2. Queries messages with `ts_unix > last_checked_at`
3. Groups by channel, sorted by priority (high watch → normal watch → rest)
4. Builds AI context and asks Claude for a structured summary
5. Updates `user_checkpoints.last_checked_at` to now

### 2.4 `watchtower sync`

Runs incremental sync (only new messages since last sync per channel).

**Flags:**
- `--full` — full re-sync (ignores sync_state, re-fetches everything within `initial_history_days`)
- `--daemon` — run in foreground as a polling daemon with wake detection
- `--channels <names>` — sync only specific channels
- `--workers <n>` — override worker pool size (default: 5)

**Phases (executed sequentially):**
1. Metadata sync (workspace info, users list, channels list)
2. Message sync (parallel workers across channels)
3. Thread sync (parallel workers for messages with replies)

### 2.5 `watchtower status`

Prints database statistics and sync health.

**Output example:**
```
Workspace: my-company (T024BE7LD)
Database:  ~/.local/share/watchtower/my-company/watchtower.db (1.2 GB)
Last sync: 2025-02-24 14:30:00 (25 minutes ago)

Channels:  342 (12 watched)
Users:     1,205
Messages:  2,451,230
Threads:   89,120

Sync coverage:
  #engineering    100%  (synced 2m ago)
  #general        100%  (synced 2m ago)
  #random          85%  (oldest: 2024-06-01)
```

### 2.6 `watchtower config init`

Interactive wizard that:
1. Asks for Slack token (or shows how to create a Slack app)
2. Asks for Anthropic API key
3. Tests both connections
4. Writes `~/.config/watchtower/config.yaml`
5. Creates DB directory

### 2.7 `watchtower config set <key> <value>`

Sets a configuration value. Dot-notation for nested keys: `watchtower config set ai.model claude-opus-4-20250918`

### 2.8 `watchtower watch add|remove|list`

Manages the watch list (high-priority channels/users for catchup).

```
watchtower watch add #engineering --priority high
watchtower watch add @alice.smith --priority normal
watchtower watch remove #random
watchtower watch list
```

### 2.9 `watchtower channels`

Lists all synced channels with stats.

**Flags:**
- `--type <public|private|dm|group_dm>` — filter by type
- `--sort <messages|name|recent>` — sort order

### 2.10 `watchtower users`

Lists all synced users.

**Flags:**
- `--active` — only non-deleted, non-bot users

---

## 3. Database Schema — Complete DDL

### 3.1 File Location

```
~/.local/share/watchtower/<workspace-name>/watchtower.db
```

The `<workspace-name>` is the Slack workspace domain (e.g., `my-company`). This allows multiple workspaces.

### 3.2 Initialization

On first open:
```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
```

WAL mode enables concurrent reads during sync writes — critical for querying while syncing.

### 3.3 Table: `workspace`

```sql
CREATE TABLE IF NOT EXISTS workspace (
    id         TEXT PRIMARY KEY,   -- Slack team_id (T024BE7LD)
    name       TEXT NOT NULL,
    domain     TEXT NOT NULL,
    synced_at  DATETIME
);
```

### 3.4 Table: `users`

```sql
CREATE TABLE IF NOT EXISTS users (
    id           TEXT PRIMARY KEY,    -- Slack user_id (U024BE7LH)
    name         TEXT NOT NULL,       -- username (handle)
    display_name TEXT NOT NULL DEFAULT '',
    real_name    TEXT NOT NULL DEFAULT '',
    email        TEXT,
    is_bot       BOOLEAN NOT NULL DEFAULT 0,
    is_deleted   BOOLEAN NOT NULL DEFAULT 0,
    profile_json TEXT,                -- full profile as JSON for future use
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_name ON users(name);
CREATE INDEX IF NOT EXISTS idx_users_is_bot ON users(is_bot);
```

### 3.5 Table: `channels`

```sql
CREATE TABLE IF NOT EXISTS channels (
    id          TEXT PRIMARY KEY,    -- Slack channel_id (C024BE7LR)
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK(type IN ('public', 'private', 'dm', 'group_dm')),
    topic       TEXT NOT NULL DEFAULT '',
    purpose     TEXT NOT NULL DEFAULT '',
    is_archived BOOLEAN NOT NULL DEFAULT 0,
    is_member   BOOLEAN NOT NULL DEFAULT 0,
    dm_user_id  TEXT,                -- for DMs: the other user's ID
    num_members INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(type);
CREATE INDEX IF NOT EXISTS idx_channels_is_archived ON channels(is_archived);
```

### 3.6 Table: `messages`

```sql
CREATE TABLE IF NOT EXISTS messages (
    channel_id  TEXT NOT NULL,
    ts          TEXT NOT NULL,        -- Slack message timestamp (unique ID)
    user_id     TEXT,
    text        TEXT NOT NULL DEFAULT '',
    thread_ts   TEXT,                 -- parent message ts (NULL if not in thread)
    reply_count INTEGER NOT NULL DEFAULT 0,
    is_edited   BOOLEAN NOT NULL DEFAULT 0,
    is_deleted  BOOLEAN NOT NULL DEFAULT 0,
    subtype     TEXT,                 -- 'bot_message', 'channel_join', etc.
    permalink   TEXT,
    ts_unix     REAL GENERATED ALWAYS AS (CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL) + CAST(SUBSTR(ts, INSTR(ts, '.')) AS REAL)) STORED,
    raw_json    TEXT,                 -- original Slack API JSON for edge cases
    PRIMARY KEY (channel_id, ts)
);

CREATE INDEX IF NOT EXISTS idx_messages_user ON messages(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(channel_id, thread_ts) WHERE thread_ts IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_ts_unix ON messages(ts_unix);
CREATE INDEX IF NOT EXISTS idx_messages_channel_ts_unix ON messages(channel_id, ts_unix);
```

**Key decisions:**
- `ts` as TEXT: Slack timestamps are unique message IDs (e.g., `1234567890.123456`), not just timestamps
- Composite PK `(channel_id, ts)`: matches Slack's uniqueness guarantee
- `ts_unix` as generated column: enables efficient time-range queries without parsing
- `is_deleted` for soft deletes: we never physically remove messages
- `raw_json` optional: stored only when message has complex attachments/blocks

### 3.7 Table: `messages_fts` (FTS5 Virtual Table)

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    text,
    channel_id UNINDEXED,
    ts UNINDEXED,
    user_id UNINDEXED,
    tokenize = 'porter unicode61'
);

-- Auto-sync triggers
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    VALUES (NEW.text, NEW.channel_id, NEW.ts, NEW.user_id);
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF text ON messages BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    VALUES (NEW.text, NEW.channel_id, NEW.ts, NEW.user_id);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
END;
```

**Tokenizer choice:** `porter unicode61`
- `porter`: English stemming — "deployment" matches "deployed", "deploying"
- `unicode61`: proper Unicode handling for international text

### 3.8 Table: `reactions`

```sql
CREATE TABLE IF NOT EXISTS reactions (
    channel_id TEXT NOT NULL,
    message_ts TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    emoji      TEXT NOT NULL,
    PRIMARY KEY (channel_id, message_ts, user_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_reactions_message ON reactions(channel_id, message_ts);
```

### 3.9 Table: `files`

```sql
CREATE TABLE IF NOT EXISTS files (
    id        TEXT PRIMARY KEY,
    message_channel_id TEXT,
    message_ts TEXT,
    name      TEXT NOT NULL DEFAULT '',
    mimetype  TEXT NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    permalink TEXT
);

CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_channel_id, message_ts);
```

Only metadata is stored — file contents are NOT downloaded.

### 3.10 Table: `sync_state`

```sql
CREATE TABLE IF NOT EXISTS sync_state (
    channel_id              TEXT PRIMARY KEY,
    last_synced_ts          TEXT,     -- newest message ts we've seen
    oldest_synced_ts        TEXT,     -- oldest message ts we've fetched
    is_initial_sync_complete BOOLEAN NOT NULL DEFAULT 0,
    cursor                  TEXT,     -- pagination cursor for resumable sync
    messages_synced         INTEGER NOT NULL DEFAULT 0,
    last_sync_at            DATETIME,
    error                   TEXT      -- last error if sync failed
);
```

### 3.11 Table: `watch_list`

```sql
CREATE TABLE IF NOT EXISTS watch_list (
    entity_type TEXT NOT NULL CHECK(entity_type IN ('channel', 'user')),
    entity_id   TEXT NOT NULL,
    entity_name TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('high', 'normal', 'low')),
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (entity_type, entity_id)
);
```

### 3.12 Table: `user_checkpoints`

```sql
CREATE TABLE IF NOT EXISTS user_checkpoints (
    id             INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),  -- singleton row
    last_checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

The `CHECK(id = 1)` constraint ensures only one row exists — this is a singleton table for the local user's last catchup timestamp.

### 3.13 Schema Migrations

Schema version is tracked via SQLite `user_version` pragma:

```sql
PRAGMA user_version;        -- read current version
PRAGMA user_version = 1;    -- set after migration
```

Migrations are embedded via `go:embed` and run sequentially on startup. Each migration is idempotent (uses `IF NOT EXISTS`).

---

## 4. Sync Engine — Detailed Design

### 4.1 Orchestrator (`internal/sync/orchestrator.go`)

The orchestrator coordinates the entire sync process:

```go
type Orchestrator struct {
    db          *db.DB
    slackClient *slack.Client
    config      *config.Config
    progress    *Progress
}

func (o *Orchestrator) Run(ctx context.Context, opts SyncOptions) error {
    // Phase 1: Metadata
    if err := o.syncMetadata(ctx); err != nil {
        return fmt.Errorf("metadata sync: %w", err)
    }

    // Phase 2: Messages
    if err := o.syncMessages(ctx); err != nil {
        return fmt.Errorf("message sync: %w", err)
    }

    // Phase 3: Threads
    if err := o.syncThreads(ctx); err != nil {
        return fmt.Errorf("thread sync: %w", err)
    }

    return nil
}
```

**SyncOptions:**
```go
type SyncOptions struct {
    Full       bool     // ignore sync_state, re-fetch everything
    Channels   []string // sync only these channels (empty = all)
    Workers    int      // override config worker count
    DaemonMode bool     // run in polling loop
}
```

### 4.2 Metadata Sync

**Step 1: Workspace info**
```
GET team.info → UPSERT workspace table
```

**Step 2: Users**
```
GET users.list (paginated, Tier 2: 20/min)
→ For each user: UPSERT users table
→ Detect deleted users (present in DB but absent from API → set is_deleted = 1)
```

**Step 3: Channels**
```
GET conversations.list (types=public_channel,private_channel,im,mpim, paginated, Tier 2: 20/min)
→ For each channel: UPSERT channels table
→ Set is_member based on API response
→ Detect archived channels
```

### 4.3 Message Sync (`internal/sync/message_sync.go`)

**Channel prioritization order:**
1. Watch list channels with priority "high"
2. Watch list channels with priority "normal"
3. Channels where `is_member = true` (sorted by most recent activity)
4. All other non-archived channels

**Per-channel sync logic:**
```
func syncChannel(ctx context.Context, channel Channel, state SyncState) error {
    params := slack.GetConversationHistoryParameters{
        ChannelID: channel.ID,
        Limit:     200,  // max per request
    }

    if !opts.Full && state.LastSyncedTS != "" {
        params.Oldest = state.LastSyncedTS
        // Add small epsilon to avoid re-fetching the boundary message
    }

    if opts.Full || !state.IsInitialSyncComplete {
        // Limit to initial_history_days for first sync
        cutoff := time.Now().AddDate(0, 0, -config.Sync.InitialHistoryDays)
        params.Oldest = fmt.Sprintf("%d.000000", cutoff.Unix())
    }

    for {
        resp, err := slackClient.GetConversationHistory(params)
        // ... handle rate limits, errors

        for _, msg := range resp.Messages {
            db.UpsertMessage(channel.ID, msg)
        }

        // Update sync_state after each page (resumable!)
        db.UpdateSyncState(channel.ID, lastTS)

        if !resp.HasMore {
            break
        }
        params.Cursor = resp.ResponseMetaData.NextCursor
    }

    db.MarkInitialSyncComplete(channel.ID)
}
```

**Key behaviors:**
- Messages are written to DB after each API page (not batched) → sync is resumable
- `sync_state.cursor` is saved → if interrupted mid-pagination, we resume from last cursor
- `sync_state.last_synced_ts` is updated progressively

### 4.4 Thread Sync (`internal/sync/thread_sync.go`)

After message sync, we identify messages that need thread sync:

```sql
SELECT channel_id, ts FROM messages
WHERE reply_count > 0
AND ts NOT IN (
    SELECT DISTINCT thread_ts FROM messages WHERE thread_ts IS NOT NULL
    GROUP BY channel_id, thread_ts
    HAVING COUNT(*) >= (
        SELECT reply_count FROM messages m2
        WHERE m2.channel_id = messages.channel_id AND m2.ts = messages.thread_ts
    )
)
```

Simplified: find parent messages where we don't have all replies yet.

For each such thread:
```
GET conversations.replies(channel, thread_ts)
→ UPSERT all reply messages into messages table (with thread_ts set)
```

### 4.5 Worker Pool (`internal/sync/worker.go`)

```go
type WorkerPool struct {
    workers   int
    tasksCh   chan SyncTask
    wg        sync.WaitGroup
    errCh     chan error
}

type SyncTask struct {
    Type      string  // "channel" or "thread"
    ChannelID string
    ThreadTS  string  // only for thread tasks
    Priority  int     // for ordering
}
```

Workers pull tasks from a priority channel. The pool size is configurable (default: 5).

### 4.6 Rate Limiting (`internal/slack/ratelimit.go`)

Slack API rate limits are **per-method-family**, not global:

| Tier | Limit      | Methods                                              |
|------|------------|------------------------------------------------------|
| 1    | 1/min      | (not used by us)                                     |
| 2    | 20/min     | users.list, conversations.list, team.info            |
| 3    | 50/min     | conversations.history, conversations.replies          |
| 4    | 100/min    | (not used by us)                                     |

**Implementation:**
```go
type RateLimiter struct {
    limiters map[string]*rate.Limiter  // keyed by tier
    mu       sync.Mutex
    backoff  map[string]time.Time      // per-method backoff from 429s
}
```

- `conversations.history` and `conversations.replies` are both Tier 3 but have **separate budgets** → we can parallelize history + replies
- On HTTP 429: read `Retry-After` header, add random jitter (0-1s), block ALL workers calling that method family
- Pre-emptive: `rate.Limiter` with `rate.Every(60s/limit)` prevents hitting 429 in the first place

### 4.7 Progress Reporting (`internal/sync/progress.go`)

During sync, progress is reported to the terminal:

```
Syncing my-company workspace...
  Metadata: users 1205/1205, channels 342/342          ✓
  Messages: [████████████████████░░░░░░░] 78%  (267/342 channels)
    #engineering  ████████████████████ 1,234 msgs  ✓
    #general      ███████████████░░░░░  892 msgs  (fetching...)
  Threads:  [░░░░░░░░░░░░░░░░░░░░░░░░░░] waiting...
```

Uses lipgloss for styling. Updates in-place using terminal escape codes.

### 4.8 Sync Estimates

For a workspace with ~500 channels, ~2 years of history:

| Phase        | API Calls | Time (Tier 3, 50/min) | Notes                          |
|--------------|-----------|----------------------|--------------------------------|
| Metadata     | ~50       | ~2.5 min             | Paginated users + channels      |
| Messages     | ~3,000    | ~60 min              | 200 msgs/page, avg 6 pages/ch  |
| Threads      | ~152,000  | ~50 hours            | 98% of total calls              |
| **Total**    | ~155,000  | **~51 hours**        | First full sync                 |

Incremental sync (daily): ~1,000 calls → **10-20 minutes**.

**Optimization:** `sync_threads: false` in config skips thread sync entirely, reducing first sync from 51h to ~1h.

### 4.9 Daemon Mode & Wake Detection

**Poll loop:**
```go
func (d *Daemon) Run(ctx context.Context) error {
    ticker := time.NewTicker(config.Sync.PollInterval)
    wakeCh := d.watchForWake(ctx)

    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            d.runSync(ctx)
        case <-wakeCh:
            d.runSync(ctx)
        }
    }
}
```

**Wake detection:**
- **macOS:** Use `IOKit` power notifications via CGO, or poll-based approach checking system uptime discontinuity
- **Linux:** D-Bus subscription to `org.freedesktop.login1.Manager.PrepareForSleep(false)`
- **Fallback:** Detect time jumps — if `time.Since(lastCheck) > 2 * pollInterval`, assume wake

---

## 5. AI Query Pipeline — Detailed Design

### 5.1 Pipeline Overview

```
User Input
    │
    ▼
QueryParser (deterministic, no AI)
    │ extracts: time range, channels, users, topics, intent
    ▼
ContextBuilder
    │ queries SQLite, formats messages, respects token budget
    ▼
PromptAssembler
    │ combines: system prompt + workspace summary + context + user question
    ▼
Claude API (streaming)
    │ generates response with references
    ▼
ResponseRenderer
    │ enriches with Slack permalinks, renders markdown
    ▼
Terminal Output
```

### 5.2 QueryParser (`internal/ai/query_parser.go`)

Purely deterministic — no AI calls. Parses the user's natural language query to extract structured parameters.

**Extracted fields:**
```go
type ParsedQuery struct {
    RawText     string
    TimeRange   *TimeRange     // from/to timestamps
    Channels    []string       // channel names or IDs
    Users       []string       // user names or IDs
    Topics      []string       // extracted keywords for FTS5
    Intent      QueryIntent    // catchup, search, question, summary
}

type QueryIntent int
const (
    IntentGeneral  QueryIntent = iota
    IntentCatchup              // "what happened", "what's new"
    IntentSearch               // "find messages about X"
    IntentPerson               // "what did @alice say"
    IntentChannel              // "summarize #general"
)
```

**Time range parsing:**
- "yesterday" → 00:00-23:59 of previous day
- "last 2h" / "past 2 hours" → now-2h to now
- "since Monday" → Monday 00:00 to now
- "today" → today 00:00 to now
- "last week" → Monday-Sunday of previous week
- "this morning" → today 06:00 to 12:00
- No time specified + catchup intent → from `user_checkpoints.last_checked_at`
- No time specified + general → last 24 hours (default)

**Channel/user extraction:**
- `#channel-name` → lookup in channels table by name
- `@username` → lookup in users table by name
- "in engineering" → fuzzy match against channel names
- "from alice" / "alice said" → fuzzy match against user names

### 5.3 ContextBuilder (`internal/ai/context_builder.go`)

The most critical component. Builds the context window that Claude will reason over.

**Token budget:** ~150,000 tokens (for claude-sonnet-4-20250514)

**Budget allocation:**
```
Total: 150K tokens
├── System prompt:        ~2K tokens (fixed)
├── Workspace summary:    ~1K tokens (channel list, user stats)
├── Priority context:     ~60K tokens (40%) — watch list, high-priority
├── Relevant context:     ~75K tokens (50%) — query-specific
└── Broad context:        ~12K tokens (10%) — general activity summary
```

**Context selection strategy:**

1. **Priority context (40%)**
   - Recent messages from high-priority watch list channels
   - Sorted by recency, newest first
   - Includes thread summaries for active threads

2. **Relevant context (50%)**
   - If specific channels mentioned → messages from those channels
   - If specific users mentioned → messages by those users
   - If keywords present → FTS5 search results
   - If time range specified → messages in that range
   - Combined and deduplicated

3. **Broad context (10%)**
   - Activity summary across all channels: "X messages in Y channels in the last Z hours"
   - Top active channels and users
   - Any channels with unusual activity spikes

**Message formatting (compact):**

Each message is formatted to maximize information density (~3-4x more efficient than raw JSON):

```
#general | 2025-02-24 14:30 | @alice (Alice Smith): We're deploying v2.3 today. The rollout starts at 3pm EST.
  [+1(3), rocket(2)] [5 replies, latest: 15:42]
    > @bob: Sounds good, I'll monitor the dashboards
    > @carol: Any breaking changes we should know about?
    > @alice: No breaking changes, but check the migration guide
```

Format spec:
```
#{channel} | {YYYY-MM-DD HH:MM} | @{username} ({real_name}): {text}
  [{emoji}({count}), ...] [{reply_count} replies, latest: {HH:MM}]
    > @{username}: {reply_text}  (indented thread replies, abbreviated)
```

**Token counting:**
- Use a simple heuristic: 1 token ≈ 4 characters (English text)
- More precise: count words, multiply by 1.3
- Track running total as context is built; stop adding when budget reached

### 5.4 Prompt Assembly (`internal/ai/prompt.go`)

**System prompt template:**
```
You are Watchtower, an AI assistant that helps analyze Slack workspace activity.

Workspace: {workspace_name} ({domain}.slack.com)
Current time: {now}
User's timezone: {timezone}

You have access to Slack messages from the workspace database. When referencing
messages, always include the Slack permalink so users can jump to the original.

Guidelines:
- Be concise but thorough
- Group information by topic/channel when summarizing
- Highlight important decisions, action items, and blockers
- Include [Slack link](permalink) for key messages
- If you're unsure about something, say so
- Use the user's language (detect from their query)
```

**User message structure:**
```
{workspace_summary}

--- Slack Messages Context ---
{formatted_messages}
--- End Context ---

User question: {user_query}
```

### 5.5 Claude Client (`internal/ai/client.go`)

```go
type Client struct {
    anthropic *anthropic.Client
    model     string  // default: claude-sonnet-4-20250514
}

func (c *Client) Query(ctx context.Context, systemPrompt string, userMessage string) (<-chan string, error) {
    // Uses streaming API
    // Returns a channel that emits text chunks as they arrive
    stream := c.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     c.model,
        MaxTokens: 4096,
        System:    systemPrompt,
        Messages:  []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)),
        },
    })

    ch := make(chan string, 100)
    go func() {
        defer close(ch)
        for stream.Next() {
            event := stream.Current()
            if delta, ok := event.Delta.Text; ok {
                ch <- delta
            }
        }
    }()
    return ch, nil
}
```

**Model configuration:**
- Default: `claude-sonnet-4-20250514` (best balance of cost/quality for analysis)
- Configurable via `config.ai.model` or `--model` flag
- Max output tokens: 4096 (configurable)

### 5.6 Response Renderer (`internal/ai/response_renderer.go`)

Post-processes Claude's response before displaying:

1. **Permalink enrichment:** Detects message references and converts to clickable Slack links
   - Pattern: `#channel-name YYYY-MM-DD HH:MM` → looks up in DB, generates permalink
   - Permalink format: `https://{domain}.slack.com/archives/{channel_id}/p{ts_without_dot}`

2. **Markdown rendering:** Uses glamour library with a custom dark theme for terminal output

3. **Reference list:** Appends a "Sources" section with all referenced messages and their Slack links

**Permalink generation:**
```go
func Permalink(domain, channelID, ts string) string {
    // Slack permalink format: remove the dot from timestamp
    tsNoDot := strings.Replace(ts, ".", "", 1)
    return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s", domain, channelID, tsNoDot)
}
```

---

## 6. Configuration System

### 6.1 Config File Location

Following XDG Base Directory spec:
```
~/.config/watchtower/config.yaml
```

### 6.2 Full Config Schema

```yaml
# Active workspace (used when no --workspace flag)
active_workspace: my-company

# Workspace configurations (supports multiple)
workspaces:
  my-company:
    # Slack User Token (xoxp-...)
    # Required OAuth scopes: channels:history, channels:read, groups:history,
    # groups:read, im:history, im:read, mpim:history, mpim:read, users:read,
    # users:read.email, files:read, reactions:read, team:read
    slack_token: "xoxp-..."    # or env: WATCHTOWER_SLACK_TOKEN

# AI configuration
ai:
  # Anthropic API key
  api_key: ""                  # or env: ANTHROPIC_API_KEY
  # Claude model to use
  model: "claude-sonnet-4-20250514"
  # Max output tokens
  max_tokens: 4096
  # Context window budget (tokens)
  context_budget: 150000

# Sync configuration
sync:
  # Number of parallel workers for message/thread sync
  workers: 5
  # How many days of history to fetch on initial sync
  initial_history_days: 30
  # Daemon mode poll interval
  poll_interval: "15m"
  # Whether to sync thread replies (disable to speed up initial sync)
  sync_threads: true
  # Trigger sync on wake from sleep
  sync_on_wake: true

# Watch list (can also be managed via CLI)
watch:
  channels:
    - name: "engineering"
      priority: "high"
    - name: "incidents"
      priority: "high"
    - name: "general"
      priority: "normal"
  users:
    - name: "alice.smith"
      priority: "high"
```

### 6.3 Environment Variable Override

All config values can be overridden via environment variables:

| Config Path               | Environment Variable              |
|---------------------------|-----------------------------------|
| workspaces.*.slack_token  | WATCHTOWER_SLACK_TOKEN            |
| ai.api_key                | ANTHROPIC_API_KEY                 |
| ai.model                  | WATCHTOWER_AI_MODEL               |
| sync.workers              | WATCHTOWER_SYNC_WORKERS           |

### 6.4 Config Loading Priority

1. CLI flags (highest priority)
2. Environment variables
3. Config file (`~/.config/watchtower/config.yaml`)
4. Defaults (lowest priority)

---

## 7. Project Structure — Complete File List

```
watchtower/
├── main.go                          # Entry point, calls cmd.Execute()
├── go.mod
├── go.sum
├── Makefile                         # build, test, lint, install targets
├── .goreleaser.yaml                 # Cross-platform release config
│
├── cmd/                             # CLI command definitions (cobra)
│   ├── root.go                      # Root command, REPL mode, global flags
│   ├── ask.go                       # `watchtower ask "<question>"`
│   ├── catchup.go                   # `watchtower catchup`
│   ├── sync.go                      # `watchtower sync [--full|--daemon]`
│   ├── status.go                    # `watchtower status`
│   ├── config.go                    # `watchtower config init|set`
│   ├── watch.go                     # `watchtower watch add|remove|list`
│   ├── channels.go                  # `watchtower channels`
│   ├── users.go                     # `watchtower users`
│   └── version.go                   # `watchtower version`
│
├── internal/                        # Private packages
│   ├── config/
│   │   ├── config.go                # Config struct, Load(), Validate()
│   │   └── defaults.go              # Default values
│   │
│   ├── db/
│   │   ├── db.go                    # Open(), Close(), migrations, pragmas
│   │   ├── schema.sql               # Embedded DDL (go:embed)
│   │   ├── models.go                # Go struct definitions
│   │   ├── messages.go              # UpsertMessage, GetMessages, GetByTimeRange
│   │   ├── channels.go              # UpsertChannel, GetChannels, GetByName
│   │   ├── users.go                 # UpsertUser, GetUsers, GetByName
│   │   ├── search.go                # FTS5 search: SearchMessages(query)
│   │   ├── sync_state.go            # GetSyncState, UpdateSyncState
│   │   └── watch.go                 # AddWatch, RemoveWatch, GetWatchList
│   │
│   ├── slack/
│   │   ├── client.go                # NewClient, rate-limited wrapper methods
│   │   ├── ratelimit.go             # RateLimiter, per-tier token buckets
│   │   └── permalink.go             # GeneratePermalink()
│   │
│   ├── sync/
│   │   ├── orchestrator.go          # Sync coordination, phase management
│   │   ├── message_sync.go          # Per-channel message fetching
│   │   ├── thread_sync.go           # Thread reply fetching
│   │   ├── progress.go              # Terminal progress display
│   │   └── worker.go                # Generic worker pool
│   │
│   ├── ai/
│   │   ├── client.go                # Claude API wrapper with streaming
│   │   ├── query_parser.go          # Natural language → structured query
│   │   ├── context_builder.go       # DB data → formatted AI context
│   │   ├── prompt.go                # System prompt templates
│   │   └── response_renderer.go     # Post-process AI output + permalinks
│   │
│   ├── repl/
│   │   ├── repl.go                  # Bubbletea REPL model
│   │   └── commands.go              # REPL slash commands
│   │
│   └── daemon/
│       ├── daemon.go                # Poll loop + wake trigger
│       ├── wake_darwin.go           # macOS wake detection
│       └── wake_linux.go            # Linux wake detection (D-Bus)
│
└── docs/
    └── plan.md                      # This file
```

---

## 8. Implementation Phases — Detailed Steps

### Phase 1: Foundation (MVP Core)

**Goal:** Project skeleton, config system, database, Slack client with rate limiting.

**Step 1.1: Project Setup**
- Initialize `go.mod` with all dependencies
- Create `main.go` that calls `cmd.Execute()`
- Create `cmd/root.go` with cobra root command + global flags (`--workspace`, `--config`, `--verbose`)
- Create `cmd/version.go`
- Create `Makefile` with targets: `build`, `test`, `lint`, `install`

**Step 1.2: Config System (`internal/config/`)**
- Define `Config` struct with all fields
- `defaults.go`: default values for all config fields
- `config.go`: `Load()` function using viper
  - Read YAML from `~/.config/watchtower/config.yaml`
  - Bind environment variables
  - Validate required fields (slack_token, api_key)
- `cmd/config.go`: `config init` wizard, `config set` command

**Step 1.3: Database (`internal/db/`)**
- `schema.sql`: full DDL as embedded SQL file
- `db.go`: `Open(dbPath)` → set pragmas, run migrations, return `*DB`
- `models.go`: Go structs for all tables
- CRUD operations split by entity:
  - `channels.go`: UpsertChannel, GetChannels, GetChannelByName, GetChannelByID
  - `users.go`: UpsertUser, GetUsers, GetUserByName, GetUserByID
  - `messages.go`: UpsertMessage, GetMessages, GetMessagesByTimeRange, GetMessagesByChannel
  - `search.go`: SearchMessages (FTS5 query builder)
  - `sync_state.go`: GetSyncState, UpdateSyncState, MarkInitialSyncComplete
  - `watch.go`: AddWatch, RemoveWatch, GetWatchList, IsWatched

**Step 1.4: Slack Client (`internal/slack/`)**
- `ratelimit.go`: `RateLimiter` with per-tier token buckets
  - `Wait(ctx, tier)` blocks until a token is available
  - Handles 429 responses with Retry-After + jitter
- `client.go`: Wrapper around `slack.Client` that calls `rateLimiter.Wait()` before each API call
  - `GetTeamInfo()`, `GetUsers()`, `GetChannels()`, `GetConversationHistory()`, `GetConversationReplies()`
- `permalink.go`: `GeneratePermalink(domain, channelID, ts) string`

### Phase 2: Sync Engine

**Goal:** Functional sync that populates the database from Slack.

**Step 2.1: Orchestrator (`internal/sync/orchestrator.go`)**
- `SyncOptions` struct with Full, Channels, Workers, DaemonMode flags
- `Run(ctx, opts)` method coordinating all phases
- Error handling: continue on non-fatal errors (log + update sync_state.error)

**Step 2.2: Metadata Sync**
- Fetch workspace info → upsert workspace table
- Fetch all users (paginated) → upsert users table
- Fetch all channels (all types, paginated) → upsert channels table

**Step 2.3: Message Sync (`internal/sync/message_sync.go`)**
- Build channel priority queue (watch high → watch normal → member → rest)
- For each channel: determine oldest parameter from sync_state
- Paginate through conversations.history, upsert each message
- Update sync_state after each page

**Step 2.4: Thread Sync (`internal/sync/thread_sync.go`)**
- Query messages where `reply_count > 0` and replies not fully synced
- For each thread: fetch conversations.replies, upsert all replies
- Skip if `config.sync.sync_threads == false`

**Step 2.5: Worker Pool (`internal/sync/worker.go`)**
- Generic pool with configurable concurrency
- Priority-based task queue
- Graceful shutdown on context cancellation

**Step 2.6: Progress Reporting (`internal/sync/progress.go`)**
- Track: channels total/done, messages fetched, threads fetched
- Render progress bars using lipgloss
- ETA calculation based on current throughput

**Step 2.7: CLI Commands**
- `cmd/sync.go`: parse flags, create orchestrator, run sync
- `cmd/status.go`: query DB for stats, display formatted output

### Phase 3: AI Query

**Goal:** User can ask questions about their Slack data and get AI-powered answers.

**Step 3.1: QueryParser (`internal/ai/query_parser.go`)**
- Parse time expressions: "yesterday", "last 2h", "since Monday", "this week"
- Extract channel references: `#channel-name`
- Extract user references: `@username`, "from alice"
- Extract search keywords for FTS5
- Determine intent: catchup, search, person, channel, general

**Step 3.2: ContextBuilder (`internal/ai/context_builder.go`)**
- `Build(query ParsedQuery) (string, error)`
- Workspace summary generator
- Priority context: messages from watch list
- Relevant context: channel-specific, user-specific, FTS5 search results
- Token budget tracking
- Message formatter (compact format)

**Step 3.3: Claude Client (`internal/ai/client.go`)**
- Initialize anthropic client from config
- `Query(ctx, system, user) (<-chan string, error)` with streaming
- Error handling: API errors, rate limits, context too long

**Step 3.4: Prompt Assembly (`internal/ai/prompt.go`)**
- System prompt template
- User message assembly (summary + context + question)

**Step 3.5: Response Renderer (`internal/ai/response_renderer.go`)**
- Detect message references in Claude's output
- Convert to Slack permalinks
- Render markdown via glamour for terminal
- Append sources section

**Step 3.6: CLI Commands**
- `cmd/ask.go`: one-shot query mode
- `cmd/catchup.go`: catchup mode with checkpoint management

### Phase 4: UX Polish

**Goal:** Interactive REPL, watch list management, utility commands.

**Step 4.1: REPL (`internal/repl/`)**
- Bubbletea model with text input + output area
- Handle slash commands: `/sync`, `/status`, `/catchup`, `/quit`, `/help`
- Stream AI responses in real-time
- Command history (up/down arrows)

**Step 4.2: Watch List CLI (`cmd/watch.go`)**
- `watch add` with channel/user resolution and priority flag
- `watch remove` by name
- `watch list` with formatted output

**Step 4.3: Utility Commands**
- `cmd/channels.go`: list channels with filtering and sorting
- `cmd/users.go`: list users with filtering

### Phase 5: Daemon + Distribution

**Goal:** Background sync, cross-platform distribution.

**Step 5.1: Daemon Mode (`internal/daemon/`)**
- Poll loop with configurable interval
- Wake detection (macOS + Linux)
- Graceful shutdown on SIGINT/SIGTERM

**Step 5.2: Distribution**
- `.goreleaser.yaml` for cross-compilation
- Homebrew formula
- Installation docs

---

## 9. Error Handling Strategy

### 9.1 Sync Errors

- **Rate limit (429):** Wait for Retry-After + jitter, retry automatically
- **Channel not found / access denied:** Log warning, skip channel, continue sync
- **Network error:** Retry with exponential backoff (3 attempts), then skip
- **Database error:** Fatal — stop sync, report to user
- **Interrupted (SIGINT):** Graceful stop, sync_state is already persisted

### 9.2 AI Errors

- **API key invalid:** Clear error message with instructions
- **Context too long:** Reduce context budget, retry with less data
- **Rate limited:** Wait and retry
- **Model unavailable:** Fall back to default model

### 9.3 User-Facing Errors

All errors displayed to the user should be:
- Clear and actionable ("Cannot connect to Slack API. Check your token in ~/.config/watchtower/config.yaml")
- Not panics or stack traces
- Logged to stderr with detail level controlled by `--verbose`

---

## 10. Testing Strategy

### 10.1 Unit Tests

| Package              | What to test                                        |
|----------------------|-----------------------------------------------------|
| `internal/db/`       | CRUD operations, FTS5 search, migrations, edge cases |
| `internal/slack/`    | Rate limiter timing, permalink generation            |
| `internal/ai/`       | QueryParser (time parsing, entity extraction)        |
| `internal/ai/`       | ContextBuilder (budget allocation, formatting)       |
| `internal/config/`   | Loading, validation, env var override                |

### 10.2 Integration Tests

- **Sync flow:** Mock Slack API → run sync → verify DB contents
- **AI flow:** Pre-populated DB → ask question → verify context contains right messages
- **End-to-end:** Mock Slack API → sync → ask → verify response references correct data

### 10.3 Test Infrastructure

- Use `testing.T` + `testify/assert` for assertions
- In-memory SQLite for DB tests (`:memory:`)
- `httptest.Server` for Slack API mocking
- Table-driven tests for QueryParser time expressions

---

## 11. Performance Considerations

### 11.1 Database Performance

- WAL mode for concurrent read/write
- Batch inserts wrapped in transactions (per API page, ~200 messages)
- Indexes on all query-hot columns (ts_unix, channel_id, user_id)
- FTS5 for full-text search (no LIKE queries)

### 11.2 Memory Usage

- Stream Slack API responses, don't buffer entire channel history in memory
- Context builder streams from DB cursor, tracks token budget
- Claude API response streaming to terminal

### 11.3 Binary Size

- Pure Go SQLite (no CGO) adds ~5MB to binary
- Total binary expected: ~15-20MB
- Compressible to ~8MB with UPX

---

## 12. Security Considerations

- Slack token stored in config file with 0600 permissions
- Support for environment variables (preferred for CI/sensitive environments)
- Database file with 0600 permissions
- No data sent to Claude beyond what user explicitly queries
- Read-only Slack access (no write scopes)
- No telemetry or phone-home

---

## 13. OAuth Scopes Reference

Minimum required scopes (13 total):

| Scope              | Purpose                           |
|--------------------|-----------------------------------|
| channels:history   | Read public channel messages       |
| channels:read      | List public channels              |
| groups:history     | Read private channel messages      |
| groups:read        | List private channels             |
| im:history         | Read direct messages              |
| im:read            | List direct message channels       |
| mpim:history       | Read group DM messages            |
| mpim:read          | List group DM channels            |
| users:read         | List users                        |
| users:read.email   | Get user email addresses          |
| files:read         | Read file metadata                |
| reactions:read     | Read message reactions            |
| team:read          | Get workspace info                |

---

## 14. Dependencies (go.mod)

```
github.com/spf13/cobra       v1.8+     # CLI framework
github.com/spf13/viper       v1.18+    # Config management
modernc.org/sqlite            v1.28+    # Pure Go SQLite
github.com/slack-go/slack     v0.12+    # Slack API client
github.com/anthropics/anthropic-sdk-go  # Claude API client
golang.org/x/time             latest    # rate.Limiter
github.com/charmbracelet/bubbletea      # TUI framework
github.com/charmbracelet/lipgloss       # Terminal styling
github.com/charmbracelet/glamour        # Markdown rendering
github.com/dustin/go-humanize           # Human-readable numbers
github.com/stretchr/testify             # Test assertions
```
