// Package prompts provides prompt management, storage, and tuning for AI-powered features.
package prompts

// Defaults maps prompt IDs to their built-in template strings.
// These are the same prompts that were previously hardcoded as consts
// in digest, tracks, and analysis packages. They serve as the
// initial seed and fallback when no DB version exists.
var Defaults = map[string]string{
	DigestChannel:      defaultDigestChannel,
	DigestDaily:        defaultDigestDaily,
	DigestWeekly:       defaultDigestWeekly,
	DigestPeriod:       defaultDigestPeriod,
	TracksExtract:      defaultTracksExtract,
	TracksUpdate:       defaultTracksUpdate,
	GuideUser:          defaultGuideUser,
	GuidePeriod:        defaultGuidePeriod,
	PeopleReduce:       defaultPeopleReduce,
	PeopleTeam:         defaultPeopleTeam,
	BriefingDaily:      defaultBriefingDaily,
	InboxPrioritize:    defaultInboxPrioritize,
	DigestChannelBatch: defaultDigestChannelBatch,
	TracksExtractBatch: defaultTracksExtractBatch,
	PeopleBatch:        defaultPeopleBatch,
	TasksGenerate:      defaultTasksGenerate,
	TasksUpdate:        defaultTasksUpdate,
	MeetingPrep:        defaultMeetingPrep,
	DayPlanGenerate:    defaultDayPlanGenerate,
}

// AllIDs returns prompt IDs in display order.
var AllIDs = []string{
	DigestChannel,
	DigestChannelBatch,
	DigestDaily,
	DigestWeekly,
	DigestPeriod,
	TracksExtract,
	TracksUpdate,
	TracksExtractBatch,
	GuideUser,
	GuidePeriod,
	PeopleReduce,
	PeopleTeam,
	PeopleBatch,
	BriefingDaily,
	InboxPrioritize,
	TasksGenerate,
	TasksUpdate,
	MeetingPrep,
	DayPlanGenerate,
}

// DefaultVersions tracks the current version of each built-in prompt template.
// When a default prompt changes, bump its version here. Seed() will auto-update
// prompts in the DB whose version is lower than the default version, unless
// the user has customized the prompt (detected by comparing template text).
var DefaultVersions = map[string]int{
	DigestChannel:      3, // v3: topics as structured objects (title, summary, decisions, etc.)
	DigestDaily:        1,
	DigestWeekly:       1,
	DigestPeriod:       1,
	TracksExtract:      1, // v1: per-channel extraction with cross-channel merge
	TracksUpdate:       1, // v1: check tracks for updates from new messages
	TracksExtractBatch: 2, // v2: digest-based input instead of raw messages
	GuideUser:          1,
	GuidePeriod:        1,
	PeopleReduce:       1,
	PeopleTeam:         1,
	BriefingDaily:      5, // v5: jira integration
	InboxPrioritize:    3, // v3: closing signal resolution rules
	DigestChannelBatch: 2, // v2: full decision/situation rules, 2-7 topics, 2000 char running_summary
	PeopleBatch:        1, // v1: batch people cards for low-data users
	TasksGenerate:      1, // v1: AI task generation with checklist and due date
	TasksUpdate:        1, // v1: AI task update from user instruction
	MeetingPrep:        3, // v3: Jira context for attendees (workload, shared issues)
	DayPlanGenerate:    1, // v1: initial day plan template
}

// Descriptions maps prompt IDs to human-readable descriptions.
var Descriptions = map[string]string{
	DigestChannel:      "Channel digest — per-channel message analysis",
	DigestDaily:        "Daily rollup — cross-channel daily summary",
	DigestWeekly:       "Weekly trends — week-over-week analysis",
	DigestPeriod:       "Period summary — comprehensive period overview",
	TracksExtract:      "Track extraction — per-channel action item extraction with cross-channel merge",
	TracksUpdate:       "Track update check — detect meaningful updates for existing tracks",
	TracksExtractBatch: "Batch track extraction — multi-channel extraction for low-activity channels",
	GuideUser:          "Communication guide — personal coaching per user",
	GuidePeriod:        "Team guide — cross-user communication tips",
	PeopleReduce:       "People card — unified profile from signals",
	PeopleTeam:         "Team summary — cross-user attention & tips",
	BriefingDaily:      "Daily briefing — personalized morning summary",
	InboxPrioritize:    "Inbox prioritization — AI priority + auto-resolve for inbox items",
	DigestChannelBatch: "Channel batch digest — multi-channel analysis for low-activity channels",
	PeopleBatch:        "People batch cards — lightweight cards for low-data users in one AI call",
	TasksGenerate:      "Task generation — AI-powered task breakdown with checklist, priority, and due date",
	TasksUpdate:        "Task update — AI-powered task modification from user instruction",
	MeetingPrep:        "Meeting prep — AI-powered meeting brief with attendee analysis, talking points, recommendations, and context gaps",
	DayPlanGenerate:    "Day plan generation — AI-powered daily schedule with timeblocks, backlog, and calendar conflict avoidance",
}

