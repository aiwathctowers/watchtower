package digest

const channelDigestPrompt = `You are analyzing Slack messages from channel #%s for the period %s to %s.

%s

Analyze the messages below and return ONLY a JSON object (no markdown fences, no explanation) with this exact structure:

{
  "summary": "2-3 sentence overview of what was discussed",
  "topics": ["topic1", "topic2"],
  "decisions": [{"text": "what was decided", "by": "@username", "message_ts": "1234567890.123456", "importance": "high"}],
  "action_items": [{"text": "what needs to be done", "assignee": "@username", "status": "open"}],
  "key_messages": ["1234567890.123456", "1234567891.123456"],
  "situations": [{"topic": "Auth refactor ownership", "type": "collaboration", "participants": [{"user_id": "U123456", "role": "initiator"}, {"user_id": "U789012", "role": "contributor"}], "dynamic": "what happened between people", "outcome": "result or current state", "red_flags": [], "observations": ["notable observation"], "message_refs": ["1234567890.123456"]}]
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
- situations: Notable INTERACTIONS between people (max 3-5). Capture dynamics BETWEEN people, not individual behavior. Each situation has:
  * topic: Short label for the topic/project (e.g. "Auth refactor ownership", "Sprint planning conflict")
  * type: "bottleneck", "conflict", "collaboration", "knowledge_transfer", "decision_deadlock", "mentoring", "escalation", "handoff", "misalignment"
  * participants: Each person involved with their role ("blocker", "affected", "initiator", "resolver", "mediator", "mentor", "mentee", "decision_maker", "contributor")
  * dynamic: What happened between the participants (1-2 sentences)
  * outcome: Result or current state (1 sentence)
  * red_flags: Specific concerns from this situation (empty [] if none)
  * observations: Notable patterns or behaviors observed (empty [] if none)
  * message_refs: Slack timestamps of key messages (e.g. ["1234567890.123456"])
  Use Slack user IDs (e.g. U123456) for participant user_id. Only include situations where the interaction pattern is noteworthy — skip routine exchanges. If no notable situations, return empty array [].
- If a field has no items, use an empty array []
- Return valid JSON only, no other text

=== MESSAGES ===
%s`

const dailyRollupPrompt = `You are creating a daily summary of Slack activity for %s.

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

const weeklyTrendsPrompt = `You are analyzing a week of Slack workspace activity for %s (%s to %s).

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

const periodSummaryPrompt = `You are creating a summary of Slack workspace activity for the period %s to %s.

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
