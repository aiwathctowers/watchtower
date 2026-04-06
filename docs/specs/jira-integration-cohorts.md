# Spec: Jira Integration into Watchtower — Cohort Analysis

## Context

Watchtower is an AI platform for analyzing Slack communications. Current integrations: Slack (primary), Google Calendar (secondary). Jira adds a "system of record" — formal work tracking that complements informal discussions from Slack.

---

## Cohort 1: Engineering Manager / Team Lead — CRITICAL

**Profile:** Coordinates the team, tracks delivery, conducts 1:1s. Uses in Watchtower: Briefing, Tracks, Tasks, People Cards, Meeting Prep.

**Key Pain Points:**
- **Blind spot on work status:** Tracks show "what's being discussed in Slack" but not "what's actually in progress." Manager sees a track "Петр promised to fix a bug" but doesn't see that PROJ-123 has been In Review for 3 days and the sprint ends tomorrow.
- **No visibility into team workload:** Can't see who is overloaded (12 open issues + constant mentions) and who is idle. No data for balancing — task redistribution happens by gut feel, not by data.
- **Stuck tasks and missed deadlines:** Learns about stuck tasks at standup or retro, not proactively. Overdue tickets pile up unnoticed — there's no unified picture of "what's on fire right now."
- **No objective performance picture:** People Cards show communication profile but not delivery metrics. Impossible to tell who is systematically overworking (many tasks, high cycle time, evening activity) and who is underloaded.

### Jobs-to-be-Done

**Functional:**
1. "When I come to the morning standup, I want to see the current Jira status of my team's tasks alongside Slack discussions, so I don't have to switch between the Jira board and Watchtower and can understand where things stand in 2 minutes."
2. "When I get a track of type `code_review` or `approval`, I want to see the linked Jira issue (key, status, priority, sprint) so I can understand context and prioritize without opening Jira."
3. "When a task has been stuck in a status for more than N days, I want to get a proactive signal in the Daily Briefing (Attention) so I can unblock the team in time."
4. "When I'm planning a sprint or redistributing tasks, I want to see the workload for each team member (open issues, story points in progress, overdue count, meetings/day) so I can balance work based on data, not gut feel."
5. "When a task deadline is approaching or has been missed, I want to see it in the Briefing with context — why it's stuck (from Slack) and who can help, so I can react before it becomes critical."

**Emotional:**
6. "When leadership asks 'how's the sprint going?', I want to feel confident because Watchtower has already correlated Slack discussions with Jira progress."
7. "When I see that an employee is overworking (many tasks + high Slack activity + evening messages), I want to react before it leads to burnout."

**Social:**
8. "When I conduct a 1:1, I want to look like a manager who's in the loop — seeing the employee's open tasks, blockers, and Slack activity related to them."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As an EM, I want to see Jira status (key, status, assignee, sprint) in each track of type task/code_review/bug_fix | **Must-have** |
| 2 | As an EM, I want Briefing → Attention to contain signals: stuck >3 days, unassigned, approaching deadline, overdue | **Must-have** |
| 3 | As an EM, I want Meeting Prep for standup to show each participant's Jira tasks + Slack activity for each | **Must-have** |
| 4 | As an EM, I want to see a **Workload Dashboard** for the team: for each employee — open issues count, total SP in progress, overdue count, avg cycle time, Slack activity — to see who is overloaded and who is underloaded | **Must-have** |
| 5 | As an EM, I want Briefing → Team Pulse to contain compound overload signals: "Петр: 12 open issues + 40 messages/day + 6 meetings — risk of burnout" and underload signals: "Дима: 2 open issues, low Slack activity — possibly blocked or needs help" | **Must-have** |
| 6 | As an EM, I want to see a **Deadline Heatmap**: all team tasks with approaching/missed deadlines, sorted by criticality, with Slack context (why it's stuck) | **Must-have** |
| 7 | As an EM, I want to ask AI Chat "what's the status of PROJ-1234?" and get Jira + Slack context | **Should-have** |
| 8 | As an EM, I want People Cards to include "Workload & Delivery": tasks closed, cycle time, current load (open issues/SP), trend (rising/falling), overwork/underload signals | **Should-have** |
| 9 | As an EM, I want Tasks to automatically get a Jira link if AI found a Jira key mention | **Should-have** |
| 10 | As an EM, I want to see in Weekly Trends sprint burndown + workload distribution trend (who closed how much, how workload changed) | **Should-have** |

### Workday Scenario

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 08:30 | Daily Briefing | **Attention**: "PROJ-142 (@Пётр, Bug, High) — In Progress for 5 days. 2 overdue tasks: PROJ-180 (deadline yesterday), PROJ-195 (deadline today)." **Team Pulse**: "Пётр is overloaded: 10 open issues (28 SP) + 45 messages/day + 5 meetings. Дима is underloaded: 2 open issues (5 SP), low activity — consider redistribution" | Problems + workload imbalance in 1 min |
| 09:00 | Meeting Prep (standup) | Each participant's Jira tasks + Slack activity. Workload: Пётр 28 SP / Аня 15 SP / Дима 5 SP. Talking point: "Redistribute PROJ-195 from Пётр to Дима" | Preparation + balancing decision |
| 11:00 | Inbox → track "code_review" | "Review PR #456 for PROJ-203" — Jira: In Review, High, Sprint 24, deadline tomorrow | Prioritization with sprint context |
| 14:00 | Meeting Prep (1:1 with Пётр) | People Card: 5 tasks closed this week, but 10 open, cycle time +30%, evening Slack activity. Signal: "Possible overwork — workload 60% above average." Recommendation: discuss prioritization and delegation | 1:1 focused on the real problem |
| 15:30 | Workload Dashboard | Team table: name → open issues → SP in progress → overdue → avg cycle time → Slack volume → meetings/day. Imbalance is visually obvious. Red zones: Пётр (overloaded), green: Дима (capacity available) | Data for task redistribution |
| 16:30 | AI Chat: "What about the DB migration?" | 3 tasks in epic: 1 Done, 1 In Progress (stuck 4 days — in Slack Дима wrote about an index issue), 1 To Do | Full picture + reason for delay |

### Pain-Gain Matrix

| Pain (without Jira) | Gain (with Jira) | Benefit |
|---------------------|------------------|---------|
| Track without task context — unclear what's blocking | Track enriched: Jira key, status, sprint, deadline | 20-30 min/day |
| Briefing doesn't know about stuck tasks and deadlines | Briefing → Attention: stuck tasks, overdue, approaching deadline with Slack context (why it's stuck) | Blockers discovered 1 day earlier, overdue visible immediately |
| **No visibility into team workload** — redistribution by gut feel | Workload Dashboard: open issues, SP, overdue, cycle time, Slack volume per person. Compound signals in Team Pulse | Data-driven balancing. Burnout prevention |
| **Can't see who's overworking** — finds out when it's too late | People Cards + Team Pulse: compound signal (Jira workload + Slack volume + Calendar meetings + evening activity) → "risk of burnout" | Early overwork detection: weeks instead of months |
| **Can't see who's underloaded** — capacity is wasted | Workload Dashboard highlights low-load employees + available capacity | Efficient resource utilization |
| Meeting Prep without work context | Jira tasks + workload + overdue for each participant | 1:1 with a concrete action plan |