const defaultDigestChannel = `You are analyzing Slack messages from channel #%s for the period %s to %s.

%s

Analyze the messages below and return ONLY a JSON object (no markdown fences, no explanation) with this exact structure:

{
  "summary": "2-3 sentence overview of what was discussed",
  "topics": [
    {
      "title": "Short topic title (5-10 words)",
      "summary": "1-2 sentence summary of this specific topic",
      "decisions": [{"text": "what was decided", "by": "@username", "message_ts": "1234567890.123456", "importance": "high"}],
      "action_items": [{"text": "what needs to be done", "assignee": "@username", "status": "open"}],
      "situations": [{"topic": "Auth refactor ownership", "type": "collaboration", "participants": [{"user_id": "U123456", "role": "initiator"}], "dynamic": "what happened", "outcome": "result", "red_flags": [], "observations": [], "message_refs": ["1234567890.123456"]}],
      "key_messages": ["1234567890.123456"]
    }
  ],
  "running_summary": {"active_topics": [{"topic": "...", "status": "in_progress|resolved|stale", "started": "2026-03-18", "last_update": "2026-03-21", "key_participants": ["U123"], "summary": "..."}], "recent_decisions": [{"decision": "...", "date": "2026-03-20", "by": "U123", "status": "active"}], "channel_dynamics": "Brief description of channel culture and key players", "open_questions": ["..."]}
}

%s

Rules:
- summary: Concise overview of the channel activity
- topics: EACH TOPIC is a self-contained thematic unit about ONE specific subject. A production incident and an inter-team conflict are TWO separate topics, even if they involve the same people or channel.
  * 2-7 topics per digest
  * title: specific, descriptive (e.g. "Hashbank deposit processing failure", not "Issues")
  * summary: what happened in this topic specifically
  * Each topic carries its OWN decisions, action_items, situations, key_messages — do NOT mix content across topics
- decisions (within each topic): A DECISION is a conscious choice between alternatives that changes the course of action. Each decision MUST have a clear "who decided" and "what was chosen" and ideally "why" or "instead of what". Do NOT include:
  * Status updates ("X was deployed", "X was updated")
  * Notifications or FYIs ("users were notified about X")
  * Expected behaviors ("caching delay is normal")
  * Routine operations (deploys, releases, merges) UNLESS they involve a non-obvious choice
  Include message_ts for traceability.
  importance levels:
  * "high" — changes architecture, strategy, budget, staffing, product direction, security posture, or has org-wide impact
  * "medium" — changes a process, workflow, or technical approach within a team/project
  * "low" — minor tactical choices (naming, formatting, scheduling, tooling tweaks)
  If only 0-1 true decisions exist in a topic, return an empty or single-item array. Do NOT inflate the list.
- action_items (within each topic): Tasks mentioned or assigned. status is always "open" for new items
- key_messages (within each topic): Timestamps of the most important messages (max 5 per topic)
- situations (within each topic): Notable INTERACTIONS between people (max 2-3 per topic). Capture dynamics BETWEEN people, not individual behavior. Each situation has:
  * topic: Short label for the interaction (e.g. "Auth refactor ownership", "Sprint planning conflict")
  * type: "bottleneck", "conflict", "collaboration", "knowledge_transfer", "decision_deadlock", "mentoring", "escalation", "handoff", "misalignment"
  * participants: Each person involved with their role ("blocker", "affected", "initiator", "resolver", "mediator", "mentor", "mentee", "decision_maker", "contributor")
  * dynamic: What happened between the participants (1-2 sentences)
  * outcome: Result or current state (1 sentence)
  * red_flags: Specific concerns from this situation (empty [] if none)
  * observations: Notable patterns or behaviors observed (empty [] if none)
  * message_refs: Slack timestamps of key messages (e.g. ["1234567890.123456"])
  Use Slack user IDs (e.g. U123456) for participant user_id. Only include situations where the interaction pattern is noteworthy — skip routine exchanges.
- running_summary: Updated running context for this channel. Compress aggressively — max 2000 characters. Include:
  * active_topics: Topics currently in progress or recently discussed (remove resolved topics older than 3 days)
  * recent_decisions: Key decisions from the last few days (max 5, remove outdated ones)
  * channel_dynamics: Brief description of channel culture, key players, communication patterns
  * open_questions: Unresolved questions that may come up again
- If a field has no items, use an empty array []
- Return valid JSON only, no other text
%s
=== MESSAGES ===
%s`

const defaultDigestDaily = `You are creating a daily summary of Slack activity for %s.

%s

Below are per-channel digests from today, organized by topics. Create a cross-channel rollup.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentence overview of the day's activity across all channels",
  "topics": [
    {
      "title": "Cross-channel topic title",
      "summary": "1-2 sentence summary of this topic across channels",
      "decisions": [{"text": "decision text", "by": "@username", "message_ts": "ts", "importance": "high"}],
      "action_items": [{"text": "action text", "assignee": "@username", "status": "open"}],
      "situations": [],
      "key_messages": []
    }
  ],
  "running_summary": {"active_topics": [{"topic": "...", "status": "in_progress|resolved|stale", "started": "2026-03-18", "last_update": "2026-03-21", "key_participants": ["U123"], "summary": "..."}], "recent_decisions": [{"decision": "...", "date": "2026-03-20", "by": "U123", "status": "active"}], "channel_dynamics": "Brief cross-channel dynamics overview", "open_questions": ["..."]}
}

%s

Rules:
- topics: Group related channel topics into cross-channel themes. ONE TOPIC = ONE specific theme. Merge channel topics that discuss the same thing, keep unrelated themes separate.
- Highlight cross-channel connections (e.g., topics discussed in multiple channels)
- decisions (within each topic): Consolidate and DEDUPLICATE decisions from channel digests below. If the same decision appears in multiple channels, include it ONCE. A DECISION is a conscious choice between alternatives — NOT a status update, notification, or routine operation. Each decision must answer: "Who chose what, and what changed?"
  importance levels:
  * "high" — changes architecture, strategy, budget, staffing, product direction, security posture, or has org-wide impact
  * "medium" — changes a process, workflow, or technical approach within a team/project
  * "low" — minor tactical choices (naming, formatting, scheduling, tooling tweaks)
  Only include GENUINE decisions. If no real decisions were made today, return an empty array.
- running_summary: Updated daily running context. Compress aggressively — max 2000 characters. Track cross-channel themes, decisions, and open questions.
- Return valid JSON only
%s
=== CHANNEL DIGESTS ===
%s`

