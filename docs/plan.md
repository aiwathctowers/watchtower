# Watchtower — Implementation Plan

## Overview

Watchtower is an open-source Go CLI tool that syncs a Slack workspace into a local SQLite database and provides an AI-powered interface for analysis via the Claude API.

**Architecture:**
```
[Slack API] --(sync)--> [SQLite DB] --(query)--> [Claude API] --> [Terminal Output]
                              ^                        |
                              |                        v
                         [FTS5 Search]          [Links to Slack]
```

**Tech Stack:** Go 1.25, cobra/viper, modernc.org/sqlite (pure Go, zero CGO), slack-go/slack, anthropic-sdk-go, bubbletea/lipgloss/glamour, golang.org/x/time/rate

---

## Progress

### Task 1: Project setup and go.mod initialization
- [x] Initialize `go.mod` with module name `watchtower` and Go 1.25
- [x] Add all dependencies to `go.mod`: cobra, viper, modernc.org/sqlite, slack-go/slack, anthropic-sdk-go, golang.org/x/time, bubbletea, lipgloss, glamour, go-humanize, testify
- [x] Create `main.go` entry point that calls `cmd.Execute()`
- [x] Create `Makefile` with targets: `build`, `test`, `lint`, `install`

**Files:** `go.mod`, `go.sum`, `main.go`, `Makefile`

### Task 2: Cobra root command and global flags
- [x] Create `cmd/root.go` with root cobra command
- [x] Add global persistent flags: `--workspace` (string), `--config` (string, default `~/.config/watchtower/config.yaml`), `--verbose` (bool)
- [x] Root command with no subcommand should print help (REPL mode added later in Task 18)
- [x] Create `cmd/version.go` that prints version info (embed via ldflags)

**Files:** `cmd/root.go`, `cmd/version.go`

### Task 3: Config system (internal/config/)
- [x] Create `internal/config/defaults.go` with default values for all config fields
- [x] Create `internal/config/config.go` with:
  - `Config` struct: `ActiveWorkspace`, `Workspaces` map (each has `SlackToken`), `AI` (ApiKey, Model, MaxTokens, ContextBudget), `Sync` (Workers, InitialHistoryDays, PollInterval, SyncThreads, SyncOnWake), `Watch` (Channels, Users)
  - `Load(configPath string) (*Config, error)` using viper: reads YAML, binds env vars (`WATCHTOWER_SLACK_TOKEN`, `ANTHROPIC_API_KEY`, `WATCHTOWER_AI_MODEL`, `WATCHTOWER_SYNC_WORKERS`)
  - `Validate() error` — checks required fields
  - `GetActiveWorkspace() (*WorkspaceConfig, error)`
  - `DBPath() string` — returns `~/.local/share/watchtower/<workspace>/watchtower.db`
- [x] Create `cmd/config.go`:
  - `config init` subcommand: interactive wizard that asks for Slack token, Anthropic API key, tests connections, writes config YAML, creates DB directory
  - `config set <key> <value>` subcommand: dot-notation setter (e.g., `ai.model`)
  - `config show` subcommand: prints current config (masks tokens)

**Config file location:** `~/.config/watchtower/config.yaml`

**Full config schema:**
```yaml
active_workspace: my-company
workspaces:
  my-company:
    slack_token: "xoxp-..."       # or env: WATCHTOWER_SLACK_TOKEN
ai:
  api_key: ""                     # or env: ANTHROPIC_API_KEY
  model: "claude-sonnet-4-20250514"
  max_tokens: 4096
  context_budget: 150000
sync:
  workers: 5
  initial_history_days: 30
  poll_interval: "15m"
  sync_threads: true
  sync_on_wake: true
watch:
  channels:
    - name: "engineering"
      priority: "high"
  users:
    - name: "alice.smith"
      priority: "high"
```

**Loading priority:** CLI flags > env vars > config file > defaults

**Files:** `internal/config/config.go`, `internal/config/defaults.go`, `cmd/config.go`

