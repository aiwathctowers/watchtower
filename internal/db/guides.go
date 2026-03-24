package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// UpsertCommunicationGuide inserts or replaces a communication guide.
func (db *DB) UpsertCommunicationGuide(g CommunicationGuide) (int64, error) {
	_, err := db.Exec(`INSERT INTO communication_guides
		(user_id, period_from, period_to,
		 message_count, channels_active, threads_initiated, threads_replied,
		 avg_message_length, active_hours_json, volume_change_pct,
		 summary, communication_preferences, availability_patterns, decision_process,
		 situational_tactics, effective_approaches, recommendations, relationship_context,
		 model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, period_from, period_to) DO UPDATE SET
			message_count = excluded.message_count,
			channels_active = excluded.channels_active,
			threads_initiated = excluded.threads_initiated,
			threads_replied = excluded.threads_replied,
			avg_message_length = excluded.avg_message_length,
			active_hours_json = excluded.active_hours_json,
			volume_change_pct = excluded.volume_change_pct,
			summary = excluded.summary,
			communication_preferences = excluded.communication_preferences,
			availability_patterns = excluded.availability_patterns,
			decision_process = excluded.decision_process,
			situational_tactics = excluded.situational_tactics,
			effective_approaches = excluded.effective_approaches,
			recommendations = excluded.recommendations,
			relationship_context = excluded.relationship_context,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		g.UserID, g.PeriodFrom, g.PeriodTo,
		g.MessageCount, g.ChannelsActive, g.ThreadsInitiated, g.ThreadsReplied,
		g.AvgMessageLength, g.ActiveHoursJSON, g.VolumeChangePct,
		g.Summary, g.CommunicationPreferences, g.AvailabilityPatterns, g.DecisionProcess,
		g.SituationalTactics, g.EffectiveApproaches, g.Recommendations, g.RelationshipContext,
		g.Model, g.InputTokens, g.OutputTokens, g.CostUSD)
	if err != nil {
		return 0, fmt.Errorf("upserting communication guide: %w", err)
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM communication_guides WHERE user_id = ? AND period_from = ? AND period_to = ?`,
		g.UserID, g.PeriodFrom, g.PeriodTo).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting guide id after upsert: %w", err)
	}
	return id, nil
}

// GuideFilter specifies criteria for querying communication guides.
type GuideFilter struct {
	UserID   string
	FromUnix float64
	ToUnix   float64
	Limit    int
}

// GetCommunicationGuides returns guides matching the filter, newest first.
func (db *DB) GetCommunicationGuides(f GuideFilter) ([]CommunicationGuide, error) {
	query := guideSelectCols + ` FROM communication_guides`
	var conditions []string
	var args []any

	if f.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.FromUnix > 0 {
		conditions = append(conditions, "period_from >= ?")
		args = append(args, f.FromUnix)
	}
	if f.ToUnix > 0 {
		conditions = append(conditions, "period_to <= ?")
		args = append(args, f.ToUnix)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY period_to DESC, period_from DESC`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	} else {
		query += ` LIMIT 1000`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying communication guides: %w", err)
	}
	defer rows.Close()

	return scanGuides(rows)
}

// GetLatestCommunicationGuide returns the most recent guide for a user, or nil.
func (db *DB) GetLatestCommunicationGuide(userID string) (*CommunicationGuide, error) {
	row := db.QueryRow(guideSelectCols+` FROM communication_guides WHERE user_id = ? ORDER BY period_to DESC LIMIT 1`, userID)
	var g CommunicationGuide
	err := scanGuide(row, &g)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest communication guide: %w", err)
	}
	return &g, nil
}

// GetCommunicationGuidesForWindow returns all guides for a specific time window.
func (db *DB) GetCommunicationGuidesForWindow(periodFrom, periodTo float64) ([]CommunicationGuide, error) {
	rows, err := db.Query(guideSelectCols+` FROM communication_guides
		WHERE period_from = ? AND period_to = ?
		ORDER BY message_count DESC`, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("querying guides for window: %w", err)
	}
	defer rows.Close()
	return scanGuides(rows)
}

// UpsertGuideSummary inserts or replaces a guide summary.
func (db *DB) UpsertGuideSummary(s GuideSummary) error {
	_, err := db.Exec(`INSERT INTO guide_summaries
		(period_from, period_to, summary, tips, model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(period_from, period_to) DO UPDATE SET
			summary = excluded.summary,
			tips = excluded.tips,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		s.PeriodFrom, s.PeriodTo, s.Summary, s.Tips,
		s.Model, s.InputTokens, s.OutputTokens, s.CostUSD)
	return err
}

// GetGuideSummary returns the guide summary for a specific window, or nil.
func (db *DB) GetGuideSummary(periodFrom, periodTo float64) (*GuideSummary, error) {
	row := db.QueryRow(`SELECT id, period_from, period_to, summary, tips,
		model, input_tokens, output_tokens, cost_usd, created_at
		FROM guide_summaries WHERE period_from = ? AND period_to = ?`,
		periodFrom, periodTo)
	var s GuideSummary
	err := row.Scan(&s.ID, &s.PeriodFrom, &s.PeriodTo, &s.Summary, &s.Tips,
		&s.Model, &s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting guide summary: %w", err)
	}
	return &s, nil
}

const guideSelectCols = `SELECT id, user_id, period_from, period_to,
	message_count, channels_active, threads_initiated, threads_replied,
	avg_message_length, active_hours_json, volume_change_pct,
	summary, communication_preferences, availability_patterns, decision_process,
	situational_tactics, effective_approaches, recommendations, relationship_context,
	model, input_tokens, output_tokens, cost_usd, created_at`

func scanGuide(row *sql.Row, g *CommunicationGuide) error {
	return row.Scan(
		&g.ID, &g.UserID, &g.PeriodFrom, &g.PeriodTo,
		&g.MessageCount, &g.ChannelsActive, &g.ThreadsInitiated, &g.ThreadsReplied,
		&g.AvgMessageLength, &g.ActiveHoursJSON, &g.VolumeChangePct,
		&g.Summary, &g.CommunicationPreferences, &g.AvailabilityPatterns, &g.DecisionProcess,
		&g.SituationalTactics, &g.EffectiveApproaches, &g.Recommendations, &g.RelationshipContext,
		&g.Model, &g.InputTokens, &g.OutputTokens, &g.CostUSD, &g.CreatedAt,
	)
}

func scanGuides(rows *sql.Rows) ([]CommunicationGuide, error) {
	var guides []CommunicationGuide
	for rows.Next() {
		var g CommunicationGuide
		err := rows.Scan(
			&g.ID, &g.UserID, &g.PeriodFrom, &g.PeriodTo,
			&g.MessageCount, &g.ChannelsActive, &g.ThreadsInitiated, &g.ThreadsReplied,
			&g.AvgMessageLength, &g.ActiveHoursJSON, &g.VolumeChangePct,
			&g.Summary, &g.CommunicationPreferences, &g.AvailabilityPatterns, &g.DecisionProcess,
			&g.SituationalTactics, &g.EffectiveApproaches, &g.Recommendations, &g.RelationshipContext,
			&g.Model, &g.InputTokens, &g.OutputTokens, &g.CostUSD, &g.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning communication guide row: %w", err)
		}
		guides = append(guides, g)
	}
	return guides, rows.Err()
}
