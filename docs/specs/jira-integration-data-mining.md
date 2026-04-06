# Jira Integration: Data Mining & Board Discovery

## Problem

Jira is an extremely flexible system. Each team has:
- **Its own statuses** — "To Do / In Progress / Done" or "Backlog / Analysis / Dev / Code Review / QA / Staging / Released" or even "New / Открыта / В работе / Тестирование / Готово"
- **Its own boards** — a single user can have 5 boards, Scrum and Kanban mixed together
- **Its own workflows** — transitions between statuses are unique per project
- **Its own custom fields** — Story Points can be called "Story Points", "SP", "Estimate", or be a custom field `customfield_10016`
- **Its own issue types** — Story, Task, Bug, Sub-task, or "Improvement", "Technical Debt", "Spike"

Watchtower cannot hardcode any of these elements. A **discover → normalize → use** approach is needed.

---

## Architectural Principle: Status Categories — the Universal Layer

### The Status Problem

```
Team A:             Team B:             Team C:
To Do               Backlog             Новая
In Progress         Analysis            В работе  
In Review           Development         На ревью
Done                Code Review         Тестирование
                    QA                  Готово
                    Staging             Релиз
                    Released
```

Watchtower cannot know that "Code Review" in Team B = "In Review" in Team A = "На ревью" in Team C.

### Solution: Jira Status Categories

Jira internally maps **every** custom status to one of **3 categories**:

| Status Category | Jira key | Meaning | Color in Jira |
|----------------|----------|---------|---------------|
| **To Do** | `new` | Work not started | Blue-gray |
| **In Progress** | `indeterminate` | In progress (any intermediate step) | Blue |
| **Done** | `done` | Completed | Green |

This is **already configured** in every Jira instance by the administrator. The API returns this for free:

```
GET /rest/api/3/status/{statusId}
→ { "statusCategory": { "key": "indeterminate", "name": "In Progress" } }

GET /rest/api/3/issue/PROJ-123
→ { "fields": { "status": { "name": "Code Review", "statusCategory": { "key": "indeterminate" } } } }
```

### Watchtower Strategy

**Store both layers:**
- `status` = original status ("Code Review", "На ревью", "QA") — for display to the user
- `status_category` = normalized category (`todo` / `in_progress` / `done`) — for logic

**Watchtower logic works ONLY on status_category:**
- "Is the issue completed?" → `status_category == 'done'`
- "Is the issue in progress?" → `status_category == 'in_progress'`  
- "Is the issue stuck?" → `status_category == 'in_progress' AND updated_at < N days ago`
- "Velocity" → count issues moved to `status_category == 'done'` per period

**Original status — for UI and AI prompts:**
- Briefing: "PROJ-142 in status 'Code Review' for 5 days" (not "In Progress for 5 days")
- AI Chat: shows the real status for accuracy

---

## Board Discovery & Selection

### Principle: user chooses, LLM analyzes

Watchtower **does not** try to automatically classify boards and **does not bind to methodologies** (Scrum, Kanban, SAFe, Scrumban, etc.). There are too many methodologies, and each team uses its own variation. Instead:

### Flow

```
Phase 1: Enumerate (show the user all boards)
  GET /rest/agile/1.0/board
  → All boards available to the user
  → For each: id, name, type (scrum/kanban), projectKey

Phase 2: Classify  
  For each board:
    GET /rest/agile/1.0/board/{id}/configuration
    → Columns with status mappings
    → Filter (JQL) — what appears on the board
    → Estimation field (story points, hours, etc.)
    → Sub-query: ranking, swimlanes
    
Phase 3: Profile
  For each board:
    Scrum? → GET /rest/agile/1.0/board/{id}/sprint?state=active
           → Current sprint, goals, dates
    Kanban? → GET /rest/agile/1.0/board/{id}/issue?maxResults=1
           → Are there any issues? Is the board active?

Phase 4: Filter
  Keep only boards where:
  - The user has assigned issues, OR
  - The user is a reporter/watcher on issues, OR
  - The board is linked to a project from selected_projects
```

### Phase 2: LLM Board Analysis

For each selected board, Watchtower fetches the raw configuration and passes it to the LLM:

```
API calls per board:
  GET /rest/agile/1.0/board/{id}/configuration
    -> columns (name, statuses with categoryId), estimation, filter
  GET /rest/agile/1.0/board/{id}/sprint?state=active,closed&maxResults=3
    -> Sprints (if any): current + 2 most recent closed
  GET /rest/agile/1.0/board/{id}/issue?maxResults=50&fields=status,assignee,priority
    -> Sample issues: actual distribution by status/assignees
```

