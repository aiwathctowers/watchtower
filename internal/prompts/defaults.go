// Package prompts provides prompt management, storage, and tuning for AI-powered features.
package prompts

// Defaults maps prompt IDs to their built-in template strings.
// These are the same prompts that were previously hardcoded as consts
// in digest, tracks, and analysis packages. They serve as the
// initial seed and fallback when no DB version exists.
var Defaults = map[string]string{
	DigestChannel:  defaultDigestChannel,
	DigestDaily:    defaultDigestDaily,
	DigestWeekly:   defaultDigestWeekly,
	DigestPeriod:   defaultDigestPeriod,
	TracksExtract:  defaultTracksExtract,
	TracksUpdate:   defaultTracksUpdate,
	AnalysisUser:   defaultAnalysisUser,
	AnalysisPeriod: defaultAnalysisPeriod,
}

// AllIDs returns prompt IDs in display order.
var AllIDs = []string{
	DigestChannel,
	DigestDaily,
	DigestWeekly,
	DigestPeriod,
	TracksExtract,
	TracksUpdate,
	AnalysisUser,
	AnalysisPeriod,
}

// Descriptions maps prompt IDs to human-readable descriptions.
var Descriptions = map[string]string{
	DigestChannel:  "Channel digest — per-channel message analysis",
	DigestDaily:    "Daily rollup — cross-channel daily summary",
	DigestWeekly:   "Weekly trends — week-over-week analysis",
	DigestPeriod:   "Period summary — comprehensive period overview",
	TracksExtract:  "Tracks — extract tasks from messages",
	TracksUpdate:   "Track update check — detect progress in threads",
	AnalysisUser:   "User analysis — communication pattern analysis",
	AnalysisPeriod: "Team summary — cross-user team dynamics",
}

const defaultDigestChannel = `You are analyzing Slack messages from channel #%s for the period %s to %s.

%s

Analyze the messages below and return ONLY a JSON object (no markdown fences, no explanation) with this exact structure:

{
  "summary": "2-3 sentence overview of what was discussed",
  "topics": ["topic1", "topic2"],
  "decisions": [{"text": "what was decided", "by": "@username", "message_ts": "1234567890.123456", "importance": "high"}],
  "action_items": [{"text": "what needs to be done", "assignee": "@username", "status": "open"}],
  "key_messages": ["1234567890.123456", "1234567891.123456"]
}

%s

Rules:
- summary: Concise overview of the channel activity
- topics: Main themes discussed (2-5 topics)
- decisions: A DECISION is a conscious choice between alternatives that changes the course of action. Each decision MUST have a clear "who decided" and "what was chosen" and ideally "why" or "instead of what". Do NOT include:
  * Status updates ("X was deployed", "X was updated")
  * Notifications or FYIs ("users were notified about X")
  * Expected behaviors ("caching delay is normal")
  * Routine operations (deploys, releases, merges) UNLESS they involve a non-obvious choice
  Include message_ts for traceability.
  importance levels:
  * "high" — changes architecture, strategy, budget, staffing, product direction, security posture, or has org-wide impact
  * "medium" — changes a process, workflow, or technical approach within a team/project
  * "low" — minor tactical choices (naming, formatting, scheduling, tooling tweaks)
  If only 0-1 true decisions exist, return an empty or single-item array. Do NOT inflate the list.
- action_items: Tasks mentioned or assigned. status is always "open" for new items
- key_messages: Timestamps of the most important messages (max 5)
- If a field has no items, use an empty array []
- Return valid JSON only, no other text

=== MESSAGES ===
%s`

