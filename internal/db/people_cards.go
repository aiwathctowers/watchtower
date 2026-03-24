package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// UpsertPeopleCard inserts or replaces a people card.
func (db *DB) UpsertPeopleCard(card PeopleCard) (int64, error) {
	if card.Status == "" {
		card.Status = "active"
	}
	_, err := db.Exec(`INSERT INTO people_cards
		(user_id, period_from, period_to,
		 message_count, channels_active, threads_initiated, threads_replied,
		 avg_message_length, active_hours_json, volume_change_pct,
		 summary, communication_style, decision_role, red_flags, highlights, accomplishments,
		 communication_guide, decision_style, tactics, relationship_context, status,
		 model, input_tokens, output_tokens, cost_usd, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, period_from, period_to) DO UPDATE SET
			message_count = excluded.message_count,
			channels_active = excluded.channels_active,
			threads_initiated = excluded.threads_initiated,
			threads_replied = excluded.threads_replied,
			avg_message_length = excluded.avg_message_length,
			active_hours_json = excluded.active_hours_json,
			volume_change_pct = excluded.volume_change_pct,
			summary = excluded.summary,
			communication_style = excluded.communication_style,
			decision_role = excluded.decision_role,
			red_flags = excluded.red_flags,
			highlights = excluded.highlights,
			accomplishments = excluded.accomplishments,
			communication_guide = excluded.communication_guide,
			decision_style = excluded.decision_style,
			tactics = excluded.tactics,
			relationship_context = excluded.relationship_context,
			status = excluded.status,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			prompt_version = excluded.prompt_version,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		card.UserID, card.PeriodFrom, card.PeriodTo,
		card.MessageCount, card.ChannelsActive, card.ThreadsInitiated, card.ThreadsReplied,
		card.AvgMessageLength, card.ActiveHoursJSON, card.VolumeChangePct,
		card.Summary, card.CommunicationStyle, card.DecisionRole,
		card.RedFlags, card.Highlights, card.Accomplishments,
		card.CommunicationGuide, card.DecisionStyle, card.Tactics, card.RelationshipContext, card.Status,
		card.Model, card.InputTokens, card.OutputTokens, card.CostUSD, card.PromptVersion)
	if err != nil {
		return 0, fmt.Errorf("upserting people card: %w", err)
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM people_cards WHERE user_id = ? AND period_from = ? AND period_to = ?`,
		card.UserID, card.PeriodFrom, card.PeriodTo).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting people card id after upsert: %w", err)
	}
	return id, nil
}

// PeopleCardFilter specifies criteria for querying people cards.
type PeopleCardFilter struct {
	UserID   string
	FromUnix float64
	ToUnix   float64
	Limit    int
}

// GetPeopleCards returns people cards matching the filter, newest first.
func (db *DB) GetPeopleCards(f PeopleCardFilter) ([]PeopleCard, error) {
	query := peopleCardSelectCols + ` FROM people_cards`
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
		return nil, fmt.Errorf("querying people cards: %w", err)
	}
	defer rows.Close()
	return scanPeopleCards(rows)
}

// GetPeopleCardsForWindow returns all cards for a specific time window.
func (db *DB) GetPeopleCardsForWindow(periodFrom, periodTo float64) ([]PeopleCard, error) {
	rows, err := db.Query(peopleCardSelectCols+` FROM people_cards
		WHERE period_from = ? AND period_to = ?
		ORDER BY message_count DESC`, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("querying people cards for window: %w", err)
	}
	defer rows.Close()
	return scanPeopleCards(rows)
}

// GetLatestPeopleCard returns the most recent card for a user, or nil.
func (db *DB) GetLatestPeopleCard(userID string) (*PeopleCard, error) {
	row := db.QueryRow(peopleCardSelectCols+` FROM people_cards WHERE user_id = ? ORDER BY period_to DESC LIMIT 1`, userID)
	var card PeopleCard
	err := scanPeopleCard(row, &card)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest people card: %w", err)
	}
	return &card, nil
}

