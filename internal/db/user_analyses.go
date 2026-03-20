package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
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

// Interaction score weights — higher = stronger signal of real connection.
const (
	weightDM          = 5.0 // DM = strongest intentional signal
	weightMention     = 4.0 // @-mention = explicit addressing
	weightThreadReply = 3.0 // thread reply = active conversation
	weightReaction    = 1.0 // reaction = light engagement
	weightChannelMsg  = 0.5 // shared channel message = ambient noise
)

// classifyConnection determines connection type from directional scores.
// peer: balanced bidirectional (±30%), i_depend: I reach out more,
// depends_on_me: they reach out more, weak: low total score.
func classifyConnection(scoreTo, scoreFrom float64) string {
	total := scoreTo + scoreFrom
	if total < 5 {
		return "weak"
	}
	if total == 0 {
		return "weak"
	}
	ratio := scoreTo / total // 0..1; 0.5 = balanced
	if ratio >= 0.35 && ratio <= 0.65 {
		return "peer"
	}
	if ratio > 0.65 {
		return "i_depend"
	}
	return "depends_on_me"
}

// ComputeUserInteractions calculates interaction metrics between currentUser and
// all other active users in the time window. Pure SQL, no AI.
// Signals: shared channels, DMs, @-mentions, thread replies, reactions.
func (db *DB) ComputeUserInteractions(currentUserID string, from, to float64) ([]UserInteraction, error) {
	if currentUserID == "" {
		return nil, nil
	}

	resultMap := make(map[string]*UserInteraction)
	ensureUser := func(uid string) *UserInteraction {
		if r, ok := resultMap[uid]; ok {
			return r
		}
		r := &UserInteraction{
			UserA:            currentUserID,
			UserB:            uid,
			PeriodFrom:       from,
			PeriodTo:         to,
			SharedChannelIDs: "[]",
		}
		resultMap[uid] = r
		return r
	}

	// 1. Shared channels (non-DM): channels where both users posted
	rows, err := db.Query(`
		SELECT b.user_id,
			COUNT(DISTINCT b.channel_id) as shared_ch,
			GROUP_CONCAT(DISTINCT b.channel_id) as ch_ids
		FROM (
			SELECT DISTINCT channel_id
			FROM messages m
			JOIN channels c ON c.id = m.channel_id
			WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
				AND m.is_deleted = 0 AND m.text != ''
				AND c.type NOT IN ('dm', 'group_dm')
		) my_channels
		JOIN messages b ON b.channel_id = my_channels.channel_id
		JOIN users u ON u.id = b.user_id
		WHERE b.user_id != ? AND b.ts_unix >= ? AND b.ts_unix <= ?
			AND b.is_deleted = 0 AND b.text != ''
			AND u.is_bot = 0 AND u.is_deleted = 0
		GROUP BY b.user_id
		HAVING shared_ch >= 1
		ORDER BY COUNT(*) DESC
		LIMIT 100`,
		currentUserID, from, to,
		currentUserID, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing shared channels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid, chIDs string
		var sharedCh int
		if err := rows.Scan(&uid, &sharedCh, &chIDs); err != nil {
			return nil, fmt.Errorf("scanning shared channels: %w", err)
		}
		r := ensureUser(uid)
		r.SharedChannels = sharedCh
		if chIDs != "" {
			parts := strings.Split(chIDs, ",")
			if data, err := json.Marshal(parts); err == nil {
				r.SharedChannelIDs = string(data)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Messages to/from in shared channels (non-DM)
	for _, dir := range []struct {
		field string
		query string
	}{
		{"to", `
			SELECT other.user_id, COUNT(*) as cnt
			FROM messages other
			JOIN (
				SELECT DISTINCT channel_id
				FROM messages m
				JOIN channels c ON c.id = m.channel_id
				WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
					AND m.is_deleted = 0 AND m.text != ''
					AND c.type NOT IN ('dm', 'group_dm')
			) my_ch ON other.channel_id = my_ch.channel_id
			JOIN users u ON u.id = other.user_id
			WHERE other.user_id != ? AND other.ts_unix >= ? AND other.ts_unix <= ?
				AND other.is_deleted = 0
				AND u.is_bot = 0 AND u.is_deleted = 0
			GROUP BY other.user_id`},
		{"from", `
			SELECT other.user_id, COUNT(*) as cnt
			FROM messages other
			JOIN (
				SELECT DISTINCT channel_id
				FROM messages m
				JOIN channels c ON c.id = m.channel_id
				WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
					AND m.is_deleted = 0 AND m.text != ''
					AND c.type NOT IN ('dm', 'group_dm')
			) my_ch ON other.channel_id = my_ch.channel_id
			JOIN users u ON u.id = other.user_id
			WHERE other.user_id != ? AND other.ts_unix >= ? AND other.ts_unix <= ?
				AND other.is_deleted = 0 AND other.text != ''
				AND u.is_bot = 0 AND u.is_deleted = 0
			GROUP BY other.user_id`},
	} {
		mRows, err := db.Query(dir.query, currentUserID, from, to, currentUserID, from, to)
		if err != nil {
			return nil, fmt.Errorf("computing messages_%s: %w", dir.field, err)
		}
		defer mRows.Close()
		for mRows.Next() {
			var uid string
			var cnt int
			if err := mRows.Scan(&uid, &cnt); err != nil {
				return nil, fmt.Errorf("scanning messages_%s: %w", dir.field, err)
			}
			if r, ok := resultMap[uid]; ok {
				if dir.field == "to" {
					r.MessagesTo = cnt
				} else {
					r.MessagesFrom = cnt
				}
			}
		}
		if err := mRows.Err(); err != nil {
			return nil, err
		}
	}

	// 3. Thread replies (non-DM): currentUser ↔ others in threads
	for _, dir := range []struct {
		field string
		query string
	}{
		{"to", `
			SELECT parent.user_id, COUNT(*) as cnt
			FROM messages reply
			JOIN messages parent ON reply.channel_id = parent.channel_id
				AND reply.thread_ts = parent.ts
			JOIN channels c ON c.id = reply.channel_id
			WHERE reply.user_id = ? AND reply.ts_unix >= ? AND reply.ts_unix <= ?
				AND reply.thread_ts IS NOT NULL
				AND reply.is_deleted = 0
				AND parent.user_id != ?
				AND c.type NOT IN ('dm', 'group_dm')
			GROUP BY parent.user_id`},
		{"from", `
			SELECT reply.user_id, COUNT(*) as cnt
			FROM messages reply
			JOIN messages parent ON reply.channel_id = parent.channel_id
				AND reply.thread_ts = parent.ts
			JOIN channels c ON c.id = reply.channel_id
			WHERE parent.user_id = ? AND reply.ts_unix >= ? AND reply.ts_unix <= ?
				AND reply.thread_ts IS NOT NULL
				AND reply.is_deleted = 0
				AND reply.user_id != ?
				AND c.type NOT IN ('dm', 'group_dm')
			GROUP BY reply.user_id`},
	} {
		trRows, err := db.Query(dir.query, currentUserID, from, to, currentUserID)
		if err != nil {
			return nil, fmt.Errorf("computing thread_replies_%s: %w", dir.field, err)
		}
		defer trRows.Close()
		for trRows.Next() {
			var uid string
			var cnt int
			if err := trRows.Scan(&uid, &cnt); err != nil {
				return nil, fmt.Errorf("scanning thread_replies_%s: %w", dir.field, err)
			}
			r := ensureUser(uid)
			if dir.field == "to" {
				r.ThreadRepliesTo = cnt
			} else {
				r.ThreadRepliesFrom = cnt
			}
		}
		if err := trRows.Err(); err != nil {
			return nil, err
		}
	}

	// 4. DM messages: count messages in DM channels between currentUser and others.
	// dm_user_id stores the "other" user from currentUser's perspective,
	// so all messages in that channel are between currentUser and dm_user_id.
	dmRows, err := db.Query(`
		SELECT c.dm_user_id,
			SUM(CASE WHEN m.user_id = ? THEN 1 ELSE 0 END) as to_them,
			SUM(CASE WHEN m.user_id != ? THEN 1 ELSE 0 END) as from_them
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE c.type = 'dm' AND c.dm_user_id IS NOT NULL AND c.dm_user_id != ''
			AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.is_deleted = 0
		GROUP BY c.dm_user_id`,
		currentUserID, currentUserID,
		from, to)
	if err != nil {
		return nil, fmt.Errorf("computing DM messages: %w", err)
	}
	defer dmRows.Close()
	for dmRows.Next() {
		var uid string
		var toThem, fromThem int
		if err := dmRows.Scan(&uid, &toThem, &fromThem); err != nil {
			return nil, fmt.Errorf("scanning DM messages: %w", err)
		}
		if uid == currentUserID {
			continue
		}
		r := ensureUser(uid)
		r.DMMessagesTo = toThem
		r.DMMessagesFrom = fromThem
	}
	if err := dmRows.Err(); err != nil {
		return nil, err
	}

	// 5. @-mentions: parse <@USERID> patterns from message text
	// A mentioned B: currentUser's messages containing <@otherUID>
	mentToRows, err := db.Query(`
		SELECT mentioned_uid, COUNT(*) as cnt
		FROM (
			SELECT m.text, u2.id as mentioned_uid
			FROM messages m
			JOIN channels c ON c.id = m.channel_id
			JOIN users u2 ON m.text LIKE '%<@' || u2.id || '>%'
			WHERE m.user_id = ? AND m.ts_unix >= ? AND m.ts_unix <= ?
				AND m.is_deleted = 0 AND m.text LIKE '%<@%>%'
				AND u2.id != ? AND u2.is_bot = 0 AND u2.is_deleted = 0
		)
		GROUP BY mentioned_uid`,
		currentUserID, from, to, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("computing mentions_to: %w", err)
	}
	defer mentToRows.Close()
	for mentToRows.Next() {
		var uid string
		var cnt int
		if err := mentToRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning mentions_to: %w", err)
		}
		r := ensureUser(uid)
		r.MentionsTo = cnt
	}
	if err := mentToRows.Err(); err != nil {
		return nil, err
	}

	// B mentioned A: other users' messages containing <@currentUserID>
	mentFromRows, err := db.Query(`
		SELECT m.user_id, COUNT(*) as cnt
		FROM messages m
		JOIN users u ON u.id = m.user_id
		WHERE m.text LIKE '%<@' || ? || '>%'
			AND m.user_id != ?
			AND m.ts_unix >= ? AND m.ts_unix <= ?
			AND m.is_deleted = 0
			AND u.is_bot = 0 AND u.is_deleted = 0
		GROUP BY m.user_id`,
		currentUserID, currentUserID, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing mentions_from: %w", err)
	}
	defer mentFromRows.Close()
	for mentFromRows.Next() {
		var uid string
		var cnt int
		if err := mentFromRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning mentions_from: %w", err)
		}
		r := ensureUser(uid)
		r.MentionsFrom = cnt
	}
	if err := mentFromRows.Err(); err != nil {
		return nil, err
	}

	// 6. Reactions: A reacted to B's messages / B reacted to A's messages
	reactToRows, err := db.Query(`
		SELECT msg.user_id, COUNT(*) as cnt
		FROM reactions r
		JOIN messages msg ON r.channel_id = msg.channel_id AND r.message_ts = msg.ts
		WHERE r.user_id = ? AND msg.user_id != ?
			AND msg.ts_unix >= ? AND msg.ts_unix <= ?
		GROUP BY msg.user_id`,
		currentUserID, currentUserID, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing reactions_to: %w", err)
	}
	defer reactToRows.Close()
	for reactToRows.Next() {
		var uid string
		var cnt int
		if err := reactToRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning reactions_to: %w", err)
		}
		r := ensureUser(uid)
		r.ReactionsTo = cnt
	}
	if err := reactToRows.Err(); err != nil {
		return nil, err
	}

	reactFromRows, err := db.Query(`
		SELECT r.user_id, COUNT(*) as cnt
		FROM reactions r
		JOIN messages msg ON r.channel_id = msg.channel_id AND r.message_ts = msg.ts
		WHERE msg.user_id = ? AND r.user_id != ?
			AND msg.ts_unix >= ? AND msg.ts_unix <= ?
		GROUP BY r.user_id`,
		currentUserID, currentUserID, from, to)
	if err != nil {
		return nil, fmt.Errorf("computing reactions_from: %w", err)
	}
	defer reactFromRows.Close()
	for reactFromRows.Next() {
		var uid string
		var cnt int
		if err := reactFromRows.Scan(&uid, &cnt); err != nil {
			return nil, fmt.Errorf("scanning reactions_from: %w", err)
		}
		r := ensureUser(uid)
		r.ReactionsFrom = cnt
	}
	if err := reactFromRows.Err(); err != nil {
		return nil, err
	}

	// 7. Compute weighted interaction score and classify connection type
	for _, r := range resultMap {
		scoreTo := float64(r.DMMessagesTo)*weightDM +
			float64(r.MentionsTo)*weightMention +
			float64(r.ThreadRepliesTo)*weightThreadReply +
			float64(r.ReactionsTo)*weightReaction +
			float64(r.MessagesTo)*weightChannelMsg

		scoreFrom := float64(r.DMMessagesFrom)*weightDM +
			float64(r.MentionsFrom)*weightMention +
			float64(r.ThreadRepliesFrom)*weightThreadReply +
			float64(r.ReactionsFrom)*weightReaction +
			float64(r.MessagesFrom)*weightChannelMsg

		r.InteractionScore = scoreTo + scoreFrom
		r.ConnectionType = classifyConnection(scoreTo, scoreFrom)
	}

	// Build result slice sorted by interaction_score DESC, limit to top 50
	if len(resultMap) == 0 {
		return nil, nil
	}
	type scored struct {
		uid   string
		score float64
	}
	var sortable []scored
	for uid, r := range resultMap {
		sortable = append(sortable, scored{uid, r.InteractionScore})
	}
	sort.Slice(sortable, func(i, j int) bool {
		return sortable[i].score > sortable[j].score
	})
	limit := 50
	if len(sortable) < limit {
		limit = len(sortable)
	}
	results := make([]UserInteraction, 0, limit)
	for _, s := range sortable[:limit] {
		results = append(results, *resultMap[s.uid])
	}
	return results, nil
}