**LLM Prompt (once when connecting a board):**

```
Analyze the Jira board configuration and generate a profile.
Do not use methodology names (Scrum, Kanban, SAFe, etc.) —
describe the process as it is.

=== BOARD CONFIGURATION ===
Name: "{board_name}"
Project: {project_key}
Columns (left to right): {columns with statuses and categories}
Estimation: {estimation field info or "not configured"}
Filter: {board JQL filter}
Sprints (if any): {active + recent closed sprints}
Sample issues (50): {distribution by status, assignee}

=== GENERATE JSON ===
{
  "workflow_stages": [
    {
      "name": "column name",
      "original_statuses": ["status1", "status2"],
      "phase": "backlog|active_work|review|testing|done|other",
      "is_terminal": false,
      "typical_duration_signal": "hours|days|week+"
    }
  ],
  "estimation_approach": {
    "type": "story_points|hours|count_only|none",
    "field": "customfield_XXXXX or null"
  },
  "iteration_info": {
    "has_iterations": true/false,
    "typical_length_days": N,
    "avg_throughput": N
  },
  "workflow_summary": "2-3 sentences describing team process",
  "stale_thresholds": { "column_name": days },
  "health_signals": ["potential bottleneck/risk observations"]
}
```

**LLM Output (cached as board profile):**

```json
{
  "workflow_stages": [
    { "name": "Backlog", "phase": "backlog", "is_terminal": false, "typical_duration_signal": "week+" },
    { "name": "In Development", "phase": "active_work", "is_terminal": false, "typical_duration_signal": "days" },
    { "name": "Code Review", "phase": "review", "is_terminal": false, "typical_duration_signal": "hours" },
    { "name": "QA", "phase": "testing", "is_terminal": false, "typical_duration_signal": "days" },
    { "name": "Done", "phase": "done", "is_terminal": true }
  ],
  "estimation_approach": { "type": "story_points", "field": "customfield_10016" },
  "iteration_info": { "has_iterations": true, "typical_length_days": 14, "avg_throughput": 40 },
  "workflow_summary": "The team works in two-week iterations. Issues go through development, code review, and QA before completion. Estimation uses story points, with an average throughput of ~40 SP per iteration.",
  "stale_thresholds": { "In Development": 5, "Code Review": 2, "QA": 3 },
  "health_signals": [
    "Code Review may be a bottleneck (80% ratio to development)",
    "Status 'Reopened' maps to Backlog — track as regression signal",
    "WIP limits not configured — monitor for accumulation"
  ]
}
```

### Why LLM Instead of Hardcoding

| Aspect | Hardcoded | LLM |
|--------|-----------|-----|
| 3 columns "Todo / Doing / Done" | Works | Works |
| 8 columns with custom stages | Requires mapping for each | Works out of the box |
| Board in Russian/German/Japanese | Requires a language parser | Understands |
| SAFe / Scrumban / custom process | Not covered | Describes as-is |
| Stages like "Legal Review", "Security Audit" | Unknown | Understands the purpose |
| Stale thresholds per column | Same for all | Adaptive: review is faster than dev |
| Bottleneck detection | Formula-based | Context-aware based on workflow |

### When to Run LLM Analysis

- **On first board connection** (required)
- **On manual refresh** (`watchtower jira boards --refresh`)
- **Automatically** if board config changed (daily check)
- **NOT on every sync** — the profile is stable

### User Validation

```
Board "Backend Sprint Board" analyzed:

  Workflow: Backlog -> Development -> Code Review -> QA -> Done
  Estimation: Story Points
  Iterations: 2-week cycles, ~40 SP throughput
  
  Stale thresholds:
    Development: 5 days
    Code Review: 2 days  
    QA: 3 days
  
  Looks correct? (y/n/edit)
```

If the user makes corrections — the edits are saved as overrides and the LLM will not overwrite them on refresh.

---

## Methodology-Agnostic Metrics

### Principle: do not bind to any methodology

Watchtower **does not know** what a "sprint" means in the methodological sense. Watchtower knows:
- **Iterations** — time windows with a start/end (if they exist)
- **Workflow stages** — what phases an issue goes through
- **Throughput** — how many issues are completed per period
- **Time in stage** — how many days an issue stays at each phase

### What We Calculate (universally, without binding to methodology)

