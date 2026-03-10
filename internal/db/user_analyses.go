package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// UpsertUserAnalysis inserts or replaces a user analysis based on the unique
// constraint (user_id, period_from, period_to).
func (db *DB) UpsertUserAnalysis(a UserAnalysis) (int64, error) {
	_, err := db.Exec(`INSERT INTO user_analyses
		(user_id, period_from, period_to,
		 message_count, channels_active, threads_initiated, threads_replied,
		 avg_message_length, active_hours_json, volume_change_pct,
		 summary, communication_style, decision_role, red_flags, highlights,
		 style_details, recommendations, concerns, accomplishments,
		 model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			style_details = excluded.style_details,
			recommendations = excluded.recommendations,
			concerns = excluded.concerns,
			accomplishments = excluded.accomplishments,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		a.UserID, a.PeriodFrom, a.PeriodTo,
		a.MessageCount, a.ChannelsActive, a.ThreadsInitiated, a.ThreadsReplied,
		a.AvgMessageLength, a.ActiveHoursJSON, a.VolumeChangePct,
		a.Summary, a.CommunicationStyle, a.DecisionRole, a.RedFlags, a.Highlights,
		a.StyleDetails, a.Recommendations, a.Concerns, a.Accomplishments,
		a.Model, a.InputTokens, a.OutputTokens, a.CostUSD)
	if err != nil {
		return 0, fmt.Errorf("upserting user analysis: %w", err)
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM user_analyses WHERE user_id = ? AND period_from = ? AND period_to = ?`,
		a.UserID, a.PeriodFrom, a.PeriodTo).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting user analysis id after upsert: %w", err)
	}
	return id, nil
}

// UserAnalysisFilter specifies criteria for querying user analyses.
type UserAnalysisFilter struct {
	UserID   string  // filter by user (empty = any)
	FromUnix float64 // period_from >= this (0 = no filter)
	ToUnix   float64 // period_to <= this (0 = no filter)
	Limit    int     // max results (0 = no limit)
}

// GetUserAnalyses returns user analyses matching the filter, newest first.
func (db *DB) GetUserAnalyses(f UserAnalysisFilter) ([]UserAnalysis, error) {
	query := `SELECT id, user_id, period_from, period_to,
		message_count, channels_active, threads_initiated, threads_replied,
		avg_message_length, active_hours_json, volume_change_pct,
		summary, communication_style, decision_role, red_flags, highlights,
		style_details, recommendations, concerns, accomplishments,
		model, input_tokens, output_tokens, cost_usd, created_at
		FROM user_analyses`
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
		// Safety limit to prevent OOM on large datasets
		query += ` LIMIT 1000`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying user analyses: %w", err)
	}
	defer rows.Close()

	return scanUserAnalyses(rows)
}

// GetLatestUserAnalysis returns the most recent analysis for a user, or nil.
func (db *DB) GetLatestUserAnalysis(userID string) (*UserAnalysis, error) {
	row := db.QueryRow(`SELECT id, user_id, period_from, period_to,
		message_count, channels_active, threads_initiated, threads_replied,
		avg_message_length, active_hours_json, volume_change_pct,
		summary, communication_style, decision_role, red_flags, highlights,
		style_details, recommendations, concerns, accomplishments,
		model, input_tokens, output_tokens, cost_usd, created_at
		FROM user_analyses WHERE user_id = ?
		ORDER BY period_to DESC LIMIT 1`, userID)

	var a UserAnalysis
	err := scanUserAnalysis(row, &a)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest user analysis: %w", err)
	}
	return &a, nil
}

// GetUserAnalysesForWindow returns all analyses for a specific time window.
func (db *DB) GetUserAnalysesForWindow(periodFrom, periodTo float64) ([]UserAnalysis, error) {
	rows, err := db.Query(`SELECT id, user_id, period_from, period_to,
		message_count, channels_active, threads_initiated, threads_replied,
		avg_message_length, active_hours_json, volume_change_pct,
		summary, communication_style, decision_role, red_flags, highlights,
		style_details, recommendations, concerns, accomplishments,
		model, input_tokens, output_tokens, cost_usd, created_at
		FROM user_analyses
		WHERE period_from = ? AND period_to = ?
		ORDER BY message_count DESC`, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("querying user analyses for window: %w", err)
	}
	defer rows.Close()

	return scanUserAnalyses(rows)
}

