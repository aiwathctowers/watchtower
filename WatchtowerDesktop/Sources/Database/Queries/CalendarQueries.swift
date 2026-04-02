import Foundation
import GRDB

enum CalendarQueries {

    // MARK: - ISO8601 Helper

    private static let iso8601Formatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    private static func iso8601(_ date: Date) -> String {
        iso8601Formatter.string(from: date)
    }

    // MARK: - Fetch Events

    static func fetchTodayEvents(_ db: Database) throws -> [CalendarEvent] {
        let cal = Calendar.current
        let startOfDay = cal.startOfDay(for: Date())
        let endOfDay = startOfDay.addingTimeInterval(86400)
        return try fetchEvents(
            db,
            from: startOfDay,
            to: endOfDay
        )
    }

    static func fetchEvents(
        _ db: Database,
        from: Date,
        to: Date
    ) throws -> [CalendarEvent] {
        try CalendarEvent.fetchAll(
            db,
            sql: """
                SELECT * FROM calendar_events
                WHERE start_time <= ? AND end_time >= ?
                ORDER BY start_time ASC
                """,
            arguments: [iso8601(to), iso8601(from)]
        )
    }

    static func fetchNextEvent(_ db: Database) throws -> CalendarEvent? {
        let now = iso8601(Date())
        return try CalendarEvent.fetchOne(
            db,
            sql: """
                SELECT * FROM calendar_events
                WHERE start_time > ?
                ORDER BY start_time ASC
                LIMIT 1
                """,
            arguments: [now]
        )
    }

    static func fetchEvent(
        _ db: Database,
        id: String
    ) throws -> CalendarEvent? {
        try CalendarEvent.fetchOne(
            db,
            sql: "SELECT * FROM calendar_events WHERE id = ?",
            arguments: [id]
        )
    }

    static func eventCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM calendar_events"
        ) ?? 0
    }

    // MARK: - Calendar List

    static func fetchCalendars(_ db: Database) throws -> [CalendarCalendarItem] {
        try CalendarCalendarItem.fetchAll(
            db,
            sql: "SELECT * FROM calendar_calendars ORDER BY is_primary DESC, name"
        )
    }

    static func fetchSelectedCalendarIDs(_ db: Database) throws -> [String] {
        try String.fetchAll(
            db,
            sql: "SELECT id FROM calendar_calendars WHERE is_selected = 1"
        )
    }

    static func calendarCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM calendar_calendars"
        ) ?? 0
    }
}
