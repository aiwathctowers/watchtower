# Jira Integration: Roadmap

Detailed implementation plan with business context and acceptance criteria.
References: [cohorts](jira-integration-cohorts.md), [data-mining](jira-integration-data-mining.md), [feature-map](jira-integration-feature-map.md).

**Principle 1: every deliverable = CLI + Desktop App.** A feature without UI in the desktop app is not considered done.

**Principle 2: roadmap by features, not by roles.** We build features — they work for everyone. User profile (role) determines which features are enabled by default and what is highlighted in AI output. Role = settings preset, not a restriction. Any user can enable/disable any feature.

---

## How role affects features

Watchtower already has roles (ic, senior_ic, middle_management, top_management, direction_owner). When connecting Jira, the role determines **defaults** — which features are active and what the AI highlights:

| Feature | IC default | TL default | EM default | PM default | Director default |
|---------|:----------:|:----------:|:----------:|:----------:|:----------------:|
| My Issues in Briefing | on | on | on | off | off |
| "Awaiting My Input" | on | on | on | off | off |
| "Who to Ping" (blockers) | on | on | on | on | off |
| Track → Jira linking | on | on | on | on | on |
| Team Workload | off | off | on | off | on |
| Blocker Map | off | on | on | on | on |
| Iteration Progress | off | off | on | on | on |
| Epic Progress & Forecast | off | off | off | on | on |
| Write-Back Suggestions | off | on | off | off | off |
| Release Dashboard | off | off | off | off | on |
| "Without Jira" detection | off | on | off | on | off |

Users can toggle any feature in **Desktop App → Settings → Jira Features** (toggle switches).

AI prompts adapt to enabled features: if Workload is off — AI does not generate workload signals in Briefing.

---

## Phase 0: Connection and Infrastructure

### What we do
Foundation: user connects Jira, selects boards, LLM analyzes workflow, users are mapped to Slack.

### 0.1 — OAuth and authorization ✅ DONE

*Acceptance criteria:*
- **Desktop App → Settings:** "Connect Jira" button → OAuth flow in browser → status "Connected to company.atlassian.net as John Smith" + Disconnect button
- **CLI:** `watchtower jira login/logout/status`
- Auto-refresh token without user involvement
- Graceful degradation: Jira unavailable → everything else works
- **Sidebar:** Jira connection indicator (similar to Calendar)

### 0.2 — Board discovery and selection ✅ DONE

*Acceptance criteria:*
- **Desktop App → Settings → Jira → Boards:** list of all available boards with toggle switches. Name, project, issues
- **CLI:** `watchtower jira boards / select / deselect`
- Selection persists between sessions

### 0.3 — LLM board analysis ✅ DONE

