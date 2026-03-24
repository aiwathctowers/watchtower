import GRDB

enum WatchQueries {
    static func fetchAll(_ db: Database) throws -> [WatchItem] {
        try WatchItem.fetchAll(db, sql: "SELECT * FROM watch_list ORDER BY created_at DESC")
    }

    static func isWatched(_ db: Database, entityType: String, entityID: String) throws -> Bool {
        let count = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM watch_list
                WHERE entity_type = ? AND entity_id = ?
                """,
            arguments: [entityType, entityID]
        )
        return (count ?? 0) > 0
    }

    static func add(_ db: Database, entityType: String, entityID: String, entityName: String, priority: String = "normal") throws {
        try db.execute(sql: """
            INSERT OR REPLACE INTO watch_list (entity_type, entity_id, entity_name, priority, created_at)
            VALUES (?, ?, ?, ?, datetime('now'))
            """, arguments: [entityType, entityID, entityName, priority])
    }

    static func remove(_ db: Database, entityType: String, entityID: String) throws {
        try db.execute(sql: """
            DELETE FROM watch_list WHERE entity_type = ? AND entity_id = ?
            """, arguments: [entityType, entityID])
    }
}
