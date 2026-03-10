package analysis

const singleUserPrompt = `You are analyzing Slack communication patterns for @%s over a 7-day window (%s to %s).

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

const periodSummaryPrompt = `You are creating a team communication summary for the period %s to %s.

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