// UpsertUserInteractions inserts or replaces interaction records for a window.
func (db *DB) UpsertUserInteractions(interactions []UserInteraction) error {
	if len(interactions) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning upsert interactions tx: %w", err)
	}
	defer tx.Rollback()

	// Delete old interactions for this window first
	first := interactions[0]
	_, err = tx.Exec(`DELETE FROM user_interactions WHERE user_a = ? AND period_from = ? AND period_to = ?`,
		first.UserA, first.PeriodFrom, first.PeriodTo)
	if err != nil {
		return fmt.Errorf("deleting old interactions: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO user_interactions
		(user_a, user_b, period_from, period_to,
		 messages_to, messages_from, shared_channels,
		 thread_replies_to, thread_replies_from, shared_channel_ids,
		 dm_messages_to, dm_messages_from, mentions_to, mentions_from,
		 reactions_to, reactions_from, interaction_score, connection_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing upsert interactions: %w", err)
	}
	defer stmt.Close()

	for _, i := range interactions {
		_, err := stmt.Exec(
			i.UserA, i.UserB, i.PeriodFrom, i.PeriodTo,
			i.MessagesTo, i.MessagesFrom, i.SharedChannels,
			i.ThreadRepliesTo, i.ThreadRepliesFrom, i.SharedChannelIDs,
			i.DMMessagesTo, i.DMMessagesFrom, i.MentionsTo, i.MentionsFrom,
			i.ReactionsTo, i.ReactionsFrom, i.InteractionScore, i.ConnectionType)
		if err != nil {
			return fmt.Errorf("inserting interaction %s→%s: %w", i.UserA, i.UserB, err)
		}
	}

	return tx.Commit()
}

// GetUserInteractions returns interaction edges for a user in a specific window.
func (db *DB) GetUserInteractions(userA string, periodFrom, periodTo float64) ([]UserInteraction, error) {
	rows, err := db.Query(`SELECT user_a, user_b, period_from, period_to,
		messages_to, messages_from, shared_channels,
		thread_replies_to, thread_replies_from, shared_channel_ids,
		dm_messages_to, dm_messages_from, mentions_to, mentions_from,
		reactions_to, reactions_from, interaction_score, connection_type
		FROM user_interactions
		WHERE user_a = ? AND period_from = ? AND period_to = ?
		ORDER BY interaction_score DESC`,
		userA, periodFrom, periodTo)
	if err != nil {
		return nil, fmt.Errorf("querying user interactions: %w", err)
	}
	defer rows.Close()

	var results []UserInteraction
	for rows.Next() {
		var i UserInteraction
		if err := rows.Scan(&i.UserA, &i.UserB, &i.PeriodFrom, &i.PeriodTo,
			&i.MessagesTo, &i.MessagesFrom, &i.SharedChannels,
			&i.ThreadRepliesTo, &i.ThreadRepliesFrom, &i.SharedChannelIDs,
			&i.DMMessagesTo, &i.DMMessagesFrom, &i.MentionsTo, &i.MentionsFrom,
			&i.ReactionsTo, &i.ReactionsFrom, &i.InteractionScore, &i.ConnectionType); err != nil {
			return nil, fmt.Errorf("scanning user interaction: %w", err)
		}
		results = append(results, i)
	}
	return results, rows.Err()
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