### Task 4: SQLite database layer (internal/db/)
- [x] Create `internal/db/schema.sql` with all DDL (embedded via `go:embed`):
  - `workspace` table: id (TEXT PK, team_id), name, domain, synced_at
  - `users` table: id (TEXT PK), name, display_name, real_name, email, is_bot, is_deleted, profile_json, updated_at; indexes on name, is_bot
  - `channels` table: id (TEXT PK), name, type (CHECK public/private/dm/group_dm), topic, purpose, is_archived, is_member, dm_user_id, num_members, updated_at; indexes on name, type, is_archived
  - `messages` table: PK(channel_id, ts), user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, ts_unix (GENERATED STORED from ts), raw_json; indexes on user_id, (channel_id, thread_ts), ts_unix, (channel_id, ts_unix)
  - `messages_fts` FTS5 virtual table: text, channel_id UNINDEXED, ts UNINDEXED, user_id UNINDEXED; tokenize='porter unicode61'; with INSERT/UPDATE/DELETE triggers on messages
  - `reactions` table: PK(channel_id, message_ts, user_id, emoji); index on (channel_id, message_ts)
  - `files` table: id (TEXT PK), message_channel_id, message_ts, name, mimetype, size, permalink; index on (message_channel_id, message_ts)
  - `sync_state` table: channel_id (TEXT PK), last_synced_ts, oldest_synced_ts, is_initial_sync_complete, cursor, messages_synced, last_sync_at, error
  - `watch_list` table: PK(entity_type, entity_id), entity_name, priority (CHECK high/normal/low), created_at
  - `user_checkpoints` table: id (INTEGER PK, CHECK id=1 singleton), last_checked_at
- [x] Create `internal/db/db.go`:
  - `type DB struct` wrapping `*sql.DB`
  - `Open(dbPath string) (*DB, error)` — creates directories, opens DB, sets pragmas (WAL, busy_timeout=5000, foreign_keys=ON, synchronous=NORMAL), runs migrations
  - `Close() error`
  - Migration system using `PRAGMA user_version`
- [x] Create `internal/db/models.go` with Go structs: `Workspace`, `User`, `Channel`, `Message`, `Reaction`, `File`, `SyncState`, `WatchItem`, `UserCheckpoint`

**Key decisions:**
- `ts` stored as TEXT (Slack timestamp = unique message ID like `1234567890.123456`)
- Composite PK `(channel_id, ts)` matches Slack uniqueness guarantee
- `ts_unix` as GENERATED column for efficient time-range queries
- `is_deleted` for soft deletes — never physically remove messages
- FTS5 with `porter unicode61` tokenizer — "deployment" matches "deployed", "errors"
- WAL mode enables concurrent reads during sync writes

**DB file location:** `~/.local/share/watchtower/<workspace-name>/watchtower.db`

**Files:** `internal/db/db.go`, `internal/db/schema.sql`, `internal/db/models.go`

### Task 5: Database CRUD operations
- [x] Create `internal/db/channels.go`: `UpsertChannel(ch)`, `GetChannels(filter)`, `GetChannelByName(name)`, `GetChannelByID(id)`
- [x] Create `internal/db/users.go`: `UpsertUser(u)`, `GetUsers(filter)`, `GetUserByName(name)`, `GetUserByID(id)`
- [x] Create `internal/db/messages.go`: `UpsertMessage(msg)`, `GetMessages(opts)`, `GetMessagesByTimeRange(channelID, from, to)`, `GetMessagesByChannel(channelID, limit)`, `GetThreadReplies(channelID, threadTS)`
- [x] Create `internal/db/search.go`: `SearchMessages(query string, opts SearchOpts) ([]Message, error)` — builds FTS5 MATCH query, joins with messages table for full data, supports channel/user/time filters
- [x] Create `internal/db/sync_state.go`: `GetSyncState(channelID)`, `UpdateSyncState(channelID, state)`, `MarkInitialSyncComplete(channelID)`
- [x] Create `internal/db/watch.go`: `AddWatch(entityType, entityID, entityName, priority)`, `RemoveWatch(entityType, entityID)`, `GetWatchList()`, `IsWatched(entityType, entityID) bool`

**Files:** `internal/db/channels.go`, `internal/db/users.go`, `internal/db/messages.go`, `internal/db/search.go`, `internal/db/sync_state.go`, `internal/db/watch.go`

