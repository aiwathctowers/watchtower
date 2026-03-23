import GRDB
import Foundation

enum StatsQueries {
    /// Dashboard aggregate stats
    static func fetchDashboardStats(_ db: Database) throws -> WorkspaceStats {
        try WorkspaceStats.fetch(db)
    }

    /// Message count per channel (for top channels display)
    static func fetchTopChannels(_ db: Database, limit: Int = 10) throws -> [(channelID: String, name: String, count: Int)] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT c.id, c.name, COUNT(m.ts) as msg_count
            FROM channels c
            LEFT JOIN messages m ON m.channel_id = c.id
            GROUP BY c.id
            ORDER BY msg_count DESC
            LIMIT ?
            """, arguments: [limit])
        return rows.map { (channelID: $0["id"], name: $0["name"], count: $0["msg_count"]) }
    }

    /// Messages per day over the last N days
    static func fetchMessageVolume(_ db: Database, days: Int = 14) throws -> [(date: String, count: Int)] {
        let cutoff = Date().timeIntervalSince1970 - Double(days * 86400)
        let rows = try Row.fetchAll(db, sql: """
            SELECT date(ts_unix, 'unixepoch') as day, COUNT(*) as cnt
            FROM messages
            WHERE ts_unix > ?
            GROUP BY day
            ORDER BY day ASC
            """, arguments: [cutoff])
        return rows.map { (date: $0["day"], count: $0["cnt"]) }
    }

    /// Sync state summary
    static func fetchSyncSummary(_ db: Database) throws -> (synced: Int, total: Int, totalMessages: Int) {
        let total = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM channels WHERE is_member = 1") ?? 0
        let synced = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM sync_state WHERE is_initial_sync_complete = 1") ?? 0
        let messages = try Int.fetchOne(db, sql: "SELECT SUM(messages_synced) FROM sync_state") ?? 0
        return (synced: synced, total: total, totalMessages: messages)
    }
}