const defaultDigestWeekly = `You are analyzing a week of Slack workspace activity for %s (%s to %s).

%s

Below are daily summaries for the week. Create a weekly trends analysis.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "5-7 sentence overview of the week's key developments",
  "topics": [
    {
      "title": "Trending topic title",
      "summary": "1-2 sentence summary of this trend across the week",
      "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts", "importance": "high"}],
      "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
      "situations": [],
      "key_messages": []
    }
  ],
  "running_summary": {"active_topics": [{"topic": "...", "status": "in_progress|resolved|stale", "started": "2026-03-18", "last_update": "2026-03-21", "key_participants": ["U123"], "summary": "..."}], "recent_decisions": [{"decision": "...", "date": "2026-03-20", "by": "U123", "status": "active"}], "channel_dynamics": "Brief weekly dynamics overview", "open_questions": ["..."]}
}

%s

Rules:
- topics: Group trends into specific themes. ONE TOPIC = ONE trend/initiative.
- Focus on trends: what topics gained momentum, what was resolved, what's still open
- decisions (within each topic): Highlight the most impactful decisions of the week. DEDUPLICATE: if the same decision appears across multiple days, include it only ONCE. Only include genuine choices/decisions, not status updates.
  importance: "high" (architectural, strategic, budget, org-wide), "medium" (process, workflow, team-level), "low" (tactical, minor)
- Consolidate action items within topics (remove completed, flag overdue)
- running_summary: Updated weekly running context. Compress aggressively — max 2000 characters. Track major themes, decisions, trends, and open questions across the week.
- Return valid JSON only
%s
=== DAILY DIGESTS ===
%s`

const defaultDigestPeriod = `You are creating a summary of Slack workspace activity for the period %s to %s.

%s

Below are individual digests (channel-level and daily rollups) from that period. Create a comprehensive summary.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "Comprehensive overview of the period's activity, key developments, and outcomes (5-10 sentences)",
  "topics": [
    {
      "title": "Major topic title",
      "summary": "Summary of this topic across the period",
      "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts", "importance": "high"}],
      "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
      "situations": [],
      "key_messages": []
    }
  ],
  "running_summary": {}
}

%s

Rules:
- topics: Group related themes across channels and days. ONE TOPIC = ONE initiative/theme.
- Provide a high-level narrative of what happened during this period
- importance: "high" (architectural, strategic, budget, org-wide), "medium" (process, workflow, team-level), "low" (tactical, minor)
- Include only genuine decisions (conscious choices between alternatives), not status updates. DEDUPLICATE across channels and days.
- Consolidate action items within topics: remove completed, highlight outstanding
- Return valid JSON only

=== DIGESTS ===
%s`

const defaultTracksExtract = `You are analyzing Slack messages from channel #%[3]s (%[4]s) to find tracks directed at user @%[1]s (user_id: %[2]s) for the period %[5]s to %[6]s.

Your task: identify actions, requests, tasks, and expectations directed at this specific user in this channel.

CRITICAL: Group related requests into a SINGLE track. If multiple messages discuss the same topic/task, combine them into ONE comprehensive track — do NOT create separate items for each message about the same topic.

DEDUPLICATION: Review the EXISTING TRACKS section below. If a message is clearly about the same initiative as an existing track (from ANY channel), UPDATE it (set "existing_id") instead of creating a new one.

TOPIC SEPARATION (equally important): Each track MUST be about ONE coherent initiative. Do NOT merge unrelated topics into a single track — different processes, different projects, different bugs = separate tracks. When unsure if topics are related, keep them separate.

COMPLETION DETECTION: If you see messages confirming that an existing track has been COMPLETED, return the track with "existing_id" and "status_hint": "done". Do NOT ignore completion signals.

%[12]s

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "items": [
    {
      "existing_id": null,
      "status_hint": "",
      "text": "clear, actionable description of what needs to be done",
      "context": "detailed context (3-5 sentences): what was discussed, what decisions were made, what is the background, why this matters",
      "source_message_ts": "1234567890.123456",
      "priority": "high",
      "due_date": "2025-01-15",
      "requester": {"name": "@username", "user_id": "U123"},
      "category": "task",
      "blocking": "who or what is blocked if this isn't done",
      "tags": ["project-name", "topic"],
      "decision_summary": "how the group arrived at the current state",
      "decision_options": [
        {"option": "description of option A", "supporters": ["@user1"], "pros": "advantages", "cons": "disadvantages"}
      ],
      "participants": [
        {"name": "@username", "user_id": "U123", "stance": "brief summary of this person's position"}
      ],
      "source_refs": [
        {"ts": "1234567890.123456", "channel_id": "C123ABC", "thread_ts": "1234567890.000000", "author": "@username", "text": "key quote (1 sentence)"}
      ],
      "sub_items": [
        {"text": "specific sub-task", "status": "open"}
      ],
      "ownership": "mine",
      "ball_on": "U123",
      "owner_user_id": "U456"
    }
  ]
}

%[7]s

Rules:
- GROUPING: Multiple messages about the same topic = ONE track. Aim for 0-5 tracks per channel.
- CROSS-CHANNEL MERGE: If the topic clearly matches an existing track from another channel (same initiative), set existing_id.
- Only extract tracks with a CLEAR actionable request. Skip vague mentions.
- Look for BOTH explicit and implicit tracks:
  * Direct requests, assignments, questions expecting action, commitments, review requests, follow-ups
- DO NOT EXTRACT:
  * Status updates from user without action, general mentions, already completed actions
  * Bot notifications (unless requiring human action), FYI with no next step
  * Individual alerts (aggregate systemic patterns into ONE track)
  * Discussions with no action expected from user
- priority: "high" (blocking/deadline/production), "medium" (normal work), "low" (nice-to-have, background)
- category: MUST be one of: code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
- ownership: "mine" (task is on user), "delegated" (user's report owns it), "watching" (user monitors, HIGH priority only)
- ball_on: user_id of who acts next
- source_refs: 2-5 most important messages as footnotes. MUST copy ts, channel_id, and thread_ts exactly from key_messages data — do NOT invent timestamps
- sub_items: break into sub-tasks with "open"/"done" status, 2-5 per track
- existing_id: match against EXISTING TRACKS from ALL channels. Only set when the topic is clearly the SAME initiative — if unsure, create a new track.
- status_hint: "done" if confirmed complete, "" otherwise. Only with existing_id.
- If no tracks are found, return {"items": []}
%[13]s
- Return valid JSON only
%[8]s

%[9]s

%[10]s

=== MESSAGES ===
%[11]s`