### Task 6: Slack client with rate limiting (internal/slack/)
- [x] Create `internal/slack/ratelimit.go`:
  - `type RateLimiter struct` with per-tier `*rate.Limiter` map (Tier2: 20/min, Tier3: 50/min) and per-method backoff tracking
  - `Wait(ctx context.Context, tier int) error` — blocks until token available, respects 429 backoff
  - `HandleRateLimit(method string, retryAfter time.Duration)` — sets backoff with jitter for all callers of that tier
- [x] Create `internal/slack/client.go`:
  - `type Client struct` wrapping `*slack.Client` + `*RateLimiter`
  - `NewClient(token string) *Client`
  - Rate-limited wrappers: `GetTeamInfo(ctx)`, `GetUsers(ctx)` (paginated), `GetChannels(ctx)` (paginated, all types), `GetConversationHistory(ctx, channelID, opts)`, `GetConversationReplies(ctx, channelID, threadTS)`
  - Each method calls `rateLimiter.Wait()` before API call, handles 429 response
- [x] Create `internal/slack/permalink.go`:
  - `GeneratePermalink(domain, channelID, ts string) string` — format: `https://{domain}.slack.com/archives/{channelID}/p{ts_without_dot}`

**Rate limit tiers:**
| Tier | Limit   | Methods |
|------|---------|---------|
| 2    | 20/min  | users.list, conversations.list, team.info |
| 3    | 50/min  | conversations.history, conversations.replies |

**Files:** `internal/slack/client.go`, `internal/slack/ratelimit.go`, `internal/slack/permalink.go`

### Task 7: Sync orchestrator (internal/sync/orchestrator.go)
- [x] Create `internal/sync/orchestrator.go`:
  - `type SyncOptions struct` with fields: Full (bool), Channels ([]string), Workers (int), DaemonMode (bool)
  - `type Orchestrator struct` with db, slackClient, config, progress
  - `NewOrchestrator(db, slackClient, config) *Orchestrator`
  - `Run(ctx context.Context, opts SyncOptions) error` — coordinates three phases sequentially:
    1. `syncMetadata(ctx)` — workspace info, users, channels
    2. `syncMessages(ctx, opts)` — parallel message sync across channels
    3. `syncThreads(ctx, opts)` — parallel thread reply sync
  - Error handling: continue on non-fatal errors (channel_not_found, access_denied), fatal on DB errors

**Files:** `internal/sync/orchestrator.go`

### Task 8: Worker pool (internal/sync/worker.go)
- [x] Create `internal/sync/worker.go`:
  - `type WorkerPool struct` with configurable concurrency, task channel, WaitGroup, error channel
  - `type SyncTask struct` with Type (channel/thread), ChannelID, ThreadTS, Priority
  - `NewWorkerPool(workers int) *WorkerPool`
  - `Start(ctx context.Context, handler func(task SyncTask) error)`
  - `Submit(task SyncTask)` — sends task to worker channel
  - `Wait() []error` — waits for all workers, returns collected errors
  - Priority-based ordering: tasks submitted in priority order (watch high → watch normal → member → rest)
  - Graceful shutdown on context cancellation

**Files:** `internal/sync/worker.go`

### Task 9: Metadata sync (workspace, users, channels)
- [x] Add `syncMetadata(ctx)` to orchestrator:
  - Call `slackClient.GetTeamInfo()` → `db.UpsertWorkspace()`
  - Call `slackClient.GetUsers()` (paginated) → for each: `db.UpsertUser()`; detect deleted users (in DB but absent from API → set is_deleted=1)
  - Call `slackClient.GetChannels()` (types=public_channel,private_channel,im,mpim, paginated) → for each: `db.UpsertChannel()` with is_member from API response

**Files:** `internal/sync/orchestrator.go` (extend)

