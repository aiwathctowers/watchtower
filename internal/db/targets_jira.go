package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// CreateTargetFromJiraIssue creates a target from a Jira issue with dedup.
// If a target with source_type='jira' and source_id=issue.Key already exists,
// it returns the existing target without creating a duplicate.
func (db *DB) CreateTargetFromJiraIssue(issue JiraIssue) (*Target, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning jira target tx: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(`SELECT `+targetSelectCols+` FROM targets WHERE source_type = 'jira' AND source_id = ?`, issue.Key)
	existing, err := scanTarget(row)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking existing jira target %s: %w", issue.Key, err)
	}

	priority := jiraPriorityToTargetPriority(issue.Priority)
	today := issue.DueDate

	res, err := tx.Exec(`INSERT INTO targets
		(text, intent, level, custom_label, period_start, period_end,
		 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
		 tags, sub_items, notes, progress, source_type, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issue.Summary, "", "day", "", issue.DueDate, today,
		"todo", priority, "mine", "", issue.DueDate, "", "",
		"[]", "[]", "[]", 0.0, "jira", issue.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("creating target from jira issue %s: %w", issue.Key, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing jira target tx: %w", err)
	}

	id, _ := res.LastInsertId()
	created, err := db.GetTargetByID(int(id))
	if err != nil {
		return &Target{
			ID: int(id), Text: issue.Summary, Status: "todo",
			Priority: priority, Ownership: "mine", DueDate: issue.DueDate,
			SourceType: "jira", SourceID: issue.Key, Tags: "[]", SubItems: "[]",
		}, err
	}
	return created, nil
}

// jiraPriorityToTargetPriority maps Jira priority names to target priority levels.
func jiraPriorityToTargetPriority(jiraPriority string) string {
	switch strings.ToLower(jiraPriority) {
	case "highest", "high":
		return "high"
	case "medium":
		return "medium"
	case "low", "lowest":
		return "low"
	default:
		return "medium"
	}
}

// SyncJiraTargetStatuses synchronizes target statuses from linked Jira issues.
func (db *DB) SyncJiraTargetStatuses() (int, error) {
	rows, err := db.Query(`SELECT ` + targetSelectCols + ` FROM targets WHERE source_type = 'jira' AND status NOT IN ('done', 'dismissed')`)
	if err != nil {
		return 0, fmt.Errorf("querying jira targets: %w", err)
	}
	defer rows.Close()

	var targets []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return 0, fmt.Errorf("scanning jira target: %w", err)
		}
		targets = append(targets, *t)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	synced := 0
	for _, t := range targets {
		issue, err := db.GetJiraIssueByKey(t.SourceID)
		if err != nil {
			log.Printf("jira-targets: error fetching issue %s: %v", t.SourceID, err)
			continue
		}
		if issue == nil {
			continue
		}

		cat := strings.ToLower(issue.StatusCategory)
		var newStatus string
		switch {
		case cat == "done":
			newStatus = "done"
		case cat == "in_progress" && t.Status == "todo":
			newStatus = "in_progress"
		default:
			continue
		}

		if err := db.UpdateTargetStatus(t.ID, newStatus); err != nil {
			log.Printf("jira-targets: error updating target %d status to %s: %v", t.ID, newStatus, err)
			continue
		}
		synced++
	}
	return synced, nil
}