### Success Criteria
- >70% of tracks of type task/code_review/bug_fix auto-linked to Jira
- Standup prep time: ~1 min (vs ~5 min)
- >50% of Briefings contain at least 1 Jira signal in Attention
- Manager stops opening the Jira board in the morning
- Workload imbalance >2x between team members is highlighted in Team Pulse
- Overdue tasks are discovered on the day the deadline is missed, not at the retro
- Overwork cases are identified via compound signals (Jira + Slack + Calendar) before escalation

---

## Cohort 2: Product Manager / Product Owner — CRITICAL

**Profile:** Manages backlog, plans sprints, tracks initiatives. Uses: Tracks, Decisions, Weekly Trends, Meeting Prep, Briefing.

**Key Pain Points:**
- **Gap between discussions and actual progress:** PM lives in Jira (epics, stories, velocity), but Watchtower only shows "X was discussed in Slack." No unified picture of "what's being discussed" + "what's actually being done."
- **No deadline control at the product level:** Can't see in one place all deadlines for epics/releases, which ones are slipping and why. Learns about timeline risks when it's already too late — at a status meeting or from stakeholders.
- **Blockers are hidden deep down:** A task is blocked in Jira, but the reason is in a Slack discussion (waiting for vendor response, architecture debate, no design). PM can't see the compound picture: "what's blocked + why + how long + who can unblock."
- **No proactive delivery control:** PM reacts to problems instead of anticipating them. No early warning: "at the current velocity the epic won't make the deadline" or "3 out of 5 critical stories are blocked — the release is at risk."

### Jobs-to-be-Done

**Functional:**
1. "When I'm preparing a status update, I want to see epic progress alongside Slack context (decisions, blockers) so I can put the picture together in 5 minutes instead of 30."
2. "When I see a track 'decision_needed', I want to know which Jira issue/epic it relates to so I can escalate properly."
3. "When preparing for sprint planning, I want Weekly Trends linked to epics — what was discussed, where the blockers are, what was closed."
4. "When an epic/release deadline is approaching, I want to see a forecast of 'on track / not on track' based on current velocity and remaining scope, so I can adjust scope or resources in advance."
5. "When tasks are blocked, I want to see the full blocker picture: what's blocked in Jira + why (from Slack) + how long + who can unblock, so I can escalate precisely rather than figuring it out in a meeting."
6. "When I'm tracking product delivery, I want to see a Timeline/Roadmap view: all epics with deadlines, current progress, at-risk signals — to manage stakeholder expectations proactively."

**Emotional:**
7. "When the CTO asks 'where are we on Q2 OKR?', I want to confidently answer based on data — progress, risks, forecast."
8. "When a deadline is burning, I want to feel in control — see exactly what needs to be done to make it, rather than panicking from uncertainty."

**Social:**
9. "When talking to the team, I want to operate with specific issue numbers and blockers, showing that I'm aware of the implementation and helping, not just asking 'how's it going?'."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As a PM, I want to see Jira epics and progress (% done, blocked count, deadline) in Tracks and Chains | **Must-have** |
| 2 | As a PM, I want Briefing → Attention to contain **at-risk signals**: epics with deadline <7 days and progress <80%, blocked critical path stories, overdue tasks | **Must-have** |
| 3 | As a PM, I want to see a **Blocker Map**: all blocked tasks across my epics with compound context (Jira status + Slack reason + duration + who can unblock) | **Must-have** |
| 4 | As a PM, I want an "Epic Progress & Forecast" section in Weekly Trends: progress + forecast "on track / not on track" based on velocity | **Must-have** |
| 5 | As a PM, I want Decisions in Digests to be linked to Jira issues | **Must-have** |
| 6 | As a PM, I want a **Delivery Timeline**: all epics/releases with deadlines, progress, status (on track / at risk / behind) in one place | **Should-have** |
| 7 | As a PM, I want to ask AI Chat "will we make the payment release?" and get a forecast with data | **Should-have** |
| 8 | As a PM, I want Briefing → "What Happened" to group by epics with emphasis on movement toward deadlines | **Should-have** |
| 9 | As a PM, I want Meeting Prep for planning to show unfinished epics + blocked items + Slack context | **Should-have** |
| 10 | As a PM, I want to receive a **weekly risk digest**: what changed over the week regarding timelines — which epics accelerated, which slowed down, new blockers | **Should-have** |