### Task 10: Message sync with worker pool
- [x] Create `internal/sync/message_sync.go`:
  - `syncMessages(ctx, opts)` — builds channel priority queue, submits SyncTask per channel to WorkerPool
  - Channel prioritization: watch high → watch normal → is_member (sorted by activity) → non-archived rest
  - `syncChannel(ctx, channel, syncState)` — per-channel logic:
    - Determines `oldest` param from sync_state.last_synced_ts (incremental) or computed cutoff (initial: now - initial_history_days)
    - If sync_state.cursor exists → resume pagination from cursor
    - Paginates through conversations.history (limit=200/page)
    - For each page: upsert all messages in a transaction, update sync_state (last_synced_ts, cursor, messages_synced)
    - On completion: clear cursor, mark initial_sync_complete if applicable
    - On `--full`: ignore sync_state, re-fetch within initial_history_days window

**Files:** `internal/sync/message_sync.go`

### Task 11: Thread sync
- [x] Create `internal/sync/thread_sync.go`:
  - `syncThreads(ctx, opts)` — skip entirely if `config.sync.sync_threads == false`
  - Query messages with `reply_count > 0` that don't have all replies synced yet
  - For each thread parent: submit SyncTask(type=thread) to WorkerPool
  - `syncThread(ctx, channelID, threadTS)`:
    - Call `slackClient.GetConversationReplies(channelID, threadTS)`
    - Upsert all reply messages (with thread_ts set) in a transaction

**Files:** `internal/sync/thread_sync.go`

### Task 12: Sync progress reporting
- [x] Create `internal/sync/progress.go`:
  - `type Progress struct` tracking: phase (metadata/messages/threads), channels total/done, messages fetched, threads fetched, current channel name
  - Thread-safe updates via mutex
  - `Render()` — formats progress for terminal display using lipgloss:
    ```
    Syncing my-company workspace...
      Metadata: users 1205/1205, channels 342/342 done
      Messages: [████████████████░░░░░░░░] 67% (230/342 channels, 45,230 msgs)
      Threads:  waiting...
    ```
  - ETA calculation based on current throughput (messages/sec or channels/sec)

**Files:** `internal/sync/progress.go`

### Task 13: CLI sync and status commands
- [x] Create `cmd/sync.go`:
  - `watchtower sync` command with flags: `--full`, `--daemon`, `--channels` (string slice), `--workers` (int)
  - Loads config, opens DB, creates Slack client, creates Orchestrator, runs sync
  - Shows progress during sync, summary on completion
- [x] Create `cmd/status.go`:
  - `watchtower status` command (no flags)
  - Queries DB for: workspace info, channel count (+ watched count), user count, message count, thread count (messages with reply_count>0), DB file size, last sync timestamp
  - Formats and prints summary:
    ```
    Workspace: my-company (T024BE7LD)
    Database:  ~/.local/share/watchtower/my-company/watchtower.db (1.2 GB)
    Last sync: 2025-02-24 14:30:00 (25 minutes ago)
    Channels: 342 (12 watched) | Users: 1,205 | Messages: 2,451,230
    ```

**Files:** `cmd/sync.go`, `cmd/status.go`

### Task 14: AI query parser (internal/ai/query_parser.go)
- [x] Create `internal/ai/query_parser.go`:
  - `type ParsedQuery struct`: RawText, TimeRange (*TimeRange with From/To time.Time), Channels ([]string), Users ([]string), Topics ([]string for FTS5), Intent (QueryIntent enum)
  - `type QueryIntent int` constants: IntentGeneral, IntentCatchup, IntentSearch, IntentPerson, IntentChannel
  - `Parse(input string) ParsedQuery` — deterministic, no AI calls:
    - Time range parsing: "yesterday" → prev day 00:00-23:59, "last 2h"/"past 2 hours" → now-2h..now, "since Monday" → Monday 00:00..now, "today" → today 00:00..now, "last week" → prev Mon-Sun, "this morning" → today 06:00-12:00; no time + catchup → from checkpoint; no time + general → last 24h
    - Channel extraction: `#channel-name` literal, "in engineering" → fuzzy match
    - User extraction: `@username` literal, "from alice" / "alice said" → fuzzy match
    - Intent detection: "what happened"/"what's new" → catchup, "find messages about" → search, "what did @X say" → person, "summarize #X" → channel
    - Remaining text after extraction → Topics (keywords for FTS5)

**Files:** `internal/ai/query_parser.go`

