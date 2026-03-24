package db

import (
	"database/sql"
	"fmt"
)

// UpsertBriefing inserts or replaces a briefing based on the unique constraint (user_id, date).
func (db *DB) UpsertBriefing(b Briefing) (int64, error) {
	_, err := db.Exec(`INSERT INTO briefings (workspace_id, user_id, date, role, attention, your_day, what_happened, team_pulse, coaching, model, input_tokens, output_tokens, cost_usd, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, date) DO UPDATE SET
			workspace_id = excluded.workspace_id,
			role = excluded.role,
			attention = excluded.attention,
			your_day = excluded.your_day,
			what_happened = excluded.what_happened,
			team_pulse = excluded.team_pulse,
			coaching = excluded.coaching,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			prompt_version = excluded.prompt_version,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		b.WorkspaceID, b.UserID, b.Date, b.Role,
		b.Attention, b.YourDay, b.WhatHappened, b.TeamPulse, b.Coaching,
		b.Model, b.InputTokens, b.OutputTokens, b.CostUSD, b.PromptVersion)
	if err != nil {
		return 0, fmt.Errorf("upserting briefing: %w", err)
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM briefings WHERE user_id = ? AND date = ?`,
		b.UserID, b.Date).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting briefing id after upsert: %w", err)
	}
	return id, nil
}

// GetBriefing returns a briefing for a given user and date, or nil if not found.
func (db *DB) GetBriefing(userID, date string) (*Briefing, error) {
	row := db.QueryRow(briefingSelectCols+` FROM briefings WHERE user_id = ? AND date = ?`, userID, date)
	return scanBriefing(row)
}

// GetBriefingByID returns a briefing by ID, or nil if not found.
func (db *DB) GetBriefingByID(id int) (*Briefing, error) {
	row := db.QueryRow(briefingSelectCols+` FROM briefings WHERE id = ?`, id)
	return scanBriefing(row)
}

// GetRecentBriefings returns briefings for a user, newest first.
func (db *DB) GetRecentBriefings(userID string, limit int) ([]Briefing, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(briefingSelectCols+` FROM briefings WHERE user_id = ? ORDER BY date DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing briefings: %w", err)
	}
	defer rows.Close()

	var briefings []Briefing
	for rows.Next() {
		var b Briefing
		if err := scanBriefingRow(rows, &b); err != nil {
			return nil, fmt.Errorf("scanning briefing: %w", err)
		}
		briefings = append(briefings, b)
	}
	return briefings, rows.Err()
}

// MarkBriefingRead marks a briefing as read with the current timestamp.
func (db *DB) MarkBriefingRead(id int) error {
	_, err := db.Exec(`UPDATE briefings SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ? AND read_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("marking briefing read: %w", err)
	}
	return nil
}

const briefingSelectCols = `SELECT id, workspace_id, user_id, date, role, attention, your_day, what_happened, team_pulse, coaching, model, input_tokens, output_tokens, cost_usd, prompt_version, read_at, created_at`

func scanBriefing(row *sql.Row) (*Briefing, error) {
	var b Briefing
	err := row.Scan(&b.ID, &b.WorkspaceID, &b.UserID, &b.Date, &b.Role,
		&b.Attention, &b.YourDay, &b.WhatHappened, &b.TeamPulse, &b.Coaching,
		&b.Model, &b.InputTokens, &b.OutputTokens, &b.CostUSD, &b.PromptVersion,
		&b.ReadAt, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning briefing: %w", err)
	}
	return &b, nil
}

type briefingScanner interface {
	Scan(dest ...any) error
}

func scanBriefingRow(scanner briefingScanner, b *Briefing) error {
	return scanner.Scan(&b.ID, &b.WorkspaceID, &b.UserID, &b.Date, &b.Role,
		&b.Attention, &b.YourDay, &b.WhatHappened, &b.TeamPulse, &b.Coaching,
		&b.Model, &b.InputTokens, &b.OutputTokens, &b.CostUSD, &b.PromptVersion,
		&b.ReadAt, &b.CreatedAt)
}