### Workday Scenario

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 09:00 | Daily Briefing | **Attention**: "Epic User Auth (deadline: Apr 20) — 2 tasks blocked >2 days, at current velocity we won't make it. Payment v2 (deadline: May 1) — on track, but PROJ-310 overdue." **What Happened** by epics with emphasis on movement toward deadlines | Timeline risks + blockers in 1 min |
| 09:30 | Blocker Map | All blocked tasks by epic: PROJ-301 (blocked 3 days, reason from Slack: waiting for OAuth provider response, @Дима escalated). PROJ-310 (blocked 1 day, depends on PROJ-301). Who can unblock: @Дима → vendor, PM escalation needed | Targeted escalation instead of "we'll figure it out in the meeting" |
| 10:00 | Meeting Prep (sync with designers) | Talking points linked to tasks + deadlines: "PROJ-340 [To Do, needed by Apr 15] — @Маша proposed 2 options, no decision made — blocks development start" | Agenda focused on timelines |
| 11:30 | Tracks → "decision_needed" | Each enriched: "Caching for PROJ-250 [High, Sprint 24, Epic: Performance, deadline: Apr 25]. Blocks 2 downstream tasks." | Decision prioritization by impact on delivery |
| 14:00 | Weekly Trends | **Epic Progress & Forecast**: User Auth 60% (+15%), forecast: at risk (velocity 8 SP/week, need 15 SP in 2 weeks). Payment 80%, on track. Performance 30%, behind (3 blocked). **Risk changes**: Payment accelerated (+10%), Auth slowed down (-5%) | Forecast + trends in 5 min |
| 15:00 | AI Chat: "Will we make the auth release?" | "At current velocity (8 SP/week) and remaining scope (15 SP) — no, need ~2 more weeks. 2 tasks blocked (reasons: ...). Options: 1) unblock PROJ-301 → +3 SP/week, 2) cut scope by 2 stories (low priority), 3) add a resource" | Data-driven decision instead of panic |
| 16:30 | Delivery Timeline | All epics: Auth (Apr 20, at risk), Payment (May 1, on track), Performance (May 15, behind). Visually obvious where it's burning | Stakeholder expectation management |

### Pain-Gain Matrix

| Pain (without Jira) | Gain (with Jira) | Benefit |
|---------------------|------------------|---------|
| Tracks without hierarchy — scattered action items | Grouped by epics with progress and deadlines | 25-35 min/day |
| **No deadline control** — learns about risks when it's too late | Briefing: at-risk epics + forecast "on track / not on track." Delivery Timeline with deadlines | Risks visible 1-2 weeks before a miss |
| **Blockers are hidden** — can't see the reason and resolution path | Blocker Map: blocked task + reason from Slack + duration + who can unblock | Escalation in hours, not days |
| **No delivery forecast** — PM reacts instead of anticipating | Epic Forecast: velocity vs remaining scope → "on track / need N weeks / cut scope" | Proactive scope and resource management |
| Weekly Trends not linked to the backlog | Epic Progress & Forecast + risk changes for the week | Sprint planning 15 min faster |
| Decisions "get lost" between Slack and Jira | Divergence highlighting | Lost decisions: -50% |

### Success Criteria
- Status update <5 min (vs ~30 min)
- >80% of Decisions linked to Jira
- PM stops manually assembling status from Jira + Slack
- At-risk epics are highlighted in Briefing >7 days before the deadline
- All blocked tasks are visible in Blocker Map with reason and path to resolution
- PM answers "will we make it?" in 30 seconds, not after a 30-minute analysis
- Weekly risk digest shows trends: what accelerated, what slowed down, new blockers

---

## Cohort 3: Tech Lead / Staff Engineer — HIGH

**Profile:** Architectural decisions, cross-team coordination, tech debt. Uses: Tracks, Decisions, Chains, People Cards, Meeting Prep.

**Key Pain Points:**
- **Decisions from Slack don't make it into Jira:** Decision "we're using Kafka" sits as a track in Watchtower, but the epic with 15 tickets and 3 blockers isn't visible. The link between discussion and implementation lives in the TL's head.
- **Discussions and changes stay in Slack — problems follow:** In Slack they discussed changing the approach for PROJ-350 ("let's use CDN, not cache"), but in Jira the task still has the old description. A developer picks up the task a week later — builds it per the old description. Or a new track "need to add rate limiting" was discussed, everyone agreed — but nobody created a Jira issue, and the work was lost.
- **Write-back gap:** Watchtower extracts tracks, decisions, action items from Slack — but it's a "shadow" system. The real system of record is Jira. Without a reverse flow (Slack → Jira), information diverges: one thing in Slack, another in Jira, a week later nobody remembers what's current.
- **Architectural decisions are not formalized:** TL made a decision in a 50-message Slack thread. A month later a junior asks "why is it this way?" — the answer is buried in Slack. The decision should land in a Jira issue/comment as a formal record.

### Jobs-to-be-Done

**Functional:**
1. "When tracking architectural decisions through Chains, I want to see affected Jira issues to understand the impact."
2. "When a 'code_review' track comes in, I want to see Jira context (description, AC, related issues) to give a quality review."
3. "When looking at Digests, I want discussions linked to tasks — what's being implemented, what's stuck."
4. "When a new action item or decision about an existing task appears in Slack, I want it to automatically land in Jira (new ticket or comment) so Jira stays current without manual transfer."
5. "When requirements or approach changes for a task are discussed in Slack, I want Watchtower to suggest updating the Jira description/comment so the next developer picking up the task works with current information."
6. "When an architectural decision is made in a 50-message Slack thread, I want AI to compose a concise ADR (Architecture Decision Record) and suggest recording it as a Jira comment on the related epic, so a month later you can find 'why we decided this way'."

**Emotional:**
7. "When a junior asks 'why did we choose this approach?', I want to say 'look in Jira' — not spend 20 minutes searching for a thread in Slack."