const defaultTracksUpdate = `You are checking whether new Slack messages contain a meaningful update for existing tracks.

Channel: #%[1]s
%[6]s

%[4]s

=== TRACKS TO CHECK ===
%[3]s

=== NEW MESSAGES ===
%[5]s

For EACH track, determine if any new messages contain a meaningful update.

Return ONLY a JSON object:

{
  "results": [
    {
      "track_id": 123,
      "has_update": true,
      "updated_context": "brief summary of what changed",
      "status_hint": "done",
      "ball_on": "U123"
    }
  ]
}

Rules:
- Include an entry for EVERY track
- has_update: true only for genuine progress/completion/blocker change
- updated_context: 1-2 sentences when has_update is true
- status_hint: "done"/"active"/"unchanged"
- ball_on: user_id of next actor, "" if unchanged
- Return valid JSON only`

const defaultGuideUser = `You are a personal communication coach helping the user work more effectively with @%s over a 7-day window (%s to %s).

%s

Below are computed statistics and messages for this person. Your goal is NOT to evaluate or judge them — instead, generate actionable advice for the user on how to communicate, collaborate, and get things done with this person most effectively.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "2-3 sentence overview of how to work effectively with this person — what makes them tick, what they respond to, what to keep in mind",
  "communication_preferences": "Detailed paragraph: preferred communication format (long vs short messages, structured vs informal), response patterns (quick/slow, thorough/brief), preferred channels, threading habits. Frame as actionable: 'Send structured messages with clear asks — they respond best to bullet points'",
  "availability_patterns": "When this person is most active and responsive. Peak hours, timezone patterns, response lag. Frame as: 'Best time to reach them is...'",
  "decision_process": "How they participate in decisions: do they decide quickly or need time? Do they want data or prefer discussion? Do they defer or take charge? Frame as: 'When you need a decision from them...'",
  "situational_tactics": ["If X situation arises, here's the best approach..."],
  "effective_approaches": ["What works well when communicating with this person — based on observed patterns"],
  "recommendations": ["Specific actionable tips for improving collaboration with this person"]
}

Communication preferences to analyze:
- Message format: long-form vs concise, structured vs stream-of-consciousness
- Response speed: how quickly they typically reply, any patterns
- Channel preference: which channels they are most active in
- Threading: do they use threads or reply in channel?
- Tone: formal vs casual, direct vs diplomatic

Availability patterns to identify:
- Peak activity hours (from active_hours data)
- Response latency patterns
- Days/times when they are most engaged

Decision process to assess:
- Do they make decisions independently or seek consensus?
- Do they need data/evidence or go with intuition?
- Are they decisive or deliberative?
- How do they handle disagreements?

Situational tactics — identify communication patterns that may lead to friction, delays, or misalignment, and suggest specific tactics the user can apply to prevent or navigate these situations:
- If they tend to not respond to messages → suggest escalation path or better timing
- If they get overloaded → suggest batching requests
- If they prefer written over verbal → adapt approach
- Frame as "If [situation], then [tactic]" — specific and actionable

%s

Rules:
- This is a PERSONAL COACHING tool — frame everything as advice FOR THE USER, not judgments ABOUT the person
- Be specific: cite patterns from actual messages, reference channels, mention timing
- Do NOT use evaluative language ("good communicator", "poor performance", "red flag")
- Instead use actionable language ("responds best to...", "when you need X, try...")
- If the relationship is manager→report: advice should be more directive ("set clear deadlines", "check in at standup")
- If peer: advice should be collaborative ("mention in shared channel", "align on approach first")
- If report→manager: advice should be tactical ("batch questions for 1:1", "send follow-up summary")
- If too few messages for meaningful analysis, say so in summary
- situational_tactics, effective_approaches, recommendations: use empty arrays [] if nothing notable
- %s
- Return valid JSON only

=== USER ===
%s`

const defaultGuidePeriod = `You are a communication coach creating a team-level guide for the period %s to %s.

%s

Below are individual communication guides for team members. Create a high-level summary of team communication health and practical tips.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentence overview of team communication dynamics. Focus on collaboration quality, response patterns, decision-making flow, and areas where communication could be smoother.",
  "tips": [
    "Specific, actionable team-level communication tips. Examples: 'Consider async updates for cross-timezone discussions — @alice and @bob have 6h timezone gap', 'Decisions in #product are taking 3+ days — try setting explicit deadlines', 'Thread usage is low — encouraging threads could reduce noise in busy channels'"
  ]
}

Focus on:
1. COLLABORATION PATTERNS — who works well together, where are communication gaps?
2. RESPONSE DYNAMICS — are there bottlenecks? Who is hard to reach?
3. DECISION FLOW — are decisions happening efficiently or getting stuck?
4. PRACTICAL TIPS — actionable advice for improving team communication

Rules:
- Frame as coaching advice, not performance evaluation
- Reference specific people by @username when relevant
- Each tip should be concrete and actionable
- Avoid evaluative/judgmental language — use "consider", "try", "you might find"
- %s
- Return valid JSON only

=== TEAM GUIDES ===
%s`