const defaultDigestDaily = `You are creating a daily summary of Slack activity for %s.

%s

Below are per-channel digests from today, including their extracted decisions. Create a cross-channel rollup.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentence overview of the day's activity across all channels",
  "topics": ["cross-channel topic1", "topic2"],
  "decisions": [{"text": "decision text", "by": "@username", "message_ts": "ts", "importance": "high"}],
  "action_items": [{"text": "action text", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

%s

Rules:
- Highlight cross-channel connections (e.g., topics discussed in multiple channels)
- decisions: Consolidate and DEDUPLICATE decisions from channel digests below. If the same decision appears in multiple channels, include it ONCE. A DECISION is a conscious choice between alternatives — NOT a status update, notification, or routine operation. Each decision must answer: "Who chose what, and what changed?"
  importance levels:
  * "high" — changes architecture, strategy, budget, staffing, product direction, security posture, or has org-wide impact
  * "medium" — changes a process, workflow, or technical approach within a team/project
  * "low" — minor tactical choices (naming, formatting, scheduling, tooling tweaks)
  Only include GENUINE decisions. If no real decisions were made today, return an empty array.
- Return valid JSON only

=== CHANNEL DIGESTS ===
%s`

const defaultDigestWeekly = `You are analyzing a week of Slack workspace activity for %s (%s to %s).

%s

Below are daily summaries for the week. Create a weekly trends analysis.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "5-7 sentence overview of the week's key developments",
  "topics": ["trending topic1", "trending topic2"],
  "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts", "importance": "high"}],
  "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

%s

Rules:
- Focus on trends: what topics gained momentum, what was resolved, what's still open
- Highlight the most impactful decisions of the week. DEDUPLICATE: if the same decision appears across multiple days, include it only ONCE. Only include genuine choices/decisions, not status updates.
  importance: "high" (architectural, strategic, budget, org-wide), "medium" (process, workflow, team-level), "low" (tactical, minor)
- Consolidate action items (remove completed, flag overdue)
- Return valid JSON only

=== DAILY DIGESTS ===
%s`

const defaultDigestPeriod = `You are creating a summary of Slack workspace activity for the period %s to %s.

%s

Below are individual digests (channel-level and daily rollups) from that period. Create a comprehensive summary.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "Comprehensive overview of the period's activity, key developments, and outcomes (5-10 sentences)",
  "topics": ["major topic1", "major topic2"],
  "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts", "importance": "high"}],
  "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

%s

Rules:
- Provide a high-level narrative of what happened during this period
- importance: "high" (architectural, strategic, budget, org-wide), "medium" (process, workflow, team-level), "low" (tactical, minor)
- Group related topics across channels and days
- Include only genuine decisions (conscious choices between alternatives), not status updates. DEDUPLICATE across channels and days.
- Consolidate action items: remove completed, highlight outstanding
- Return valid JSON only

=== DIGESTS ===
%s`

