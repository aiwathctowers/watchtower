import GRDB

enum FeedbackQueries {

    // MARK: - Write

    static func addFeedback(
        _ db: Database,
        entityType: String,
        entityID: String,
        rating: Int,
        comment: String = ""
    ) throws {
        guard try db.tableExists("feedback") else { return }
        try db.execute(
            sql: """
                INSERT INTO feedback (entity_type, entity_id, rating, comment)
                VALUES (?, ?, ?, ?)
                """,
            arguments: [entityType, entityID, rating, comment]
        )
    }

    // MARK: - Read

    static func getFeedback(
        _ db: Database,
        entityType: String,
        entityID: String
    ) throws -> Feedback? {
        guard try db.tableExists("feedback") else { return nil }
        return try Feedback.fetchOne(db, sql: """
            SELECT * FROM feedback
            WHERE entity_type = ? AND entity_id = ?
            ORDER BY created_at DESC LIMIT 1
            """, arguments: [entityType, entityID])
    }

    static func getStats(_ db: Database) throws -> [FeedbackStats] {
        guard try db.tableExists("feedback") else { return [] }
        let rows = try Row.fetchAll(db, sql: """
            SELECT entity_type,
                SUM(CASE WHEN rating = 1 THEN 1 ELSE 0 END) as positive,
                SUM(CASE WHEN rating = -1 THEN 1 ELSE 0 END) as negative,
                COUNT(*) as total
            FROM feedback GROUP BY entity_type ORDER BY entity_type
            """)
        return rows.map { row in
            FeedbackStats(
                entityType: row["entity_type"],
                positive: row["positive"],
                negative: row["negative"],
                total: row["total"]
            )
        }
    }

    static func getAllFeedback(_ db: Database, limit: Int = 100) throws -> [Feedback] {
        guard try db.tableExists("feedback") else { return [] }
        return try Feedback.fetchAll(db, sql: """
            SELECT * FROM feedback ORDER BY created_at DESC LIMIT ?
            """, arguments: [limit])
    }
}
