package digest

const channelDigestPrompt = `You are analyzing Slack messages from channel #%s for the period %s to %s.

Analyze the messages below and return ONLY a JSON object (no markdown fences, no explanation) with this exact structure:

{
  "summary": "2-3 sentence overview of what was discussed",
  "topics": ["topic1", "topic2"],
  "decisions": [{"text": "what was decided", "by": "@username", "message_ts": "1234567890.123456"}],
  "action_items": [{"text": "what needs to be done", "assignee": "@username", "status": "open"}],
  "key_messages": ["1234567890.123456", "1234567891.123456"]
}

Rules:
- summary: Concise overview. %s
- topics: Main themes discussed (2-5 topics)
- decisions: Only explicit decisions, not suggestions. Include message_ts for traceability
- action_items: Tasks mentioned or assigned. status is always "open" for new items
- key_messages: Timestamps of the most important messages (max 5)
- If a field has no items, use an empty array []
- Return valid JSON only, no other text

=== MESSAGES ===
%s`

const dailyRollupPrompt = `You are creating a daily summary of Slack activity for %s.

Below are per-channel digests from today. Create a cross-channel rollup.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "3-5 sentence overview of the day's activity across all channels",
  "topics": ["cross-channel topic1", "topic2"],
  "decisions": [{"text": "decision text", "by": "@username", "message_ts": "ts"}],
  "action_items": [{"text": "action text", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

Rules:
- Highlight cross-channel connections (e.g., topics discussed in multiple channels)
- Prioritize decisions and action items
- %s
- Return valid JSON only

=== CHANNEL DIGESTS ===
%s`

const weeklyTrendsPrompt = `You are analyzing a week of Slack workspace activity for %s (%s to %s).

Below are daily summaries for the week. Create a weekly trends analysis.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "5-7 sentence overview of the week's key developments",
  "topics": ["trending topic1", "trending topic2"],
  "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts"}],
  "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

Rules:
- Focus on trends: what topics gained momentum, what was resolved, what's still open
- Highlight the most impactful decisions of the week
- Consolidate action items (remove completed, flag overdue)
- %s
- Return valid JSON only

=== DAILY DIGESTS ===
%s`

const periodSummaryPrompt = `You are creating a summary of Slack workspace activity for the period %s to %s.

Below are individual digests (channel-level and daily rollups) from that period. Create a comprehensive summary.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "summary": "Comprehensive overview of the period's activity, key developments, and outcomes (5-10 sentences)",
  "topics": ["major topic1", "major topic2"],
  "decisions": [{"text": "key decision", "by": "@username", "message_ts": "ts"}],
  "action_items": [{"text": "outstanding action", "assignee": "@username", "status": "open"}],
  "key_messages": []
}

Rules:
- Provide a high-level narrative of what happened during this period
- Group related topics across channels and days
- Include only the most significant decisions (not every minor one)
- Consolidate action items: remove completed, highlight outstanding
- %s
- Return valid JSON only

=== DIGESTS ===
%s`