const defaultTracksExtract = `You are analyzing Slack messages from channel #%[3]s (%[4]s) to find tracks directed at user @%[1]s (user_id: %[2]s) for the period %[5]s to %[6]s.

Your task: identify actions, requests, tasks, and expectations directed at this specific user in this channel.

CRITICAL: Group related requests into a SINGLE track. If multiple messages discuss the same topic/task (e.g., "reserve equipment", "assess datacenter", "list critical components" all about the same infrastructure project), combine them into ONE comprehensive track — do NOT create separate items for each message about the same topic.

DEDUPLICATION: Review the EXISTING TRACKS section below. If a message relates to an existing track, UPDATE it (set "existing_id" to the track's ID) instead of creating a new one. Only create new tracks for genuinely new topics not covered by existing tracks.

COMPLETION DETECTION: If you see messages confirming that an existing track has been COMPLETED (e.g., "done", "deployed", "opened access", "fixed", "released", status updates showing the task is finished), return the track with "existing_id" set to that track's ID and "status_hint": "done". This is critical — do NOT ignore completion signals just because they are not new tracks.

%[12]s

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "items": [
    {
      "existing_id": null,
      "status_hint": "",
      "text": "clear, actionable description of what needs to be done",
      "context": "detailed context (3-5 sentences): what was discussed, what decisions were made, what is the background, why this matters. Include enough detail so the reader does NOT need to read the original thread.",
      "source_message_ts": "1234567890.123456",
      "priority": "high",
      "due_date": "2025-01-15",
      "requester": {"name": "@username", "user_id": "U123"},
      "category": "task",
      "blocking": "who or what is blocked if this isn't done (empty string if nothing is blocked)",
      "tags": ["project-name", "topic"],
      "decision_summary": "how the group arrived at the current state: what was discussed, what arguments were made, what was the outcome",
      "decision_options": [
        {"option": "description of option A", "supporters": ["@user1"], "pros": "advantages", "cons": "disadvantages"}
      ],
      "participants": [
        {"name": "@username", "user_id": "U123", "stance": "brief summary of this person's position or opinion on the topic"}
      ],
      "source_refs": [
        {"ts": "1234567890.123456", "author": "@username", "text": "key quote or summary of this message (1 sentence)"}
      ],
      "sub_items": [
        {"text": "specific sub-task or checklist item", "status": "open"}
      ],
      "ownership": "mine",
      "ball_on": "U123",
      "owner_user_id": "U456"
    }
  ]
}

%[7]s

Rules:
- GROUPING: This is the most important rule. Multiple messages about the same topic/project/task MUST be merged into ONE track. Look at the broader topic, not individual messages.
- Only extract tracks with a CLEAR actionable request. Skip vague mentions.
- Look for BOTH explicit and implicit tracks:
  * Direct requests: "@user, can you...", "@user please do X"
  * Assignments: "user will handle X", "assigned to @user"
  * Questions expecting action: "@user, what about X?", "can you check X?"
  * Commitments made by the user: "I'll do X", "I will take care of Y"
  * Review requests: "please review", "can you take a look"
  * Follow-ups: "user, any update on X?"
- Do NOT include:
  * Messages FROM the user that don't imply an action (status updates, answers)
  * General discussions where the user is mentioned but nothing is expected
  * Already completed actions (if the user clearly responded "done" or completed the task)
  * Bot messages and automated notifications (unless they require human action)
- priority levels:
  * "high" — blocking others, deadline-sensitive, production issues, executive requests
  * "medium" — normal work tasks, code reviews, questions
  * "low" — nice-to-have, FYIs that might need action later
- due_date: extract if mentioned in the conversation (ISO format YYYY-MM-DD), otherwise omit the field
- source_message_ts: the Slack timestamp of the MOST important message (the original request or assignment)
- context: detailed explanation (3-5 sentences) of the situation, decisions made, and why this action is needed. The reader should understand the full picture without reading the original thread.
- requester: the SPECIFIC person who made the request or assigned the task. Must include name and user_id. If the user committed to doing something themselves, the requester is themselves.
- category: classify the track type. MUST be one of:
  * "code_review" — PR review, code feedback
  * "decision_needed" — a decision must be made
  * "info_request" — someone asked for information/answer
  * "task" — a concrete task to complete
  * "approval" — needs sign-off or approval
  * "follow_up" — check back, provide update, follow up on something
  * "bug_fix" — fix a bug or issue
  * "discussion" — participate in a discussion or give opinion
- blocking: describe who or what is blocked if this track is NOT done. E.g., "Release v2.1 is blocked", "Backend team is waiting", "@designer can't proceed". Leave empty string "" if nothing is explicitly blocked.
- tags: 1-3 short lowercase tags for the project, topic, or area (e.g., ["infrastructure", "security", "q1-planning"]). Extract from context — channel name, project mentions, etc.
- decision_summary: if a decision was discussed or made, describe HOW the group arrived at it — what arguments were raised, who advocated for what, and what the outcome was. This tells the story of the decision process. Leave empty string "" if no decision context.
- decision_options: if a decision is PENDING (not yet made), list the options being considered. Each option should have: description, who supports it, pros and cons. Leave empty array [] if the decision is already made or there are no options.
- participants: list ALL people involved in the discussion about this topic. For each person, summarize their stance/opinion/role. Include people who made decisions, raised concerns, proposed alternatives, or were assigned tasks. Omit participants only if they added nothing meaningful (e.g., just emoji reactions).
- source_refs: list the 2-5 most important messages related to this track. For each, include the Slack timestamp, author, and a 1-sentence summary of what was said. These serve as "footnotes" so the reader can jump to key messages.
- sub_items: break down the track into concrete sub-tasks or checklist items. Each sub-item has "text" (what to do) and "status" ("open" or "done"). If a sub-task was clearly completed in the conversation, set status to "done". Aim for 2-5 sub-items per track. Leave empty array [] if the track is atomic and doesn't need breakdown.
- existing_id: if the track matches an existing track from the EXISTING TRACKS section below, set this to the track's numeric ID. The AI should UPDATE the existing track (merge new info into context, update priority/due_date if changed). Set to null for genuinely new tracks not covered by any existing track. Prefer updating over creating duplicates.
- status_hint: set to "done" if messages clearly confirm the existing track has been completed (someone did the work, deployed, confirmed, etc.). Set to "active" or leave empty ("") for tracks still in progress. This field is ONLY used with existing_id — for new tracks, leave it empty.
- ownership: MUST be one of "mine", "delegated", "watching":
  * "mine" — the task/request is directed at the user, the ball is on them, they need to act
  * "delegated" — the task involves the user's direct report as the responsible person; the user oversees it
  * "watching" — the task/decision affects the user's area but they are not the primary actor; important to stay informed
  * Default to "mine" if unsure — better to surface than miss
- ball_on: the user_id of the person who needs to act NEXT on this track. If the user asked a question and is waiting for a reply, ball_on is the other person's user_id. If someone asked the user something, ball_on is the user's own user_id. Leave empty string "" if unclear.
- owner_user_id: the user_id of the person who "owns" the track. For "mine" tracks, this is the current user. For "delegated" tracks, this is the direct report's user_id. For "watching" tracks, this can be whoever is responsible. Leave empty string "" if same as the current user.
- If no tracks are found, return {"items": []}
- Return valid JSON only, no other text

%[8]s

%[9]s

%[10]s

=== MESSAGES ===
%[11]s`