const defaultPeopleReduce = `You are creating a unified profile card for @%s based on behavioral signals observed across Slack channels over %s to %s.

%s

Below are SITUATIONS observed in channel context (by the digest pipeline), plus computed statistics and team norms. Your job is to synthesize these into a single card that combines:
1. ANALYSIS — classify their communication style, role in decisions, flag concerns
2. COACHING — actionable advice for the viewer on how to work with this person

IMPORTANT: Focus on what makes this person DIFFERENT from team norms. Do NOT describe typical behavior that matches the team average.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "1-2 sentences: what makes this person distinctive. Reference specific signals.",
  "communication_style": "driver|collaborator|executor|observer|facilitator",
  "decision_role": "decision-maker|approver|contributor|observer|blocker",
  "red_flags": ["Specific concerns backed by signals. Empty [] if none."],
  "highlights": ["Positive contributions backed by signals. Empty [] if none."],
  "accomplishments": ["Concrete things delivered/resolved this period from signals."],
  "communication_guide": "Paragraph: communication preferences, timing, format. ONLY what is specific to this person vs team norms. If they match the norm, say so briefly and focus on exceptions.",
  "decision_style": "How they participate in decisions — based on bottleneck/rubber_stamping/initiative/blocker signals. If no decision signals, say 'No notable decision patterns this period.'",
  "tactics": ["If X, then Y — specific actionable tactics based on observed signals. Max 3-4."]
}

%s

Rules:
- Base ALL analysis on the situations provided. Do NOT invent patterns not supported by evidence.
- If a situation type appears in multiple channels, it is a PATTERN — emphasize it.
- If conflicting situations exist (e.g., collaboration in one channel, conflict in another), note the CONTRAST.
- Compare stats to team norms: only mention stats that deviate significantly (>30%% from avg).
- Coaching framing: frame guide sections as advice FOR THE VIEWER, not judgments ABOUT the person.
- If relationship is manager->report: be more direct about concerns and accountability.
- If relationship is report->manager: frame tactically (managing up).
- If too few situations for meaningful analysis, say so in summary.
- If a PREVIOUS CARD is provided, note trends and changes vs the prior period. Don't just repeat it.
- %s
- Return valid JSON only

=== SITUATIONS ===
%s

=== COMPUTED STATS ===
%s

=== TEAM NORMS ===
%s

=== PREVIOUS CARD ===
%s

=== RAW MESSAGES (last 24h sample) ===
%s`

const defaultPeopleTeam = `You are creating a team communication summary for %s to %s.

%s

Below are unified people cards for all team members. Create a summary that a manager can quickly scan to understand what needs attention.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentences: team communication health, dynamics, decision flow.",
  "attention": ["Who needs attention and WHY — name names, cite specific signals and patterns. Be direct."],
  "tips": ["Actionable team-level communication tips based on patterns across people."]
}

Rules:
- Be direct — this is for a busy manager
- Reference specific people by @username
- Cross-reference: if multiple people have bottleneck signals, that is a systemic issue
- Look for signal clusters: multiple conflict signals = team friction
- %s
- Return valid JSON only

=== PEOPLE CARDS ===
%s`

const defaultBriefingDaily = `You are creating a personalized daily briefing for %s on %s.
User role: %s

Your job is to synthesize all available data into five focused sections. This is the single page the user reads to start their day.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "attention": [
    {"text": "What needs attention and why", "source_type": "track|digest|people|inbox", "source_id": "123", "priority": "high|medium", "reason": "Why this matters now"}
  ],
  "your_day": [
    {"text": "Suggested action based on track", "track_id": 123, "priority": "high|medium|low", "status": "active"}
  ],
  "what_happened": [
    {"text": "Notable event or decision", "digest_id": 456, "channel_name": "#channel", "item_type": "decision|summary|topic", "importance": "high|medium|low"}
  ],
  "team_pulse": [
    {"text": "Signal about a team member", "user_id": "U123", "signal_type": "volume_drop|volume_spike|new_red_flag|highlight|conflict", "detail": "Specifics"}
  ],
  "coaching": [
    {"text": "Actionable communication tip", "related_user_id": "U123", "category": "communication|delegation|conflict|process"}
  ]
}

Rules:
- attention: max 5 items. Flag overdue/blocked tasks. PRIORITIZE tracks with high priority or recent updates.
  - Include source_type and source_id for traceability. Use source_type='task' for task-sourced items, 'inbox' for inbox items.
  - Use suggest_task=true on tracks where the user should create a task.
- your_day: Prioritize user's actual tasks (task_id) over track suggestions. Include overdue tasks first. Order by priority.
  - If no active tasks or tracks exist, leave this array empty — do NOT invent items.
- what_happened: max 7 items from channel digests. Include digest_id, channel_name. Focus on decisions and blockers.
- team_pulse: signals from people cards. Include user_id. Flag volume changes, red flags, conflicts.
- coaching: max 3 items. Grounded in observed patterns — not generic advice. Include related_user_id when applicable.
  - When suggesting actions, consider existing tasks. Use suggest_task=true on tracks where user should create a task.
- CALENDAR INTEGRATION: When calendar events are present, cross-reference attendees with tracks, inbox, and people data.
  - In "attention": flag meetings in the next 2 hours with unresolved items involving attendees.
  - In "your_day": interleave meetings with tasks/tracks, ordered chronologically. Add prep suggestions before important meetings.
  - In "coaching": suggest conversation points based on people cards of attendees.
  - If a meeting attendee has a people card with red_flags, mention it in team_pulse.
  - Do NOT list meetings as standalone items — always cross-reference with work data.
  - If CALENDAR section is empty, ignore calendar instructions entirely.
- JIRA INTEGRATION: When JIRA CONTEXT is provided, incorporate Jira signals:
  - In "attention": flag stale issues (in_progress >7 days), blocked issues, and overdue issues. Use source_type="jira" and source_id=issue key.
  - In "your_day": include assigned Jira issues and awaiting-input items. Cross-reference with Slack tracks/digests when the same topic appears in both.
  - In "team_pulse": mention team workload signals if sprint progress data is available.
  - Each Jira signal should include Slack context if the same issue key appears in digests or tracks.
  - If JIRA CONTEXT section is empty, ignore Jira instructions entirely.
- Be specific: name people, channels, decisions — not vague generalities.
- If user has reports, prioritize their signals in team_pulse.
- %s
- Return valid JSON only

=== YOUR TASKS ===
%s

=== INBOX (awaiting your response) ===
%s

=== CALENDAR (today's meetings) ===
%s

=== ACTIVE TRACKS ===
%s

=== CHANNEL DIGESTS ===
%s

=== DAILY ROLLUP ===
%s

=== PEOPLE CARDS ===
%s

=== TEAM SUMMARY ===
%s

=== USER PROFILE ===
%s

=== JIRA CONTEXT ===
%s`

