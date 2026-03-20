import Foundation
import GRDB

enum PeopleCardQueries {
    static func fetchForWindow(_ db: Database, from: Double, to: Double) throws -> [PeopleCard] {
        try PeopleCard.fetchAll(db, sql: """
            SELECT * FROM people_cards WHERE period_from = ? AND period_to = ? ORDER BY message_count DESC
            """, arguments: [from, to])
    }

    static func fetchLatestWindow(_ db: Database) throws -> (from: Double, to: Double)? {
        let row = try Row.fetchOne(db, sql: """
            SELECT period_from, period_to FROM people_cards ORDER BY period_to DESC LIMIT 1
            """)
        guard let row = row else { return nil }
        return (from: row["period_from"], to: row["period_to"])
    }

    static func fetchLatest(_ db: Database) throws -> [PeopleCard] {
        guard let window = try fetchLatestWindow(db) else { return [] }
        return try fetchForWindow(db, from: window.from, to: window.to)
    }

    static func fetchByUser(_ db: Database, userID: String, limit: Int = 10) throws -> [PeopleCard] {
        try PeopleCard.fetchAll(db, sql: """
            SELECT * FROM people_cards WHERE user_id = ? ORDER BY period_to DESC LIMIT ?
            """, arguments: [userID, limit])
    }

    static func fetchAvailableWindows(_ db: Database) throws -> [(from: Double, to: Double)] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT DISTINCT period_from, period_to FROM people_cards ORDER BY period_to DESC
            """)
        return rows.map { (from: $0["period_from"], to: $0["period_to"]) }
    }

    static func fetchSummary(_ db: Database, from: Double, to: Double) throws -> PeopleCardSummary? {
        try PeopleCardSummary.fetchOne(db, sql: """
            SELECT * FROM people_card_summaries WHERE period_from = ? AND period_to = ?
            """, arguments: [from, to])
    }
}