| Metric | Formula | Purpose |
|--------|---------|---------|
| **Issue stuck** | `status_category = in_progress AND days > stale_threshold[stage]` | Briefing |
| **Overdue** | `due_date < now AND status_category != done` | Deadline heatmap |
| **Workload per person** | `count(assigned, status_category != done)` + `sum(SP)` | Workload Dashboard |
| **Blocked** | Has issue link "blocks"/"is blocked by" OR flagged | Blocker Map |
| **Done this period** | `status_category changed to done, date in range` | Throughput |
| **Reopened** | `status_category was done -> changed to non-done` | Quality signal |
| **Cycle time** | `done_at - first_in_progress_at` | People Cards |

### Iteration-Aware Metrics (if the board has iterations)

If `board_profile.iteration_info.has_iterations = true`:

| Metric | Formula | Purpose |
|--------|---------|---------|
| **Iteration progress** | `count(done) / count(total)` in current iteration | Briefing |
| **SP progress** | `sum(SP done) / sum(SP total)` | Forecast |
| **Forecast** | `remaining / avg_throughput_per_day * days_left` | PM: "are we on track?" |
| **Scope change** | `issues added after iteration start` | Scope creep signal |

If there are no iterations — these metrics are not generated. No error.

### What AI Calculates (not hardcoded)

Instead of formulas for every edge case, we give the AI **raw data + board profile** and ask it to interpret:

```
Prompt for Briefing:
  "Here is data for board '{board_name}':
   Workflow: {board_profile.workflow_summary}
   Issues in progress: 12 (5 dev, 4 review, 3 QA)
   Stale: PROJ-142 (Code Review, 5 days, threshold 2 days)
   Iteration: 3 days left, 60% done, avg suggests 75% at end
   Workload: Пётр 28SP, Аня 15SP, Дима 5SP
   
   Generate an Attention section."
```

The AI decides what is important based on the context of the specific board.

---

## Board Selection: User Controls

### CLI

```bash
watchtower jira boards
  #   Name                        Project   Issues   Status
  1   Backend Sprint Board        BACK      23       [not synced]
  2   Frontend Kanban             FRONT     15       [not synced]
  3   Platform Team               PLAT      8        [not synced]
  4   Cross-team Dependencies     DEPS      3        [not synced]
  5   Mobile App                  MOB       31       [not synced]
  
watchtower jira boards select 1 2 5
  -> Analyzing "Backend Sprint Board"... done
  -> Analyzing "Frontend Kanban"... done  
  -> Analyzing "Mobile App"... done
  
  3 boards configured. Run 'watchtower jira sync' to start.

watchtower jira boards deselect 5
  -> "Mobile App" removed from sync.
```

### Config

```yaml
jira:
  boards:
    - id: 42
      sync: true
      # LLM-generated profile cached in DB, user overrides here:
      stale_overrides:          # optional manual override of LLM thresholds
        "Code Review": 1        # stricter than LLM suggested
    - id: 55
      sync: true
    - id: 99
      sync: false              # explicitly disabled
```

For each discovered board, Watchtower saves a **profile** (LLM-generated + raw config):

```
BoardProfile (DB: jira_boards):
  id: 42
  name: "Backend Sprint Board"
  project_key: "BACK"
  is_selected: true                      # user chose this board
  
  # Raw config from Jira API (factual data)
  raw_columns_json: [...]                # columns with statuses, categories
  raw_estimation_field: "customfield_10016"
  raw_filter_jql: "project = BACK AND type != Epic"
  raw_config_json: "{...}"               # full board config response
  
  # LLM-generated profile (interpretation)
  llm_profile_json: "{...}"             # workflow_stages, stale_thresholds, etc.
  workflow_summary: "The team works in two-week iterations..."
  
  # User overrides (takes precedence over LLM)
  user_overrides_json: "{...}"           # manual corrections to stale thresholds etc.
  
  # Meta
  profile_generated_at: "2026-04-05"     # when LLM last analyzed
  config_hash: "abc123"                  # detect config changes for auto-refresh
  synced_at: "2026-04-05T10:00Z"
```

### Why a Board Profile

| Watchtower Question | How the Profile Helps |
|---------------------|----------------------|
| "Is the issue stuck?" | LLM determined stale thresholds per stage: "Code Review > 2 days = stuck" |
| "Velocity?" | LLM determined estimation_approach: SP, hours, or count |
| "Forecast?" | LLM determined has_iterations + avg_throughput |
| "What is the workflow?" | workflow_summary is injected into AI prompts for context |
| "Bottleneck?" | health_signals suggest what to pay attention to |

---

## What to Sync per Board

### For Each Selected Board (regardless of methodology)

