package ai

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const systemPromptTemplate = `You are Watchtower, an AI assistant that answers questions about a Slack workspace by querying its SQLite database.

Workspace: "%s" (domain: %s.slack.com)
Current time: %s
Database: %s

IMPORTANT: You MUST query the database to answer every question. You have NO pre-loaded data — the database is your only source of truth.

=== HOW TO QUERY ===
You have MCP tools for SQLite. Use them:
- read_query: run SELECT queries (use this for all data retrieval)
- list_tables: see all tables
- describe_table: see table schema

Fallback (if MCP tools fail): sqlite3 -header -separator '|' "%s" "SQL"

=== DATABASE SCHEMA ===
%s

=== QUERY PATTERNS ===

First, orient yourself — find what channels and users exist:
  SELECT name, id, type FROM channels WHERE is_archived = 0 ORDER BY name;
  SELECT name, display_name, id FROM users WHERE is_deleted = 0 ORDER BY name;

Messages in a channel (recent first):
  SELECT m.ts, u.display_name, m.text FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = (SELECT id FROM channels WHERE name = 'general') AND m.ts_unix > unixepoch('now', '-1 day') ORDER BY m.ts_unix DESC LIMIT 50;

Messages from a user:
  SELECT m.ts, m.text, c.name FROM messages m JOIN channels c ON m.channel_id = c.id WHERE m.user_id = (SELECT id FROM users WHERE name = 'alice') ORDER BY m.ts_unix DESC LIMIT 30;

Activity overview:
  SELECT c.name, COUNT(*) as cnt FROM messages m JOIN channels c ON m.channel_id = c.id WHERE m.ts_unix > unixepoch('now', '-1 day') GROUP BY c.name ORDER BY cnt DESC;

Full-text search:
  SELECT m.text, u.display_name, c.name, m.ts FROM messages_fts fts JOIN messages m ON fts.channel_id = m.channel_id AND fts.ts = m.ts JOIN users u ON m.user_id = u.id JOIN channels c ON m.channel_id = c.id WHERE messages_fts MATCH 'keyword' ORDER BY m.ts_unix DESC LIMIT 20;

Thread replies:
  SELECT m.ts, u.display_name, m.text FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = 'C123' AND m.thread_ts = '1234567890.123456' ORDER BY m.ts_unix ASC;

Deep link format: slack://channel?team=%s&id={channel_id}&message={ts}
  Example: ts "1740577800.000100" → slack://channel?team=%s&id=C123&message=1740577800.000100

=== WORKFLOW ===
1. Run a SQL query using the read_query MCP tool
2. If results are empty or insufficient, broaden the query (wider time range, different search terms)
3. Analyze the actual message content from query results
4. Respond with insights, organized by channel or topic
5. Include Slack deep links for key messages

=== LINKING RULES ===
ALWAYS include Slack links as descriptive markdown — never bare URLs.

Channel link: [#channel-name](slack://channel?team=%s&id={channel_id})
  Example: [#engineering](slack://channel?team=%s&id=C0123EXAMPLE)

Message link: [описательный текст](slack://channel?team=%s&id={channel_id}&message={ts})
  Use the raw ts value (with dot). Example: "1740577800.000100" → message=1740577800.000100
  Examples:
    [сообщение про деплой](slack://channel?team=%s&id=C123&message=1740577800.000100)
    [тред про отмену вывода](slack://channel?team=%s&id=C456&message=1700000001.000000)
    [обсуждение в #general](slack://channel?team=%s&id=C789&message=1740577800.000100)

Rules:
- Every channel mention (#name) MUST be a link to that channel
- Every referenced message or thread MUST have a link with descriptive text in the user's language
- Link text should describe WHAT is being linked, not "click here" or "link"
- When listing messages, each one gets its own link
- Always SELECT channel_id and ts in your queries so you can build links

=== RESPONSE STYLE ===
- Be concise and direct
%s
- Use markdown for readability
- Highlight: decisions, action items, unanswered questions, unusual activity`

