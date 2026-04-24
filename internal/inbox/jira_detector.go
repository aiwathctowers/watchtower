package inbox

import (
	"context"
	"fmt"
	"time"

	"watchtower/internal/db"
)

// DetectJira scans jira_issues for signals targeting currentUserID since sinceTS
// and inserts new inbox_items. Returns the count of items created.
//
// Implemented signals:
//   - jira_assigned: issues where assignee_account_id = currentUserID and updated_at > sinceTS
//
// No-op signals (schema not available — follow-up required):
//   - jira_comment_mention: requires jira_comments table (not in current schema)
//   - jira_status_change: requires jira_issue_history table (not in current schema)
//   - jira_priority_change: requires jira_issue_history table (not in current schema)
//   - jira_comment_watching: requires jira_watchers table (not in current schema)
//
// TODO(inbox-pulse v2): add comment mention detection once jira_comments is added to schema.
// TODO(inbox-pulse v2): add status/priority change detection once jira_issue_history is added.
// TODO(inbox-pulse v2): add watching detection once jira_watchers is added.
func DetectJira(ctx context.Context, database *db.DB, currentUserID string, sinceTS time.Time) (int, error) {
	if currentUserID == "" {
		return 0, nil
	}
	created := 0
	sinceISO := sinceTS.UTC().Format(time.RFC3339)

	// --- jira_assigned: issues assigned to me updated since sinceTS ---
	// Collect all candidates first, then close rows before running dedup queries.
	// This avoids a deadlock on in-memory SQLite with MaxOpenConns(1).
	type jiraCandidate struct {
		key, summary, updatedAt string
	}
	var assignedCandidates []jiraCandidate
	rows, err := database.Query(`
		SELECT key, summary, updated_at
		FROM jira_issues
		WHERE assignee_account_id = ?
		  AND updated_at > ?
		  AND is_deleted = 0`,
		currentUserID, sinceISO)
	if err != nil {
		return created, fmt.Errorf("jira detector: query jira_issues: %w", err)
	}
	for rows.Next() {
		var c jiraCandidate
		if err := rows.Scan(&c.key, &c.summary, &c.updatedAt); err != nil {
			rows.Close() //nolint:errcheck
			return created, fmt.Errorf("jira detector: scan jira_issues: %w", err)
		}
		assignedCandidates = append(assignedCandidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //nolint:errcheck
		return created, fmt.Errorf("jira detector: rows error: %w", err)
	}
	rows.Close() //nolint:errcheck

	for _, c := range assignedCandidates {
		if jiraInboxExists(database, c.key, c.updatedAt, "jira_assigned") {
			continue
		}
		item := db.InboxItem{
			ChannelID:    c.key,
			MessageTS:    c.updatedAt,
			SenderUserID: c.key, // Jira issue key used as "sender" for routing/display
			TriggerType:  "jira_assigned",
			Snippet:      c.summary,
			ItemClass:    DefaultItemClass("jira_assigned"),
			Status:       "pending",
			Priority:     "medium",
		}
		if _, err := database.CreateInboxItem(item); err == nil {
			created++
		}
	}

	// --- jira_comment_mention: detect when jira_comments table is available ---
	// The jira_comments table is not part of the core schema but may be created by
	// the Jira sync extension or by tests. We check for its existence at runtime and
	// skip gracefully if it is absent.
	if jiraCommentsTableExists(database) {
		type commentCandidate struct {
			issueKey, commentID, body, createdAt string
		}
		var commentCandidates []commentCandidate
		// Look for comments that mention ~currentUserID in the body.
		mentionPattern := "%[~" + currentUserID + "]%"
		cRows, err := database.Query(`
			SELECT issue_key, id, body, created_at
			FROM jira_comments
			WHERE body LIKE ?
			  AND created_at > ?`,
			mentionPattern, sinceISO)
		if err == nil {
			for cRows.Next() {
				var c commentCandidate
				if scanErr := cRows.Scan(&c.issueKey, &c.commentID, &c.body, &c.createdAt); scanErr != nil {
					cRows.Close() //nolint:errcheck
					break
				}
				commentCandidates = append(commentCandidates, c)
			}
			cRows.Close() //nolint:errcheck
		}
		for _, c := range commentCandidates {
			if jiraInboxExists(database, c.issueKey, c.createdAt, "jira_comment_mention") {
				continue
			}
			item := db.InboxItem{
				ChannelID:    c.issueKey,
				MessageTS:    c.createdAt,
				SenderUserID: c.issueKey,
				TriggerType:  "jira_comment_mention",
				Snippet:      c.body,
				ItemClass:    DefaultItemClass("jira_comment_mention"),
				Status:       "pending",
				Priority:     "medium",
			}
			if _, err := database.CreateInboxItem(item); err == nil {
				created++
			}
		}
	}

	// --- jira_status_change: no-op until jira_issue_history table is added ---
	// TODO(inbox-pulse v2): detect status changes on issues assigned to currentUserID
	// using jira_issue_history once that table is added to the schema.

	// --- jira_priority_change: no-op until jira_issue_history table is added ---
	// TODO(inbox-pulse v2): detect priority changes analogous to status_change.

	// --- jira_comment_watching: no-op until jira_watchers table is added ---
	// TODO(inbox-pulse v2): detect new comments on issues where currentUserID is a watcher
	// using jira_watchers once that table is added to the schema.

	return created, nil
}

// jiraCommentsTableExists returns true if the jira_comments table is present
// in the SQLite database. The table is not part of the core schema and may be
// absent when the Jira sync extension has not run yet.
func jiraCommentsTableExists(d *db.DB) bool {
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='jira_comments'`).Scan(&n) //nolint:errcheck
	return n > 0
}

// jiraInboxExists returns true if an inbox_item already exists for the given
// Jira issue key (channel_id), timestamp (message_ts), and trigger_type.
// This prevents duplicate inbox items on repeated detector runs.
func jiraInboxExists(d *db.DB, channelID, messageTS, triggerType string) bool {
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM inbox_items
		WHERE channel_id = ? AND message_ts = ? AND trigger_type = ?`,
		channelID, messageTS, triggerType).Scan(&n) //nolint:errcheck
	return n > 0
}