// DeleteUserAnalysesOlderThan removes analyses with period_to before the given timestamp.
func (db *DB) DeleteUserAnalysesOlderThan(beforeUnix float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM user_analyses WHERE period_to < ?`, beforeUnix)
	if err != nil {
		return 0, fmt.Errorf("deleting old user analyses: %w", err)
	}
	return res.RowsAffected()
}

// ActiveUsersInWindow returns user IDs that have messages in the given time range,
// excluding bots and deleted users.
func (db *DB) ActiveUsersInWindow(from, to float64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT m.user_id
		FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.user_id != ''
			AND u.is_bot = 0
			AND u.is_deleted = 0
			AND c.type NOT IN ('dm', 'group_dm')
		ORDER BY m.user_id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying active users in window: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning active user id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UserStats holds pre-computed statistics for a single user in a time window.
// These are computed via pure SQL with no AI involvement.
type UserStats struct {
	UserID           string
	MessageCount     int
	ChannelsActive   int
	ThreadsInitiated int
	ThreadsReplied   int
	AvgMessageLength float64
	ActiveHoursJSON  string  // JSON: {"9":12,"10":8,...}
	VolumeChangePct  float64 // % change vs previous window of same duration
}

// ComputeUserStats calculates communication statistics for a single user
// within the given time window.
func (db *DB) ComputeUserStats(userID string, from, to float64) (*UserStats, error) {
	s := &UserStats{UserID: userID}

	// Core stats: message count, channels, avg length (excluding DMs)
	err := db.QueryRow(`
		SELECT
			COUNT(*) as msg_count,
			COUNT(DISTINCT m.channel_id) as channels,
			COALESCE(AVG(LENGTH(m.text)), 0) as avg_len
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.is_deleted = 0 AND m.text != ''
			AND c.type NOT IN ('dm', 'group_dm')`,
		userID, from, to).Scan(&s.MessageCount, &s.ChannelsActive, &s.AvgMessageLength)
	if err != nil {
		return nil, fmt.Errorf("computing core stats for %s: %w", userID, err)
	}

	// Threads initiated (user posted a message that got replies)
	err = db.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.reply_count > 0 AND m.thread_ts IS NULL
			AND c.type NOT IN ('dm', 'group_dm')`,
		userID, from, to).Scan(&s.ThreadsInitiated)
	if err != nil {
		return nil, fmt.Errorf("computing threads initiated for %s: %w", userID, err)
	}

	// Threads replied (user posted a reply in a thread)
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT m.thread_ts) FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.thread_ts IS NOT NULL
			AND c.type NOT IN ('dm', 'group_dm')`,
		userID, from, to).Scan(&s.ThreadsReplied)
	if err != nil {
		return nil, fmt.Errorf("computing threads replied for %s: %w", userID, err)
	}

	// Active hours distribution
	rows, err := db.Query(`
		SELECT CAST(strftime('%H', m.ts_unix, 'unixepoch') AS INTEGER) as hour,
			COUNT(*) as cnt
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND c.type NOT IN ('dm', 'group_dm')
		GROUP BY hour
		ORDER BY hour`,
		userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing active hours for %s: %w", userID, err)
	}
	defer rows.Close()

	hourMap := make(map[int]int)
	for rows.Next() {
		var hour, cnt int
		if err := rows.Scan(&hour, &cnt); err != nil {
			return nil, fmt.Errorf("scanning hour row: %w", err)
		}
		hourMap[hour] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build JSON manually to avoid import cycle
	s.ActiveHoursJSON = buildHoursJSON(hourMap)

	// Volume change vs previous window of same duration (excluding DMs)
	windowDuration := to - from
	if windowDuration <= 0 {
		s.VolumeChangePct = 0
		return s, nil
	}
	prevFrom := from - windowDuration
	prevTo := from
	var prevCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND c.type NOT IN ('dm', 'group_dm')`,
		userID, prevFrom, prevTo).Scan(&prevCount)
	if err != nil {
		return nil, fmt.Errorf("computing previous volume for %s: %w", userID, err)
	}
	if prevCount > 0 {
		s.VolumeChangePct = float64(s.MessageCount-prevCount) / float64(prevCount) * 100
	}

	return s, nil
}