**Social:**
8. "When my area is discussed at a design review, I want to be confident that all decisions are formalized — there's a record in Jira, not just in someone's memory."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As a TL, I want Chains to contain links to Jira issues/epics — impact of decisions on the backlog | **Must-have** |
| 2 | As a TL, I want code_review tracks to show Jira description and acceptance criteria | **Must-have** |
| 3 | As a TL, I want **new tracks of type task/bug_fix/follow_up to suggest creating a Jira issue** if none is linked — one click "Create in Jira" with AI-filled title, description, labels | **Must-have** |
| 4 | As a TL, I want **decisions from Slack to be automatically added as comments** to linked Jira issues — with decision summary, participants, link to Slack thread | **Must-have** |
| 5 | As a TL, I want that when **requirement changes** for an existing task are discussed in Slack, Watchtower suggests "Update PROJ-350? New context: [summary]" — and upon confirmation adds a comment to Jira | **Must-have** |
| 6 | As a TL, I want Digests to highlight **"discussion without a Jira issue"** for substantial work — with a "Create issue" button | **Should-have** |
| 7 | As a TL, I want AI to compose a **concise ADR** from Slack discussion (context, options, decision, rationale) and suggest recording it in the Jira epic as a comment | **Should-have** |
| 8 | As a TL, I want AI Chat to find "all tasks related to API refactoring" from Jira + Slack | **Should-have** |
| 9 | As a TL, I want Briefing to contain Bugs in Reopened status as attention items | **Should-have** |

### Workday Scenario

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 09:00 | Briefing | **Attention**: "PROJ-410 (Bug, Critical) reopened. Chain 'API v3' — 2 decisions linked to tasks. **Slack→Jira gaps**: 3 discussions from yesterday not reflected in Jira" | Regressions + what will be lost if not recorded |
| 09:30 | Slack→Jira suggestions | Watchtower suggests: 1) "Yesterday in #backend the team decided to use CDN instead of cache for PROJ-350 → Add comment to PROJ-350?" 2) "New action item: add rate limiting → Create issue in Epic Performance?" | One click — Slack into Jira |
| 10:00 | Chains → "API v3 Migration" | 5 Jira issues + decisions from 3 channels. Each decision marked: ✅ recorded in Jira / ⚠️ only in Slack | Visible what's formalized and what isn't |
| 11:30 | Track "code_review" | "Review PR #789 for PROJ-350". Jira description + AC + **Slack update**: "approach changed to CDN (yesterday, #backend)". Suggestion: "Update PROJ-350 description?" | Review with up-to-date context |
| 14:00 | Design review → Meeting Prep | Talking points include decisions from Slack marked "formalized / not formalized". ADR suggestion: "Decision on event sourcing — compose ADR in PROJ-400?" | Decisions don't get lost after the meeting |
| 16:00 | AI Chat: "What tech debt tasks were discussed?" | Jira label=tech-debt + Slack discussions. **Without issues**: "Logger refactoring discussion (12 messages, #backend) — Jira issue not created" → "Create" button | Full picture + nothing is lost |

### Pain-Gain Matrix

| Pain | Gain | Benefit |
|------|------|---------|
| **Discussions and changes stay in Slack** — a week later a developer builds per the old description | Watchtower suggests updating Jira: comment or description edit. One click | No Slack vs Jira divergence. Hours saved on rework |
| **New action items get lost** — discussed, agreed, forgot to create an issue | Track without Jira → suggestion "Create issue?" with AI-filled title/description/labels | Lost tasks: ~0%. 5-10 min saved on manual creation |
| **Decisions are not formalized** — a month later nobody remembers why | Decisions → Jira comment with summary, participants, rationale. ADR for architectural decisions | "Why is it this way?" → "Look in Jira" in 30 sec instead of 20 min searching Slack |
| Chains without link to implementation | Chains + Jira issues + formalization status (✅/⚠️) | 10-15 min/day |
| Code review without up-to-date context | Track with description + Slack updates + suggestion to update Jira | Review based on current information |

### Success Criteria
- >80% of decisions from Slack land in Jira as comments (automatically or in 1 click)
- >90% of new tracks of type task/bug_fix without a Jira issue get a "Create in Jira" suggestion
- Cases of "built per old description" → ~0 (Jira is up to date)
- TL spends <1 min formalizing a decision (vs 5-10 min manual transfer)
- Architectural decisions are found in Jira in <30 sec

---

## Cohort 4: Individual Contributor / Developer — HIGH

**Profile:** Code, review, participation in discussions. Uses: Inbox, Tasks, Digests, AI Chat, Search.

**Key Pain Points:**
- **Changes come in Slack, not in Jira:** In Slack people write "also add validation" or "approach changed, now we're doing it via CDN" — this doesn't make it into the issue. The developer works per the Jira description, then it turns out it's outdated. Rework, frustration.
- **Don't know where I'm needed:** Someone mentioned me in a thread, someone assigned a review, someone DM'd "check out PROJ-350" — everything is scattered across Slack inbox, Jira notifications, email. There's no single place showing "here's what's expected from you right now."
- **Don't know who to ping when blocked:** The task is blocked, but it's unclear who can help — the assignee of the dependent task? The TL? The person who discussed this in Slack? You have to dig through Jira links + Slack threads to find the right person.
- **Switching between systems:** In the morning I check Watchtower (Slack context) + Jira (my tickets) + email (review requests) separately. Tasks in Watchtower duplicate Jira tickets without a link.

### Jobs-to-be-Done

**Functional:**
1. "When I start my day, I want to see a **single actionable list**: my Jira tasks + who's waiting for my input (review, response, decision) + my blockers and who to ping — all in one Briefing."
2. "When changes/clarifications about my task come in Slack, I want them to be **automatically linked to the Jira issue** (comment or update) so I always work with the current description rather than gathering context from 5 threads."
3. "When I'm blocked, I want Watchtower to show **who to ping**: the assignee of the blocking task, who discussed this topic in Slack, who can make a decision — with a direct link and context."
4. "When someone is waiting for my input (review, response, approve), I want to see it as a **prioritized list** with context: what issue it is, how long they've been waiting, whether it's blocking someone."
5. "When a 'task' track comes in, I want to know if it's linked to Jira — to avoid duplicating work."