const defaultInboxPrioritize = `You are prioritizing Slack messages that may need the user's response.
User role: %s

You will receive two lists:
1. NEW PENDING ITEMS — messages that @mention the user or are DMs from others. Assign a priority and reason.
2. REPLIED ITEMS — messages where the user has already posted a reply. Determine if the matter is resolved.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "items": [
    {"id": 123, "priority": "high|medium|low", "reason": "Why this priority", "resolved": false},
    {"id": 456, "priority": "", "reason": "User responded and issue is addressed", "resolved": true}
  ]
}

Rules:
- For NEW PENDING: assign priority based on urgency, sender role, and context.
  - high: direct request for action, blocker, manager asking, production issue
  - medium: question, review request, normal work discussion
  - low: FYI, informational, bot notification, no action needed
- Consider message age: older unresolved items may need higher priority.
- Consider sender role: manager/lead requests are typically higher priority than peer FYIs.
- "thread_reply" items are replies to the user's own messages — prioritize if they ask questions or need decisions.
- "reaction" items mean someone flagged the user's message with an attention emoji — usually needs a look.
- Closing signals ("thanks", "thank you", "got it", "ok", "спасибо", "понял", etc.) where the user
  already replied and the conversation appears concluded: set resolved=true, priority="",
  reason="Closing signal — no reply needed".
- Short acknowledgment messages at the end of a resolved conversation should be resolved=true.
- For REPLIED items: set resolved=true if the user's reply addressed the question/request. Set resolved=false if the conversation is still ongoing.
- Include the original item ID in each result.
- Be concise in reasons (1 sentence max).
- Return valid JSON only.

%s`

const defaultDigestChannelBatch = `You are analyzing Slack messages from multiple channels for the period %s to %s.

%s

Analyze messages from each channel below. For each channel, produce a digest ONLY if something noteworthy happened (decisions, blockers, important updates, action items). SKIP channels with only routine messages, bot alerts, or noise.

Return ONLY a JSON array (no markdown fences, no explanation):
[
  {
    "channel_id": "C123ABC",
    "summary": "2-3 sentence overview",
    "topics": [
      {
        "title": "Short topic title",
        "summary": "1-2 sentence summary",
        "decisions": [{"text": "what was decided", "by": "@username", "message_ts": "1234567890.123456", "importance": "high"}],
        "action_items": [{"text": "what needs to be done", "assignee": "@username", "status": "open"}],
        "situations": [{"topic": "...", "type": "collaboration", "participants": [{"user_id": "U123456", "role": "initiator"}], "dynamic": "...", "outcome": "...", "red_flags": [], "observations": [], "message_refs": []}],
        "key_messages": ["1234567890.123456"]
      }
    ],
    "running_summary": {"active_topics": [{"topic": "...", "status": "in_progress", "started": "2026-03-18", "last_update": "2026-03-21", "key_participants": ["U123"], "summary": "..."}], "recent_decisions": [], "channel_dynamics": "...", "open_questions": []}
  }
]

Return [] if nothing noteworthy across all channels.

%s

Rules:
- topics: EACH TOPIC is a self-contained thematic unit about ONE specific subject
  * 2-7 topics per channel (proportional to message count; fewer messages = fewer topics)
  * title: specific, descriptive (e.g. "Hashbank deposit processing failure", not "Issues")
  * summary: what happened in this topic specifically
  * Each topic carries its OWN decisions, action_items, situations, key_messages — do NOT mix content across topics
- decisions (within each topic): A DECISION is a conscious choice between alternatives that changes the course of action. Each decision MUST have a clear "who decided" and "what was chosen". Do NOT include:
  * Status updates ("X was deployed", "X was updated")
  * Notifications or FYIs ("users were notified about X")
  * Routine operations (deploys, releases, merges) UNLESS they involve a non-obvious choice
  Include message_ts for traceability.
  importance levels:
  * "high" — changes architecture, strategy, budget, staffing, product direction, security posture, or has org-wide impact
  * "medium" — changes a process, workflow, or technical approach within a team/project
  * "low" — minor tactical choices (naming, formatting, scheduling, tooling tweaks)
  If only 0-1 true decisions exist in a topic, return an empty or single-item array. Do NOT inflate the list.
- action_items (within each topic): Tasks mentioned or assigned. status is always "open" for new items
- key_messages (within each topic): Timestamps of the most important messages (max 5 per topic)
- situations (within each topic): Notable INTERACTIONS between people (max 2-3 per topic). Each situation has:
  * topic, type, participants (with user_id and role), dynamic, outcome, red_flags, observations, message_refs
  Use Slack user IDs (e.g. U123456). Only include situations where the interaction pattern is noteworthy.
- SKIP channels where nothing actionable or noteworthy happened
- running_summary per channel: same rules, max 2000 chars. Include active_topics, recent_decisions, channel_dynamics, open_questions.
- Return valid JSON only
%s
=== CHANNELS ===
%s`