const defaultTracksUpdate = `You are checking whether new Slack thread messages contain a meaningful update for an existing track.

Track: %[1]s
Previous context: %[2]s
Channel: #%[3]s

%[6]s

%[4]s

New messages since last check (thread replies and channel messages):
%[5]s

Analyze the new messages and determine:
1. Is there a meaningful update related to this track? (progress, completion, blocker, change in scope, deadline change, etc.)
2. If yes, provide a brief updated context summarizing what changed.
3. Does the update suggest the track is now done?

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "has_update": true,
  "updated_context": "brief summary of what changed or progressed",
  "status_hint": "done",
  "ball_on": "U123"
}

Rules:
- has_update: true only if the messages contain genuine progress, completion, or a meaningful change related to the track
- has_update: false for unrelated chatter, bot messages, emoji-only reactions, or off-topic replies in the same thread
- updated_context: 1-2 sentences summarizing the update. Only provided when has_update is true. Omit or leave empty when has_update is false.
- status_hint: one of "done", "active", or "unchanged"
  * "done" — the track appears to be completed based on the messages
  * "active" — there is progress but the track is not yet done
  * "unchanged" — use when has_update is false
- ball_on: the user_id of the person who needs to act next based on the new messages. If someone replied and the ball moved to another person, update this. Leave empty string "" if the ball hasn't moved or if unclear.
- Return valid JSON only, no other text`