```
Every sync (15 min):
  Issues updated since last sync (JQL: updated >= last_sync)
  Active iterations/sprints (if board has them)

Every hour:
  Issue links (blocks/blocked-by) for in_progress issues
  Issue comments for issues mentioned in Slack

Daily:
  Board configuration check (detect changes -> re-run LLM if needed)
  Closed iterations (last 5, for throughput calculation)

On first connect:
  Full board discovery + LLM analysis
  Full issue backlog (initial load)
```

Watchtower syncs the same set of data for any board. The only difference is in the interpretation — and the LLM profile is responsible for that.

---

## Custom Fields Discovery: Story Points and Others

### Problem

Story Points is not a standard Jira field. It is a **custom field**, and its ID differs across instances:
- Atlassian default: `customfield_10016`
- Some instances: `customfield_10028`
- Plugins: `customfield_10002` (Greenhopper legacy)
- May not exist at all

### Discovery Strategy

```
Step 1: Board estimation field (most reliable)
  GET /rest/agile/1.0/board/{id}/configuration
  → response.estimation.field.fieldId = "customfield_10016"
  → This is the Story Points field for this board

Step 2: Fallback — search by name
  GET /rest/api/3/field
  → All fields (standard + custom)
  → Search by name: "Story Points", "Story point estimate", "SP"
  → There may be multiple matches — take the first one

Step 3: Fallback — nothing found
  → estimation_type = "none"
  → Velocity is calculated by count, not SP
```

### Other Custom Fields That Are Needed

| Field | Purpose | How to Find |
|-------|---------|-------------|
| **Story Points** | Velocity, workload, sprint forecast | Board config → estimation field |
| **Epic Link** | Issue hierarchy → epic progress | Standard field `epic` (Jira Cloud) or `customfield_10014` (Server) |
| **Sprint** | Sprint membership | `sprint` field in Agile API (not a custom field) |
| **Flagged** | "Marked as blocked" | `customfield_10021` (usually) or search by name "Flagged" |

### Strategy: Do Not Try to Know Everything

Watchtower **does not** try to understand all custom fields. Strategy:

1. **Board config** provides the estimation field → this is sufficient for SP
2. **Epic link** — standard in Cloud API, one fallback for Server
3. **Sprint** — Agile API returns it automatically
4. **Everything else** — store in `raw_json` for future use
5. **User custom fields** — Phase 3, on demand via config

---

## Issue Type Normalization

### Problem

```
Team A: Story, Task, Bug, Sub-task
Team B: Story, Bug, Improvement, Technical Debt, Spike, Sub-task
Team C: Задача, Баг, Подзадача, Эпик
```

### Solution: Issue Type Scheme

Jira API returns for each type:
```json
{
  "name": "Technical Debt",
  "subtask": false,
  "hierarchyLevel": 0    // 0 = standard, -1 = subtask, 1 = epic
}
```

**Normalization:**

| Jira hierarchyLevel | Watchtower category | Examples |
|---------------------|--------------------|---------| 
| `1` (Epic) | `epic` | Epic |
| `0` (Standard) | `standard` | Story, Task, Bug, Improvement, Technical Debt, Spike |
| `-1` (Subtask) | `subtask` | Sub-task, Подзадача |

**Additionally — Bug detection:**
- `issueType.name` contains "bug" (case-insensitive) → `is_bug = true`
- For metrics like "bug rate", "reopened bugs", etc.

**Everything else — store the original name:**
- AI sees: "Technical Debt" (original) — can use in prompts
- Logic sees: `category = standard, is_bug = false` — for metrics

---

## Sync Strategy: What and When to Fetch

### Tiered Sync

```
Tier 1: Every sync cycle (15 min)
  ├── Issues updated since last sync
  │   JQL: updated >= "{last_sync}" AND project IN ({selected_projects})
  │   Fields: key, summary, status, statusCategory, assignee, reporter,
  │           priority, issuetype, updated, created, duedate,
  │           {estimation_field}, sprint, epic, labels, components
  │   Max: 100 per request, paginate
  │
  └── Active iterations (if board has them)
      GET /rest/agile/1.0/board/{id}/sprint?state=active
      → sprint state, dates, goal

Tier 2: Every hour (or on demand)
  ├── Issue links (blocks/blocked-by)
  │   Fetched as part of issue fields: fields=issuelinks
  │   Only for issues with status_category = in_progress
  │
  └── Issue comments (last N)
      GET /rest/api/3/issue/{key}/comment?maxResults=5&orderBy=-created
      Only for issues mentioned in Slack (jira_slack_links)

Tier 3: Daily (or on board profile refresh)
  ├── Board configurations (workflow, columns, WIP)
  │   GET /rest/agile/1.0/board/{id}/configuration
  │
  ├── Project metadata
  │   GET /rest/api/3/project/{key}
  │
  └── Sprint velocity (last 5 closed sprints)
      GET /rest/agile/1.0/board/{id}/sprint?state=closed&maxResults=5

Tier 4: On first connect / manual refresh
  ├── Full board discovery (all boards, profiles)
  ├── Field discovery (custom fields, estimation)
  └── Full issue backlog (initial load)
```