// ComputeAllUserStats computes stats for all active (non-bot, non-deleted) users
// in the given time window using bulk SQL queries. Returns only users with at least minMessages.
func (db *DB) ComputeAllUserStats(from, to float64, minMessages int) ([]UserStats, error) {
	// 1. Core stats for all users in a single query
	rows, err := db.Query(`
		SELECT m.user_id,
			COUNT(*) as msg_count,
			COUNT(DISTINCT m.channel_id) as channels,
			COALESCE(AVG(LENGTH(m.text)), 0) as avg_len
		FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.is_deleted = 0 AND m.text != ''
			AND m.user_id != ''
			AND u.is_bot = 0 AND u.is_deleted = 0
			AND c.type NOT IN ('dm', 'group_dm')
		GROUP BY m.user_id
		HAVING msg_count >= ?
		ORDER BY m.user_id`, from, to, minMessages)
	if err != nil {
		return nil, fmt.Errorf("computing bulk core stats: %w", err)
	}
	defer rows.Close()

	statsMap := make(map[string]*UserStats)
	var userOrder []string
	for rows.Next() {
		var s UserStats
		if err := rows.Scan(&s.UserID, &s.MessageCount, &s.ChannelsActive, &s.AvgMessageLength); err != nil {
			return nil, fmt.Errorf("scanning core stats: %w", err)
		}
		statsMap[s.UserID] = &s
		userOrder = append(userOrder, s.UserID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(statsMap) == 0 {
		return nil, nil
	}

	// 2. Threads initiated (bulk)
	tiRows, err := db.Query(`
		SELECT m.user_id, COUNT(*) FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.reply_count > 0 AND m.thread_ts IS NULL
			AND m.user_id != '' AND u.is_bot = 0 AND u.is_deleted = 0
			AND c.type NOT IN ('dm', 'group_dm')
		GROUP BY m.user_id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing bulk threads initiated: %w", err)
	}
	defer tiRows.Close()
	for tiRows.Next() {
		var uid string
		var cnt int
		if err := tiRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning threads initiated: %w", err)
		}
		if s, ok := statsMap[uid]; ok {
			s.ThreadsInitiated = cnt
		}
	}
	if err := tiRows.Err(); err != nil {
		return nil, err
	}

	// 3. Threads replied (bulk)
	trRows, err := db.Query(`
		SELECT m.user_id, COUNT(DISTINCT m.thread_ts) FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.thread_ts IS NOT NULL
			AND m.user_id != '' AND u.is_bot = 0 AND u.is_deleted = 0
			AND c.type NOT IN ('dm', 'group_dm')
		GROUP BY m.user_id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing bulk threads replied: %w", err)
	}
	defer trRows.Close()
	for trRows.Next() {
		var uid string
		var cnt int
		if err := trRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning threads replied: %w", err)
		}
		if s, ok := statsMap[uid]; ok {
			s.ThreadsReplied = cnt
		}
	}
	if err := trRows.Err(); err != nil {
		return nil, err
	}

	// 4. Active hours (bulk)
	ahRows, err := db.Query(`
		SELECT m.user_id,
			CAST(strftime('%H', m.ts_unix, 'unixepoch') AS INTEGER) as hour,
			COUNT(*) as cnt
		FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN channels c ON c.id = m.channel_id
		WHERE m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.user_id != '' AND u.is_bot = 0 AND u.is_deleted = 0
			AND c.type NOT IN ('dm', 'group_dm')
		GROUP BY m.user_id, hour
		ORDER BY m.user_id, hour`, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing bulk active hours: %w", err)
	}
	defer ahRows.Close()

	hourMaps := make(map[string]map[int]int)
	for ahRows.Next() {
		var uid string
		var hour, cnt int
		if err := ahRows.Scan(&uid, &hour, &cnt); err != nil {
			return nil, fmt.Errorf("scanning active hours: %w", err)
		}
		if _, ok := hourMaps[uid]; !ok {
			hourMaps[uid] = make(map[int]int)
		}
		hourMaps[uid][hour] = cnt
	}
	if err := ahRows.Err(); err != nil {
		return nil, err
	}
	for uid, hm := range hourMaps {
		if s, ok := statsMap[uid]; ok {
			s.ActiveHoursJSON = buildHoursJSON(hm)
		}
	}

	// 5. Previous window volume (bulk) for volume change calculation
	windowDuration := to - from
	if windowDuration > 0 {
		prevFrom := from - windowDuration
		prevTo := from
		pvRows, err := db.Query(`
			SELECT m.user_id, COUNT(*) FROM messages m
			JOIN users u ON u.id = m.user_id
			JOIN channels c ON c.id = m.channel_id
			WHERE m.ts_unix >= ? AND m.ts_unix <= ?
				AND m.user_id != '' AND u.is_bot = 0 AND u.is_deleted = 0
				AND c.type NOT IN ('dm', 'group_dm')
			GROUP BY m.user_id`, prevFrom, prevTo)
		if err != nil {
			return nil, fmt.Errorf("computing bulk previous volume: %w", err)
		}
		defer pvRows.Close()
		for pvRows.Next() {
			var uid string
			var prevCount int
			if err := pvRows.Scan(&uid, &prevCount); err != nil {
				return nil, fmt.Errorf("scanning previous volume: %w", err)
			}
			if s, ok := statsMap[uid]; ok && prevCount > 0 {
				s.VolumeChangePct = float64(s.MessageCount-prevCount) / float64(prevCount) * 100
			}
		}
		if err := pvRows.Err(); err != nil {
			return nil, err
		}
	}

	// Build result slice preserving order
	results := make([]UserStats, 0, len(userOrder))
	for _, uid := range userOrder {
		s := statsMap[uid]
		if s.ActiveHoursJSON == "" {
			s.ActiveHoursJSON = "{}"
		}
		results = append(results, *s)
	}
	return results, nil
}

func buildHoursJSON(m map[int]int) string {
	if len(m) == 0 {
		return "{}"
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for h := range 24 {
		cnt, ok := m[h]
		if !ok {
			continue
		}
		if !first {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf(`"%d":%d`, h, cnt))
		first = false
	}
	sb.WriteByte('}')
	return sb.String()
}

func scanUserAnalysis(row *sql.Row, a *UserAnalysis) error {
	return row.Scan(
		&a.ID, &a.UserID, &a.PeriodFrom, &a.PeriodTo,
		&a.MessageCount, &a.ChannelsActive, &a.ThreadsInitiated, &a.ThreadsReplied,
		&a.AvgMessageLength, &a.ActiveHoursJSON, &a.VolumeChangePct,
		&a.Summary, &a.CommunicationStyle, &a.DecisionRole, &a.RedFlags, &a.Highlights,
		&a.StyleDetails, &a.Recommendations, &a.Concerns, &a.Accomplishments,
		&a.Model, &a.InputTokens, &a.OutputTokens, &a.CostUSD, &a.CreatedAt,
	)
}

func scanUserAnalyses(rows *sql.Rows) ([]UserAnalysis, error) {
	var analyses []UserAnalysis
	for rows.Next() {
		var a UserAnalysis
		err := rows.Scan(
			&a.ID, &a.UserID, &a.PeriodFrom, &a.PeriodTo,
			&a.MessageCount, &a.ChannelsActive, &a.ThreadsInitiated, &a.ThreadsReplied,
			&a.AvgMessageLength, &a.ActiveHoursJSON, &a.VolumeChangePct,
			&a.Summary, &a.CommunicationStyle, &a.DecisionRole, &a.RedFlags, &a.Highlights,
			&a.StyleDetails, &a.Recommendations, &a.Concerns, &a.Accomplishments,
			&a.Model, &a.InputTokens, &a.OutputTokens, &a.CostUSD, &a.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning user analysis row: %w", err)
		}
		analyses = append(analyses, a)
	}
	return analyses, rows.Err()
}

// UpsertPeriodSummary inserts or replaces a period summary.
func (db *DB) UpsertPeriodSummary(s PeriodSummary) error {
	_, err := db.Exec(`INSERT INTO period_summaries
		(period_from, period_to, summary, attention, model, input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(period_from, period_to) DO UPDATE SET
			summary = excluded.summary,
			attention = excluded.attention,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		s.PeriodFrom, s.PeriodTo, s.Summary, s.Attention,
		s.Model, s.InputTokens, s.OutputTokens, s.CostUSD)
	return err
}

// GetPeriodSummary returns the period summary for a specific window, or nil.
func (db *DB) GetPeriodSummary(periodFrom, periodTo float64) (*PeriodSummary, error) {
	row := db.QueryRow(`SELECT id, period_from, period_to, summary, attention,
		model, input_tokens, output_tokens, cost_usd, created_at
		FROM period_summaries WHERE period_from = ? AND period_to = ?`,
		periodFrom, periodTo)
	var s PeriodSummary
	err := row.Scan(&s.ID, &s.PeriodFrom, &s.PeriodTo, &s.Summary, &s.Attention,
		&s.Model, &s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting period summary: %w", err)
	}
	return &s, nil
}
