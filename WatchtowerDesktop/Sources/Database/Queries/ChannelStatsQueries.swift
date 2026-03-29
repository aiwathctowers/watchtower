import GRDB
import Foundation

enum ChannelStatsQueries {

    /// Fetch channel statistics for the current user.
    /// Matches Go `GetChannelStats` — userMessages/mentionCount are relative to currentUserID.
    static func fetchAll(_ db: Database, currentUserID: String) throws -> [ChannelStat] {
        let mentionPattern = "%<@\(currentUserID)>%"

        let sql = """
            SELECT
                c.id,
                c.name,
                c.type,
                c.is_archived,
                c.is_member,
                c.num_members,
                COALESCE(ms.total_msgs, 0) AS total_messages,
                COALESCE(ms.user_msgs, 0) AS user_messages,
                COALESCE(ms.bot_msgs, 0) AS bot_messages,
                CASE WHEN COALESCE(ms.total_msgs, 0) > 0
                    THEN CAST(COALESCE(ms.bot_msgs, 0) AS REAL) / ms.total_msgs
                    ELSE 0.0
                END AS bot_ratio,
                COALESCE(ms.mention_count, 0) AS mention_count,
                COALESCE(ms.last_activity, 0) AS last_activity,
                COALESCE(ms.last_user_activity, 0) AS last_user_activity,
                COALESCE(cs.is_muted_for_llm, 0) AS is_muted_for_llm,
                COALESCE(cs.is_favorite, 0) AS is_favorite,
                CASE WHEN w.entity_id IS NOT NULL THEN 1 ELSE 0 END AS is_watched,
                COALESCE(ds.digest_count, 0) AS digest_count,
                ds.last_digest_at,
                COALESCE(pending.messages_since_digest, 0) AS messages_since_digest
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
            LEFT JOIN (
                SELECT
                    channel_id,
                    COUNT(*) AS digest_count,
                    MAX(created_at) AS last_digest_at
                FROM digests
                WHERE type = 'channel'
                GROUP BY channel_id
            ) ds ON ds.channel_id = c.id
            LEFT JOIN (
                SELECT
                    m.channel_id,
                    COUNT(*) AS messages_since_digest
                FROM messages m
                JOIN (
                    SELECT channel_id, MAX(period_to) AS last_period_to
                    FROM digests WHERE type = 'channel'
                    GROUP BY channel_id
                ) ld ON ld.channel_id = m.channel_id
                WHERE m.is_deleted = 0 AND m.ts_unix > ld.last_period_to
                GROUP BY m.channel_id
            ) pending ON pending.channel_id = c.id
            ORDER BY COALESCE(ms.total_msgs, 0) DESC, c.name
            """
        return try ChannelStat.fetchAll(db, sql: sql, arguments: [currentUserID, mentionPattern, currentUserID])
    }

    /// Compute recommendations matching Go `ComputeRecommendations` thresholds exactly.
    static func computeRecommendations(from stats: [ChannelStat], signals: [String: ChannelValueSignals] = [:]) -> [ChannelRecommendation] {
        let now = Date().timeIntervalSince1970
        let thirtyDaysAgo = now - 30 * 24 * 3600

        var recs: [ChannelRecommendation] = []

        for s in stats {
            let vs = signals[s.id]

            // Mute: total>=50 AND userMsgs==0 AND mentions==0; OR botRatio>=0.70
            // Skip if already favorite or already muted
            // Blocked by pending inbox or active tasks
            if !s.isFavorite && !s.isMutedForLLM {
                if (s.totalMessages >= 50 && s.userMessages == 0 && s.mentionCount == 0)
                    || s.botRatio >= 0.70 {
                    if (vs?.pendingInboxCount ?? 0) > 0 || (vs?.taskCount ?? 0) > 0 {
                        // Value signals block mute — fall through
                    } else {
                        var reason = s.botRatio >= 0.70
                            ? "\(Int(s.botRatio * 100))% bot messages"
                            : "high volume with no participation"
                        if (vs?.decisionCount ?? 0) == 0 && !signals.isEmpty {
                            reason += " (no value signals)"
                        }
                        recs.append(ChannelRecommendation(
                            channelID: s.id,
                            channelName: s.name,
                            action: .mute,
                            reason: reason
                        ))
                        continue
                    }
                }
            }

            // Leave: userMsgs==0 OR lastUserActivity 30+ days ago
            // AND not favorite, not watched, not DM, is member
            // Blocked by pending inbox, active tasks, active tracks, or >=3 decisions
            if !s.isFavorite && !s.isWatched
                && s.type != "dm" && s.type != "group_dm" && s.isMember {
                if s.userMessages == 0
                    || (s.lastUserActivity > 0 && s.lastUserActivity < thirtyDaysAgo) {
                    if (vs?.pendingInboxCount ?? 0) > 0 || (vs?.taskCount ?? 0) > 0
                        || (vs?.activeTrackCount ?? 0) > 0 || (vs?.decisionCount ?? 0) >= 3 {
                        // Value signals block leave — fall through
                    } else {
                        let reason = (s.lastUserActivity > 0 && s.lastUserActivity < thirtyDaysAgo)
                            ? "inactive for 30+ days"
                            : "no messages from you"
                        recs.append(ChannelRecommendation(
                            channelID: s.id,
                            channelName: s.name,
                            action: .leave,
                            reason: reason
                        ))
                        continue
                    }
                }
            }

            // Favorite: userMsgs>=10 AND mentions>=3; OR isWatched AND not favorite
            // NEW: decisions>=5 OR active tracks>=2
            // Skip if already favorite
            if !s.isFavorite {
                if (s.userMessages >= 10 && s.mentionCount >= 3)
                    || (s.isWatched && !s.isFavorite) {
                    let reason = s.isWatched ? "watched channel" : "high engagement"
                    recs.append(ChannelRecommendation(
                        channelID: s.id,
                        channelName: s.name,
                        action: .favorite,
                        reason: reason
                    ))
                } else if (vs?.decisionCount ?? 0) >= 5 || (vs?.activeTrackCount ?? 0) >= 2 {
                    let reason = "high-value channel (\(vs?.decisionCount ?? 0) decisions, \(vs?.activeTrackCount ?? 0) active tracks)"
                    recs.append(ChannelRecommendation(
                        channelID: s.id,
                        channelName: s.name,
                        action: .favorite,
                        reason: reason
                    ))
                }
            }
        }

        return recs
    }

