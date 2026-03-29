# Daemon Pipeline — Data Collection and Analysis Cycle

The Watchtower daemon is a background process that runs a full cycle every 15 minutes: fetch new data from Slack, analyze it with AI, enrich context for the user. The same flow works both on the first run (empty DB) and on every subsequent one — the only difference is how much data has already been accumulated.

---

## Full Stage Map

A single daemon cycle consists of **12 stages**:

```
│
├─ Stage 1.  Sync: Workspace Info
├─ Stage 2.  Sync: Custom Emojis
├─ Stage 3.  Sync: Messages (search or full)
├─ Stage 4.  Sync: Channel Read State
├─ Stage 5.  Sync: User Profiles
├─ Stage 6.  Save Sync Result
│
├─ Stage 7.  Inbox (parallel, does not block others) ────────────┐
│                                                                  │
├─ Stage 8.  Channel Digests (MAP)                                 │
├─ Stage 9.  Unsnooze (tasks + inbox)                             │
│                                                                  │
├─ Parallel Group A:                                               │
│   ├─ Stage 10a. Tracks                                          │
│   ├─ Stage 10b. Track Context Injection into Rollups            │
│   └─ Stage 10c. Rollups (Daily/Weekly)                          │
│                                                                  │
├─ Parallel Group B (concurrent with A):                           │
│   └─ Stage 11.  People Cards (once per 24h)                     │
│                                                               ◄──┘
├─ (wait for Groups A + B to complete)
├─ Auto-Mark Read (digests + rollups + tracks — single pass)
│
└─ Stage 12. Daily Briefing (once per day)
```

### Cycle Triggers

- **On daemon start** — immediately
- **On timer** — every 15 minutes (`sync.poll_interval`)
- **On computer wake** — if `sync.sync_on_wake` is enabled (default: yes). The timer resets so the next tick is a full interval away

### Error Resilience

If sync (stages 1-6) completed with a partial error (rate limit, inaccessible channel), all analysis stages **still run** — the database already has data to process. The cycle is interrupted only on shutdown (context cancellation).

### Adaptive Behavior (Empty DB vs. Accumulated Data)

There is no separate "first run" flow in the code. Each stage checks what already exists in the database and adapts:
- No workspace → API call. Exists → cache.
- No watermark → wide window. Exists → incremental.
- No running summary → prompt works without context. Exists → injected.
- No existing tracks → everything is new. Exists → merge and dedup.

---

## Stage 1. Sync: Workspace Info

**What it does:** identifies the Slack workspace and current user.

Checks for workspace in DB:
- **No record** → two API calls: `team.info` (ID, name, domain) + `auth.test` (current_user_id — token owner)
- **Record exists** → uses cache. Only exception: if `current_user_id` is empty (previous `auth.test` failed), retries

**Why it matters:** without `current_user_id` it's impossible to determine which messages are addressed to the user. Inbox, Tracks, Briefing — all depend on this ID.

---

## Stage 2. Sync: Custom Emojis

**What it does:** loads workspace custom emojis (`emoji.list` — one API call).

**Non-fatal:** failure → logged, sync continues. Needed for correct message rendering and text formatting for AI.

---

## Stage 3. Sync: Messages

The core of data collection — everything else operates on these messages.

### Incremental Mode (default)

Uses the Slack API method `search.messages` with paginated loading (up to 100 results per page). Results are sorted **from oldest to newest** (asc) — this ensures progressive catchup with large data volumes.

1. **Start date determination.** The system stores a `search_last_date` watermark:
   - Watermark exists → goes back **2 days** (compensates for Slack indexing delay)
   - Watermark empty → goes back `initial_history_days` (default 2 days)

2. **Search.** Query `search.messages after:YYYY-MM-DD`. Each page is a separate API call. From each result:
   - Channel — if new, a record is created (type auto-detected: public/private/DM/group DM)
   - User — if new, a record is created
   - Message — text, author, timestamp, channel, permalink
   - Thread TS — extracted from permalink for thread association

3. **Saving.** Batch upsert in transactions (one transaction per page)

4. **Watermark.** If all pages processed → watermark = today. If page limit hit (200) → watermark = date of the **newest** retrieved message. Since results go from oldest to newest, all messages up to this date are already loaded — the next cycle will continue from this point.