### Incremental Sync Detail

```
SyncCycle():
  1. Load last_synced_at from jira_sync_state
  
  2. Query: JQL = updated >= "{last_synced_at - 2min overlap}"
                  AND project IN (selected_projects)
     Fields: key, summary, status, assignee, priority, issuetype,
             updated, created, duedate, sprint, epic, labels, 
             components, {estimation_field}, statusCategory
     Expand: none (keep payload small)
     Paginate: startAt=0, maxResults=100, repeat until total exhausted
  
  3. For each issue:
     a. Upsert into jira_issues (by key)
     b. Resolve assignee email → slack_user_id (via jira_user_map cache)
     c. If status_category changed → update status_category_changed_at
     d. Set synced_at = now
  
  4. Detect deleted issues:
     For selected_projects: issues in DB with synced_at < (now - 7 days)
     AND not returned by recent syncs → mark as deleted (soft delete)
     NOTE: Jira doesn't reliably report deletions. Conservative approach:
     flag stale, don't hard delete.
  
  5. Update jira_sync_state.last_synced_at = now
```

### Rate Limiting

Jira Cloud: ~8 requests/second per user. Strategy:
- Batch issue fetches: 100 per request (max)
- Parallelize across projects: 1 goroutine per project, shared rate limiter
- Board config fetches: sequential (rare, Tier 3)
- Backoff: exponential on 429, max 3 retries

---

## User Mapping: Jira Account → Slack User

### Strategy: Email-first, display name fallback

```
Phase 1: Email match (high accuracy)
  Jira: issue.fields.assignee.emailAddress
  Slack: users.email (already in DB)
  JOIN: jira_email = slack_email
  
  Problem: Jira Cloud may hide email (GDPR)
  Solution: GET /rest/api/3/user?accountId={id} → emailAddress
  If email is hidden → fallback

Phase 2: Display name match (medium accuracy)  
  Jira: assignee.displayName = "Пётр Иванов"
  Slack: users.real_name = "Пётр Иванов" OR users.display_name
  Match: case-insensitive, trim whitespace
  
  Problem: "Peter Ivanov" vs "Пётр Иванов" (different languages)
  Solution: fuzzy match with threshold >0.85 similarity → candidate,
  but save match_confidence for filtering

Phase 3: Manual mapping (100% accuracy)
  Config:
    jira:
      user_map:
        "5f1234abc": "U0123SLACK"    # jira_account_id → slack_user_id
  
  Used for edge cases and correcting automation errors

Phase 4: Unresolved
  If no method worked:
  → slack_user_id = null
  → Issue is visible but not linked to a Slack profile
  → In Briefing: "PROJ-123 (assignee: Пётр Иванов, Slack profile not linked)"
```

### User Map Cache

```
jira_user_map:
  jira_account_id: "5f1234abc"     # PK
  email: "peter@company.com"        # from Jira
  slack_user_id: "U0123SLACK"       # resolved
  display_name: "Пётр Иванов"       # for display
  match_method: "email"              # email | display_name | manual | unresolved
  match_confidence: 1.0              # 0.0-1.0
  resolved_at: "2026-04-05T10:00Z"  # cache timestamp
```

---

## Done Detection & Stale Thresholds

### "Is the Issue Completed?" — Status Category

The only reliable indicator: `statusCategory.key == "done"`

The Jira admin maps each status to a category (todo/in_progress/done). Watchtower trusts this mapping.
The original status is stored for UI: "PROJ-142 in status 'Released' for 2 days".

### "Is the Issue Stuck?" — LLM-Driven Thresholds

Stale thresholds are determined by the **LLM during board analysis** (not hardcoded):
- The LLM sees the workflow and determines: Code Review = 2 days, Development = 5 days, QA = 3 days
- The user can override via config or UI
- Fallback (if LLM did not determine): 3 days for any stage

### "Reopened?" — Compare on Sync

On each sync we compare:
```
old_category = db.get(issue.key).status_category
new_category = api_response.status_category
if old == 'done' AND new != 'done' -> reopened event
```

## Data Flow: From Jira API to AI Prompt

