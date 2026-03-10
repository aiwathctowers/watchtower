import GRDB

enum ChannelQueries {
    // H8: removed dead SortOrder enum (was never applied to query)

    static func fetchAll(_ db: Database, type: String? = nil) throws -> [Channel] {
        var sql = "SELECT * FROM channels"
        var args: [any DatabaseValueConvertible] = []

        if let type {
            sql += " WHERE type = ?"
            args.append(type)
        }

        sql += " ORDER BY name ASC"
        return try Channel.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchByID(_ db: Database, id: String) throws -> Channel? {
        try Channel.fetchOne(db, sql: "SELECT * FROM channels WHERE id = ?", arguments: [id])
    }

    static func fetchByName(_ db: Database, name: String) throws -> Channel? {
        try Channel.fetchOne(db, sql: "SELECT * FROM channels WHERE name = ?", arguments: [name])
    }

    static func fetchWatched(_ db: Database) throws -> [Channel] {
        try Channel.fetchAll(db, sql: """
            SELECT c.* FROM channels c
            JOIN watch_list w ON w.entity_type = 'channel' AND w.entity_id = c.id
            ORDER BY c.name ASC
            """)
    }
}