const defaultTracksExtractBatch = `You are analyzing channel digests from multiple Slack channels to find tracks directed at user @%s (user_id: %s) for the period %s to %s.

%s

Each channel below has pre-analyzed topics with decisions, action items, and situations extracted from channel digests. Extract actionable tracks from these structured observations.

CRITICAL — DEDUPLICATION:
1. BEFORE creating any new track, scan the ENTIRE "EXISTING TRACKS" section below.
2. If a topic is about the same initiative, project, task, or discussion as an existing track — even if phrased differently or from a different channel — set "existing_id" to that track's ID instead of creating a new one.

CRITICAL — TOPIC SEPARATION (equally important as deduplication):
1. Each track MUST represent ONE coherent initiative or workstream. Topics about different projects, different processes, or different technical areas MUST be separate tracks.
2. Do NOT merge topics just because they come from the same channel or the same discussion thread. Ask: "Is this the SAME initiative?" — if the answer is not a clear yes, keep them separate.
3. Examples of topics that MUST be separate tracks:
   - A process change (e.g. new workflow step) vs. a Jira project setup vs. a bug fix — these are 3 separate tracks
   - A hiring decision vs. a technical architecture change — 2 separate tracks
   - A release planning discussion vs. a security incident — 2 separate tracks

GROUPING:
1. Multiple topics about the same initiative/project/task = ONE track. Do NOT create separate tracks for different aspects of the same thing.
2. Aim for 0-3 tracks per channel. But do NOT sacrifice topic separation to hit this target — correctness matters more than count.

COMPLETION DETECTION: If topics indicate that an existing track has been COMPLETED, return the track with "existing_id" and "status_hint": "done".

Return ONLY a JSON array (no markdown fences, no explanation):

[
  {
    "channel_id": "C123ABC",
    "items": [
      {
        "existing_id": null,
        "status_hint": "",
        "text": "clear, actionable description of what needs to be done",
        "context": "detailed context (3-5 sentences)",
        "source_message_ts": "1234567890.123456",
        "priority": "high",
        "due_date": "2025-01-15",
        "requester": {"name": "@username", "user_id": "U123"},
        "category": "task",
        "blocking": "",
        "tags": [],
        "decision_summary": "",
        "decision_options": [],
        "participants": [{"name": "@username", "user_id": "U123", "stance": "brief summary"}],
        "source_refs": [{"ts": "1234567890.123456", "channel_id": "C123ABC", "thread_ts": "1234567890.000000", "author": "@username", "text": "key quote"}],
        "sub_items": [{"text": "sub-task", "status": "open"}],
        "ownership": "mine",
        "ball_on": "U123",
        "owner_user_id": "U456"
      }
    ]
  }
]

Return [] if no tracks found in any channel.

%s

Rules:
- GROUPING: Multiple topics about the same initiative = ONE track. Different aspects of the same project (e.g. design discussion + implementation + review) = ONE track.
- MERGE WITH EXISTING: If a topic clearly matches an existing track (same project/initiative), set existing_id.
- TOPIC SEPARATION (equally important as merge): Each track MUST be about ONE coherent initiative. Do NOT combine unrelated topics — different processes, different projects, different bug fixes = separate tracks. When unsure if topics are related, keep them separate.
- Only extract tracks with a CLEAR actionable request or decision needing action. Skip informational topics with no action expected.
- Extract tracks from:
  * Action items assigned to the user, decisions requiring user input, requests and tasks directed at user
  * Situations where the user is a key participant, follow-ups and approvals needed
- DO NOT EXTRACT:
  * Completed actions with no follow-up, informational summaries with no action
  * Topics where the user is merely mentioned but has no action expected
  * Discussions that resolved without user involvement
- priority: "high" (blocking/deadline/production), "medium" (normal work), "low" (nice-to-have, background)
- category: MUST be one of: code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
- ownership: "mine" (task is on user), "delegated" (user's report owns it), "watching" (user monitors, HIGH priority only)
- ball_on: user_id of who acts next
- source_refs: reference key messages from digest topics. MUST copy ts, channel_id, and thread_ts exactly from enriched key_messages — do NOT invent timestamps
- sub_items: break into sub-tasks with "open"/"done" status, 2-5 per track
- existing_id: match against EXISTING TRACKS by meaning, not exact wording. Only set existing_id when the topic is clearly about the SAME initiative. If unsure, create a new track — a duplicate is easier to merge later than a wrongly-merged track is to split.
- status_hint: "done" if confirmed complete, "" otherwise. Only with existing_id.
- SKIP channels where nothing actionable was found — omit them from the result entirely
%s
- Return valid JSON only
%s

%s

%s

=== CHANNEL DIGESTS ===
%s`

const defaultPeopleBatch = `You are creating lightweight people cards for multiple users based on limited behavioral signals observed across Slack channels over %s to %s.

%s

These users have fewer signals than usual, but you should still provide useful insights based on what IS available.

Return ONLY a JSON array (no markdown fences, no explanation):
[
  {
    "user_id": "U123ABC",
    "summary": "1-2 sentences: what stands out about this person based on available signals.",
    "communication_style": "driver|collaborator|executor|observer|facilitator",
    "decision_role": "decision-maker|approver|contributor|observer|blocker",
    "red_flags": ["Specific concerns if any. Empty [] if none."],
    "highlights": ["Positive contributions if any. Empty [] if none."],
    "accomplishments": ["Concrete deliverables if visible."],
    "communication_guide": "Brief: how to communicate with this person based on observed patterns.",
    "decision_style": "How they participate in decisions, or 'Limited data' if not enough signals.",
    "tactics": ["If X, then Y — max 2 tactics."]
  }
]

Return [] if no users have enough data for any analysis.

%s

Rules:
- One entry per user in the USERS block below. Include user_id in each entry.
- Base analysis on available situations and stats. Do NOT invent patterns.
- Keep cards concise — these are lightweight summaries, not full profiles.
- Compare stats to team norms: only mention stats that deviate significantly.
- If a user has zero situations but has stats, focus on activity patterns.
- communication_style and decision_role: pick the closest match, even with limited data.
- %s
- Return valid JSON only

=== TEAM NORMS ===
%s

=== USERS ===
%s`