    /// Fetch value signals (decisions, tracks, tasks, inbox) per channel.
    /// Mirrors Go `GetChannelValueSignals` — only channels with non-zero signals are returned.
    static func fetchValueSignals(_ db: Database) throws -> [String: ChannelValueSignals] {
        let cutoff = Date().timeIntervalSince1970 - 30 * 86400
        let sql = """
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
            SELECT c.id AS channel_id,
                COALESCE(dc.cnt, 0) AS decision_count,
                COALESCE(tc.cnt, 0) AS active_track_count,
                COALESCE(tk.cnt, 0) AS task_count,
                COALESCE(ic.cnt, 0) AS pending_inbox_count
            FROM channels c
            LEFT JOIN decision_counts dc ON dc.channel_id = c.id
            LEFT JOIN track_channels tc ON tc.channel_id = c.id
            LEFT JOIN task_counts tk ON tk.channel_id = c.id
            LEFT JOIN inbox_counts ic ON ic.channel_id = c.id
            WHERE COALESCE(dc.cnt, 0) + COALESCE(tc.cnt, 0) + COALESCE(tk.cnt, 0) + COALESCE(ic.cnt, 0) > 0
            """

        // Check if digest_topics table exists (may not in older DBs)
        guard try db.tableExists("digest_topics") else {
            // Fall back: only tracks, tasks, inbox (no decisions)
            let fallbackSQL = """
                WITH track_channels AS (
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
                SELECT c.id AS channel_id,
                    0 AS decision_count,
                    COALESCE(tc.cnt, 0) AS active_track_count,
                    COALESCE(tk.cnt, 0) AS task_count,
                    COALESCE(ic.cnt, 0) AS pending_inbox_count
                FROM channels c
                LEFT JOIN track_channels tc ON tc.channel_id = c.id
                LEFT JOIN task_counts tk ON tk.channel_id = c.id
                LEFT JOIN inbox_counts ic ON ic.channel_id = c.id
                WHERE COALESCE(tc.cnt, 0) + COALESCE(tk.cnt, 0) + COALESCE(ic.cnt, 0) > 0
                """
            let rows = try ChannelValueSignals.fetchAll(db, sql: fallbackSQL)
            var result: [String: ChannelValueSignals] = [:]
            for row in rows { result[row.channelID] = row }
            return result
        }

        let rows = try ChannelValueSignals.fetchAll(db, sql: sql, arguments: [cutoff])
        var result: [String: ChannelValueSignals] = [:]
        for row in rows { result[row.channelID] = row }
        return result
    }

    /// Fetch the current user ID from workspace table.
    static func fetchCurrentUserID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
    }

    /// Toggle mute_for_llm setting for a channel (upsert into channel_settings).
    static func toggleMuteForLLM(_ db: Database, channelID: String, muted: Bool) throws {
        try db.execute(sql: """
            INSERT INTO channel_settings (channel_id, is_muted_for_llm, is_favorite)
            VALUES (?, ?, 0)
            ON CONFLICT(channel_id) DO UPDATE SET is_muted_for_llm = excluded.is_muted_for_llm
            """, arguments: [channelID, muted ? 1 : 0])
    }

    /// Toggle favorite setting for a channel (upsert into channel_settings).
    static func toggleFavorite(_ db: Database, channelID: String, favorite: Bool) throws {
        try db.execute(sql: """
            INSERT INTO channel_settings (channel_id, is_muted_for_llm, is_favorite)
            VALUES (?, 0, ?)
            ON CONFLICT(channel_id) DO UPDATE SET is_favorite = excluded.is_favorite
            """, arguments: [channelID, favorite ? 1 : 0])
    }

    /// Fetch the workspace team ID for Slack deep links.
    static func fetchWorkspaceTeamID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT id FROM workspace LIMIT 1")
    }
}