**Emotional:**
6. "I don't want to feel chaos from 15 tracks + 8 Jira issues + 20 mentions — I want to see a prioritized list and calmly work through it in order."
7. "When changes come in Slack, I want to be confident they won't be lost — Watchtower will catch them and link them to the issue."

**Social:**
8. "When my manager asks 'what are you working on?', I want to quickly respond with specifics. When a colleague is waiting for my review — I don't want to forget and look like the one slowing things down."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As an IC, I want to see in Briefing → Your Day a **single list**: my sprint Jira tasks + "Waiting on you" (review requests, mentions with questions, approvals) with priority and context "how long they've been waiting, is it blocking someone" | **Must-have** |
| 2 | As an IC, I want **changes and clarifications from Slack** about my tasks to be automatically linked to Jira (comment with summary + link to thread) so the task description stays current | **Must-have** |
| 3 | As an IC, I want tracks to auto-link to Jira by PROJ-XXX mention | **Must-have** |
| 4 | As an IC, I want to see **"Who to ping"** when blocked: assignee of the blocking task, who discussed the topic in Slack, who's the decision-maker (from People Card) — with a direct link | **Must-have** |
| 5 | As an IC, I want to see **"Waiting on you"** as a separate section/filter: review requests + questions + approvals, sorted by "how long they've been waiting" and "is it blocking someone" | **Must-have** |
| 6 | As an IC, I want AI Chat "what about PROJ-123?" with Jira + Slack context + all Slack changes collected together | **Should-have** |
| 7 | As an IC, I want Meeting Prep for 1:1 with my blocked tasks + "who to ping" as talking points | **Should-have** |
| 8 | As an IC, I want Watchtower to highlight "PROJ-250: 3 new Slack comments today — check, requirements may have changed" | **Should-have** |

### Workday Scenario

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 09:30 | Briefing → Your Day | **My tasks**: PROJ-250 [In Progress, High] — 2 new Slack clarifications linked. PROJ-260 [To Do]. **Waiting on you**: Review PR #456 for PROJ-203 (@Аня has been waiting 2 days, blocks her sprint task). Reply to @Пётр about the caching approach (yesterday in #backend). **My blockers**: PROJ-250 depends on PROJ-251 (@Дима, In Progress) → ping @Дима | Everything in one place. I know what to do, who's waiting, who to ping |
| 10:00 | Opens PROJ-250 | Sees Jira description + **Slack updates**: "Yesterday @TL wrote: 'add rate limiting' (#backend, 15:30). Today @PM clarified: 'limit 100 req/min' (#product, 09:15)". All already as Jira comments | Working from the current description, not an outdated one |
| 11:00 | Blocked on PROJ-250 | Watchtower shows: "PROJ-250 blocked by PROJ-251 (assignee: @Дима, In Progress 2 days). In Slack @Дима wrote that he's waiting for data from @Infra-team (#backend, yesterday). **Ping**: @Дима (assignee) or @Лид (decision-maker for infra)". Button "Message @Дима" | Knows exactly who to ping and with what context. Doesn't spend 10 min searching |
| 14:00 | Inbox → "Waiting on you" | 3 items: 1) Review for @Аня (2 days, blocking). 2) Reply to @Пётр (yesterday, not blocking). 3) Approve design doc (today, @PM is waiting). Sorted by impact | Doesn't forget, doesn't slow down colleagues |
| 16:00 | Meeting Prep (1:1) | **My blockers**: PROJ-250 → who to ping. **Changes this week**: 5 Slack clarifications on 2 tasks — all in Jira. **Waiting on me**: 1 overdue review | Prepared 1:1 with specifics |
| 17:00 | Briefing notification | "PROJ-250: @Дима updated PROJ-251 → In Review. Your blocker will be resolved soon" | Proactive notification without monitoring |

### Pain-Gain Matrix

| Pain | Gain | Benefit |
|------|------|---------|
| **Changes in Slack don't make it to Jira** — working from outdated description, rework | Slack clarifications automatically → Jira comments. "3 new updates" highlighted | No rework. Hours saved per task |
| **Don't know where I'm needed** — scattered across Slack/Jira/email | "Waiting on you": single prioritized list (reviews, responses, approvals) + "how long" + "blocking someone?" | Don't slow down colleagues. 10-15 min/day |
| **Don't know who to ping when blocked** — digging through Jira links + Slack | "Who to ping": assignee + Slack context + decision-maker from People Card | Unblocked in minutes, not hours |
| Morning: Watchtower + Jira separately | Single "Your Day": tasks + waiting on me + blockers | 5-10 min/morning |
| Track not linked to Jira | Auto-linking by PROJ-XXX | No duplication |
| Blocker in Slack — manager doesn't know | Auto-linking + highlight in manager's Briefing | Faster unblocking |

### Success Criteria
- >90% of Slack clarifications on tasks automatically linked to Jira (comment + link to thread)
- "Waiting on you" contains all pending requests with >85% accuracy
- When blocked, IC sees "who to ping" in <30 sec (vs 5-10 min searching)
- IC doesn't open the Jira board in the morning — Briefing is enough
- Cases of "built per old description" → ~0

---

## Cohort 5: Director / Leader — MEDIUM-HIGH

**Profile:** Strategic overview, team health, roadmap and release control. Uses: Briefing, Weekly Trends, People Cards, Team Summary.

**Key Pain Points:**
- **Briefing is subjective** — no velocity, sprint completion, blocked items. Only Slack "sentiment."
- **No release visibility:** Can't see in one place all planned releases, their status, what's included, and what's at risk of not making it. Learns about a release slip from a manager, not proactively.
- **Strategic initiatives are not linked to Jira:** In Slack they discuss the "payments initiative," in Weekly Trends the "payments topic" is visible — but there's no link to specific Jira epics, release versions, % completion. The director can't verify: is this even captured in Jira? Or is it just Slack conversations?
- **Gap detection:** A strategic decision was made ("we're building a mobile app"), discussed in Slack — but there's no epic in Jira, tasks aren't decomposed. The gap between strategy and execution isn't visible.