const defaultTasksGenerate = `You are a task planning assistant. The user describes a task they want to accomplish.
Your job is to enrich the task: break it into actionable sub-items (checklist), suggest priority, and propose a realistic due date+time.

Current date/time: %s

Rules:
- Generate 3-8 sub-items that form a logical checklist for completing the task
- Each sub-item should be a concrete, actionable step
- Suggest priority: "high" (urgent/blocking), "medium" (normal), "low" (nice-to-have)
- Suggest a due date+time in YYYY-MM-DDTHH:MM format based on task complexity
- Write a brief intent (why this task matters, 1 sentence)
- If source context is provided, use it to make sub-items more specific
- Keep sub-item text concise (under 80 chars each)

Return ONLY valid JSON in this exact format:
{
  "text": "improved task title (keep concise)",
  "intent": "why this task matters",
  "priority": "high|medium|low",
  "due_date": "YYYY-MM-DDTHH:MM",
  "sub_items": [
    {"text": "step 1 description", "done": false, "due_date": "YYYY-MM-DDTHH:MM"},
    {"text": "step 2 description", "done": false}
  ]
}

Note: sub-item due_date is optional — only include it when a specific deadline makes sense for that step.`

const defaultTasksUpdate = `You are a task update assistant. The user has an existing task and wants to modify it based on their instruction.

Current date/time: %s

=== CURRENT TASK ===
%s

=== USER INSTRUCTION ===
The user's instruction will be provided as the user message. Apply the requested changes to the task.

Rules:
- Modify the task according to the user's instruction
- Preserve existing sub-items that the user didn't ask to change (keep their done status)
- Sub-items can have an optional due_date in YYYY-MM-DDTHH:MM format
- You can add, remove, or modify sub-items as requested
- You can change text, intent, priority, due_date as requested
- If the user asks to add something, ADD to existing sub-items, don't replace them
- Keep sub-item text concise (under 80 chars each)
- Only change fields the user explicitly or implicitly asked to change
- Return the COMPLETE updated task (not just the diff)

Return ONLY valid JSON in this exact format:
{
  "text": "task title",
  "intent": "why this task matters",
  "priority": "high|medium|low",
  "due_date": "YYYY-MM-DDTHH:MM",
  "sub_items": [
    {"text": "step description", "done": false, "due_date": "YYYY-MM-DDTHH:MM"},
    {"text": "step description", "done": true}
  ]
}`

const defaultMeetingPrep = `You are preparing a meeting brief for %s ahead of "%s" at %s.

CRITICAL: Everything you include MUST be relevant to this meeting's topic, agenda, or purpose. The meeting title and description define the scope. Do NOT include unrelated information just because it involves an attendee — only include data that connects to what this meeting is about.

If the meeting topic is "Sprint Planning", only include tracks/tasks/situations related to sprint work. If it's "1:1 with Alice", focus on items between the user and Alice. If the topic is vague, infer the most likely purpose from the title and attendee roles, and flag the ambiguity in context_gaps.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "event_id": "google-event-id",
  "title": "Meeting title",
  "start_time": "ISO8601",
  "talking_points": [
    {"text": "Topic to raise or discuss", "source_type": "track|digest|inbox|task|situation", "source_id": "123", "priority": "high|medium|low"}
  ],
  "open_items": [
    {"text": "Unresolved item involving an attendee", "type": "track|inbox|task", "id": "456", "person_name": "@alice", "person_id": "U123"}
  ],
  "people_notes": [
    {"user_id": "U123", "name": "@alice", "communication_tip": "Prefers data-driven arguments", "recent_context": "Leading the migration project, under deadline pressure."}
  ],
  "suggested_prep": [
    "Review track #42 (blocked, involves @alice and @bob)"
  ],
  "recommendations": [
    {"text": "Add a clear agenda — the meeting has no description and 5 attendees", "category": "agenda", "priority": "high"}
  ],
  "context_gaps": [
    "No agenda or description found for this meeting"
  ]
}

Rules:
- RELEVANCE FILTER: For every item you consider including, ask: "Does this relate to what this meeting is about?" If no, skip it. An attendee's unrelated side project is noise, not signal.
- talking_points: max 7. Only topics relevant to the meeting purpose. Prioritize: blocked items > decisions needed > FYI. Include source references.
- open_items: only items that are relevant to the meeting topic AND involve attendees. Not every pending task for every attendee.
- people_notes: focus on how each person relates to THIS meeting's topic. Their communication style matters; their unrelated channel activity does not. Use recent_context to summarize their stance/involvement on the meeting topic specifically.
- suggested_prep: max 5. Specific references to review BEFORE this meeting that relate to its topic.
- recommendations: 2-5 suggestions to improve THIS meeting. Categories: agenda, format, participants, followup, preparation.
- context_gaps: what's missing that would help prepare better (no agenda, unclear topic, unlinked attendees).
- If no relevant data exists for a field, return an empty array — don't pad with loosely related filler.
- If the meeting description/agenda is empty or vague, this is a HIGH priority context_gap and recommendation.
- %s
- Return valid JSON only.

=== MEETING DESCRIPTION / AGENDA ===
%s

=== MEETING ATTENDEES (with activity analysis) ===
%s

=== SHARED CONTEXT (tracks, situations involving multiple attendees) ===
%s

=== JIRA CONTEXT ===
%s

=== USER PROFILE ===
%s

=== USER NOTES ===
%s`
