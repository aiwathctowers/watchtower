# LLM Pipelines — AI Algorithm Reference

## Architecture: Daemon → Pipelines → Generator → Claude CLI

```
╔═══════════════════════════════════════════════════════════════════════╗
║                          DAEMON LOOP                                  ║
║                     internal/daemon/daemon.go                         ║
║                                                                       ║
║  every PollInterval (default 15 min) OR wake-from-sleep event:        ║
║                                                                       ║
║    1. Reactivate snoozed tracks (if snooze_until passed)              ║
║    2. Slack Sync (search.messages API, no AI)                         ║
║    3. Run AI Pipelines (see phases below)                             ║
╚══════════════════════════════╤════════════════════════════════════════╝
                               │
                               ▼
```

### Invocation: all AI calls via Claude CLI subprocess

```
ClaudeGenerator.Generate(ctx, systemPrompt, userMessage, sessionID)
    │
    ▼
exec.CommandContext(ctx, "claude",
    "-p", userMessage,
    "--output-format", "json",
    "--model", model,              // haiku/sonnet/opus
    "--no-session-persistence"
)
    │
    ├─ stdout → parse JSON (single object or streaming array)
    ├─ stderr → error diagnostics (max 64KB)
    ├─ SIGINT on ctx cancel → SIGKILL after 5s
    └─ Returns: (rawJSON, Usage{input, output, cost}, sessionID, error)
```

### Session Pool: PooledGenerator

```
PooledGenerator wraps ClaudeGenerator:
    pool.Acquire(ctx)  ← block if all slots busy
    inner.Generate()   ← one Claude subprocess
    pool.Release()     ← free slot

All pipelines share ONE pool → limits total concurrent Claude processes.
```

---

## Execution Phases After Each Sync