### Jobs-to-be-Done

**Functional:**
1. "When looking at Weekly Trends, I want to see progress on strategic epics linked to Slack decisions — and **release status**: what's planned, what's at risk, what's slipping."
2. "When preparing for an exec meeting, I want a **Release Dashboard**: all releases with dates, scope (epics/stories), progress, risks — to assemble the picture in 5 minutes."
3. "When looking at Team Summary, I want Jira metrics for teams (velocity, blocked, cycle time) alongside Slack signals."
4. "When a strategic decision is made, I want to see **whether it's in Jira**: is there an epic, is it decomposed, are owners assigned — or is it still just words in Slack."
5. "When a new initiative is discussed in Slack, I want to see its **lifecycle**: discussion → decision → Jira epic → decomposition → execution → release — and what stage it's at now."

**Emotional:**
6. "When the board asks 'when is the release?', I want to answer with data: date, scope, progress, risks — not 'let me check with the team.'"
7. "When I see that an initiative has been discussed in Slack for 2 weeks but there's nothing in Jira — I want to escalate it as an execution gap risk."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As a Director, I want a **Release Dashboard**: all Jira Fix Versions/Releases with dates, included epics, progress (% stories done), at-risk items — to see roadmap execution | **Must-have** |
| 2 | As a Director, I want "Strategic Initiatives" in Weekly Trends — epic progress + Slack + link to releases | **Must-have** |
| 3 | As a Director, I want Jira signals in Briefing → Attention: at-risk releases, >30% blocked, deadlines <7 days | **Must-have** |
| 4 | As a Director, I want to see **Strategy-to-Execution gaps**: initiatives from Slack (Chains/Tracks) that have no Jira epic — "discussed for 2 weeks, Jira issue not created" | **Must-have** |
| 5 | As a Director, I want AI Chat "how's the Q2 roadmap going?" with data: releases, epics, progress, risks, Slack context | **Should-have** |
| 6 | As a Director, I want "Jira health" in Team Summary (velocity trend, blocked rate, release burndown) | **Should-have** |
| 7 | As a Director, I want to see **Initiative Lifecycle**: Slack discussion → decision → Jira epic → in progress → release — what stage each initiative is at | **Should-have** |

### Workday Scenario

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 08:30 | Briefing | **Attention**: "Release v2.5 (Apr 25) — at risk: 3 of 5 epics not completed, Payment v2 40% blocked." **Strategy gaps**: "Initiative 'Mobile App' discussed in Slack for 2 weeks (chain, 40 messages) — Jira epic not created" | Releases + execution gaps in 1 min |
| 10:00 | Release Dashboard | **v2.5 (Apr 25)**: Auth 75% ✅, Payment 45% ⚠️ at risk, Performance 60% ✅. **v3.0 (Jun 1)**: Mobile App 0% ❌ (no epic!), API v3 30% ✅. Scope changes for the week: +2 stories in Payment, -1 story in Auth | Entire roadmap on one screen |
| 10:30 | Weekly Trends | **Strategic Initiatives**: linked to releases. "Payment v2 → Release v2.5. Velocity 8 SP/week, need 20 SP → won't make it. Key decision: chose Stripe (from Slack). Blocker: OAuth provider (3 days)." **Initiative Lifecycle**: Mobile App — stage: "Discussion" (no Jira). API v3 — stage: "Execution" (30% done) | Progress + forecast + gaps |
| 14:00 | AI Chat: "What about release 2.5?" | "Release v2.5 (Apr 25): 3 epics. Auth — on track (75%). Payment — at risk (45%, 2 blocked, insufficient velocity). Performance — on track (60%). Recommendation: cut Payment scope or push the release by 1 week. In Slack @PM discussed this yesterday in #product" | Data-driven decision in 30 sec |
| 15:00 | Strategy gaps review | List: 1) "Mobile App" — Chain active, 40 messages, 3 channels, decision made → **Jira: nothing**. 2) "Data Platform" — Track active, 2 weeks → **Jira: epic created, 0 stories**. 3) "Internal Tools" — discussion → **Jira: epic + 8 stories, 2 In Progress** ✅ | Visible where strategy = words vs where strategy = execution |
| 16:00 | Team Summary | By team: velocity trend, release contribution, blocked rate. "Backend: velocity -20%, main contributor to at-risk Payment v2. Frontend: velocity stable, Auth on track" | Team health in the context of releases |

### Pain-Gain Matrix

| Pain | Gain | Benefit |
|------|------|---------|
| **No release visibility** — learns about a slip from a manager | Release Dashboard: all releases, scope, progress, at-risk | Proactive management. Exec meeting prep: 5 min vs 30 |
| **Strategic initiatives not linked to Jira** — unclear execution status | Initiative Lifecycle: discussion → decision → Jira → execution → release | Visible what stage each initiative is at |
| **No gap detection** — decision made, but nothing in Jira | Strategy-to-Execution gaps: "discussed for N days, Jira epic not created" | Execution gaps visible in days, not months |
| Trends not linked to roadmap | Epic Progress linked to releases + forecast | Risk forecasting 1-2 weeks ahead |
| Team Summary without productivity | Jira health + release contribution per team | Holistic view of teams |
| Risks discovered late | At-risk releases and blocked epics in Briefing | 1-2 weeks earlier |

### Success Criteria
- All Jira releases visible in Release Dashboard with current progress
- At-risk releases highlighted in Briefing >7 days before the date
- Strategy-to-Execution gaps identified: initiative in Slack >7 days without a Jira epic → signal
- Director answers "when is the release?" in 30 sec with data
- Initiative Lifecycle covers >80% of active initiatives (Slack + Jira)

---

## Cohort 6: Onboarding (cross-role) — MEDIUM

