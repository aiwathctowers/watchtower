package targets

// ExtractPromptTemplate is the prompt used by the AI extraction pipeline.
// Placeholders filled by buildExtractPrompt:
//   - RawText verbatim
//   - ENRICHMENTS block
//   - ACTIVE TARGETS snapshot
//   - CURRENT DATE
//   - Optional USER HINT
const ExtractPromptTemplate = `You are a goal-extraction assistant. Given raw text (a Slack message, email paste, or form input), extract actionable targets (goals, tasks, deliverables) and return them as structured JSON.

=== RAW TEXT ===
%s
=== /RAW TEXT ===

%s

%s

=== CURRENT DATE ===
%s
=== /CURRENT DATE ===

%s

Return ONLY a JSON object (no markdown fences, no explanation) matching this exact schema:

{
  "extracted": [
    {
      "text": "string (required, <=280 chars)",
      "intent": "string (optional — why this target matters)",
      "level": "quarter|month|week|day|custom",
      "custom_label": "string (required iff level=custom, empty otherwise)",
      "level_confidence": 0.85,
      "period_start": "YYYY-MM-DD",
      "period_end": "YYYY-MM-DD",
      "priority": "high|medium|low",
      "due_date": "YYYY-MM-DDTHH:MM or empty string",
      "parent_id": 123,
      "secondary_links": [
        {"target_id": 7, "relation": "contributes_to", "confidence": 0.72},
        {"external_ref": "jira:PROJ-123", "relation": "contributes_to"}
      ]
    }
  ],
  "omitted_count": 0,
  "notes": "optional message shown to user in preview"
}

Rules:
- Extract up to 10 targets. If there are more, set omitted_count to the number not extracted and explain briefly in notes.
- level guidance: timeframe >1 month → quarter; 1-4 weeks → month or week; within this week → week; today-only → day; unclear → custom (set custom_label).
- period_start and period_end must be YYYY-MM-DD. period_end >= period_start always.
- parent_id must be an id from the ACTIVE TARGETS snapshot, or null. Do not invent ids.
- secondary_links: max 3 per target. Relation must be one of: contributes_to, blocks, related, duplicates.
  Use target_id (from snapshot) OR external_ref (e.g. "jira:PROJ-123", "slack:C123:1714567890.123456"), never both.
- If a URL in the enrichments block is referenced by an extracted target, include it as a secondary link with external_ref.
- text must be <=280 chars. intent is optional but helpful.
- Return empty extracted array if no actionable targets found.`

// LinkPromptTemplate is the smaller prompt used by LinkExisting for a single manual target.
const LinkPromptTemplate = `You are a goal-linking assistant. Given an existing target and a snapshot of active targets, propose a parent_id and up to 3 secondary links.

=== TARGET ===
[id=%d level=%s period=%s..%s priority=%s status=%s] %s
%s
=== /TARGET ===

%s

Return ONLY a JSON object (no markdown fences):
{
  "parent_id": 123,
  "secondary_links": [
    {"target_id": 7, "relation": "contributes_to", "confidence": 0.8},
    {"external_ref": "jira:PROJ-1", "relation": "related"}
  ]
}

Rules:
- parent_id must be an id from the ACTIVE TARGETS snapshot, or null.
- secondary_links: max 3, relation must be contributes_to|blocks|related|duplicates.
- Only propose links that make semantic sense. Return null parent_id and empty secondary_links if nothing fits.`