```
┌─────────────────────────────────────────────────────────────┐
│                     JIRA CLOUD API                          │
│  /board  /sprint  /issue  /board/{id}/configuration         │
└──────────┬──────────────────────────────────────────────────┘
           │
    ┌──────▼──────┐
    │  Jira Sync  │  internal/jira/sync.go
    │  (Tier 1-4) │  - Incremental by updated_at
    │             │  - Rate limited (8 req/s)
    │             │  - User mapping (email → Slack)
    └──────┬──────┘
           │
    ┌──────▼──────────────────────────────────────────────┐
    │                    SQLite DB                         │
    │                                                      │
    │  jira_boards        → Board profiles (type, columns) │
    │  jira_issues        → Issues (status, category, SP)  │
    │  jira_sprints       → Sprint data (dates, goals)     │
    │  jira_issue_links   → Blocks/blocked-by              │
    │  jira_user_map      → Account → Slack mapping        │
    │  jira_sync_state    → Watermarks per project         │
    │  jira_slack_links   → Issue ↔ Slack message links    │
    └──────┬──────────────────────────────────────────────┘
           │
    ┌──────▼──────┐
    │  Enrichment │  Runs after Jira sync, before AI pipelines
    │  Pipeline   │
    │             │  1. Detect Jira keys in Slack messages (regex)
    │             │  2. Link tracks to Jira issues
    │             │  3. Compute workload per user
    │             │  4. Compute sprint metrics
    │             │  5. Detect stale/overdue/reopened
    └──────┬──────┘
           │
    ┌──────▼───────────────────────────────────────────────┐
    │                   AI Pipelines                        │
    │                                                       │
    │  Briefing prompt:                                     │
    │    + "JIRA CONTEXT: Sprint 24 (3 days left),          │
    │       5 issues done, 3 in progress, 2 blocked.        │
    │       Workload: Пётр 28SP, Аня 15SP, Дима 5SP.       │
    │       Overdue: PROJ-180 (1 day), PROJ-195 (today)."   │
    │                                                       │
    │  Track enrichment:                                    │
    │    + track.jira_key = "PROJ-203"                      │
    │    + track.jira_status = "Code Review"                │
    │    + track.jira_status_category = "in_progress"       │
    │                                                       │
    │  Meeting prep:                                        │
    │    + attendee.jira_issues = [PROJ-142, PROJ-250]      │
    │    + attendee.workload = {open: 10, overdue: 1}       │
    │                                                       │
    │  People cards:                                        │
    │    + jira_metrics = {velocity, cycle_time, overdue}   │
    └──────────────────────────────────────────────────────┘
```

---

## DB Schema

### Main Tables