**Profile:** First 1-3 months **in any role** — new IC, new TL, new PM, new Director. Onboarding is not a separate role but a **phase** for any of the previous cohorts. Each role has its own onboarding focus, but the common pain points are the same: unclear who is who, how work is organized, who to go to.

Uses: Digests, People Cards, AI Chat, Search + all features of their primary role.

**Key Pain Points:**
- **Don't know who's working with me on tasks:** I get a Jira task, but it's unclear who else is working on this epic, who's the reviewer, who tests, who depends on my result. In Jira — dry assignee/reporter fields, in Slack — unclear which threads relate to my task.
- **Don't know who to go to for information:** Need context on a task/project/architecture — unclear who the expert is. People Cards show communication style but not "this person is the owner of topic X and knows everything about Y."
- **Don't know who to resolve problems with:** Hit a blocker or have a question — who's the decision-maker? Who can help? Who discussed this before? A newcomer spends hours finding the right person.
- **Can't see what's stuck:** Don't understand which tasks in my area are stuck, what's overdue, where the gaps are — because I don't know the "normal" pace and context. Afraid to ask "is it normal that PROJ-350 has been in Review for a week?"
- **Context is fragmented:** Slack discussions aren't linked to Jira — a newcomer doesn't understand "as part of what work" a conversation is happening.

### Important: onboarding depends on the role

| Role | Onboarding Focus | What's Critical from Jira |
|------|------------------|--------------------------|
| **New IC** | My tasks, who's nearby, who to ping, how the workflow works | Assignees on my epic, reviewers, blockers, task dependencies |
| **New TL** | Architecture, decisions, tech debt, who owns what | Epics + owners, issue links, labels/components, decision history |
| **New PM** | Roadmap, epics, releases, velocity, who are the product stakeholders | Epics + releases + progress, sprint data, backlog structure |
| **New EM** | Team, workload, processes, who's overloaded, what's on fire | Team workload, sprint health, overdue/blocked per person |
| **New Director** | Strategic initiatives, releases, team health, execution gaps | Release dashboard, initiative lifecycle, team velocity |

### Jobs-to-be-Done

**Functional:**
1. "When I get a task, I want to see a **map of people around it**: who else is working on this epic (assignees), who tests (QA assignee), who reviews (reviewer/approver), who depends on my result (blocked by links) — to know my surroundings."
2. "When I need information on a topic, I want to ask AI Chat and get not only an answer but also **'ask @Аня — she's the owner of this epic and discussed it in #backend 3 times this week'** — to know who to go to."
3. "When I see a stuck task and don't understand if that's normal, I want to see **context**: the team's average cycle time, whether there's a blocker, whether it was discussed in Slack — to understand if I need to escalate or it's in progress."
4. "When getting to know the team/project, I want to see a **Project Map**: epics → owners → current status → key decisions from Slack — to understand the work structure in 15 minutes."
5. "When I hit a problem, I want to see **'Who can help'**: who discussed this topic in Slack + who's the decision-maker from People Card + who's the assignee of related Jira issues."

**Emotional:**
6. "I want to feel 'in the loop' by the end of the first week — understand who owns what, what's in progress, what the priorities are — not be lost for 3 weeks."
7. "I don't want to look like the one asking obvious questions — I want to check Watchtower first and go to the person already with context."

### User Stories

| # | Story | Priority |
|---|-------|----------|
| 1 | As a newcomer, I want to see for each of my tasks **"People around the task"**: who else is on this epic (assignees), who tests, who reviews, who depends on me, who can help — with People Card summary | **Must-have** |
| 2 | As a newcomer, I want AI Chat "tell me about PROJ-500" — and get: Jira description + history of Slack discussions + decisions made + **who's the expert on this topic** | **Must-have** |
| 3 | As a newcomer, I want to see a **Project Map**: epics in my area → owner of each → % done → key decisions → stuck tasks — to understand the landscape in 15 min | **Must-have** |
| 4 | As a newcomer, I want my tasks in "Your Day" with Slack context + flag if something is stuck (task in status >N days, no activity) | **Must-have** |
| 5 | As a newcomer, I want to see **"Who can help"** when blocked/with a question: topic expert (from Slack activity + Jira ownership) + decision-maker + who discussed it before | **Must-have** |
| 6 | As a newcomer, I want to see in People Cards: colleagues' tasks/epics + their expertise (by Jira labels/components + Slack channel activity) — to know who to go to for what topic | **Should-have** |
| 7 | As a newcomer, I want discussions linked to Jira in Digests — "this discussion is about PROJ-350 [In Review, @Аня]" | **Should-have** |
| 8 | As a newcomer, I want to see **"Stale tasks"** in my area: tasks with no activity for >N days, overdue, unassigned — to understand where the gaps are | **Should-have** |

### Workday Scenario (week 1, new IC)

| Time | Action | What They See | Value |
|------|--------|---------------|-------|
| 09:00 | Briefing → Your Day | **My tasks**: PROJ-500 [To Do] — onboarding task. **People around**: @Ментор (epic owner), @Аня (testing), @Дима (parallel task PROJ-501). **Stuck nearby**: PROJ-499 in Review for 5 days (assignee: @Петр) | Know my surroundings from day one |
| 10:00 | AI Chat: "Tell me about PROJ-500" | "PROJ-500: Set up dev environment. Epic: Onboarding (owner: @Ментор). Jira description: [...]. In Slack: @Ментор recommended Docker (#onboarding, 2 days ago). Decision in #infra: switched to Podman. **Expert**: @Лид-infra (discussed 5 times, owner of component 'infra')" | Full context + know who to go to |
| 11:00 | Project Map | **Epic "Onboarding"**: 3 tasks (1 mine, 2 others). **Epic "API v3"**: owner @TL, 12 tasks, 60% done. **Epic "Performance"**: owner @Аня, 8 tasks, 30% done, 2 blocked. Key decisions for each from Slack | Project landscape in 15 min |
| 14:00 | Hit a problem | Watchtower: **"Who can help"**: 1) @Дима — assignee of PROJ-501 (similar task, Done). 2) @Лид-infra — discussed setup in #infra (3 times). 3) @Ментор — epic owner, decision-maker | Found help in 1 min instead of 30 min asking around |
| 15:00 | People Cards — studying the team | @Аня: Staff Eng, owner of Epic "Performance", expertise: caching, profiling (Jira components + Slack channels). @Дима: Senior, Epic "API v3", expertise: REST, gRPC. @Петр: Mid, 3 open tasks, PROJ-499 stuck in Review | Know who owns what and who to go to for each topic |
| 16:00 | Stale tasks in my area | "PROJ-499 (Review, 5 days, @Петр) — no Slack activity for 3 days. PROJ-502 (To Do, unassigned, 2 weeks) — mentioned in #backend once." Context: team avg review time = 1.5 days | Understand where gaps are, can ask informed questions |

