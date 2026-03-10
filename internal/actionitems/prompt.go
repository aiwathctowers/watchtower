package actionitems

const actionItemsPrompt = `You are analyzing Slack messages from channel #%[3]s (%[4]s) to find action items directed at user @%[1]s (user_id: %[2]s) for the period %[5]s to %[6]s.

Your task: identify actions, requests, tasks, and expectations directed at this specific user in this channel.

CRITICAL: Group related requests into a SINGLE action item. If multiple messages discuss the same topic/task (e.g., "reserve equipment", "assess datacenter", "list critical components" all about the same infrastructure project), combine them into ONE comprehensive action item — do NOT create separate items for each message about the same topic.

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "items": [
    {
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
      ]
    }
  ]
}

%[7]s

Rules:
- GROUPING: This is the most important rule. Multiple messages about the same topic/project/task MUST be merged into ONE item. Look at the broader topic, not individual messages.
- Only extract items with a CLEAR actionable request. Skip vague mentions.
- Look for BOTH explicit and implicit action items:
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
- category: classify the action item type. MUST be one of:
  * "code_review" — PR review, code feedback
  * "decision_needed" — a decision must be made
  * "info_request" — someone asked for information/answer
  * "task" — a concrete task to complete
  * "approval" — needs sign-off or approval
  * "follow_up" — check back, provide update, follow up on something
  * "bug_fix" — fix a bug or issue
  * "discussion" — participate in a discussion or give opinion
- blocking: describe who or what is blocked if this action item is NOT done. E.g., "Release v2.1 is blocked", "Backend team is waiting", "@designer can't proceed". Leave empty string "" if nothing is explicitly blocked.
- tags: 1-3 short lowercase tags for the project, topic, or area (e.g., ["infrastructure", "security", "q1-planning"]). Extract from context — channel name, project mentions, etc.
- decision_summary: if a decision was discussed or made, describe HOW the group arrived at it — what arguments were raised, who advocated for what, and what the outcome was. This tells the story of the decision process. Leave empty string "" if no decision context.
- decision_options: if a decision is PENDING (not yet made), list the options being considered. Each option should have: description, who supports it, pros and cons. Leave empty array [] if the decision is already made or there are no options.
- participants: list ALL people involved in the discussion about this topic. For each person, summarize their stance/opinion/role. Include people who made decisions, raised concerns, proposed alternatives, or were assigned tasks. Omit participants only if they added nothing meaningful (e.g., just emoji reactions).
- source_refs: list the 2-5 most important messages related to this action item. For each, include the Slack timestamp, author, and a 1-sentence summary of what was said. These serve as "footnotes" so the reader can jump to key messages.
- sub_items: break down the action item into concrete sub-tasks or checklist items. Each sub-item has "text" (what to do) and "status" ("open" or "done"). If a sub-task was clearly completed in the conversation, set status to "done". Aim for 2-5 sub-items per action item. Leave empty array [] if the action item is atomic and doesn't need breakdown.
- If no action items are found, return {"items": []}
- Return valid JSON only, no other text

=== MESSAGES ===
%[8]s`

// updateCheckPrompt is the prompt template for checking if thread replies
// contain meaningful updates related to an existing action item.
// Args: %[1]s = action item text, %[2]s = action item context,
// %[3]s = channel name, %[4]s = language instruction, %[5]s = new messages
const updateCheckPrompt = `You are checking whether new Slack thread messages contain a meaningful update for an existing action item.

Action item: %[1]s
Previous context: %[2]s
Channel: #%[3]s

%[4]s

New thread messages since last check:
%[5]s

Analyze the new messages and determine:
1. Is there a meaningful update related to this action item? (progress, completion, blocker, change in scope, deadline change, etc.)
2. If yes, provide a brief updated context summarizing what changed.
3. Does the update suggest the action item is now done?

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "has_update": true,
  "updated_context": "brief summary of what changed or progressed",
  "status_hint": "done"
}

Rules:
- has_update: true only if the messages contain genuine progress, completion, or a meaningful change related to the action item
- has_update: false for unrelated chatter, bot messages, emoji-only reactions, or off-topic replies in the same thread
- updated_context: 1-2 sentences summarizing the update. Only provided when has_update is true. Omit or leave empty when has_update is false.
- status_hint: one of "done", "active", or "unchanged"
  * "done" — the action item appears to be completed based on the messages
  * "active" — there is progress but the item is not yet done
  * "unchanged" — use when has_update is false
- Return valid JSON only, no other text`
