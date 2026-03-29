package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ChannelStatRow holds computed statistics for a single channel.
type ChannelStatRow struct {
	ChannelID        string
	ChannelName      string
	ChannelType      string // "public", "private", "dm", "group_dm"
	IsArchived       bool
	IsMember         bool
	NumMembers       int
	TotalMsgs        int
	UserMsgs         int     // messages from currentUserID
	BotMsgs          int     // messages from bot users
	BotRatio         float64 // BotMsgs / TotalMsgs (0 if TotalMsgs == 0)
	Mentions         int
	LastActivity     float64
	LastUserActivity float64
	IsMuted          bool
	IsFavorite       bool
	IsWatched        bool
}

// ChannelValueSignals holds value indicators from related entities for a channel.
type ChannelValueSignals struct {
	DecisionCount     int
	ActiveTrackCount  int
	TaskCount         int
	PendingInboxCount int
}

// GetChannelStats returns per-channel statistics for the current user.
// Counts are over all synced messages (not windowed).
func (db *DB) GetChannelStats(currentUserID string) ([]ChannelStatRow, error) {
	if currentUserID == "" {
		return nil, fmt.Errorf("currentUserID is required")
	}

	mentionPattern := fmt.Sprintf("%%<@%s>%%", currentUserID)

	rows, err := db.Query(`
		SELECT
			c.id,
			c.name,
			c.type,
			c.is_archived,
			c.is_member,
			c.num_members,
			COALESCE(ms.total_msgs, 0),
			COALESCE(ms.user_msgs, 0),
			COALESCE(ms.bot_msgs, 0),
			COALESCE(ms.mention_count, 0),
			ms.last_activity,
			ms.last_user_activity,
			COALESCE(cs.is_muted_for_llm, 0),
			COALESCE(cs.is_favorite, 0),
			CASE WHEN w.entity_id IS NOT NULL THEN 1 ELSE 0 END
		FROM channels c
		LEFT JOIN (
			SELECT
				m.channel_id,
				COUNT(*) AS total_msgs,
				SUM(CASE WHEN m.user_id = ? THEN 1 ELSE 0 END) AS user_msgs,
				SUM(CASE WHEN m.user_id = '' OR COALESCE(u.is_bot_override, u.is_bot) = 1 THEN 1 ELSE 0 END) AS bot_msgs,
				SUM(CASE WHEN m.text LIKE ? THEN 1 ELSE 0 END) AS mention_count,
				MAX(m.ts_unix) AS last_activity,
				MAX(CASE WHEN m.user_id = ? THEN m.ts_unix END) AS last_user_activity
			FROM messages m
			LEFT JOIN users u ON u.id = m.user_id
			WHERE m.is_deleted = 0
			GROUP BY m.channel_id
		) ms ON ms.channel_id = c.id
		LEFT JOIN channel_settings cs ON cs.channel_id = c.id
		LEFT JOIN watch_list w ON w.entity_type = 'channel' AND w.entity_id = c.id
		ORDER BY COALESCE(ms.total_msgs, 0) DESC, c.name`,
		currentUserID, mentionPattern, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("querying channel stats: %w", err)
	}
	defer rows.Close()

	var stats []ChannelStatRow
	for rows.Next() {
		var s ChannelStatRow
		var lastActivity, lastUserActivity sql.NullFloat64
		err := rows.Scan(
			&s.ChannelID, &s.ChannelName, &s.ChannelType,
			&s.IsArchived, &s.IsMember, &s.NumMembers,
			&s.TotalMsgs, &s.UserMsgs, &s.BotMsgs,
			&s.Mentions, &lastActivity, &lastUserActivity,
			&s.IsMuted, &s.IsFavorite, &s.IsWatched,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning channel stat row: %w", err)
		}
		if lastActivity.Valid {
			s.LastActivity = lastActivity.Float64
		}
		if lastUserActivity.Valid {
			s.LastUserActivity = lastUserActivity.Float64
		}
		if s.TotalMsgs > 0 {
			s.BotRatio = float64(s.BotMsgs) / float64(s.TotalMsgs)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetChannelValueSignals returns per-channel value signals from related entities (decisions, tracks, tasks, inbox).
// Only channels with at least one non-zero signal are included in the result map.
func (db *DB) GetChannelValueSignals() (map[string]ChannelValueSignals, error) {
	cutoff := float64(time.Now().AddDate(0, 0, -30).Unix())
	rows, err := db.Query(`
		WITH decision_counts AS (
			SELECT d.channel_id, COUNT(*) AS cnt
			FROM digest_topics dt
			JOIN digests d ON d.id = dt.digest_id
			WHERE d.type = 'channel'
			  AND d.channel_id != ''
			  AND dt.decisions != '[]' AND dt.decisions != ''
			  AND d.period_to >= ?
			GROUP BY d.channel_id
		),
		track_channels AS (
			SELECT je.value AS channel_id, COUNT(DISTINCT t.id) AS cnt
			FROM tracks t, json_each(t.channel_ids) je
			GROUP BY je.value
		),
		task_via_digest AS (
			SELECT d.channel_id, COUNT(*) AS cnt
			FROM tasks t
			JOIN digests d ON t.source_type = 'digest' AND t.source_id = CAST(d.id AS TEXT)
			WHERE t.status IN ('todo','in_progress','blocked')
			  AND d.channel_id != ''
			GROUP BY d.channel_id
		),
		task_via_inbox AS (
			SELECT i.channel_id, COUNT(*) AS cnt
			FROM tasks t
			JOIN inbox_items i ON t.source_type = 'inbox' AND t.source_id = CAST(i.id AS TEXT)
			WHERE t.status IN ('todo','in_progress','blocked')
			GROUP BY i.channel_id
		),
		task_counts AS (
			SELECT channel_id, SUM(cnt) AS cnt
			FROM (SELECT * FROM task_via_digest UNION ALL SELECT * FROM task_via_inbox)
			GROUP BY channel_id
		),
		inbox_counts AS (
			SELECT channel_id, COUNT(*) AS cnt
			FROM inbox_items WHERE status = 'pending'
			GROUP BY channel_id
		)
		SELECT c.id,
			COALESCE(dc.cnt, 0), COALESCE(tc.cnt, 0),
			COALESCE(tk.cnt, 0), COALESCE(ic.cnt, 0)
		FROM channels c
		LEFT JOIN decision_counts dc ON dc.channel_id = c.id
		LEFT JOIN track_channels tc ON tc.channel_id = c.id
		LEFT JOIN task_counts tk ON tk.channel_id = c.id
		LEFT JOIN inbox_counts ic ON ic.channel_id = c.id
		WHERE COALESCE(dc.cnt, 0) + COALESCE(tc.cnt, 0) + COALESCE(tk.cnt, 0) + COALESCE(ic.cnt, 0) > 0`,
		cutoff)
	if err != nil {
		return nil, fmt.Errorf("querying channel value signals: %w", err)
	}
	defer rows.Close()

	result := make(map[string]ChannelValueSignals)
	for rows.Next() {
		var channelID string
		var vs ChannelValueSignals
		if err := rows.Scan(&channelID, &vs.DecisionCount, &vs.ActiveTrackCount, &vs.TaskCount, &vs.PendingInboxCount); err != nil {
			return nil, fmt.Errorf("scanning channel value signal row: %w", err)
		}
		result[channelID] = vs
	}
	return result, rows.Err()
}

// Recommendation represents a suggested action for a channel.
type Recommendation struct {
	ChannelID   string
	ChannelName string
	Action      string // "mute", "leave", "favorite"
	Reason      string
}

// ComputeRecommendations returns channel action recommendations based on heuristics.
func ComputeRecommendations(stats []ChannelStatRow, currentUserID string, signals map[string]ChannelValueSignals) []Recommendation {
	now := float64(time.Now().Unix())
	thirtyDaysAgo := now - 30*24*3600

	var recs []Recommendation
	for _, s := range stats {
		var vs ChannelValueSignals
		if signals != nil {
			vs = signals[s.ChannelID]
		}

		// Mute candidates:
		// - total>=50 AND user_msgs==0 AND mentions==0; OR bot_ratio>=0.70
		// - Skip if already favorite or already muted
		// - Blocked by pending inbox or active tasks
		if !s.IsFavorite && !s.IsMuted {
			if (s.TotalMsgs >= 50 && s.UserMsgs == 0 && s.Mentions == 0) || s.BotRatio >= 0.70 {
				if vs.PendingInboxCount > 0 || vs.TaskCount > 0 {
					// Value signals block mute — fall through to leave/favorite checks
				} else {
					reason := "high volume with no participation"
					if s.BotRatio >= 0.70 {
						reason = fmt.Sprintf("%.0f%% bot messages", s.BotRatio*100)
					}
					if vs.DecisionCount == 0 && signals != nil {
						reason += " (no value signals)"
					}
					recs = append(recs, Recommendation{
						ChannelID:   s.ChannelID,
						ChannelName: s.ChannelName,
						Action:      "mute",
						Reason:      reason,
					})
					continue // don't suggest leave+mute for same channel
				}
			}
		}

		// Leave candidates:
		// - user_msgs==0 OR last_activity 30+ days ago
		// - AND not favorite, not watched, not DM
		// - Blocked by pending inbox, active tasks, active tracks, or >=3 decisions
		if !s.IsFavorite && !s.IsWatched && s.ChannelType != "dm" && s.ChannelType != "group_dm" && s.IsMember {
			if s.UserMsgs == 0 || (s.LastUserActivity > 0 && s.LastUserActivity < thirtyDaysAgo) {
				if vs.PendingInboxCount > 0 || vs.TaskCount > 0 || vs.ActiveTrackCount > 0 || vs.DecisionCount >= 3 {
					// Value signals block leave — fall through to favorite check
				} else {
					reason := "no messages from you"
					if s.LastUserActivity > 0 && s.LastUserActivity < thirtyDaysAgo {
						reason = "inactive for 30+ days"
					}
					recs = append(recs, Recommendation{
						ChannelID:   s.ChannelID,
						ChannelName: s.ChannelName,
						Action:      "leave",
						Reason:      reason,
					})
					continue
				}
			}
		}

		// Favorite candidates:
		// - user_msgs>=10 AND mentions>=3; OR is_watched AND not favorite
		// - NEW: decisions>=5 OR active tracks>=2
		// - Skip if already favorite
		if !s.IsFavorite {
			if (s.UserMsgs >= 10 && s.Mentions >= 3) || (s.IsWatched && !s.IsFavorite) {
				reason := "high engagement"
				if s.IsWatched {
					reason = "watched channel"
				}
				recs = append(recs, Recommendation{
					ChannelID:   s.ChannelID,
					ChannelName: s.ChannelName,
					Action:      "favorite",
					Reason:      reason,
				})
			} else if vs.DecisionCount >= 5 || vs.ActiveTrackCount >= 2 {
				reason := fmt.Sprintf("high-value channel (%d decisions, %d active tracks)", vs.DecisionCount, vs.ActiveTrackCount)
				recs = append(recs, Recommendation{
					ChannelID:   s.ChannelID,
					ChannelName: s.ChannelName,
					Action:      "favorite",
					Reason:      reason,
				})
			}
		}
	}
	return recs
}