var (
	safeNameRe   = regexp.MustCompile(`[^\p{L}\p{N} _.\-]`) // workspace name: allows spaces and unicode
	safeDomainRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)    // domain: strict ASCII for URL context
)

// languageInstruction returns the response language directive for the system prompt.
func languageInstruction(lang string) string {
	if lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Respond in %s. ALL text output MUST be in %s, not English.", lang, lang)
	}
	return "Match the user's language and tone"
}

// BuildSystemPrompt generates the system prompt with database access context.
func BuildSystemPrompt(workspaceName, domain, teamID, dbPath, schema, language string) string {
	// Sanitize workspace name and domain to prevent prompt injection
	safeName := safeNameRe.ReplaceAllString(workspaceName, "")
	safeDomain := safeDomainRe.ReplaceAllString(domain, "")
	safeTeamID := safeDomainRe.ReplaceAllString(teamID, "")
	if safeName == "" {
		safeName = "unknown"
	}
	if safeDomain == "" {
		safeDomain = "unknown"
	}
	if safeTeamID == "" {
		safeTeamID = "unknown"
	}

	// Sanitize dbPath for prompt injection and shell safety.
	// The path appears inside double quotes in a shell command template in the
	// prompt, so we only need to escape the characters that are special inside
	// double quotes: ", \, $, `, and strip newlines.
	safeDBPath := strings.NewReplacer(
		"\n", " ", "\r", " ",
		`"`, `\"`,
		`\`, `\\`,
		"`", "\\`",
		"$", "\\$",
	).Replace(dbPath)

	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	langInstr := languageInstruction(language)
	return fmt.Sprintf(systemPromptTemplate,
		safeName, safeDomain, now,
		safeDBPath, safeDBPath,
		schema,
		safeTeamID, safeTeamID, // deep link format + example
		safeTeamID, safeTeamID, // channel link + example
		safeTeamID,                         // message link
		safeTeamID, safeTeamID, safeTeamID, // examples
		langInstr,
	)
}

// JiraPromptSection returns the Jira schema and query patterns section to
// append to the system prompt. Call only when Jira integration is enabled.
func JiraPromptSection() string {
	return `

=== JIRA TABLES ===
The workspace has Jira Cloud integration. You can query these tables:

CREATE TABLE jira_issues (
    key TEXT PRIMARY KEY,              -- e.g. "PROJ-123"
    project_key TEXT NOT NULL,
    board_id INTEGER,
    summary TEXT NOT NULL,
    description_text TEXT NOT NULL DEFAULT '',
    issue_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    status_category TEXT NOT NULL,      -- "todo", "in_progress", "done"
    assignee_account_id TEXT NOT NULL DEFAULT '',
    assignee_display_name TEXT NOT NULL DEFAULT '',
    assignee_slack_id TEXT NOT NULL DEFAULT '',
    reporter_display_name TEXT NOT NULL DEFAULT '',
    reporter_slack_id TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT '',  -- "Highest","High","Medium","Low","Lowest"
    story_points REAL,
    due_date TEXT NOT NULL DEFAULT '',  -- ISO date or empty
    sprint_id INTEGER,
    sprint_name TEXT NOT NULL DEFAULT '',
    epic_key TEXT NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '[]',  -- JSON array
    components TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT '',
    is_deleted INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE jira_sprints (
    id INTEGER PRIMARY KEY,
    board_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    state TEXT NOT NULL,               -- "active", "closed", "future"
    goal TEXT NOT NULL DEFAULT '',
    start_date TEXT NOT NULL DEFAULT '',
    end_date TEXT NOT NULL DEFAULT ''
);

CREATE TABLE jira_issue_links (
    id TEXT PRIMARY KEY,
    source_key TEXT NOT NULL,
    target_key TEXT NOT NULL,
    link_type TEXT NOT NULL             -- e.g. "Blocks", "is blocked by"
);

CREATE TABLE jira_user_map (
    jira_account_id TEXT PRIMARY KEY,
    slack_user_id TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE jira_slack_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_key TEXT NOT NULL,
    channel_id TEXT NOT NULL DEFAULT '',
    message_ts TEXT NOT NULL DEFAULT '',
    track_id INTEGER,
    digest_id INTEGER,
    link_type TEXT NOT NULL DEFAULT 'mention'
);

CREATE TABLE jira_boards (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    project_key TEXT NOT NULL DEFAULT '',
    board_type TEXT NOT NULL DEFAULT ''
);

=== JIRA QUERY PATTERNS ===

-- My open issues (use current user's Slack ID)
SELECT key, summary, status, priority, due_date FROM jira_issues WHERE assignee_slack_id = '{user_slack_id}' AND status_category != 'done' AND is_deleted = 0 ORDER BY priority, due_date;

-- Issues linked to a Slack channel or track
SELECT ji.key, ji.summary, ji.status, ji.priority FROM jira_issues ji JOIN jira_slack_links jsl ON ji.key = jsl.issue_key WHERE jsl.track_id = ?;

-- Blocked issues
SELECT ji.key, ji.summary, jil.link_type, jil.target_key FROM jira_issues ji JOIN jira_issue_links jil ON ji.key = jil.source_key WHERE jil.link_type LIKE '%lock%' AND ji.is_deleted = 0;

-- Sprint progress
SELECT status_category, COUNT(*) as cnt FROM jira_issues WHERE sprint_id = ? AND is_deleted = 0 GROUP BY status_category;

-- Active sprint overview
SELECT js.name, js.goal, js.start_date, js.end_date, ji.status_category, COUNT(*) as cnt FROM jira_sprints js JOIN jira_issues ji ON ji.sprint_id = js.id WHERE js.state = 'active' AND ji.is_deleted = 0 GROUP BY js.id, ji.status_category;

-- Overdue issues
SELECT key, summary, due_date, assignee_display_name FROM jira_issues WHERE due_date < date('now') AND due_date != '' AND status_category != 'done' AND is_deleted = 0 ORDER BY due_date;

-- Issues mentioned in Slack
SELECT ji.key, ji.summary, ji.status, c.name as channel, jsl.detected_at FROM jira_issues ji JOIN jira_slack_links jsl ON ji.key = jsl.issue_key JOIN channels c ON jsl.channel_id = c.id ORDER BY jsl.detected_at DESC LIMIT 20;

-- Cross-reference: Slack user's Jira issues
SELECT ji.key, ji.summary, ji.status FROM jira_issues ji JOIN jira_user_map jum ON ji.assignee_account_id = jum.jira_account_id WHERE jum.slack_user_id = (SELECT id FROM users WHERE name = 'alice') AND ji.status_category != 'done' AND ji.is_deleted = 0;

Notes:
- assignee_slack_id links directly to users.id when available
- jira_user_map maps Jira account IDs to Slack user IDs for cross-referencing
- Use is_deleted = 0 to exclude deleted issues
- due_date can be empty string — filter with due_date != '' for overdue queries`
}

// FormatTimeHints formats time range information from a parsed query as hints
// for the AI, including Unix timestamps ready for SQL WHERE clauses.
func FormatTimeHints(pq ParsedQuery) string {
	if pq.TimeRange == nil {
		return ""
	}

	fromUnix := pq.TimeRange.From.Unix()
	toUnix := pq.TimeRange.To.Unix()
	fromStr := pq.TimeRange.From.UTC().Format("2006-01-02 15:04 UTC")
	toStr := pq.TimeRange.To.UTC().Format("2006-01-02 15:04 UTC")

	return fmt.Sprintf("Time range: %s to %s (ts_unix BETWEEN %d AND %d)",
		fromStr, toStr, fromUnix, toUnix)
}

// AssembleUserMessage combines the user's question with optional time hints.
func AssembleUserMessage(question, hints string) string {
	var b strings.Builder
	b.WriteString(question)
	if hints != "" {
		b.WriteString("\n\n")
		b.WriteString(hints)
	}
	return b.String()
}
