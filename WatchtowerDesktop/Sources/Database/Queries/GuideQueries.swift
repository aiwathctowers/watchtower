import GRDB

enum GuideQueries {
    /// Fetch all guides for a specific time window, ordered by message count descending.
    static func fetchForWindow(
        _ db: Database,
        periodFrom: Double,
        periodTo: Double
    ) throws -> [CommunicationGuide] {
        try CommunicationGuide.fetchAll(
            db,
            sql: """
                SELECT * FROM communication_guides
                WHERE period_from = ? AND period_to = ?
                ORDER BY message_count DESC
                """,
            arguments: [periodFrom, periodTo]
        )
    }

    /// Fetch the latest guide window available.
    static func fetchLatestWindow(_ db: Database) throws -> (from: Double, to: Double)? {
        let row = try Row.fetchOne(db, sql: """
            SELECT period_from, period_to FROM communication_guides
            ORDER BY period_to DESC LIMIT 1
            """)
        guard let row else { return nil }
        return (from: row["period_from"], to: row["period_to"])
    }

    /// Fetch all guides in the latest available window.
    static func fetchLatest(_ db: Database) throws -> [CommunicationGuide] {
        guard let window = try fetchLatestWindow(db) else { return [] }
        return try fetchForWindow(db, periodFrom: window.from, periodTo: window.to)
    }

    /// Fetch all guides for a specific user, newest first.
    static func fetchByUser(
        _ db: Database,
        userID: String,
        limit: Int = 10
    ) throws -> [CommunicationGuide] {
        try CommunicationGuide.fetchAll(
            db,
            sql: """
                SELECT * FROM communication_guides
                WHERE user_id = ?
                ORDER BY period_to DESC LIMIT ?
                """,
            arguments: [userID, limit]
        )
    }

    /// Fetch available guide windows (distinct period ranges).
    static func fetchAvailableWindows(_ db: Database) throws -> [(from: Double, to: Double)] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT DISTINCT period_from, period_to FROM communication_guides
            ORDER BY period_to DESC
            """)
        return rows.map { (from: $0["period_from"], to: $0["period_to"]) }
    }

    /// Fetch guide summary for a specific window.
    static func fetchGuideSummary(
        _ db: Database,
        periodFrom: Double,
        periodTo: Double
    ) throws -> GuideSummary? {
        try GuideSummary.fetchOne(
            db,
            sql: """
                SELECT * FROM guide_summaries
                WHERE period_from = ? AND period_to = ?
                """,
            arguments: [periodFrom, periodTo]
        )
    }
}
