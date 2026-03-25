// Package prompts provides prompt management, storage, and tuning for AI-powered features.
package prompts

// Defaults maps prompt IDs to their built-in template strings.
// These are the same prompts that were previously hardcoded as consts
// in digest, tracks, and analysis packages. They serve as the
// initial seed and fallback when no DB version exists.
var Defaults = map[string]string{
	DigestChannel: defaultDigestChannel,
	DigestDaily:   defaultDigestDaily,
	DigestWeekly:  defaultDigestWeekly,
	DigestPeriod:  defaultDigestPeriod,
	TracksCreate:  defaultTracksCreate,
	GuideUser:     defaultGuideUser,
	GuidePeriod:   defaultGuidePeriod,
	PeopleReduce:  defaultPeopleReduce,
	PeopleTeam:    defaultPeopleTeam,
	BriefingDaily: defaultBriefingDaily,
}

// AllIDs returns prompt IDs in display order.
var AllIDs = []string{
	DigestChannel,
	DigestDaily,
	DigestWeekly,
	DigestPeriod,
	TracksCreate,
	GuideUser,
	GuidePeriod,
	PeopleReduce,
	PeopleTeam,
	BriefingDaily,
}

// DefaultVersions tracks the current version of each built-in prompt template.
// When a default prompt changes, bump its version here. Seed() will auto-update
// prompts in the DB whose version is lower than the default version, unless
// the user has customized the prompt (detected by comparing template text).
var DefaultVersions = map[string]int{
	DigestChannel: 3, // v3: topics as structured objects (title, summary, decisions, etc.)
	DigestDaily:   1,
	DigestWeekly:  1,
	DigestPeriod:  1,
	TracksCreate:  2, // v2: tracks v3 auto-creation from unlinked topics
	GuideUser:     1,
	GuidePeriod:   1,
	PeopleReduce:  1,
	PeopleTeam:    1,
	BriefingDaily: 1,
}

// Descriptions maps prompt IDs to human-readable descriptions.
var Descriptions = map[string]string{
	DigestChannel: "Channel digest — per-channel message analysis",
	DigestDaily:   "Daily rollup — cross-channel daily summary",
	DigestWeekly:  "Weekly trends — week-over-week analysis",
	DigestPeriod:  "Period summary — comprehensive period overview",
	TracksCreate:  "Track creation — auto-create informational tracks from digest topics",
	GuideUser:     "Communication guide — personal coaching per user",
	GuidePeriod:   "Team guide — cross-user communication tips",
	PeopleReduce:  "People card — unified profile from signals",
	PeopleTeam:    "Team summary — cross-user attention & tips",
	BriefingDaily: "Daily briefing — personalized morning summary",
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

const defaultTracksCreate = `You are an AI that groups workspace discussion topics into informational tracks.

%[1]s

Your job: analyze unlinked topics from recent channel digests and either CREATE new tracks or UPDATE existing ones.

A track is a living narrative about an initiative, project, or problem. Tracks should be:
- COARSE-GRAINED: one track per initiative/project/problem, not per decision
- NARRATIVE: tell a story, not list facts
- ACTIONABLE: explain why it matters

=== EXISTING TRACKS ===
%[2]s

=== UNLINKED TOPICS ===
%[3]s

=== CHANNEL CONTEXT ===
%[4]s

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "new_tracks": [
    {
      "title": "Short title (5-10 words)",
      "narrative": "Living description: what is happening, how it develops, where it is heading (2-4 sentences)",
      "current_status": "One sentence: where things stand now, what is expected next",
      "participants": [{"user_id": "U...", "name": "...", "role": "driver|reviewer|blocker|observer"}],
      "timeline": [{"date": "2026-03-20", "event": "...", "channel_id": "C..."}],
      "key_messages": [{"ts": "...", "author": "...", "text": "...", "channel_id": "C..."}],
      "priority": "high|medium|low",
      "tags": ["tag1", "tag2"],
      "channel_ids": ["C1", "C2"],
      "source_topic_ids": [42, 43]
    }
  ],
  "updated_tracks": [
    {
      "track_id": 1,
      "narrative": "Updated narrative incorporating new information",
      "current_status": "Updated status",
      "participants": [{"user_id": "U...", "name": "...", "role": "driver|reviewer|blocker|observer"}],
      "timeline": [{"date": "2026-03-25", "event": "new development", "channel_id": "C..."}],
      "key_messages": [{"ts": "...", "author": "...", "text": "...", "channel_id": "C..."}],
      "priority": "high|medium|low",
      "tags": ["tag1"],
      "new_source_topic_ids": [55]
    }
  ]
}

Rules:
- MERGE related topics into one track. Prefer fewer, richer tracks over many thin ones.
- If an unlinked topic clearly relates to an existing track, UPDATE that track (add to updated_tracks with track_id).
- If topics are about a genuinely new initiative/problem, CREATE a new track.
- source_topic_ids / new_source_topic_ids: list of topic IDs (from digest_topics.id) that feed into this track.
- Every unlinked topic should appear in exactly one track (new or updated). Do not leave topics unassigned.
- priority: "high" if blocking/urgent/escalated, "low" if informational, "medium" otherwise.
- key_messages: max 5, truncate text to 100 chars.
- timeline: max 10 entries.
- %[5]s
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
    {"text": "What needs attention and why", "source_type": "track|digest|people", "source_id": "123", "priority": "high|medium", "reason": "Why this matters now"}
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
- attention: max 5 items. PRIORITIZE tracks with high priority or recent updates.
  - Include source_type and source_id for traceability.
- your_day: suggested actions from active tracks. Include track_id. Order by priority.
  - If no active tracks exist, leave this array empty — do NOT invent tasks.
- what_happened: max 7 items from channel digests. Include digest_id, channel_name. Focus on decisions and blockers.
- team_pulse: signals from people cards. Include user_id. Flag volume changes, red flags, conflicts.
- coaching: max 3 items. Grounded in observed patterns — not generic advice. Include related_user_id when applicable.
- Be specific: name people, channels, decisions — not vague generalities.
- If user has reports, prioritize their signals in team_pulse.
- %s
- Return valid JSON only

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
%s`