```
Sync complete
    │
    ├─ ctx cancelled? → STOP (graceful shutdown)
    ├─ sync error? → CONTINUE (process whatever data we have)
    │
    ▼
┌─────────────────────────────────────────────────────────────────┐
│  PHASE 1: PARALLEL                                               │
│                                                                   │
│  ┌──────────────────────────┐  ┌──────────────────────────────┐  │
│  │  Channel Digests          │  │  People Analysis              │  │
│  │  RunChannelDigestsOnly()  │  │  Run()                        │  │
│  │                           │  │                               │  │
│  │  Throttle: NONE           │  │  Throttle: 1×/24h            │  │
│  │  (skipped by new msgs     │  │  (persisted: last_analysis)  │  │
│  │   query)                  │  │  Workers: 10                 │  │
│  │  Workers: 5               │  │                               │  │
│  └──────────────────────────┘  └──────────────────────────────┘  │
│                                                                   │
│  wg.Wait() — wait for both                                       │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  PHASE 2: Chains (sequential)                                    │
│                                                                   │
│  chainsPipe.Run()  → link unlinked decisions → create/update     │
│  Throttle: NONE (1 AI call, skipped if no unlinked decisions)    │
│                                                                   │
│  chainCtx = FormatActiveChainsForPrompt()                        │
│  digestPipe.ChainContext = chainCtx  ← inject into rollups       │
│  tracksPipe.ChainContext = chainCtx  ← inject into tracks        │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  PHASE 3: Rollups (sequential)                                   │
│                                                                   │
│  digestPipe.RunRollups()                                         │
│    ├─ Daily Rollup: 1 call (from today's channel digests)        │
│    └─ Weekly Trends: 1 call (from 7 days of daily digests)       │
│  Throttle: NONE (skipped if <2 channel/daily digests)            │
└──────────────────────┬──────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────────┐
│  PHASE 4: Tracks (sequential)                                    │
│                                                                   │
│  A. Tracks Extract (FULL)                                        │
│     Throttle: 1×/hour (persisted: last_action_items.txt)         │
│     tracksPipe.Run() → extract new tracks from messages          │
│     Workers: 3                                                   │
│                                                                   │
│  B. Tracks Update (LIGHTWEIGHT)                                  │
│     Throttle: NONE — every sync!                                 │
│     tracksPipe.CheckForUpdates() → batch check thread replies    │
│     Workers: 2                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Throttle persistence (survives daemon restart)

| Pipeline | File on disk | Interval |
|---|---|---|
| People Analysis | `{workspace}/last_analysis.txt` | 24 hours |
| Tracks Extract | `{workspace}/last_action_items.txt` | 1 hour (config) |

---

## Pipeline 1: Channel Digests

### Algorithm: RunChannelDigests

```
RunChannelDigests(ctx)
    │
    ├─ config.Digest.Enabled == false? → return 0
    │
    ├─ loadCaches()
    │    ├─ userNames: map[userID → displayName]
    │    ├─ channelNames: map[channelID → name]
    │    └─ profile: UserProfile (for personalization)
    │
    ├─ isFirstRun()? (no channel digests in DB)
    │    │
    │    ├─ YES → runInitialDayByDay()
    │    │         for d = historyDays..1:
    │    │           dayStart = midnight(now - d days)
    │    │           dayEnd = dayStart + 24h
    │    │           RunChannelDigestsForWindow(dayStart, dayEnd)
    │    │           RunDailyRollupForDate(dayStart)
    │    │         process today (partial)
    │    │         RunWeeklyTrends()
    │    │
    │    └─ NO → standard incremental flow ↓
    │
    ├─ Determine time window
    │    since = latest channel digest's period_to
    │    now = time.Now()
    │
    ├─ Find channels with new messages since `since`
    │    SELECT DISTINCT channel_id FROM messages WHERE ts_unix > since
    │
    ├─ For each channel: filter by threshold
    │    │
    │    │  msgs = GetMessagesByTimeRange(channelID, since, now)
    │    │  visible = count(msgs where text != "" && !isDeleted)
    │    │
    │    ├─ visible < MinMessages (default 5)? → SKIP
    │    └─ visible >= MinMessages → add to tasks[]
    │
    ├─ Worker Pool (N = min(5, len(tasks)))
    │    │
    │    │  for task in tasks (parallel):
    │    │    ┌──────────────────────────────────────────────┐
    │    │    │  1. Sort messages by timestamp (oldest first) │
    │    │    │                                               │
    │    │    │  2. Format messages:                          │
    │    │    │     [15:04] @user: text                       │
    │    │    │     (sanitize === and --- to prevent          │
    │    │    │      prompt injection)                        │
    │    │    │                                               │
    │    │    │  3. Build prompt:                             │
    │    │    │     template(channelName, from, to,           │
    │    │    │       profileContext, langInstr, messages)     │
    │    │    │                                               │
    │    │    │  4. generator.Generate() → Claude CLI         │
    │    │    │                                               │
    │    │    │  5. Parse JSON response:                      │
    │    │    │     {summary, topics[], decisions[],          │
    │    │    │      action_items[], key_messages[]}          │
    │    │    │                                               │
    │    │    │  6. UpsertDigest(type="channel")              │
    │    │    │     key: (channel_id, type, period_from, to)  │
    │    │    │                                               │
    │    │    │  Error? → log, continue (non-fatal)           │
    │    │    └──────────────────────────────────────────────┘
    │    │
    │    └─ wg.Wait()
    │
    └─ Return (generated count, total usage)
```

### Prompt template: digest.channel

```
Input:  channelName, fromTime, toTime, profileContext, langInstruction, formattedMessages
Output: JSON {summary, topics[], decisions[], action_items[], key_messages[]}

Decisions: only conscious choices between alternatives (NOT status updates, deploys, routine ops)
Importance: high (org-wide), medium (team), low (tactical)
```

### Profile Context injection (all pipelines)

```
=== USER PROFILE CONTEXT ===
<custom_prompt_context from onboarding>