### Onboarding Scenario for Other Roles

**New PM (week 1):**
- Project Map → sees all epics, releases, % completion, owners
- Release Dashboard → understands the roadmap in 15 min
- AI Chat: "which initiatives are at risk?" → immediately aware of priorities
- People Cards → who's the product stakeholder, who's the TL for each epic

**New EM (week 1):**
- Workload Dashboard → sees each team member's workload
- People Cards → delivery metrics + communication style for each
- Stale tasks → what's stuck, where intervention is needed
- AI Chat: "what problems does the team have?" → compound signals

**New TL (week 1):**
- Chains → all architectural initiatives + Jira links + decisions
- Project Map → epics + tech debt + dependencies (issue links)
- AI Chat: "what architectural decisions were made in the last month?" → decisions + Jira context
- People Cards → who's the expert in which components (Jira components + Slack activity)

### Pain-Gain Matrix

| Pain | Gain | Benefit |
|------|------|---------|
| **Don't know who's working with me on tasks** — working in a vacuum | "People around the task": assignees, reviewers, QA, dependents | Know your surroundings from day one |
| **Don't know who to go to for information** — asking everyone randomly | "Who can help" + People Cards with expertise (Jira + Slack) | Right person in 1 min vs 30 min |
| **Don't know who to resolve problems with** — escalating to the wrong person | Decision-makers from People Cards + Jira owners + Slack experts | Correct escalation on the first try |
| **Can't see what's stuck** — afraid to ask | Stale tasks + avg cycle time for context "normal/abnormal" | Informed questions instead of silence |
| **Context is fragmented** | Project Map + AI Chat with Jira + Slack | Onboarding 1-2 weeks faster |
| **Onboarding = one-size-fits-all** — same for everyone | Jira data adapts to the role (IC sees tasks, PM — epics, EM — workload) | Each role onboards in its own way |

### Success Criteria
- Newcomer understands "who owns what" by end of day 1 (vs end of week 2)
- "Who can help" is relevant in >80% of cases
- Project Map covers >90% of active epics in the newcomer's area
- Stale tasks are identified from day one — newcomer can ask informed questions
- Time-to-productivity decreases by 30-40% (self-reported)
- Newcomer in a PM/EM/TL role gets role-specific Jira context from the first Briefing

---

## Cross-Cohort Synergy: Slack + Calendar + Jira Triangle

```
         SLACK (communications)
        /                    \
       /   Watchtower AI      \
      /    connects everything \
     /                          \
  JIRA (work)  ———————  CALENDAR (time)
```

### Compound Insights

| Signal | Slack | Jira | Calendar | Compound |
|---|---|---|---|---|
| Overload | 40+ messages | 12 open issues | 6 meetings | "Risk of burnout" |
| Blockage | "waiting for review" x5 | Blocked 3 days | — | "Red flag — no response" |
| Scope creep | New discussions | +3 stories mid-sprint | — | "Scope +20%" |
| Out of sync | Decision made | No ticket | — | "Jira issue not created" |
| Meeting prep | Tracks + people | Open/overdue issues | 1:1 in an hour | Full preparation |

---

## Feature Enrichment Prioritization by Cohort

| Watchtower Feature | EM | PM | TL | IC | Dir | New | Priority |
|--------------------|:--:|:--:|:--:|:--:|:---:|:---:|:--------:|
| Tracks → Jira key/status | +++ | ++ | +++ | +++ | + | ++ | **P0** |
| Briefing → Jira in Attention | +++ | +++ | ++ | ++ | +++ | + | **P0** |
| Briefing → Jira in Your Day | +++ | + | + | +++ | + | +++ | **P0** |
| Meeting Prep → Jira per attendee | +++ | ++ | +++ | ++ | ++ | + | **P1** |
| AI Chat → Jira in responses | ++ | +++ | +++ | +++ | ++ | +++ | **P1** |
| Digests → decisions linked to Jira | ++ | +++ | +++ | + | + | +++ | **P1** |
| Weekly Trends → Epic Progress | ++ | +++ | ++ | + | +++ | + | **P1** |
| Chains → Jira issues/epics | + | ++ | +++ | + | ++ | ++ | **P2** |
| People Cards → Jira metrics | +++ | + | ++ | + | +++ | ++ | **P2** |
| Tasks → Jira link | ++ | ++ | + | +++ | + | + | **P2** |
| Team Summary → Jira health | +++ | + | + | - | +++ | + | **P3** |

---

## Summary ROI by Cohort

| Cohort | Primary Gain | Savings/Day | Risk Reduction |
|--------|-------------|-------------|----------------|
| **EM** | Dashboard: team + Jira + Slack | 30-45 min | Blockers discovered 1 day earlier |
| **PM** | Initiative status without manual assembly | 25-35 min | Lost decisions: -50% |
| **TL** | Decisions linked to implementation | 15-25 min | Quality issues detected hours faster |
| **IC** | Single "my day" | 10-15 min | Blockers escalated automatically |
| **Director** | Executive overview | 25-30 min | At-risk: 1-2 weeks earlier |
| **Newcomer** | Task context | 30-60 min/task | Onboarding 1-2 weeks faster |