**Progressive catchup:** with large volumes (first run, long downtime), the system doesn't try to load everything in one cycle. It loads a batch of the oldest messages, advances the watermark, and picks up the next batch on the next cycle. Over several cycles (every 15 min) it catches up to the present.

**Volume:** during regular operation — typically 1-5 search pages + emoji.list + auth.test + users.info for new users. Total ~8-10 API calls. During catchup — up to 200 pages per cycle.

**Fallback:** if search found 0 channels (no `search:read` scope) and DB is empty → automatic switch to full sync.

### Full Mode (explicit request)

Triggered via `watchtower sync --full` or `--channels`:

1. **Metadata** — `conversations.list` + `users.list` → full list of channels and users
2. **Messages** — `conversations.history` for each channel (200 messages/page, worker pool). If `sync_threads` is enabled, thread replies are loaded inline for each message with reply_count > 0
3. **User profiles** — `users.info` for each unknown user

**Volume:** ~50+ API calls. For workspaces with hundreds of channels, may take hours.

### Slack Error Handling

| Error | Fatal? | Behavior |
|-------|--------|----------|
| `channel_not_found` | No | Skipped |
| `is_archived` | No | Skipped |
| `not_in_channel` | No | Skipped |
| `missing_scope` | No | Logged |
| `access_denied` | No | Logged |
| Rate limit | No | Next sync continues from cursor |
| Network error | Yes | Sync aborted |

---

## Stage 4. Sync: Channel Read State

**What it does:** loads read cursors from Slack — how far the user has read each channel.

**Optimization:** `conversations.info` is only requested for channels with **unread digests**. No unread digests → stage is skipped.

**Why:** cursors are used in Stage 9 (Auto-Mark Read) to automatically mark digests as read.

---

## Stage 5. Sync: User Profiles

**What it does:** fetches full profiles (`users.info`) for users discovered through search who don't yet have a full profile.

Search returns only `user_id` + `username`, but AI analysis needs: display_name, real_name, email, is_bot. Loading is lazy — only for unknown user_ids.

---

## Stage 6. Save Sync Result

- Logs API call statistics (by tier, retry count)
- Auto-marks digests as read based on Slack read cursors
- Updates `synced_at` in workspace (desktop shows "last sync")
- Saves `last_sync.json` to disk (available via `watchtower status`)

---

## Stage 7. Inbox — Detecting Messages Awaiting Response

**Parallel.** Runs in a separate goroutine, does not block any other stage.

### Step 1. Detection

Searches new messages (from `inbox_last_processed_ts`; watermark empty → 7 days back):

- **@-mentions** of the user
- **Incoming DMs**
- **Replies in threads** where the user participated (posted any message)
- **Reaction requests** (👀, 🔍)

Text is cleaned of Slack markup: `<@U123>` → `@Ivan`, `<#C456|backend>` → `#backend`. For each item, context is loaded — up to 10 messages from the thread or 5 preceding channel messages.

### Step 2. Auto-resolve

For each pending item: checks whether the user has responded. If yes → candidate for closure.

### Step 3. AI Prioritization

Batches of 50 items → Claude. AI receives: sender name and role, channel, message age, thread reply count, conversation context. Returns: priority (high/medium/low), reason, closure decision.

---

## Stage 8. Channel Digests (MAP Phase)

Transforms each channel's message stream into a structured digest. This is the **MAP** in the MAP-REDUCE architecture: each channel independently, results then used in Tracks, Rollups, People Cards.

### Channel Selection and Grouping

1. Channels with new messages since last digest (watermark empty → from `initial_history_days` back)
2. Muted channels are excluded
3. **1:1 DMs** are excluded — private conversations aren't needed in digests. Group DMs are kept (often contain team discussions)
4. **Bot-heavy channels** (≥90% messages from bots): if there are no human messages — skipped. If a human responded — only their messages + context are extracted:
   - Entire thread if reply is in a thread (parent + all replies)
   - 3 neighboring messages before and after (context window)
   - Remaining bot alerts are discarded
5. Only "visible" messages (with text, not deleted)
4. **Tiered batching** — channels are grouped by activity level:

| Tier | Visible msgs | Channels per batch | Logic |
|------|-------------|-------------------|-------|
| High | >50 | 1 (individual prompt) | High-quality individual analysis |
| Medium | 10-50 | up to `batch_max_channels` (10) | Standard grouping |
| Low | <10 | up to `batch_max_channels × 2` (20) | Aggressive grouping, saves AI calls |

5. Within each tier: max `batch_max_messages` (800) messages per batch
6. Parallel processing: up to 5 workers

### What AI Generates

- **Summary** — brief description of activity
- **Topics** — each with:
  - Title and description
  - **Decisions** (who made it, importance, message reference)
  - **Action items** (assignee, status)
  - **Situations** — interpersonal dynamics: participants with roles (driver/reviewer/blocker), type (bottleneck/conflict/collaboration), outcome, red flags. **This is input data for People Cards (MAP phase)**
  - Key messages
- **Running summary** — compressed context for the next cycle

### Channel Memory (Running Context)

Compressed context (~2000 characters) is passed between cycles:
- Active topics (status, participants, since when)
- Recent decisions
- Channel dynamics
- Open questions

AI receives previous context → analyzes → generates updated context. This prevents topic duplication, tracks their development, notices closures.

| Condition | Behavior |
|-----------|----------|
| < 7 days | Used as-is |
| 7-30 days | Passed with "outdated" label |
| > 30 days | Not passed |
| None (first cycle or new channel) | Prompt works without it |
| Corrupted | Ignored with warning |
| **< 10 visible messages** | **Not passed** — context (~600 tokens) is heavier than the messages themselves |

Manual reset: `watchtower digest reset-context [channel]`.

### Protection Against Parallel Runs

File lock (`digest.lock`) prevents simultaneous CLI + daemon execution. Stale duplicates are cleaned up on start.

---

## Stage 9. Auto-Mark Read + Unsnooze

### Auto-Mark Read

Compares Slack read cursors (Stage 4) with digests: if the user has already read messages in Slack — the digest is marked as read in Watchtower.

Runs **once** after all AI stages complete (Groups A + B), when channel digests, rollups, and tracks are already generated.

### Unsnooze

- Tasks with expired `snooze_until` → from `snoozed` to `todo`
- Inbox items with expired `snooze_until` → from `snoozed` to `pending`

---

## Stage 10a. Tracks — Narrative Tracks

**Group A** (parallel with People Cards).

Combines scattered topics from digests into coherent narratives that track situation development across channels and over time.

### Time Window (Adaptive)

- No tracks in DB → `initial_history_days` (default 2 days)
- Tracks exist → incrementally from `period_to` of the last run (typically ~15 minutes of new data). Cap: if daemon was off >24h, window is trimmed to the last 24h to avoid reprocessing a week in one call

### Context for AI

Tracks operate on top of **channel digests (Stage 8)**, not raw messages — zero analysis duplication:

- **Channel digests** for the window: summary + topics (with decisions, action_items, situations, key_messages)
- **Existing tracks** — for updating, not duplicating
- **Tracks from other channels** — for cross-channel merging
- **Running summary** of the channel
- **User profile**

Channels are grouped into batches considering context budget (~200 tokens per topic, ~20,000 for overhead). Typical savings: 30-50% tokens compared to raw messages.

### What AI Generates

- **New tracks** — title, narrative (2-4 sentences), current status (1 sentence), participants (driver/reviewer/blocker), priority, tags
- **Updates to existing tracks** — new narrative, status, additional references

**Principle:** AI prefers **merging** with an existing track over creating a new one.

### Track Model

- Status `active` (main working status), updated via `status_hint` from AI
- Ownership: `mine` (on the user), `delegated` (on a report), `watching` (monitoring)
- Priority: `high` / `medium` / `low`, category (`code_review`, `task`, `decision_needed`, etc.)
- `ball_on` — user_id of whoever acts next
- `has_updates` + `read_at` — reading a track cascades to mark linked digests as read

---

## Stage 10b. Track Context Injection

Immediately after Tracks. Active track narratives are formatted into a text block and passed to the digest pipeline.

**Why:** without this, the rollup would retell topics already tracked in tracks. With track context — it references them: "Continues [Track #7: API Migration]".

