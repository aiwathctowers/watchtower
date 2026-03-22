import GRDB

enum InteractionQueries {
    /// Fetch all interactions for a user in a specific time window, ordered by score.
    static func fetchForUser(
        _ db: Database,
        userID: String,
        periodFrom: Double,
        periodTo: Double
    ) throws -> [UserInteraction] {
        try UserInteraction.fetchAll(
            db,
            sql: """
                SELECT * FROM user_interactions
                WHERE user_a = ? AND period_from = ? AND period_to = ?
                ORDER BY interaction_score DESC
                """,
            arguments: [userID, periodFrom, periodTo]
        )
    }

    /// Fetch top N interactions by score for a user in the latest window.
    static func fetchTopInteractions(
        _ db: Database,
        userID: String,
        periodFrom: Double,
        periodTo: Double,
        limit: Int = 20
    ) throws -> [UserInteraction] {
        try UserInteraction.fetchAll(
            db,
            sql: """
                SELECT * FROM user_interactions
                WHERE user_a = ? AND period_from = ? AND period_to = ?
                ORDER BY interaction_score DESC
                LIMIT ?
                """,
            arguments: [userID, periodFrom, periodTo, limit]
        )
    }
}