### Task 15: AI context builder (internal/ai/context_builder.go)
- [x] Create `internal/ai/context_builder.go`:
  - `type ContextBuilder struct` with db, config, domain
  - `Build(query ParsedQuery) (string, error)` — assembles context string within token budget (~150K tokens)
  - Token budget allocation: system prompt ~2K (fixed), workspace summary ~1K, priority context 40% (~60K), relevant context 50% (~75K), broad context 10% (~12K)
  - **Workspace summary:** channel count, user count, top active channels, watch list info
  - **Priority context (40%):** recent messages from high-priority watch list channels, newest first, includes thread summaries
  - **Relevant context (50%):** channel-specific messages (if channels mentioned), user-specific messages (if users mentioned), FTS5 search results (if keywords present), time-range filtered messages; combined and deduplicated
  - **Broad context (10%):** activity summary across all channels, top active channels/users, unusual activity spikes
  - Token counting heuristic: 1 token ≈ 4 characters
  - **Compact message format:**
    ```
    #general | 2025-02-24 14:30 | @alice (Alice Smith): We're deploying v2.3 today.
      [+1(3), rocket(2)] [5 replies, latest: 15:42]
        > @bob: Sounds good, I'll monitor the dashboards
        > @carol: Any breaking changes we should know about?
    ```

**Files:** `internal/ai/context_builder.go`

### Task 16: Claude API client with streaming (internal/ai/client.go)
- [x] Create `internal/ai/client.go`:
  - `type Client struct` with anthropic client and model string
  - `NewClient(apiKey, model string) *Client`
  - `Query(ctx context.Context, systemPrompt, userMessage string) (<-chan string, error)` — uses anthropic-sdk-go streaming API, returns channel emitting text chunks as they arrive
  - Error handling: invalid API key (clear message), context too long (reduce and retry), rate limited (wait and retry)
