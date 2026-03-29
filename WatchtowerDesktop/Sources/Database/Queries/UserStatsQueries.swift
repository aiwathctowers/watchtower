import GRDB
import Foundation

enum UserStatsQueries {

    /// Fetch user statistics: message counts, channel activity, thread replies.
    static func fetchAll(_ db: Database) throws -> [UserStat] {
        let sql = """
            SELECT
                u.id,
                u.name,
                u.display_name,
                u.real_name,
                u.email,
                u.is_bot,
                u.is_deleted,
                u.is_bot_override,
                u.is_muted_for_llm,
                COALESCE(ms.total_messages, 0) AS total_messages,
                COALESCE(ms.channel_count, 0) AS channel_count,
                COALESCE(ms.thread_replies, 0) AS thread_replies,
                COALESCE(ms.last_activity, 0) AS last_activity,
                u.updated_at
            FROM users u
            LEFT JOIN (
                SELECT
                    m.user_id,
                    COUNT(*) AS total_messages,
                    COUNT(DISTINCT m.channel_id) AS channel_count,
                    SUM(CASE WHEN m.thread_ts != '' AND m.thread_ts != m.ts THEN 1 ELSE 0 END) AS thread_replies,
                    MAX(m.ts_unix) AS last_activity
                FROM messages m
                WHERE m.is_deleted = 0 AND m.user_id != ''
                GROUP BY m.user_id
            ) ms ON ms.user_id = u.id
            WHERE u.is_stub = 0
            ORDER BY COALESCE(ms.total_messages, 0) DESC, u.name
            """
        return try UserStat.fetchAll(db, sql: sql)
    }

    /// Set or clear the bot override for a user.
    /// - pass `true` to force bot, `false` to force not-bot, `nil` to revert to Slack value.
    static func setBotOverride(_ db: Database, userID: String, isBot: Bool?) throws {
        if let isBot {
            try db.execute(
                sql: "UPDATE users SET is_bot_override = ? WHERE id = ?",
                arguments: [isBot ? 1 : 0, userID]
            )
        } else {
            try db.execute(
                sql: "UPDATE users SET is_bot_override = NULL WHERE id = ?",
                arguments: [userID]
            )
        }
    }

    /// Set or clear the muted-for-LLM flag for a user.
    static func setMutedForLLM(_ db: Database, userID: String, muted: Bool) throws {
        try db.execute(
            sql: "UPDATE users SET is_muted_for_llm = ? WHERE id = ?",
            arguments: [muted ? 1 : 0, userID]
        )
    }

    /// Fetch aggregate message stats: total messages, human messages, bot messages.
    static func fetchMessageStats(_ db: Database) throws -> (total: Int, human: Int, bot: Int) {
        let row = try Row.fetchOne(db, sql: """
            SELECT
                COUNT(*) AS total,
                SUM(CASE WHEN COALESCE(u.is_bot_override, u.is_bot) = 0 AND m.user_id != '' THEN 1 ELSE 0 END) AS human,
                SUM(CASE WHEN COALESCE(u.is_bot_override, u.is_bot) = 1 OR m.user_id = '' THEN 1 ELSE 0 END) AS bot
            FROM messages m
            LEFT JOIN users u ON u.id = m.user_id
            WHERE m.is_deleted = 0
            """)
        guard let row else { return (0, 0, 0) }
        return (row["total"], row["human"], row["bot"])
    }
}