PERSONALIZATION RULES:
- Prioritize decisions/items relevant to user's role
- Highlight topics in user's area of focus
STARRED CHANNELS: [list]
STARRED PEOPLE: [list]
MY REPORTS: [list]
```

### Language Instruction (all pipelines)

```
if language == "" → "Write in the language most commonly used in the messages"
if language == "English" → "Write all text in English"
else → "IMPORTANT: You MUST write ALL text in {language}. Do NOT use English."
```

---

## Pipeline 2: Daily/Weekly Rollups

### Algorithm: RunRollups

```
RunRollups(ctx)
    │
    ├─ RunDailyRollup(ctx)
    │    │
    │    ├─ Get today's channel digests (type="channel", today's midnight..midnight+24h)
    │    ├─ < 2 channel digests? → SKIP
    │    │
    │    ├─ Format input:
    │    │    for each channelDigest:
    │    │      ### #channelName (N messages)
    │    │      Summary: {sanitized summary}
    │    │      Decisions: {sanitized decisions JSON}
    │    │
    │    ├─ Prepend ChainContext (if set by chains pipeline):
    │    │    === CHAIN UPDATES ===
    │    │    Chain "DB Migration" (5 items, 2 new):
    │    │      - [#eng] "Choose RDS type" (high)
    │    │    === STANDALONE DECISIONS ===
    │    │      - [#marketing] "Launch blog" (low)
    │    │
    │    ├─ generator.Generate() → daily rollup prompt
    │    └─ UpsertDigest(type="daily", channelID="")
    │
    └─ RunWeeklyTrends(ctx)
         │
         ├─ Get last 7 days of daily digests
         ├─ < 2 daily digests? → SKIP
         │
         ├─ Format input:
         │    for each dailyDigest:
         │      ### 2026-03-18 (N messages)
         │      Summary: ...
         │      Decisions: ...
         │
         ├─ generator.Generate() → weekly trends prompt
         └─ UpsertDigest(type="weekly", channelID="")
```

---

## Pipeline 3: People Analysis

### Algorithm: Run

```
Run(ctx)
    │
    ├─ config.Digest.Enabled == false? → return 0
    │
    ├─ Window: from = now - 7 days, to = now
    │
    ├─ Window already analyzed? (GetUserAnalysesForWindow)
    │    └─ YES && !ForceRegenerate → SKIP
    │
    ├─ PHASE: SQL Stats (5 bulk queries, NO AI)
    │    │
    │    │  ComputeAllUserStats(from, to, minMessages=3)
    │    │    ├─ Query 1: Core stats (msg_count, channels, avg_len)
    │    │    │   WHERE msg_count >= 3, non-bot, non-deleted, non-DM
    │    │    ├─ Query 2: Threads initiated (reply_count > 0)
    │    │    ├─ Query 3: Threads replied (distinct thread_ts)
    │    │    ├─ Query 4: Active hours distribution (hour 0-23 UTC)
    │    │    └─ Query 5: Previous window volume → VolumeChangePct
    │    │
    │    └─ 0 users? → return 0
    │
    ├─ Detect hasThreadData (search sync doesn't provide threads)
    │    if !hasThreadData → add instruction: "Do NOT penalize for low thread participation"
    │
    ├─ PHASE: AI Analysis (parallel workers)
    │    │
    │    │  Workers = min(10, totalUsers)
    │    │
    │    │  for each user (parallel):
    │    │    ┌───────────────────────────────────────────────────┐
    │    │    │  1. GetMessages(userID, from, to, limit=5000)     │
    │    │    │     sorted chronologically                        │
    │    │    │                                                   │
    │    │    │  2. Format user block:                            │
    │    │    │     User ID: U123                                 │
    │    │    │     Stats: 42 msgs, 5 channels, threads...       │
    │    │    │     Volume change: +12.5%                         │
    │    │    │     Active hours: {"9":8,"10":12,...}             │
    │    │    │     Messages:                                     │
    │    │    │       [2026-03-18 09:30 #general] message text   │
    │    │    │       ...                                         │
    │    │    │                                                   │
    │    │    │  3. Build prompt (analysis.user template):        │
    │    │    │     @username, date range, profile context,       │
    │    │    │     language, user messages block                 │
    │    │    │                                                   │
    │    │    │  4. generator.Generate() → Claude CLI             │
    │    │    │                                                   │
    │    │    │  5. Parse JSON:                                   │
    │    │    │     {summary, communication_style,                │
    │    │    │      decision_role, style_details,                │
    │    │    │      red_flags[], highlights[],                   │
    │    │    │      recommendations[], concerns[],              │
    │    │    │      accomplishments[]}                           │
    │    │    │                                                   │
    │    │    │  6. UpsertUserAnalysis()                          │
    │    │    │     key: (user_id, period_from, period_to)        │
    │    │    │                                                   │
    │    │    │  Error? → log, continue (non-fatal)               │
    │    │    └───────────────────────────────────────────────────┘
    │    │
    │    └─ wg.Wait()
    │
    ├─ PHASE: Period Summary (1 AI call)
    │    │
    │    │  analyses = GetUserAnalysesForWindow(from, to)
    │    │
    │    │  Format all user analyses (sanitized):
    │    │    === @alice ===
    │    │    Style: driver | Role: decision-maker | 42 msgs | +12%
    │    │    Summary: ...
    │    │    Red flags: [...]
    │    │    === @bob ===
    │    │    ...
    │    │
    │    │  generator.Generate() → analysis.period prompt
    │    │  Parse: {summary, attention[]}
    │    │  UpsertPeriodSummary()
    │    │
    │
    └─ PHASE: Social Graph (pure SQL, NO AI)
         │
         │  ComputeUserInteractions(currentUserID, from, to)
         │    ├─ Shared channels (both users posted)
         │    ├─ Messages to/from (by channel overlap)
         │    ├─ Thread replies to/from
         │    └─ Top 50 by volume
         │
         └─ UpsertUserInteractions()
```

### Output classification

| Field | Values |
|---|---|
| communication_style | driver, collaborator, executor, observer, facilitator |
| decision_role | decision-maker, approver, contributor, observer, blocker |
| red_flags | volume drop >40%, unresponsive, conflicts, missed commitments |

---

## Pipeline 4: Chains

### Algorithm: Run

```
Run(ctx)
    │
    ├─ cutoff = now - 14 days (DefaultStaleDays)
    │
    ├─ Fetch unlinked decisions from channel digests
    │    db.GetUnlinkedDecisions(cutoff)
    │    (decisions not linked to any chain via chain_refs table)
    │
    ├─ 0 unlinked? → MarkStaleChains(cutoff) → return 0
    │
    ├─ Fetch active chains
    │    db.GetActiveChains(14 days)
    │    (status='active' AND last_seen >= cutoff)
    │
    ├─ Build prompt (1 AI call for ALL decisions):
    │    │
    │    │  === ACTIVE CHAINS ===
    │    │  Chain #1: "DB Migration" (slug: db-migration)
    │    │    Summary: Migrating from PostgreSQL to RDS
    │    │    Channels: #engineering, #infra
    │    │    Items: 5
    │    │  ...
    │    │
    │    │  === UNLINKED DECISIONS ===
    │    │  [0] #engineering | high | @alice: "Choose RDS instance type"
    │    │  [1] #backend | medium | @bob: "Set up Read Replicas"
    │    │  ...
    │    │
    │    │  Return JSON array:
    │    │  {"index": 0, "action": "EXISTING", "chain_id": 5}
    │    │  {"index": 1, "action": "NEW", "title": "...", "slug": "..."}
    │    │  {"index": 2, "action": "SKIP"}
    │
    ├─ generator.Generate() → system prompt + user prompt
    │
    ├─ Parse response → []assignment
    │
    ├─ Apply assignments:
    │    │
    │    │  for each assignment:
    │    │    ├─ SKIP → continue
    │    │    │
    │    │    ├─ NEW → CreateChain(title, slug, summary, channels)
    │    │    │         InsertChainRef(chainID, decision)
    │    │    │
    │    │    └─ EXISTING → validate chain exists
    │    │                   AddChannelToChain(chainID, channelID)
    │    │                   InsertChainRef(chainID, decision)
    │    │                   UpdateChainSummary(lastSeen, itemCount)
    │
    ├─ Update chain summaries (for EXISTING chains that got new items)
    │
    └─ MarkStaleChains(cutoff)
         UPDATE chains SET status='stale' WHERE last_seen < cutoff
```

### Chain lifecycle

```
active ──(14 days no activity)──→ stale
active ──(user command)─────────→ resolved
```

### Downstream: chain context injection

```
FormatActiveChainsForPrompt() → string:
    === ACTIVE CHAINS ===
    Chain #1: "DB Migration" — Migrating from PostgreSQL to RDS
    Chain #2: "API Redesign" — Building REST+GraphQL hybrid API

Injected into:
  ├─ digestPipe.ChainContext → rollups collapse chain decisions
  └─ tracksPipe.ChainContext → tracks link to chain_id
```

---

## Pipeline 5: Tracks Extract

### Algorithm: Run

```
Run(ctx)
    │
    ├─ config.Digest.Enabled == false? → return 0
    │
    ├─ Get currentUserID from workspace
    │
    ├─ loadCaches() (channelNames, userNames, profile)
    │
    ├─ isFirstRun? (!HasTracksForUser)
    │    │
    │    ├─ YES → runInitialDayByDay()
    │    │         for each day in InitialHistDays (30):
    │    │           RunForWindow(dayStart, dayEnd)
    │    │         then process today
    │    │
    │    └─ NO → standard flow ↓
    │
    ├─ Window: DayWindow(now)
    │    from = midnight(today)    ← day-aligned!
    │    to = midnight(tomorrow)
    │    (all runs within same day = same window = no duplicates)
    │
    ├─ Get messages by channel
    │    GetMessages(from, to, excludeDMs, limit=50000)
    │    Filter: empty text, deleted
    │    Group by channelID
    │
    ├─ Delete inbox tracks for this window (clean slate)
    │    DeleteTracksForWindow(userID, from, to)
    │    (preserves active/done/snoozed — only inbox deleted)
    │
    ├─ Worker Pool (N = min(3, channels))
    │    │
    │    │  for each channel (parallel):
    │    │    ┌─────────────────────────────────────────────────────┐
    │    │    │  1. Format messages:                                │
    │    │    │     [HH:MM] @user (ts:1234.5678): text             │
    │    │    │                                                     │
    │    │    │  2. Build dedup context:                            │
    │    │    │     A. Existing tracks in this channel              │
    │    │    │        (active + inbox, not done)                   │
    │    │    │     B. Cross-channel tracks                        │
    │    │    │        (other channels' active tracks)              │
    │    │    │     C. Related digest decisions                    │
    │    │    │        (from same time window)                      │
    │    │    │                                                     │
    │    │    │  3. Build extraction prompt:                        │
    │    │    │     userName, userID, channelName, channelID,       │
    │    │    │     dateRange, langInstr, existingTracks,           │
    │    │    │     decisions, crossChannel, messages,              │
    │    │    │     profileContext, roleRules                       │
    │    │    │                                                     │
    │    │    │     + ChainContext (if set):                        │
    │    │    │       "If track relates to chain, include chain_id" │
    │    │    │                                                     │
    │    │    │  4. Role-specific rules:                            │
    │    │    │     Manager → expand to delegated tasks,            │
    │    │    │       decisions in domain, escalations              │
    │    │    │     Lead → technical decisions, code quality        │
    │    │    │     IC → strict (only explicit mentions)            │
    │    │    │                                                     │
    │    │    │  5. generator.Generate() → Claude CLI               │
    │    │    │                                                     │
    │    │    │  6. Parse JSON:                                     │
    │    │    │     {items: [{text, context, priority,              │
    │    │    │       source_message_ts, category, ownership,       │
    │    │    │       requester, blocking, tags, participants,      │
    │    │    │       source_refs, sub_items, ball_on,              │
    │    │    │       chain_id, decision_summary, ...}]}            │
    │    │    │                                                     │
    │    │    │  7. For each item:                                  │
    │    │    │     ├─ existing_id != null → UPDATE existing track  │
    │    │    │     └─ existing_id == null → INSERT new track       │
    │    │    │        status = "inbox"                             │
    │    │    │        Link to digest IDs                           │
    │    │    │        Link to chain (if chain_id provided)         │
    │    │    │                                                     │
    │    │    │  8. Divide token cost by item count                 │
    │    │    │                                                     │
    │    │    │  Error? → log, continue (non-fatal)                 │
    │    │    └─────────────────────────────────────────────────────┘
    │    │
    │    └─ wg.Wait()
    │
    └─ Return (total tracks stored, usage)
```

### Track categories

```
code_review | decision_needed | info_request | task |
approval    | follow_up       | bug_fix      | discussion
```

### Track ownership

```
mine      — task directed at current user
delegated — task for user's report
watching  — domain discussion, user not primary actor
```

### Track status lifecycle

```
inbox ──(user accepts)──→ active ──(completion detected)──→ done
  │                          │
  │                          ├──(user snoozes)──→ snoozed ──(expires)──→ inbox
  │                          │
  └──(user dismisses)──→ dismissed
```

---

## Pipeline 6: Tracks Update

### Algorithm: CheckForUpdates

```
CheckForUpdates(ctx)
    │
    ├─ Get tracks for update check
    │    GetTracksForUpdateCheck()
    │    (inbox/active tracks WITH source_message_ts)
    │    (excludes done, dismissed, snoozed)
    │
    ├─ Group by channel
    │
    ├─ Worker Pool (N = min(2, channels))
    │    │
    │    │  for each channel (parallel):
    │    │    ┌───────────────────────────────────────────────────┐
    │    │    │  1. For each track in channel:                    │
    │    │    │     afterTS = lastCheckedTS || sourceMessageTS    │
    │    │    │     Fetch thread replies after afterTS            │
    │    │    │     Fetch channel messages after afterTS (max 200)│
    │    │    │                                                   │
    │    │    │  2. No new messages? → SKIP (return 0)            │
    │    │    │                                                   │
    │    │    │  3. Build batch update prompt:                    │
    │    │    │     channelName, langInstr,                       │
    │    │    │     tracksList (all tracks in channel),           │
    │    │    │     profileContext, newMessages                   │
    │    │    │                                                   │
    │    │    │  4. generator.Generate() → Claude CLI             │
    │    │    │                                                   │
    │    │    │  5. Parse JSON:                                   │
    │    │    │     {results: [{track_id, has_update,             │
    │    │    │       updated_context, status_hint, ball_on}]}    │
    │    │    │                                                   │
    │    │    │  6. Apply updates:                                │
    │    │    │     Update lastCheckedTS for ALL tracks           │
    │    │    │     if has_update:                                │
    │    │    │       SetTrackHasUpdates(true)                    │
    │    │    │       UpdateTrackContext(new context)             │
    │    │    │       if ball_on changed → UpdateBallOn()         │
    │    │    │       if status_hint == "done" → set status done  │
    │    │    └───────────────────────────────────────────────────┘
    │    │
    │    └─ wg.Wait()
    │
    └─ Return (updated count)
```

---

## Pipeline 7: Interactive Chat (Desktop / CLI Ask)

### Algorithm

```
User sends message
    │
    ├─ Build context budget (DefaultAIContextBudget = 150K tokens)
    │    │
    │    │  Tier 1 (always): Workspace summary, watched channels  ~1K
    │    │  Tier 2 (40%):    Messages from starred entities       ~60K
    │    │  Tier 3 (50%):    FTS search results by user query     ~75K
    │    │  Tier 4 (10%):    Broad activity overview              ~15K
    │    │
    │    └─ Inject digest summaries as background knowledge
    │
    ├─ Model: sonnet (default) — 5× more expensive than haiku!
    │
    ├─ Multi-turn: sessionID reused, system prompt sent once
    │
    └─ ClaudeService.stream() → Process() with onReceiveOutput callback
```

---

## Pipeline 8: Onboarding (one-time)

Detailed algorithm: [docs/onboarding-flow.md](onboarding-flow.md)

**5 AI calls total:**

```
1. Health check:     claude -p "respond with: OK" → ~10 tokens
2. AI interview:     4-6 turns × ~2K tokens = ~12K (streaming, multi-turn)
3. Profile extract:  transcript → {role, team, pain_points} JSON = ~3K
4. Context generate: transcript + metadata → 5-10 sentences = ~3K
                                                      TOTAL: ~20K tokens
```

---

## Prompt Templates (8 total)

Stored in `internal/prompts/defaults.go`, managed via DB (`prompts` table).

| ID | Pipeline | Input | Output JSON |
|---|---|---|---|
| `digest.channel` | Channel Digests | formatted messages | `{summary, topics[], decisions[], action_items[], key_messages[]}` |
| `digest.daily` | Daily Rollup | channel digest summaries | same |
| `digest.weekly` | Weekly Trends | daily digest summaries | same |
| `digest.period` | Period Summary (CLI) | all digests | same |
| `tracks.extract` | Tracks Extract | messages + existing tracks + chains | `{items: [{text, context, priority, category, ...}]}` |
| `tracks.update` | Tracks Update | tracks + new messages | `{results: [{track_id, has_update, status_hint, ...}]}` |
| `analysis.user` | People Analysis | user stats + messages | `{summary, communication_style, decision_role, ...}` |
| `analysis.period` | Period Summary | all user analyses | `{summary, attention[]}` |

### Prompt loading: DB-first with fallback

```
getPrompt(id, fallback):
    if promptStore != nil:
        tmpl, version = promptStore.GetForRole(id, profile.Role)
        if roleInstruction exists:
            tmpl = roleInstruction + "\n\n" + tmpl
        return tmpl, version
    else:
        return fallback (const from defaults.go), version=0
```

### Role-based prompt prefix

```
RoleInstructions[role] prepended to every prompt when role is set.
Example for "Director": extra instructions about strategic focus, delegation patterns.
```

---

## Prompt Injection Protection

All pipelines sanitize data before inserting into prompts:

```
sanitizePromptValue(text):
    remove \n, \r
    replace "===" → "= = ="     (section markers)
    replace "---" → "- - -"     (dividers)
    replace "```" → "` ` `"     (code fences)
```

---

## Error Handling

**General principle: non-fatal per-item, log and continue.**

| Level | Behavior |
|---|---|
| Single channel/user failed | Log error, continue with the rest |
| All items failed | Return error to caller |
| Claude CLI not found | Return "claude CLI not found" |
| Empty AI response | Return "claude returned empty result" |
| JSON parse error | Log with first 200 chars of raw, continue |
| Context cancelled | Workers exit gracefully |
| Sync failed | Pipelines still run (process existing data) |

---

## File Index

| File | Contents |
|---|---|
| `internal/daemon/daemon.go` | Daemon loop, pipeline orchestration, throttling |
| `internal/digest/pipeline.go` | Digest pipeline (channel, daily, weekly) |
| `internal/digest/generator.go` | ClaudeGenerator: CLI subprocess invocation |
| `internal/digest/pooled.go` | PooledGenerator: session pool wrapper |
| `internal/analysis/pipeline.go` | People analytics pipeline |
| `internal/tracks/pipeline.go` | Tracks extract + update pipeline |
| `internal/chains/pipeline.go` | Chain linking pipeline |
| `internal/prompts/defaults.go` | 8 prompt templates |
| `internal/prompts/store.go` | DB-backed prompt management, GetForRole() |
| `internal/sessions/pool.go` | Session pool (concurrency limiter) |
| `internal/sessions/log.go` | Session event logging |
| `internal/repl/repl.go` | Interactive chat (CLI ask) |
| `docs/llm-usage.md` | Token cost breakdown and optimization |
| `docs/onboarding-flow.md` | Onboarding flow (detailed) |
