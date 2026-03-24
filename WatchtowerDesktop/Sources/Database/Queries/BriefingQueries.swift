import GRDB

enum BriefingQueries {
    static func fetchRecent(
        _ db: Database,
        limit: Int = 30,
        offset: Int = 0
    ) throws -> [Briefing] {
        try Briefing.fetchAll(
            db,
            sql: """
                SELECT * FROM briefings
                ORDER BY created_at DESC
                LIMIT ? OFFSET ?
                """,
            arguments: [limit, offset]
        )
    }

    static func fetchByID(_ db: Database, id: Int) throws -> Briefing? {
        try Briefing.fetchOne(
            db,
            sql: "SELECT * FROM briefings WHERE id = ?",
            arguments: [id]
        )
    }

    static func fetchLatest(_ db: Database) throws -> Briefing? {
        try Briefing.fetchOne(
            db,
            sql: "SELECT * FROM briefings ORDER BY created_at DESC LIMIT 1"
        )
    }

    static func markRead(_ db: Database, id: Int) throws {
        try db.execute(
            sql: """
                UPDATE briefings SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ? AND read_at IS NULL
                """,
            arguments: [id]
        )
    }

    static func unreadCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM briefings WHERE read_at IS NULL"
        ) ?? 0
    }

    static func maxID(_ db: Database) throws -> Int {
        try Int.fetchOne(db, sql: "SELECT COALESCE(MAX(id), 0) FROM briefings") ?? 0
    }

    static func fetchNewSince(_ db: Database, afterID: Int) throws -> [Briefing] {
        try Briefing.fetchAll(
            db,
            sql: """
                SELECT * FROM briefings
                WHERE id > ?
                ORDER BY id ASC
                """,
            arguments: [afterID]
        )
    }
}