const defaultAnalysisUser = `You are analyzing Slack communication patterns for @%s over a 7-day window (%s to %s).

%s

Below are the user's computed statistics and ALL their messages from this period. Perform a deep analysis of their communication patterns, effectiveness, and areas of concern.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "2-3 sentence overview of this person's communication patterns, role, and impact on the team",
  "communication_style": "one of: driver, collaborator, executor, observer, facilitator",
  "decision_role": "one of: decision-maker, approver, contributor, observer, blocker",
  "style_details": "Detailed paragraph evaluating communication quality. What this person does well and what they do poorly. Are they constructive? Do they provide clear context? Do they follow up? Are they responsive? Do they create friction? Be specific with examples from messages.",
  "red_flags": ["list of concerns — be specific: quote messages, name channels, describe situations"],
  "highlights": ["positive contributions — be specific with examples"],
  "recommendations": ["actionable suggestions for improving communication effectiveness"],
  "concerns": ["specific issues: unconstructive behavior, missed commitments, dropped balls, conflicts — cite evidence from messages"],
  "accomplishments": ["what this person delivered/completed/moved forward this week — specific tasks, decisions made, problems solved, features shipped, reviews done. Be concrete: 'launched X', 'resolved issue with Y', 'reviewed and approved Z'"]
}

Communication styles:
- driver: Initiates discussions, sets direction, proposes ideas
- collaborator: Engages actively, builds on others' ideas, provides feedback
- executor: Focused on tasks, updates progress, asks clarifying questions
- observer: Reads but rarely contributes, occasional reactions/short replies
- facilitator: Coordinates between people, summarizes, mediates

Decision roles:
- decision-maker: Makes final calls, sets direction
- approver: Reviews and approves/rejects proposals
- contributor: Provides input and analysis for decisions
- observer: Present but doesn't influence decisions
- blocker: Delays or blocks decision progress (use only with clear evidence)

ANALYSIS FOCUS — be thorough on these:

1. CONSTRUCTIVENESS: Is this person constructive in discussions? Do they offer solutions or just complain? Do they provide helpful feedback or just criticize? Look for passive-aggressive patterns, dismissive responses, or lack of engagement.

2. ACCOUNTABILITY: Did they commit to something and not follow through? Did they miss deadlines mentioned in messages? Did they drop a task or ignore a request? Cite specific messages.

3. CONFLICT & FRICTION: Are there signs of tension with other team members? Unresolved disagreements? Blocking behavior? Dismissing others' input?

4. COMMUNICATION QUALITY: Are messages clear and actionable? Or vague and confusing? Do they provide context? Do they over-communicate or under-communicate? Are they responsive in threads?

5. DECISION PARTICIPATION: Do they engage in decision-making or avoid it? Do they rubber-stamp or provide genuine input? Do they delay decisions?

6. TEAM IMPACT: How does this person affect team dynamics? Are they a multiplier (making others more effective) or a bottleneck?

Red flags to watch for:
- Volume drop >40%% vs previous period → potential disengagement
- Only short messages without substance → low engagement
- Never participates in threads → not collaborating
- Blocked decisions or unresolved conflicts
- Sudden tone shift
- Ignoring questions or requests from teammates
- Making commitments without follow-through
- Unconstructive criticism without offering alternatives

Rules:
- Base analysis ONLY on the data provided — do not invent facts
- Be direct and honest — this is for a manager who needs real insights
- If there are problems, say so clearly with evidence
- If too few messages for meaningful analysis, say so in summary
- red_flags, highlights, recommendations, concerns: use empty arrays [] if nothing notable
- style_details must be a substantive paragraph, not a single sentence
- %s
- Return valid JSON only

=== USER ===
%s`

const defaultAnalysisPeriod = `You are creating a team communication summary for the period %s to %s.

%s

Below are individual analyses for all team members. Create a high-level summary that a manager can quickly scan to understand what needs attention.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentence overview of team communication health. Overall dynamics, collaboration quality, decision-making effectiveness.",
  "attention": [
    "Specific actionable items that need manager attention. Reference specific people and situations. Examples: '@john has gone silent — 0 messages this week, was active last week', '@alice and @bob have unresolved tension in #engineering about deployment process', '@charlie is blocking decisions in #product — 3 threads without resolution'"
  ]
}

Focus on:
1. WHO needs attention and WHY — name names, be specific
2. TEAM DYNAMICS — any friction, silos, or collaboration gaps?
3. RISKS — disengagement, burnout indicators, communication breakdowns
4. DECISIONS — any stuck or delayed decisions? Who is blocking?
5. POSITIVE — who is doing great work that should be recognized?

Rules:
- Be direct and actionable — this is for a busy manager
- Reference specific people by @username
- Each attention item should be one clear, actionable insight
- Don't be vague — "some team members are less active" is useless; "@john dropped from 50 msgs to 3" is useful
- %s
- Return valid JSON only

=== TEAM ANALYSES ===
%s`
