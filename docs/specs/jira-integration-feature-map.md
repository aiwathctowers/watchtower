# Jira Integration: Feature Enhancement Map

How each Watchtower feature is enriched with Jira data from the user's perspective.

---

## 1. Daily Briefing

### Attention (what's on fire)

**Without Jira:** Only Slack signals — unanswered tracks, mentions, team red flags.

**With Jira:**
- "PROJ-142 (@Пётр) stuck in Code Review for 5 days. In #backend Пётр wrote that he's waiting for review from @Аня"
- "Sprint 24: 3 days left, 65% done. At the current pace — ~75%. 3 tasks blocked"
- "2 overdue: PROJ-142 (yesterday), PROJ-180 (day before yesterday)"
- "PROJ-301 (@Дима) blocked 3 days — depends on PROJ-300. In Slack @Дима escalated in #infra"

**Key principle:** Every Jira signal is enriched with Slack context (why it's stuck, who discussed it, what was proposed). A bare Jira status is useless — the value is in the combination.

### Your Day (what to do today)

**Without Jira:** Slack tracks + inbox mentions + calendar.

**With Jira:**
- My Jira tasks intermixed with calendar and Slack tracks, sorted by priority
- Each task with context: "PROJ-250 [In Development, High] — yesterday @TL clarified requirements in #backend"
- **"Waiting on you"**: review requests, approvals, answers to questions — with a note on "how long they've been waiting" and "whether it blocks someone"
- **"Who to ping"** about my blockers: assignee of the blocking task + who discussed it in Slack

### Team Pulse (team health)

**Without Jira:** Only communication signals — volume drop, conflicts, red flags.

**With Jira — compound signals:**
- "@Пётр: 10 open issues (28 SP) + 45 messages/day + 5 meetings — risk of burnout"
- "@Дима: 2 open issues (5 SP), low Slack activity — available capacity or blocked?"
- "Workload imbalance: Пётр 28 SP, Дима 5 SP — consider redistribution"
- "PROJ-142 reopened 3 times in 2 weeks — possible quality issue"

---

## 2. Tracks

### Enriching existing tracks

**Without Jira:** Track = AI-extracted action item from Slack: "Need to do code review for PR #456".

**With Jira:** Track + linked to a task:
- "Need to do code review for PR #456 **→ PROJ-203 [Code Review, High, Sprint 24, due: tomorrow]**"
- Visible: what the task is, its Jira priority, which sprint, when the deadline is
- If the track and Jira task are about the same thing — they are linked, no duplication

### New types of insights

- **"Slack-only" detection:** "Discussed logger refactoring in #backend (12 messages) — no Jira task found. Create one?"
- **Out of sync:** "Track 'migration complete' marked as done, but PROJ-301 in Jira is still In Progress"
- **Jira context for tracks:** Track "code review" shows not only the Slack discussion, but also the task description, acceptance criteria, story points

### Write-back (suggestions)

When a new track appears without a Jira task:
- Watchtower suggests: "Create a task in Jira?" with AI-filled title and description
- When a decision is made in Slack on an existing task — suggests adding a comment in Jira
- When requirement changes are discussed — suggests updating Jira

Everything requires user confirmation, never automatic.

---

## 3. Tasks

### Unified task list

**Without Jira:** Tasks in Watchtower = personal todo from Slack (tracks, digest, manual). Jira tickets — separate.

**With Jira:**
- One place for everything: my Jira tickets + Slack tracks + manual tasks
- Jira tasks automatically sync status: closed in Jira → done in Watchtower
- No duplication: if a track is linked to a Jira task, there's one task with both sources

### Prioritization

- Tasks are sorted taking into account Jira priority + sprint deadline + Slack urgency
- "Waiting on you" items (review, approve) with a note on how long and who it blocks
- Due date from Jira is visible next to the task

---

## 4. Meeting Prep

### Enrichment by attendees

**Without Jira:** For each attendee: communication style, recent Slack activity, open tracks, inbox items.

**With Jira — added:**
- Attendee's current tasks: open issues, overdue, blocked
- Workload: "10 open issues, 28 SP — may be overloaded"
- Shared issues: tasks where 2+ meeting attendees are involved

### Enriching talking points

**Without Jira:** Talking points from Slack tracks and digest decisions.

**With Jira:**
- "Discuss PROJ-142 — overdue 1 day, @Пётр waiting for review from @Аня. Workaround discussed in #backend"
- "Sprint 24 at risk — discuss scope reduction for remaining blocked items"
- Blocked issues with path to resolution: "PROJ-301 blocked by PROJ-300 → ping @infra-team"

### Adaptation by meeting type

- **1:1:** Focus on attendee's tasks — blocked, overdue, workload. "Who to ping" about blockers
- **Standup:** Status of each participant's tasks + what changed since last time
- **Planning:** Iteration progress + backlog items + Slack context for each
- **Sprint review:** Delivered stories + Slack decisions for each + carry-over with reasons

---

## 5. People Cards

### Enriching profiles

**Without Jira:** Communication style, decision role, red flags, highlights — all from Slack analysis.

**With Jira — delivery dimension:**

**Accomplishments:**
- "Closed 5 issues this week, including critical bug fix PROJ-180"
- "Average cycle time 2.3 days (vs team avg 3.1) — fast executor"

**Red flags (compound):**
- "Workload 60% above average (28 SP vs 17 SP avg) + cycle time increasing +15% — risk of burnout"
- "4 overdue issues + 30% decrease in Slack activity — possible burnout"
- "Blocker to 3 other tasks (PROJ-301, PROJ-302, PROJ-303) — bottleneck"

**Communication guide:**
- "Reference specific Jira tickets — @Пётр responds faster to 'how's PROJ-142?' than to 'how's it going?'"
- "With 28 SP open — if assigning new work, first discuss deprioritization"

**Expertise mapping:**
- From Jira components + labels + Slack channels activity → "Expert in: caching, API design, payments"
- For onboarding: "For payment questions → @Пётр (epic owner, 15 tasks)"

---

## 6. Channel Digests

### Enriching discussions

**Without Jira:** Digest = summary + topics + decisions + action items from Slack messages.

**With Jira:**

**Decisions linked to tasks:**
- "Decided to use Redis for caching → **PROJ-250 [In Development, @CurrentUser]**"
- The connection is visible: decision in Slack → task in Jira → who's doing it → what status

**Action items with Jira context:**
- "Review PR for payment fix → **PROJ-142 [Code Review, overdue 1 day]**"
- Action item from Slack enriched with Jira status — visible whether it's stuck

**"No task" detection:**
- "⚠️ Discussion of logger refactoring (12 messages, @Аня and @Дима) — no Jira task found"
- Helps PM and TL not lose work: discussed → should be in Jira

**Running summary enriched:**
- "Actively discussed in this channel: PROJ-250 (caching, in progress), PROJ-301 (OAuth, blocked)"
- Next digest takes into account the Jira context of the previous one

---

## 7. AI Chat

### New types of questions

**About tasks:**
- "What are my open tickets?" → Jira issues + Slack context for each
- "What's up with PROJ-142?" → Jira status + all Slack discussions + decisions made
- "Which tasks are overdue?" → List with reasons from Slack

**About people:**
- "Who is overloaded?" → Workload table + compound signals (Jira + Slack + Calendar)
- "Who should I ask about caching?" → Jira expertise (components/labels) + Slack activity

**About progress:**
- "How's Sprint 24 going?" → Progress + forecast + blocked items + Slack context
- "Will we make the release?" → Epic progress + velocity forecast + risks
- "Which initiatives are at risk?" → Epics behind schedule + blocked + Slack sentiment

**About dependencies:**
- "What's blocking PROJ-301?" → Jira issue links + Slack discussions + who can unblock
- "What decisions were made on the payments epic?" → Decisions from Slack linked to the Jira epic

**Cross-source:**
- "Why is PROJ-142 delayed?" → Jira status history + Slack discussions + People Card assignee → compound answer

---

## 8. Weekly Trends

### Enriching trends

**Without Jira:** Trending topics, key decisions, hot discussions — all from Slack.

**With Jira:**

**Epic Progress:**
- "User Auth: 75% done (+15% this week), on track"
- "Payment v2: 45% done (+5%), at risk — 3 blocked, velocity insufficient"
- "Performance: 30% done (+10%), 2 blocked"

**Delivery forecast:**
- "Payment v2 (deadline: May 1): at the current velocity need 3 more weeks → won't make it"

**Risk changes (what changed):**
- "New risk: Performance epic — 2 tasks blocked since Wednesday"
- "Improvement: User Auth unblocked PROJ-305, pace increased"

**Strategy-to-Execution gaps (for Director):**
- "Initiative 'Mobile App' discussed in Slack for 2 weeks (chain, 40 messages) — no Jira epic created"

---

## 9. Workload Dashboard (new feature)

### For EM: team workload

Table for each team member:

| Who | Open | SP | Overdue | Blocked | Cycle Time | Slack Volume | Meetings | Signal |
|-----|------|----|---------|---------|-----------|-------------|----------|--------|
| @Пётр | 10 | 28 | 2 | 0 | 3.2d ↑ | 45/day | 5/day | ⚠️ Overload |
| @Аня | 5 | 15 | 0 | 0 | 2.1d | 20/day | 3/day | ✅ Normal |
| @Дима | 2 | 5 | 0 | 1 | 1.5d | 8/day | 2/day | 💤 Low load |

Compound signals (Jira + Slack + Calendar):
- ⚠️ Overload: many tasks + high Slack volume + many meetings
- 💤 Low load: few tasks + low activity — available capacity or blocked?
- 🔴 Burnout risk: overload + evening activity + cycle time increasing

---

## 10. Blocker Map (new feature)

### For PM/EM: all blockers in one place

Each blocked issue with compound context:

**PROJ-301** "OAuth integration" [Blocked 3 days]
- Blocked by: PROJ-300 (@Infra-team)
- Slack context: @Дима escalated in #infra 2 days ago, no response
- Impact: blocks PROJ-302, PROJ-303 → Epic "User Auth" at risk
- **Who can unblock:** @Лид-infra (decision-maker) or @CTO (escalation)

**PROJ-142** "Fix payment bug" [Stale in Code Review 5 days]
- Waiting for: review from @Аня
- Slack context: @Пётр mentioned 3 times in #backend, @Аня did not respond
- Impact: overdue 1 day, blocks release
- **Who can unblock:** @Аня (reviewer) or assign another reviewer

---

## 11. Write-Back Suggestions (new feature)

### Slack → Jira: one-click suggestions

**New task:**
- "Discussed adding rate limiting in #backend (Track #45) — no Jira task found"
- → "Create in BACK? Title: 'Add API rate limiting'. Type: Task. Labels: [api, performance]"
- User: ✅ Create / ❌ Skip

**Comment on a task:**
- "In #backend decided to use Redis for PROJ-250 (Decision from Digest #78)"
- → "Add a comment to PROJ-250? 'Decision: Redis instead of in-memory. Participants: @Пётр, @Аня. Rationale: ...'"
- User: ✅ Add comment / ❌ Skip

**Task update:**
- "Discussion in #backend: approach for PROJ-350 changed (CDN instead of cache)"
- → "Update PROJ-350? Add comment: 'Approach changed to CDN. Context: ...'"
- User: ✅ Update / ❌ Skip

**Principle:** Never automatic. Always preview + confirmation.

---

## Summary: what changes in each feature

| Feature | Before (Slack only) | After (Slack + Jira) |
|---------|---------------------|----------------------|
| **Briefing** | Slack signals | + tasks, sprint, blockers, workload, overdue |
| **Tracks** | Action items from Slack | + linked to Jira, out of sync, "no task" detection |
| **Tasks** | Personal todo from Slack | + Jira tickets, unified list, auto-sync status |
| **Meeting Prep** | Slack context of attendees | + Jira tasks, workload, blocked items per attendee |
| **People Cards** | Communication profile | + delivery metrics, expertise, workload signals |
| **Digests** | Discussion summary | + linked to tasks, "no task" detection |
| **AI Chat** | Questions about Slack | + questions about tasks, progress, blockers, workload |
| **Weekly Trends** | Topics and decisions | + Epic Progress, forecast, risk changes, execution gaps |
| **Workload** | None | NEW: team workload with compound signals |
| **Blocker Map** | None | NEW: all blockers with context and path to resolution |
| **Write-Back** | None | NEW: Slack → Jira suggestions with confirmation |