---

## Stage 10c. Rollups (Daily/Weekly)

### Daily Rollup

Aggregates all channel digests for the day into a cross-channel summary. Uses running context (memory) and track context (Stage 10b). Does not duplicate tracked topics.

### Weekly Rollup

Aggregates daily rollups for the week. Higher level of abstraction — trends and strategic observations.

---

## Stage 11. People Cards (REDUCE Phase)

**Group B** (parallel with Tracks+Rollups).

**Throttling:** no more than once per 24 hours. Timestamp on disk (`last_people.txt`).

Synthesizes observations about people from all channels into personal cards. This is the **REDUCE**: situations from channel digests (MAP, Stage 8) are grouped by user_id → AI generates a card.

### Skip Conditions

- < 24h since last run
- Cards for the given 7-day window already exist
- No situations from digests (digests haven't run yet or didn't produce situations)

**First cycle note:** People Cards are almost always skipped. Reason: throttling (24h) and lack of situations — the first cycle hasn't accumulated enough data yet. Cards will appear on the next people pipeline run (after 24h).

### Data Collection (7-Day Window)

1. User statistics (messages, channels, threads, average length)
2. Situations from `digest_participants`
3. Team norms (averages across all users for comparison)

### Classification and Processing

| Criterion | Processing |
|-----------|-----------|
| ≥ 3 situations **or** ≥ 10 messages in situations | Individual AI call (full card) |
| Below thresholds | Batch of 10 people per AI call, or `insufficient_data` |

For full card, AI receives: situations, statistics, team norms, relationship context (manager/report/peer), previous card (continuity), last 50 raw messages (grounding).

### Card Contents

**Analysis:** summary, communication style, decision role, red flags, highlights, accomplishments

**Guidance:** communication guide, decision style, tactics, relationship context

### Team Summary

Separate AI call across all cards → overall team health overview, areas of attention, recommendations.

---

## Stage 12. Daily Briefing

**Last stage.** Waits for both parallel groups to complete.

**Throttling:** once per day after `briefing.hour` (default 8:00). Conditions:
1. ≥ 24h since last briefing
2. Current hour ≥ configured hour
3. There is at least some data (digests, tracks, tasks, or inbox)
4. Briefing for this date doesn't yet exist (deduplication by user + date)

### Data Collection from All Previous Stages

| Source | What it takes | From |
|--------|--------------|------|
| Tasks | Active (todo/in_progress/blocked), OVERDUE flag | Stage 9 + user input |
| Inbox | Pending items with priority and reason | Stage 7 |
| Tracks | Active with narrative and participants | Stage 10a |
| Channel Digests | From yesterday, with topics and decisions | Stage 8 |
| Daily Rollup | Latest cross-channel overview | Stage 10c |
| People Cards | Cards (style, red flags, highlights) | Stage 11 |
| Team Summary | Team health | Stage 11 |
| User Profile | Role, team, responsibilities | Onboarding |

Muted channels are excluded. `insufficient_data` cards are skipped.

### Role-Based Personalization

- **IC** — focus on technical tasks and blockers
- **Manager** — focus on team, processes, people
- **Direction Owner** — strategic decisions and cross-team coordination

### 5 Sections

1. **Attention** — what requires immediate attention. References the source (track, digest, inbox, task). May suggest creating a task.
2. **Your Day** — active tasks and tracks, by priority and deadline. Overdue items are highlighted.
3. **What Happened** — events from digests: decisions, discussions, new topics.
4. **Team Pulse** — signals about people: activity drops/spikes, red flags, conflicts, achievements.
5. **Coaching** — communication and process recommendations for specific people.

---

## How Context Accumulates Between Cycles

```
Cycle 1:  messages → digests (no context) → tracks → rollups → briefing
                      │
                      └─ running_summary₁ saved
                      └─ situations₁ saved

Cycle 2:  messages → digests (with running_summary₁) → tracks (merge with existing) → rollups (track-aware)
                      │                                                                  │
                      ├─ running_summary₂                                                └─ people cards (from situations₁ + ₂)
                      └─ situations₂

Cycle N:  messages → digests (with running_summaryₙ₋₁) → tracks (merge, dedup) → rollups
                      │
                      └─ context grows richer, AI becomes more accurate
```

