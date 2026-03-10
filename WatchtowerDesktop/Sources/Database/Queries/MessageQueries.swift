import GRDB

enum MessageQueries {
    static func fetchByChannel(_ db: Database, channelID: String, limit: Int = 50, offset: Int = 0) throws -> [Message] {
        // Load the latest messages by using a subquery with DESC, then re-order ASC for display
        try Message.fetchAll(db, sql: """
            SELECT * FROM (
                SELECT * FROM messages
                WHERE channel_id = ?
                ORDER BY ts_unix DESC
                LIMIT ? OFFSET ?
            ) ORDER BY ts_unix ASC
            """, arguments: [channelID, limit, offset])
    }

    static func fetchByTimeRange(_ db: Database, channelID: String, from: Double, to: Double) throws -> [Message] {
        try Message.fetchAll(db, sql: """
            SELECT * FROM messages
            WHERE channel_id = ? AND ts_unix >= ? AND ts_unix <= ?
            ORDER BY ts_unix ASC
            """, arguments: [channelID, from, to])
    }

    static func fetchThreadReplies(_ db: Database, channelID: String, threadTS: String) throws -> [Message] {
        try Message.fetchAll(db, sql: """
            SELECT * FROM messages
            WHERE channel_id = ? AND thread_ts = ?
            ORDER BY ts_unix ASC
            """, arguments: [channelID, threadTS])
    }

    static func fetchRecentWatched(_ db: Database, sinceUnix: Double, limit: Int = 50) throws -> [MessageWithContext] {
        try MessageWithContext.fetchAll(db, sql: """
            SELECT m.*, c.name as channel_name, u.display_name as user_name
            FROM messages m
            JOIN channels c ON c.id = m.channel_id
            LEFT JOIN users u ON u.id = m.user_id
            JOIN watch_list w ON w.entity_type = 'channel' AND w.entity_id = m.channel_id
            WHERE m.ts_unix > ?
            ORDER BY m.ts_unix DESC
            LIMIT ?
            """, arguments: [sinceUnix, limit])
    }

    static func countByChannel(_ db: Database, channelID: String) throws -> Int {
        try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM messages WHERE channel_id = ?", arguments: [channelID]) ?? 0
    }
}

struct MessageWithContext: FetchableRecord, Decodable, Identifiable, Equatable {
    let channelID: String
    let ts: String
    let userID: String
    let text: String
    let threadTS: String?
    let replyCount: Int
    let isEdited: Bool
    let isDeleted: Bool
    let subtype: String
    let permalink: String
    let tsUnix: Double
    let rawJSON: String
    let channelName: String?
    let userName: String?

    var id: String { "\(channelID)_\(ts)" }

    enum CodingKeys: String, CodingKey {
        case ts, text, subtype, permalink
        case channelID = "channel_id"
        case userID = "user_id"
        case threadTS = "thread_ts"
        case replyCount = "reply_count"
        case isEdited = "is_edited"
        case isDeleted = "is_deleted"
        case tsUnix = "ts_unix"
        case rawJSON = "raw_json"
        case channelName = "channel_name"
        case userName = "user_name"
    }
}