// GetPeopleCardHistory returns a user's card history, newest first.
func (db *DB) GetPeopleCardHistory(userID string, limit int) ([]PeopleCard, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Query(peopleCardSelectCols+` FROM people_cards WHERE user_id = ? ORDER BY period_to DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying people card history: %w", err)
	}
	defer rows.Close()
	return scanPeopleCards(rows)
}

// ChannelSignals groups signals for one user from one channel digest.
type ChannelSignals struct {
	ChannelID   string
	ChannelName string
	PeriodFrom  float64
	PeriodTo    float64
	Signals     []SignalEntry
}

// SignalEntry is a typed observation parsed from digest people_signals JSON.
type SignalEntry struct {
	Type       string `json:"type"`
	Detail     string `json:"detail"`
	EvidenceTS string `json:"evidence_ts,omitempty"`
}

type personSignalsJSON struct {
	UserID  string        `json:"user_id"`
	Signals []SignalEntry `json:"signals"`
}

// GetPeopleSignalsForUser returns all signals for a specific user
// from channel digests within the given time window.
func (db *DB) GetPeopleSignalsForUser(userID string, from, to float64) ([]ChannelSignals, error) {
	// Pre-load channel names to avoid cursor conflicts on single-connection DB.
	channelNames := make(map[string]string)
	chRows, err := db.Query(`SELECT id, name FROM channels`)
	if err == nil {
		defer chRows.Close()
		for chRows.Next() {
			var id, name string
			if chRows.Scan(&id, &name) == nil {
				channelNames[id] = name
			}
		}
	}

	rows, err := db.Query(`SELECT channel_id, period_from, period_to, people_signals
		FROM digests
		WHERE type = 'channel'
		  AND period_from >= ?
		  AND period_to <= ?
		  AND people_signals IS NOT NULL
		  AND people_signals != '[]'`, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying people signals: %w", err)
	}
	defer rows.Close()

	var result []ChannelSignals
	for rows.Next() {
		var channelID, signalsJSON string
		var pFrom, pTo float64
		if err := rows.Scan(&channelID, &pFrom, &pTo, &signalsJSON); err != nil {
			return nil, fmt.Errorf("scanning people signals row: %w", err)
		}

		var allPersons []personSignalsJSON
		if err := json.Unmarshal([]byte(signalsJSON), &allPersons); err != nil {
			continue // skip malformed JSON
		}

		for _, ps := range allPersons {
			if ps.UserID == userID && len(ps.Signals) > 0 {
				chName := channelID
				if name, ok := channelNames[channelID]; ok {
					chName = name
				}
				result = append(result, ChannelSignals{
					ChannelID:   channelID,
					ChannelName: chName,
					PeriodFrom:  pFrom,
					PeriodTo:    pTo,
					Signals:     ps.Signals,
				})
			}
		}
	}
	return result, rows.Err()
}

// GetAllPeopleSignals returns signals for ALL users, grouped by user.
func (db *DB) GetAllPeopleSignals(from, to float64) (map[string][]ChannelSignals, error) {
	channelNames := make(map[string]string)
	chRows, err := db.Query(`SELECT id, name FROM channels`)
	if err == nil {
		defer chRows.Close()
		for chRows.Next() {
			var id, name string
			if chRows.Scan(&id, &name) == nil {
				channelNames[id] = name
			}
		}
	}

	rows, err := db.Query(`SELECT channel_id, period_from, period_to, people_signals
		FROM digests
		WHERE type = 'channel'
		  AND period_from >= ?
		  AND period_to <= ?
		  AND people_signals IS NOT NULL
		  AND people_signals != '[]'`, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying all people signals: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]ChannelSignals)
	for rows.Next() {
		var channelID, signalsJSON string
		var pFrom, pTo float64
		if err := rows.Scan(&channelID, &pFrom, &pTo, &signalsJSON); err != nil {
			return nil, fmt.Errorf("scanning people signals row: %w", err)
		}

		var allPersons []personSignalsJSON
		if err := json.Unmarshal([]byte(signalsJSON), &allPersons); err != nil {
			continue
		}

		chName := channelID
		if name, ok := channelNames[channelID]; ok {
			chName = name
		}

		for _, ps := range allPersons {
			if len(ps.Signals) > 0 {
				result[ps.UserID] = append(result[ps.UserID], ChannelSignals{
					ChannelID:   channelID,
					ChannelName: chName,
					PeriodFrom:  pFrom,
					PeriodTo:    pTo,
					Signals:     ps.Signals,
				})
			}
		}
	}
	return result, rows.Err()
}

// ChannelSituations groups situations for one user from one channel digest.
type ChannelSituations struct {
	ChannelID   string
	ChannelName string
	PeriodFrom  float64
	PeriodTo    float64
	Situations  []Situation
}

// GetSituationsForUser returns all situations involving a specific user
// from channel digests within the given time window.
func (db *DB) GetSituationsForUser(userID string, from, to float64) ([]ChannelSituations, error) {
	channelNames := make(map[string]string)
	chRows, err := db.Query(`SELECT id, name FROM channels`)
	if err == nil {
		defer chRows.Close()
		for chRows.Next() {
			var id, name string
			if chRows.Scan(&id, &name) == nil {
				channelNames[id] = name
			}
		}
	}

	// Use digest_participants to efficiently find digests involving this user
	rows, err := db.Query(`SELECT d.channel_id, d.period_from, d.period_to, d.situations
		FROM digests d
		INNER JOIN digest_participants dp ON dp.digest_id = d.id
		WHERE dp.user_id = ?
		  AND d.type = 'channel'
		  AND d.period_from >= ?
		  AND d.period_to <= ?
		  AND d.situations != '[]'`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying situations for user: %w", err)
	}
	defer rows.Close()

	var result []ChannelSituations
	for rows.Next() {
		var channelID, situationsJSON string
		var pFrom, pTo float64
		if err := rows.Scan(&channelID, &pFrom, &pTo, &situationsJSON); err != nil {
			return nil, fmt.Errorf("scanning situations row: %w", err)
		}

		var allSituations []Situation
		if err := json.Unmarshal([]byte(situationsJSON), &allSituations); err != nil {
			continue
		}

		// Filter situations where this user is a participant
		var userSituations []Situation
		for _, s := range allSituations {
			for _, p := range s.Participants {
				if p.UserID == userID {
					userSituations = append(userSituations, s)
					break
				}
			}
		}

		if len(userSituations) > 0 {
			chName := channelID
			if name, ok := channelNames[channelID]; ok {
				chName = name
			}
			result = append(result, ChannelSituations{
				ChannelID:   channelID,
				ChannelName: chName,
				PeriodFrom:  pFrom,
				PeriodTo:    pTo,
				Situations:  userSituations,
			})
		}
	}
	return result, rows.Err()
}

// GetSituationsForWindow returns all situations from all channel digests
// in the given time window, grouped by user.
func (db *DB) GetSituationsForWindow(from, to float64) (map[string][]ChannelSituations, error) {
	channelNames := make(map[string]string)
	chRows, err := db.Query(`SELECT id, name FROM channels`)
	if err == nil {
		defer chRows.Close()
		for chRows.Next() {
			var id, name string
			if chRows.Scan(&id, &name) == nil {
				channelNames[id] = name
			}
		}
	}

	rows, err := db.Query(`SELECT channel_id, period_from, period_to, situations
		FROM digests
		WHERE type = 'channel'
		  AND period_from >= ?
		  AND period_to <= ?
		  AND situations != '[]'`, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying situations for window: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]ChannelSituations)
	for rows.Next() {
		var channelID, situationsJSON string
		var pFrom, pTo float64
		if err := rows.Scan(&channelID, &pFrom, &pTo, &situationsJSON); err != nil {
			return nil, fmt.Errorf("scanning situations row: %w", err)
		}

		var allSituations []Situation
		if err := json.Unmarshal([]byte(situationsJSON), &allSituations); err != nil {
			continue
		}

		chName := channelID
		if name, ok := channelNames[channelID]; ok {
			chName = name
		}

		// Group by participant user_id
		for _, s := range allSituations {
			for _, p := range s.Participants {
				if p.UserID == "" {
					continue
				}
				result[p.UserID] = append(result[p.UserID], ChannelSituations{
					ChannelID:   channelID,
					ChannelName: chName,
					PeriodFrom:  pFrom,
					PeriodTo:    pTo,
					Situations:  []Situation{s},
				})
			}
		}
	}
	return result, rows.Err()
}

// UpsertPeopleCardSummary inserts or replaces a people card summary.
func (db *DB) UpsertPeopleCardSummary(s PeopleCardSummary) error {
	_, err := db.Exec(`INSERT INTO people_card_summaries
		(period_from, period_to, summary, attention, tips, model, input_tokens, output_tokens, cost_usd, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(period_from, period_to) DO UPDATE SET
			summary = excluded.summary,
			attention = excluded.attention,
			tips = excluded.tips,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			prompt_version = excluded.prompt_version,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		s.PeriodFrom, s.PeriodTo, s.Summary, s.Attention, s.Tips,
		s.Model, s.InputTokens, s.OutputTokens, s.CostUSD, s.PromptVersion)
	return err
}

// GetPeopleCardSummary returns the summary for a specific window, or nil.
func (db *DB) GetPeopleCardSummary(periodFrom, periodTo float64) (*PeopleCardSummary, error) {
	row := db.QueryRow(`SELECT id, period_from, period_to, summary, attention, tips,
		model, input_tokens, output_tokens, cost_usd, prompt_version, created_at
		FROM people_card_summaries WHERE period_from = ? AND period_to = ?`,
		periodFrom, periodTo)
	var s PeopleCardSummary
	err := row.Scan(&s.ID, &s.PeriodFrom, &s.PeriodTo, &s.Summary, &s.Attention, &s.Tips,
		&s.Model, &s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.PromptVersion, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting people card summary: %w", err)
	}
	return &s, nil
}

// GetLatestPeopleCardSummary returns the most recent summary, or nil.
func (db *DB) GetLatestPeopleCardSummary() (*PeopleCardSummary, error) {
	row := db.QueryRow(`SELECT id, period_from, period_to, summary, attention, tips,
		model, input_tokens, output_tokens, cost_usd, prompt_version, created_at
		FROM people_card_summaries ORDER BY period_to DESC LIMIT 1`)
	var s PeopleCardSummary
	err := row.Scan(&s.ID, &s.PeriodFrom, &s.PeriodTo, &s.Summary, &s.Attention, &s.Tips,
		&s.Model, &s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.PromptVersion, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest people card summary: %w", err)
	}
	return &s, nil
}

const peopleCardSelectCols = `SELECT id, user_id, period_from, period_to,
	message_count, channels_active, threads_initiated, threads_replied,
	avg_message_length, active_hours_json, volume_change_pct,
	summary, communication_style, decision_role, red_flags, highlights, accomplishments,
	communication_guide, decision_style, tactics, relationship_context, status,
	model, input_tokens, output_tokens, cost_usd, prompt_version, created_at`

func scanPeopleCard(row *sql.Row, card *PeopleCard) error {
	return row.Scan(
		&card.ID, &card.UserID, &card.PeriodFrom, &card.PeriodTo,
		&card.MessageCount, &card.ChannelsActive, &card.ThreadsInitiated, &card.ThreadsReplied,
		&card.AvgMessageLength, &card.ActiveHoursJSON, &card.VolumeChangePct,
		&card.Summary, &card.CommunicationStyle, &card.DecisionRole,
		&card.RedFlags, &card.Highlights, &card.Accomplishments,
		&card.CommunicationGuide, &card.DecisionStyle, &card.Tactics, &card.RelationshipContext, &card.Status,
		&card.Model, &card.InputTokens, &card.OutputTokens, &card.CostUSD, &card.PromptVersion, &card.CreatedAt,
	)
}

func scanPeopleCards(rows *sql.Rows) ([]PeopleCard, error) {
	var cards []PeopleCard
	for rows.Next() {
		var card PeopleCard
		err := rows.Scan(
			&card.ID, &card.UserID, &card.PeriodFrom, &card.PeriodTo,
			&card.MessageCount, &card.ChannelsActive, &card.ThreadsInitiated, &card.ThreadsReplied,
			&card.AvgMessageLength, &card.ActiveHoursJSON, &card.VolumeChangePct,
			&card.Summary, &card.CommunicationStyle, &card.DecisionRole,
			&card.RedFlags, &card.Highlights, &card.Accomplishments,
			&card.CommunicationGuide, &card.DecisionStyle, &card.Tactics, &card.RelationshipContext, &card.Status,
			&card.Model, &card.InputTokens, &card.OutputTokens, &card.CostUSD, &card.PromptVersion, &card.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning people card row: %w", err)
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}