```sql
-- Board profiles (user-selected, LLM-analyzed)
CREATE TABLE jira_boards (
    id INTEGER PRIMARY KEY,              -- Jira board ID
    name TEXT NOT NULL,
    project_key TEXT,
    is_selected INTEGER DEFAULT 0,       -- user chose to sync this board
    
    -- Raw config from Jira API
    raw_columns_json TEXT,               -- JSON: columns with statuses, categories
    raw_estimation_field TEXT,            -- custom field ID (from board config)
    raw_filter_jql TEXT,                  -- board filter
    raw_config_json TEXT,                 -- full board configuration response
    config_hash TEXT,                     -- hash to detect config changes
    
    -- LLM-generated profile
    llm_profile_json TEXT,               -- JSON: workflow_stages, stale_thresholds, etc.
    workflow_summary TEXT,                -- human-readable process description
    profile_generated_at TEXT,           -- when LLM last analyzed
    
    -- User overrides (takes precedence over LLM)
    user_overrides_json TEXT,            -- manual corrections to thresholds etc.
    
    -- Meta
    synced_at TEXT
);

-- Issues (core entity)
CREATE TABLE jira_issues (
    key TEXT PRIMARY KEY,                 -- "PROJ-123"
    id TEXT,                              -- Jira internal ID
    project_key TEXT NOT NULL,
    board_id INTEGER,                     -- nullable (issue may be outside a board)
    
    -- Content
    summary TEXT NOT NULL,
    description TEXT,                     -- sanitized, truncated
    issue_type TEXT,                      -- original: "Story", "Bug", etc.
    issue_type_category TEXT,             -- normalized: "epic" | "standard" | "subtask"
    is_bug INTEGER DEFAULT 0,
    
    -- Status (dual layer)
    status TEXT NOT NULL,                 -- original: "Code Review", "QA", etc.
    status_category TEXT NOT NULL,        -- normalized: "todo" | "in_progress" | "done"
    status_category_changed_at TEXT,      -- when category last changed (for cycle time)
    
    -- People
    assignee_account_id TEXT,
    assignee_email TEXT,
    assignee_slack_id TEXT,               -- resolved via jira_user_map
    reporter_account_id TEXT,
    reporter_email TEXT,
    reporter_slack_id TEXT,
    
    -- Planning
    priority TEXT,                        -- "Highest" | "High" | "Medium" | "Low" | "Lowest"
    story_points REAL,                    -- from estimation_field
    due_date TEXT,
    sprint_id INTEGER,
    sprint_name TEXT,
    epic_key TEXT,                        -- parent epic issue key
    
    -- Classification
    labels TEXT,                          -- JSON array
    components TEXT,                      -- JSON array
    
    -- Timestamps
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,             -- watermark for incremental sync
    resolved_at TEXT,                     -- when moved to done (if available)
    
    -- Meta
    raw_json TEXT,                        -- full API response for extensibility
    synced_at TEXT NOT NULL,              -- for stale detection
    is_deleted INTEGER DEFAULT 0          -- soft delete
);

CREATE INDEX idx_jira_issues_project ON jira_issues(project_key);
CREATE INDEX idx_jira_issues_assignee ON jira_issues(assignee_slack_id);
CREATE INDEX idx_jira_issues_status_cat ON jira_issues(status_category);
CREATE INDEX idx_jira_issues_sprint ON jira_issues(sprint_id);
CREATE INDEX idx_jira_issues_epic ON jira_issues(epic_key);
CREATE INDEX idx_jira_issues_updated ON jira_issues(updated_at);
CREATE INDEX idx_jira_issues_due ON jira_issues(due_date);

-- Sprints (Scrum boards)
CREATE TABLE jira_sprints (
    id INTEGER PRIMARY KEY,
    board_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    state TEXT NOT NULL,                  -- "active" | "closed" | "future"
    goal TEXT,
    start_date TEXT,
    end_date TEXT,
    complete_date TEXT,
    synced_at TEXT
);

-- Issue links (dependencies)
CREATE TABLE jira_issue_links (
    id TEXT PRIMARY KEY,
    source_key TEXT NOT NULL,             -- "PROJ-123"
    target_key TEXT NOT NULL,             -- "PROJ-456"
    link_type TEXT NOT NULL,              -- "blocks" | "is blocked by" | "relates to" | "duplicates"
    synced_at TEXT
);

CREATE INDEX idx_jira_links_source ON jira_issue_links(source_key);
CREATE INDEX idx_jira_links_target ON jira_issue_links(target_key);

-- Jira ↔ Slack user mapping cache
CREATE TABLE jira_user_map (
    jira_account_id TEXT PRIMARY KEY,
    email TEXT,
    slack_user_id TEXT,                   -- nullable if unresolved
    display_name TEXT,
    match_method TEXT,                    -- "email" | "display_name" | "manual" | "unresolved"
    match_confidence REAL DEFAULT 0,
    resolved_at TEXT
);

-- Jira issue ↔ Slack message links (auto-detected)
CREATE TABLE jira_slack_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_key TEXT NOT NULL,
    channel_id TEXT,
    message_ts TEXT,
    track_id INTEGER,                     -- nullable
    link_type TEXT NOT NULL,              -- "mention" | "track" | "decision" | "manual"
    detected_at TEXT NOT NULL
);

CREATE INDEX idx_jira_slack_issue ON jira_slack_links(issue_key);
CREATE INDEX idx_jira_slack_track ON jira_slack_links(track_id);

-- Sync state (watermarks per project)
CREATE TABLE jira_sync_state (
    project_key TEXT PRIMARY KEY,
    last_synced_at TEXT,                  -- ISO8601, watermark for incremental
    issues_synced INTEGER DEFAULT 0,
    last_error TEXT,
    last_error_at TEXT
);

-- Computed: workload snapshots (refreshed each sync)
CREATE TABLE jira_workload (
    slack_user_id TEXT NOT NULL,
    computed_at TEXT NOT NULL,
    open_issue_count INTEGER DEFAULT 0,
    story_points_in_progress REAL DEFAULT 0,
    overdue_count INTEGER DEFAULT 0,
    blocked_count INTEGER DEFAULT 0,
    avg_cycle_time_days REAL,
    closed_last_7_days INTEGER DEFAULT 0,
    PRIMARY KEY (slack_user_id, computed_at)
);
```

---

## Jira Key Detection in Slack Messages

### Regex Pattern

```
Pattern: \b([A-Z][A-Z0-9_]+-\d+)\b

Matches:
  PROJ-123        ✅
  BACK-1          ✅
  MY_TEAM-456     ✅
  ABC-1234        ✅

Does not match:
  proj-123        ❌ (lowercase)
  A-1             ❌ (single letter prefix)
  PROJ-            ❌ (no number)
  PROJ 123        ❌ (space)
```