- [x] Create `internal/ai/prompt.go`:
  - System prompt template: "You are Watchtower, an AI assistant that helps analyze Slack workspace activity..." with workspace name, domain, current time, timezone, guidelines (be concise, include permalinks, use user's language)
  - `AssembleUserMessage(summary, context, question string) string` — combines workspace summary + message context + user question

**Files:** `internal/ai/client.go`, `internal/ai/prompt.go`

### Task 17: Response renderer (internal/ai/response_renderer.go)
- [x] Create `internal/ai/response_renderer.go`:
  - `type ResponseRenderer struct` with db (for permalink lookups), domain
  - `Render(response string) (string, error)`:
    - Detects message references (patterns like `#channel-name YYYY-MM-DD HH:MM`) → looks up in DB → converts to Slack permalinks (`https://{domain}.slack.com/archives/{channelID}/p{ts_no_dot}`)
    - Renders markdown via glamour with dark terminal theme
    - Appends "Sources" section listing all referenced messages with Slack links

**Files:** `internal/ai/response_renderer.go`

### Task 18: CLI ask and catchup commands
- [x] Create `cmd/ask.go`:
  - `watchtower ask "<question>"` command
  - Flags: `--model` (string), `--no-stream` (bool), `--channel` (string), `--since` (duration)
  - Pipeline: parse query → build context → assemble prompt → call Claude (streaming) → render response → print
- [x] Create `cmd/catchup.go`:
  - `watchtower catchup` command
  - Flags: `--since` (duration, overrides checkpoint), `--watched-only` (bool), `--channel` (string)
  - Behavior:
    1. Read `user_checkpoints.last_checked_at` (or use `--since`)
    2. Query messages since that time, grouped by channel, sorted by watch priority
    3. Build context and ask Claude for structured summary
    4. Render and print
    5. Update `user_checkpoints.last_checked_at` to now

**Files:** `cmd/ask.go`, `cmd/catchup.go`

### Task 19: Watch list CLI (cmd/watch.go)
- [ ] Create `cmd/watch.go`:
  - `watchtower watch add <target> [--priority high|normal|low]` — resolves `#channel-name` or `@username` to entity ID via DB lookup, calls `db.AddWatch()`
  - `watchtower watch remove <target>` — resolves target, calls `db.RemoveWatch()`
  - `watchtower watch list` — calls `db.GetWatchList()`, formats output with entity name, type, priority

**Files:** `cmd/watch.go`

### Task 20: Channels and users CLI commands
- [ ] Create `cmd/channels.go`:
  - `watchtower channels` command
  - Flags: `--type` (public/private/dm/group_dm), `--sort` (messages/name/recent)
  - Lists all synced channels with: name, type, member count, message count, last activity, watched status
- [ ] Create `cmd/users.go`:
  - `watchtower users` command
  - Flags: `--active` (exclude deleted/bot users)
  - Lists all synced users with: name, display_name, email, is_bot status

**Files:** `cmd/channels.go`, `cmd/users.go`

### Task 21: REPL mode (internal/repl/)
- [ ] Create `internal/repl/repl.go`:
  - Bubbletea model with text input area + scrollable output area
  - On Enter: send input through AI pipeline (parse → context → Claude → render), stream response to output
  - Command history via up/down arrow keys
  - Ctrl+C or `/quit` to exit
- [ ] Create `internal/repl/commands.go`:
  - Slash command handler for: `/sync` (trigger incremental sync), `/status` (show DB stats), `/catchup` (run catchup), `/quit` or `/exit` (exit), `/help` (list commands)
- [ ] Wire REPL into `cmd/root.go`: when no subcommand is given, launch REPL

**Files:** `internal/repl/repl.go`, `internal/repl/commands.go`, `cmd/root.go` (modify)

### Task 22: Daemon mode with wake detection (internal/daemon/)
- [ ] Create `internal/daemon/daemon.go`:
  - `type Daemon struct` with orchestrator, config, poll ticker
  - `Run(ctx context.Context) error` — poll loop: on ticker (config poll_interval) or wake signal → run incremental sync
  - Graceful shutdown on SIGINT/SIGTERM via context cancellation
- [ ] Create `internal/daemon/wake_darwin.go` (build tag `//go:build darwin`):
  - Detect wake-from-sleep: poll-based approach checking system uptime discontinuity (if `time.Since(lastCheck) > 2 * pollInterval` → assume wake)
  - Returns `<-chan struct{}` that fires on wake
- [ ] Create `internal/daemon/wake_linux.go` (build tag `//go:build linux`):
  - D-Bus subscription to `org.freedesktop.login1.Manager.PrepareForSleep(false)`
  - Returns `<-chan struct{}` that fires on wake
  - Fallback: same time-jump detection as darwin

**Files:** `internal/daemon/daemon.go`, `internal/daemon/wake_darwin.go`, `internal/daemon/wake_linux.go`

### Task 23: Unit tests
- [ ] `internal/db/` tests: CRUD operations for all entities, FTS5 search (stemming, multi-word, channel/user filter), migrations on fresh DB, edge cases (duplicate upsert, empty text, unicode)
- [ ] `internal/slack/` tests: rate limiter timing (wait blocks correctly, handles 429 backoff), permalink generation (dot removal, URL format)
- [ ] `internal/ai/` tests: QueryParser — table-driven tests for time expressions ("yesterday", "last 2h", "since Monday"), channel/user extraction, intent detection; ContextBuilder — budget allocation stays within limits, message formatting matches spec
- [ ] `internal/config/` tests: loading from YAML, env var override, validation of missing fields

**Test infrastructure:** `testing.T` + `testify/assert`, in-memory SQLite (`:memory:`), `httptest.Server` for Slack API mocking, table-driven tests

**Files:** `internal/db/*_test.go`, `internal/slack/*_test.go`, `internal/ai/*_test.go`, `internal/config/*_test.go`

### Task 24: Integration tests
- [ ] Sync flow test: mock Slack API (httptest.Server returning canned responses) → run full sync → verify DB contains expected workspace, users, channels, messages, threads
- [ ] AI flow test: pre-populate SQLite DB with test data → run ask pipeline → verify context contains correct messages for the query
- [ ] End-to-end test: mock Slack API → sync → ask question → verify response references correct channels/users

**Files:** `internal/sync/orchestrator_test.go`, `internal/ai/pipeline_test.go`

### Task 25: GoReleaser and distribution
- [ ] Create `.goreleaser.yaml` for cross-compilation (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64)
- [ ] Homebrew formula generation in goreleaser config
- [ ] Verify `go build` produces working binary on current platform

**Files:** `.goreleaser.yaml`