> See [data-mining: Board Discovery](jira-integration-data-mining.md#board-discovery--selection)

*Acceptance criteria:*
- For each selected board, LLM generates a profile: workflow stages, stale thresholds, estimation, health signals
- **Desktop App → Settings → Board card:** workflow visualization (stages), sliders for stale thresholds, "Re-analyze" button
- Notification on board config change: "Board config changed, re-analyze?"
- No methodology lock-in

### 0.4 — User mapping ✅ DONE

*Acceptance criteria:*
- Auto-mapping by email, fallback by display name
- **Desktop App → Settings → User Mapping:** matched/unmatched table, dropdown for manual mapping
- **CLI:** `watchtower jira users`

### 0.5 — Issue sync ✅ DONE

*Acceptance criteria:*
- Incremental sync every 15 minutes (like Slack)
- Initial load: all issues from selected boards
- **Desktop App → Sidebar:** progress indicator during sync
- **Desktop App → Settings:** last sync time, manual sync button
- Graceful degradation: Jira unavailable → stale data OK, no crash

### 0.6 — Jira key detection in Slack ✅ DONE

*Acceptance criteria:*
- Pattern PROJ-123 in Slack messages is automatically detected and linked to the Jira issue
- Links are available for all downstream features
- Invalid keys are ignored

### 0.7 — Feature toggles by role ✅ DONE

*Acceptance criteria:*
- **Desktop App → Settings → Jira Features:** list of all Jira features with toggle switches
- On first connection: defaults based on user role (see table above)
- User can toggle any feature at any time
- AI prompts only account for enabled features

### Phase 0 Result
Jira is connected, boards are selected and analyzed, issues are syncing, Slack messages are linked to Jira. In Desktop App — full Jira Settings. Feature toggles are configured by role.

---

## Phase 1: Enriching Existing Features

### What we do
Existing Watchtower features start showing Jira data. No new screens — we enrich what already exists.

### 1.1 — Track → Jira linking ✅ DONE

> See [feature-map: Tracks](jira-integration-feature-map.md#2-tracks)

*Acceptance criteria:*
- Tracks are automatically linked to Jira issues (by keys in source messages)
- **Desktop App → Tracks view:** Jira badge on track card: key (clickable → Jira in browser), status with color, priority, sprint, due date. Overdue highlighted
- **Desktop App → Tracks view:** filters "With Jira" / "Without Jira"
- **CLI:** `watchtower tracks` shows Jira badge (key, status, priority) next to linked tracks. Graceful: no Jira → output as before
- No duplication: one track = one issue
- AI takes Jira context into account during extraction

### 1.1b — Track ↔ Jira out-of-sync detection (DEFERRED — design needed)

> Dependency: 1.1

*Problem:* Track marked done in Watchtower but linked Jira issue still In Progress (or vice versa). Need to detect and surface this.

*Open questions:*
- How to surface: badge on track? separate section in Briefing? notification?
- Auto-resolve or manual? (e.g., track done + Jira done → auto-close?)
- Which direction is source of truth? (Jira → Watchtower? bidirectional?)
- Threshold: how long out-of-sync before flagging?

### 1.2 — Briefing → Jira signals ✅ DONE

> See [feature-map: Daily Briefing](jira-integration-feature-map.md#1-daily-briefing)

*Acceptance criteria (depends on enabled feature toggles):*

**If "My Issues" on:**
- **Briefing → Your Day:** my Jira issues mixed with calendar and tracks in timeline. Jira badge, overdue/stale highlighted

**If "Awaiting My Input" on:**
- **Briefing → Your Day:** "Awaiting Your Input" section: review requests, approvals, responses. "How long they've been waiting" + "is it blocking someone"

**If "Team Workload" on:**
- **Briefing → Team Pulse:** compound signals: "@Peter: 28 SP + 45 msg/day + 5 meetings — overload risk"

**If "Iteration Progress" on:**
- **Briefing → Attention:** "Sprint 24: 3 days left, 65% done, 3 blocked"

**Always (if Jira is connected):**
- Stale and overdue issues in Attention with Slack context (why it's stuck)
- Blocked issues in Attention with path to resolution
- **Desktop App:** Jira items in Briefing with Jira icon, clickable keys
- Graceful: Jira off → Briefing as before, no empty sections

### 1.3 — Digests → Jira enrichment ✅ DONE

> See [feature-map: Digests](jira-integration-feature-map.md#6-channel-digests)

*Acceptance criteria:*
- **Desktop App → Digest view:** decisions with Jira badge: "Decided on Redis → PROJ-250 [In Dev]"
- Action items with Jira status (overdue/stale highlighted)
- **If "Without Jira detection" on:** warning icon on discussions not linked to Jira

### 1.4 — Meeting Prep → Jira context ✅ DONE

> See [feature-map: Meeting Prep](jira-integration-feature-map.md#4-meeting-prep)

*Acceptance criteria:*
- **Desktop App → Meeting Prep view:** for each attendee — Jira section: open/overdue/blocked issues, workload bar
- Talking points with Jira badge, clickable keys
- Shared Jira issues between attendees — separate section
- Adaptation by meeting type: 1:1/standup/planning/review
- Context gaps: "PROJ-142 overdue but not discussed — bring it up?"

### 1.5 — People Cards → delivery metrics ✅ DONE

> See [feature-map: People Cards](jira-integration-feature-map.md#5-people-cards)

*Acceptance criteria:*
- **Desktop App → People view → card:** "Delivery" section: issues closed, cycle time, velocity, workload. Trend (rising/falling)
- Expertise tags from Jira components/labels: "caching", "payments" — clickable
- Compound red flags as warning badges: burnout risk, bottleneck, overload
- Accomplishments from Jira in Highlights
- Communication guide and Tactics enriched with workload context

### 1.6 — Tasks → unified view ✅ DONE

> See [feature-map: Tasks](jira-integration-feature-map.md#3-tasks)

*Acceptance criteria:*
- **Desktop App → Tasks view:** Jira tickets alongside Slack tracks and manual tasks. Jira badge, source indicator (Jira/Slack/manual)
- Auto-sync status: done in Jira → done in Watchtower
- No duplication: track + Jira = one task, both source icons
- Filters: "All" / "Jira" / "Slack" / "Manual"
- Due date from Jira, overdue highlighted

### 1.7 — AI Chat → Jira awareness ✅ DONE

> See [feature-map: AI Chat](jira-integration-feature-map.md#7-ai-chat)

*Acceptance criteria:*
- "What are my open tickets?" → list with Slack context
- "What about PROJ-142?" → Jira status + Slack discussions
- "Who is overloaded?" → Workload + compound signals
- "Will we make the release?" → Epic progress + forecast
- Cross-source: "Why is PROJ-142 delayed?" → Jira + Slack + People Card

### Phase 1 Result
All existing Watchtower features are enriched with Jira data in Desktop App. Tracks with badges, Briefing with Jira signals, Digests with linking, Meeting Prep with attendee tasks, People Cards with delivery, Tasks unified, AI Chat knows about Jira. What exactly is shown depends on feature toggles.

---

## Phase 2: New Features

### What we do
Three new views in Desktop App: Workload Dashboard, Blocker Map, Write-Back Suggestions. Plus Epic Progress in Weekly Trends.

### 2.1 — Workload Dashboard

> See [feature-map: Workload Dashboard](jira-integration-feature-map.md#9-workload-dashboard-new-feature)

*Acceptance criteria:*
- **Desktop App → Sidebar → "Workload":** team table: name, open issues, SP, overdue, blocked, cycle time, Slack volume, meetings, signal badge
- Color coding: green (normal), yellow (watch), red (overload), gray (low load)
- Click on person → their issues with Jira statuses
- Compound signals from Jira + Slack + Calendar
- Workload imbalance visually highlighted
- Updates every 15 minutes (with sync)
- **CLI:** `watchtower jira workload`

### 2.2 — Blocker Map

> See [feature-map: Blocker Map](jira-integration-feature-map.md#10-blocker-map-new-feature)

*Acceptance criteria:*
- **Desktop App → Sidebar → "Blockers":** list of blocked and stale issues
- Each blocker — a card: Jira key + summary + who/what is blocking + Slack context + how long + impact chain
- **"Who to ping"** on each card: avatar + name + role + "Open in Slack" button
- Sorted by impact (more downstream → higher)
- Color urgency: red / yellow / gray
- **Dependency:** requires issue links from Phase 0 sync

*Issue links acceptance criteria:*
- blocks / is blocked by / relates to are synced
- Chains: A blocked by B blocked by C → root cause
- **Desktop App:** linked issues visible on issue/track card

### 2.3 — Write-Back Suggestions

> See [feature-map: Write-Back](jira-integration-feature-map.md#11-write-back-suggestions-new-feature), [cohorts: TL](jira-integration-cohorts.md#cohort-3-tech-lead--staff-engineer--high)

**2.3a — Create issue from Track**

*Acceptance criteria:*
- **Desktop App → Track card:** if track has no Jira → "Create in Jira" button → preview with AI-filled title, description, type, labels → user can edit → confirm
- **Desktop App → Tracks list:** "N without Jira" badge as shortcut
- After creation: Jira badge appears on the track
- Never automatic

**2.3b — Decision → Jira comment**

*Acceptance criteria:*
- **Desktop App → Digest → Decision card:** if linked to Jira → "Add to Jira" button
- Preview: AI text (summary + participants + rationale + Slack link). Can be edited
- Toggle "Format as ADR" for architectural decisions
- After confirm: "Synced to Jira" indicator on decision

**2.3c — Requirement update → Jira comment**

*Acceptance criteria:*
- If Slack discussion changes the approach/requirements for a Jira issue → suggestion
- **Desktop App:** notification or suggestion card with comment preview
- Only adds a comment (never changes description)

**2.3d — Suggestions Dashboard**

*Acceptance criteria:*
- **Desktop App → Sidebar → badge** with pending count on relevant views (Tracks, Digests)
- **Desktop App → Jira Suggestions view:** list of pending suggestions: type, source, preview, Accept/Edit/Dismiss
- Bulk actions: Accept all / Dismiss all
- History tab: accepted/dismissed for audit
- Deduplication: no clutter
- **CLI:** `watchtower jira suggestions / accept N / dismiss N`

### 2.4 — Weekly Trends → Epic Progress & Forecast

> See [feature-map: Weekly Trends](jira-integration-feature-map.md#8-weekly-trends)

*Acceptance criteria:*
- **Desktop App → Weekly Trends view:** "Epic Progress" section — epic cards: name, progress bar, weekly delta, status badge (on track / at risk / behind)
- Forecast: "at current velocity, N weeks needed" under progress bar
- "What Changed" section: green (improved) and red (worsened) items
- **If "Without Jira detection" on:** warning cards "Discussed in Slack for N days, not in Jira"

### Phase 2 Result
Three new views in Desktop App: Workload, Blockers, Write-Back Suggestions. Weekly Trends enriched with Epic Progress. User enables what they need via feature toggles.

---

## Phase 3: Advanced & Onboarding

### What we do
Features for deep project understanding: Project Map, Release Dashboard, "Who to ping", initiative lifecycle.

### 3.1 — Project Map

> See [cohorts: Onboarding](jira-integration-cohorts.md#cohort-6-onboarding-cross-role--medium)

*Acceptance criteria:*
- **Desktop App → Sidebar → "Project Map":** visual map of epics. Each: name, owner (avatar), progress bar, key decisions, stale/blocked indicators
- Click on epic → issues, participants, timeline, Slack discussions
- "People around the issue": avatars of assignee, reviewer, QA, dependents. Hover → People Card summary
- "Who can help": contextual hint — topic expert (Jira ownership + Slack activity)
- Stale tasks tab: stuck tasks with avg cycle time context
- Adaptation: IC sees issues, PM — epics/releases, EM — workload overlay

### 3.2 — Release Dashboard

> See [cohorts: Director](jira-integration-cohorts.md#cohort-5-director--leader--medium-high)

*Acceptance criteria:*
- **Desktop App → Sidebar → "Releases":** release timeline. Each: date, epics (progress bars), at-risk items, scope changes
- Click on release → epics, blocked items, Slack context
- **Strategy-to-Execution gaps:** cards "discussed in Slack, not in Jira" + "Create Epic" button
- Initiative Lifecycle: visualization Discussion → Decision → Epic → Execution → Release

### 3.3 — "Who to ping" (enhanced)

*Acceptance criteria:*
- **Desktop App:** on any blocked/stale issue — "Who to ping" block
- Avatar + name + why this person (assignee / expert / decision-maker)
- "Open in Slack" button for direct message
- AI determines best contact from: Jira assignee of blocking task + Slack discussion participants + People Card decision-makers

### 3.4 — Auto-refresh LLM profiles

*Acceptance criteria:*
- Daily check: config changed → notification "Board config changed, re-analyze?"
- User overrides are preserved during re-analysis
- New columns/statuses are included automatically

### Phase 3 Result
Desktop App: Project Map for project navigation, Release Dashboard for strategic overview, "Who to ping" on every blocker, auto-refresh profiles.

---

## Dependencies

```
Phase 0 (Infrastructure: auth, boards, sync, key detection, feature toggles)
  └── Phase 1 (Enrichment: Tracks, Briefing, Digests, Meeting Prep, People, Tasks, AI Chat)
        └── Phase 2 (New features: Workload, Blockers, Write-Back, Epic Progress)
              └── Phase 3 (Advanced: Project Map, Releases, Who to Ping)
```

Phase 1 deliverables (1.1-1.7) can be developed in parallel.
Phase 2 deliverables (2.1-2.4) can run in parallel after Phase 1.
Phase 3 depends on Phase 2 (Workload for Project Map overlay, Blockers for Who to Ping).

---

## Feature toggles: what is enabled by role (defaults)

On first Jira connection, Watchtower suggests a preset based on role. User can change any toggle.

### IC / Developer
**On:** My Issues, Awaiting My Input, Who to Ping, Track linking, Tasks unified, AI Chat
**Off:** Team Workload, Blocker Map, Epic Progress, Write-Back, Releases, Without Jira detection

*Focus: "What should I do, who's waiting on me, who to ping"*

### Tech Lead / Staff Engineer
**On:** My Issues, Awaiting My Input, Who to Ping, Track linking, Blocker Map, Write-Back, Without Jira detection, AI Chat
**Off:** Team Workload, Epic Progress, Releases

*Focus: "Architectural decisions in Jira, blockers are visible, nothing gets lost"*

### Engineering Manager / Team Lead
**On:** My Issues, Team Workload, Blocker Map, Iteration Progress, Who to Ping, Track linking, AI Chat
**Off:** Write-Back, Epic Progress, Releases, Without Jira detection

*Focus: "Team workload, what's on fire, sprint health"*

### Product Manager / Product Owner
**On:** Epic Progress, Blocker Map, Iteration Progress, Who to Ping, Track linking, Without Jira detection, AI Chat
**Off:** My Issues, Team Workload, Write-Back, Releases, Awaiting My Input

*Focus: "Feature progress, blockers, forecast, nothing gets lost"*

### Director / Leader
**On:** Epic Progress, Team Workload, Releases, Iteration Progress, Blocker Map, AI Chat
**Off:** My Issues, Awaiting My Input, Write-Back, Without Jira detection, Who to Ping

*Focus: "Strategic overview, team health, release readiness"*

---

## What the user sees on first Jira connection

1. **Settings → Connect Jira** → OAuth → Connected
2. **Settings → Boards** → board list → selects desired ones → LLM analyzes
3. **Settings → Board Profiles** → sees workflow summary, confirms thresholds
4. **Settings → Jira Features** → sees preset for their role, can toggle switches
5. **First sync** → issues load (progress bar)
6. **Briefing** → already enriched with Jira data based on enabled features
7. **Tracks** → automatically linked to Jira

Entire onboarding — 5-10 minutes. After that, Jira works in the background.