### Validation

A found key is validated:
1. Project prefix must exist in `jira_issues` or `jira_boards` (known projects)
2. If the prefix is unknown — skip it (may be a non-Jira pattern like "AWS-123")

### Timing

Jira key detection runs:
- **During Slack message sync** — for new messages, inserts into `jira_slack_links`
- **During tracks extraction** — AI extracts tracks, post-processing searches for keys in source_refs
- **During digest generation** — searches for keys in decisions and action_items for linking

---

## Write-Back: Slack → Jira (Phase 2)

### Architecture

Write-back is a separate pipeline, **not part of sync**. It runs after AI pipelines.

```
Write-back pipeline:
  1. Scan new tracks/decisions since last run
  2. For each with jira_key:
     a. Track type=task WITHOUT jira_key → suggest "Create issue?"
     b. Decision linked to jira_key → suggest "Add comment?"
     c. Requirement change detected → suggest "Update description?"
  3. Queue suggestions → UI shows them
  4. On user confirm → POST /rest/api/3/issue (create) or 
                        POST /rest/api/3/issue/{key}/comment (comment)
```

### Safety: Everything Through Confirmation

Write-back **never** happens automatically. Always:
1. Watchtower forms a suggestion with preview
2. The user sees "Add comment to PROJ-350?" with the text
3. The user confirms → API call
4. The result is logged in `jira_write_log`

---

## Config Model

```yaml
jira:
  enabled: false                          # opt-in
  url: "https://company.atlassian.net"    # Jira Cloud URL
  
  # Project selection
  selected_projects: []                   # empty = auto-discover from boards
  
  # Sync tuning
  sync_lookback_days: 7                   # initial sync window
  sync_overlap_minutes: 2                 # overlap for incremental
  
  # Board overrides
  boards: {}                              # per-board sync/skip overrides
  
  # User mapping overrides
  user_map: {}                            # manual jira_account_id → slack_user_id
  
  # Thresholds
  stale_threshold_days: 3                 # default "stuck" threshold
  
  # Write-back (Phase 2)
  write_back:
    enabled: false
    auto_comment_decisions: false         # suggest adding decisions as comments
    auto_create_tasks: false              # suggest creating issues from tracks
```

---

## Graceful Degradation

Following the Calendar pattern:

| Situation | Behavior |
|-----------|----------|
| Jira not connected | Everything works as before. "Run `watchtower jira login` to connect." |
| Jira API unavailable | Use cached data. Log & continue. |
| Project inaccessible (403) | Skip the project, sync the rest. Log warning. |
| Rate limit (429) | Exponential backoff. Partial sync is acceptable. |
| User not mapped | Show Jira display name, slack_user_id = null. |
| Board without estimation | Velocity by issue count, not SP. |
| Kanban without WIP limits | Skip WIP metrics, everything else works. |
| Custom field not found | estimation_type = "none", fallback to count. |
| Status category missing | Fallback: if status contains "done/closed/resolved" → done, otherwise → in_progress |

---

## CLI Commands

```bash
watchtower jira login [url]           # OAuth2 flow → Jira Cloud
watchtower jira logout                # Disconnect, cleanup tokens
watchtower jira status                # Connection status + boards summary
watchtower jira sync                  # Manual sync
watchtower jira boards                # List discovered boards with classification
watchtower jira projects              # List projects with sync status  
watchtower jira select {project}      # Toggle project for sync
watchtower jira issues [--mine] [--project PROJ] [--status in_progress]
watchtower jira workload              # Team workload table
```

---

## Phased Implementation

### Phase 1: Read-only core (MVP)
- OAuth + token management
- Board enumeration + user selection
- **LLM board analysis** (profile generation)
- Issue sync (Tier 1-2)
- User mapping (email)
- Status category normalization
- Jira key detection in Slack
- Track enrichment (jira_key, jira_status)
- Briefing: Jira signals in Attention + Your Day

### Phase 2: Deep integration
- Iteration metrics + forecast (using LLM profile)
- Workload dashboard
- Issue links (blocks/blocked-by)
- Meeting Prep enrichment
- People Cards + Jira metrics
- AI Chat + Jira context
- Write-back (suggestions + confirm)

### Phase 3: Intelligence
- LLM profile auto-refresh on config changes
- Strategy-to-Execution gaps
- Release tracking (Fix Versions)
- Initiative Lifecycle
- Cross-board dependency detection
- Board health monitoring (using LLM health_signals)
