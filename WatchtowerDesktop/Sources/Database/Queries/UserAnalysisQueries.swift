import GRDB

enum UserAnalysisQueries {
    /// Fetch all analyses for a specific time window, ordered by message count descending.
    static func fetchForWindow(
        _ db: Database,
        periodFrom: Double,
        periodTo: Double
    ) throws -> [UserAnalysis] {
        try UserAnalysis.fetchAll(db, sql: """
            SELECT * FROM user_analyses
            WHERE period_from = ? AND period_to = ?
            ORDER BY message_count DESC
            """, arguments: [periodFrom, periodTo])
    }

    /// Fetch the latest analysis window available.
    static func fetchLatestWindow(_ db: Database) throws -> (from: Double, to: Double)? {
        let row = try Row.fetchOne(db, sql: """
            SELECT period_from, period_to FROM user_analyses
            ORDER BY period_to DESC LIMIT 1
            """)
        guard let row else { return nil }
        return (from: row["period_from"], to: row["period_to"])
    }

    /// Fetch all analyses in the latest available window.
    static func fetchLatest(_ db: Database) throws -> [UserAnalysis] {
        guard let window = try fetchLatestWindow(db) else { return [] }
        return try fetchForWindow(db, periodFrom: window.from, periodTo: window.to)
    }

    /// Fetch all analyses for a specific user, newest first.
    static func fetchByUser(
        _ db: Database,
        userID: String,
        limit: Int = 10
    ) throws -> [UserAnalysis] {
        try UserAnalysis.fetchAll(db, sql: """
            SELECT * FROM user_analyses
            WHERE user_id = ?
            ORDER BY period_to DESC LIMIT ?
            """, arguments: [userID, limit])
    }

    /// Fetch available analysis windows (distinct period ranges).
    static func fetchAvailableWindows(_ db: Database) throws -> [(from: Double, to: Double)] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT DISTINCT period_from, period_to FROM user_analyses
            ORDER BY period_to DESC
            """)
        return rows.map { (from: $0["period_from"], to: $0["period_to"]) }
    }

    /// Count analyses with red flags in the latest window.
    static func countRedFlags(_ db: Database) throws -> Int {
        guard let window = try fetchLatestWindow(db) else { return 0 }
        return try Int.fetchOne(db, sql: """
            SELECT COUNT(*) FROM user_analyses
            WHERE period_from = ? AND period_to = ?
            AND red_flags != '[]' AND red_flags != ''
            """, arguments: [window.from, window.to]) ?? 0
    }

    /// Fetch period summary for a specific window.
    static func fetchPeriodSummary(
        _ db: Database,
        periodFrom: Double,
        periodTo: Double
    ) throws -> PeriodSummary? {
        try PeriodSummary.fetchOne(db, sql: """
            SELECT * FROM period_summaries
            WHERE period_from = ? AND period_to = ?
            """, arguments: [periodFrom, periodTo])
    }
}