With each cycle:
- Running summary accumulates channel history
- Tracks are updated and enriched, not created from scratch
- People Cards receive more situations for analysis
- Briefing sees a more complete picture

---

## Throttling and Resume Points

| Stage | Frequency | Resume Marker | What it stores |
|-------|-----------|--------------|----------------|
| Sync | Every 15 min | DB: `search_last_date` | Date of last message |
| Inbox | Every 15 min | DB: `inbox_last_processed_ts` | Unix timestamp of processing |
| Digests | Every 15 min | DB: UNIQUE(channel, type, period) | Window + file lock |
| Tracks | Every 15 min | DB: `pipeline_runs.period_to` | End of last window |
| People Cards | Once per 24h | File: `last_people.txt` | Unix timestamp |
| Briefing | Once per day | File: `last_briefing.txt` + DB: UNIQUE(user, date) | Unix timestamp |

Files `last_people.txt` and `last_briefing.txt` survive daemon restarts.

---

## Error Handling

| Situation | What happens |
|-----------|-------------|
| Slack rate limit | Cursor saved, next cycle continues |
| Channel not found / no access | Logged, skipped |
| Slack authorization error | Sync stops |
| AI error on one channel | Channel skipped, others processed |
| AI error on one user (People) | `insufficient_data` card created |
| AI error in Tracks | Skipped, rollups work without track context |
| AI error in Briefing | Briefing not created today |
| Inbox error | Logged, does not block anything |
| Sync with partial errors | All AI stages still run |
| Shutdown (ctrl+C) | Each stage checks `ctx.Done` at every step |

---

## Token Usage & Cost Tracking

Each AI stage (Inbox, Digests, Tracks, People, Briefing) registers a pipeline run:
- Pipeline name and source (daemon/CLI)
- Model
- Number of processed items
- Input/output tokens and cost in USD
- Time window (period_from, period_to)
- Execution time
- Error (if any)

Data is available in the desktop app (Usage tab).

---

## Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| **Sync** | | |
| `sync.poll_interval` | 15 min | Interval between cycles |
| `sync.initial_history_days` | 2 | Lookback depth when no watermark |
| `sync.sync_on_wake` | true | Sync on wake |
| `sync.sync_threads` | true | Load thread replies inline during full sync |
| **AI** | | |
| `ai.model` | claude-sonnet-4-6 | Default model |
| `ai.context_budget` | 150,000 | Token budget per request |
| `ai.workers` | 5 | Parallel AI calls |
| **Digests** | | |
| `digest.enabled` | true | Generate digests |
| `digest.model` | claude-haiku-4-5 | Model (cheap) |
| `digest.min_messages` | 5 | Minimum visible messages |
| `digest.language` | Russian | Output language |
| `digest.batch_max_channels` | 10 | Max channels per batch (medium tier; low tier = ×2) |
| `digest.batch_max_messages` | 800 | Max messages per batch |
| **Tracks** | | |
| `tracks.min_messages` | 3 | (deprecated — tracks now operate on digests) |
| **Briefing** | | |
| `briefing.enabled` | true | Generate briefings |
| `briefing.hour` | 8 | Generation hour (0-23) |
| **Inbox** | | |
| `inbox.enabled` | true | Mention detection |
| `inbox.max_items_per_run` | 100 | Max items per run |
| `inbox.initial_lookback_days` | 7 | Lookback depth when no watermark |

---

## Data Paths

| Resource | Path |
|----------|------|
| Configuration | `~/.local/share/watchtower/{workspace}/config.yaml` |
| Database | `~/.local/share/watchtower/{workspace}/watchtower.db` |
| Daemon log | `~/.local/share/watchtower/{workspace}/daemon.log` |
| Sync result | `~/.local/share/watchtower/{workspace}/last_sync.json` |
| People timestamp | `~/.local/share/watchtower/{workspace}/last_people.txt` |
| Briefing timestamp | `~/.local/share/watchtower/{workspace}/last_briefing.txt` |
| Daemon PID | `~/.local/share/watchtower/{workspace}/daemon.pid` |
| Digest lock | `~/.local/share/watchtower/{workspace}/digest.lock` |
