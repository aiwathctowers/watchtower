import Foundation
import GRDB

enum TaskQueries {

    // MARK: - Fetch

    static func fetchAll(
        _ db: Database,
        status: String? = nil,
        priority: String? = nil,
        ownership: String? = nil,
        includeDone: Bool = false,
        limit: Int = 200
    ) throws -> [TaskItem] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let status {
            conditions.append("status = ?")
            args.append(status)
        } else if !includeDone {
            conditions.append("status NOT IN ('done', 'dismissed')")
        }

        if let priority {
            conditions.append("priority = ?")
            args.append(priority)
        }

        if let ownership {
            conditions.append("ownership = ?")
            args.append(ownership)
        }

        var sql = "SELECT * FROM tasks"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += """
             ORDER BY \
            CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 1 END, \
            CASE WHEN due_date = '' THEN 1 ELSE 0 END, \
            due_date, \
            created_at DESC
            """
        sql += " LIMIT ?"
        args.append(limit)

        return try TaskItem.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchByID(_ db: Database, id: Int) throws -> TaskItem? {
        try TaskItem.fetchOne(db, sql: "SELECT * FROM tasks WHERE id = ?", arguments: [id])
    }

    static func fetchBySourceRef(
        _ db: Database,
        sourceType: String,
        sourceID: String
    ) throws -> [TaskItem] {
        try TaskItem.fetchAll(
            db,
            sql: "SELECT * FROM tasks WHERE source_type = ? AND source_id = ? ORDER BY created_at DESC",
            arguments: [sourceType, sourceID]
        )
    }

    // MARK: - Counts

    static func fetchCounts(_ db: Database) throws -> (active: Int, overdue: Int) {
        let active = try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM tasks WHERE status IN ('todo', 'in_progress', 'blocked')"
        ) ?? 0
        let today = Self.todayDateString()
        let overdue = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM tasks
                WHERE status IN ('todo', 'in_progress', 'blocked')
                AND due_date != '' AND due_date < ?
                """,
            arguments: [today]
        ) ?? 0
        return (active, overdue)
    }

    /// Returns a map of track ID → active task count for all tracks that have tasks.
    static func fetchActiveCountsBySourceTrack(_ db: Database) throws -> [Int: Int] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT source_id, COUNT(*) AS cnt FROM tasks
            WHERE source_type = 'track' AND status IN ('todo', 'in_progress', 'blocked')
            GROUP BY source_id
            """)
        var result: [Int: Int] = [:]
        for row in rows {
            if let trackID = Int(row["source_id"] as String) {
                result[trackID] = row["cnt"]
            }
        }
        return result
    }

    // MARK: - Create

    @discardableResult
    static func create(
        _ db: Database,
        text: String,
        intent: String = "",
        priority: String = "medium",
        ownership: String = "mine",
        dueDate: String = "",
        sourceType: String = "manual",
        sourceID: String = "",
        tags: String = "[]",
        subItems: String = "[]"
    ) throws -> Int64 {
        try db.execute(sql: """
            INSERT INTO tasks (text, intent, priority, ownership, due_date,
                source_type, source_id, tags, sub_items)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [text, intent, priority, ownership, dueDate,
                             sourceType, sourceID, tags, subItems])
        return db.lastInsertedRowID
    }

    // MARK: - Update

    static func updateText(_ db: Database, id: Int, text: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET text = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [text, id]
        )
    }

    static func updateIntent(_ db: Database, id: Int, intent: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET intent = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [intent, id]
        )
    }

    static func updateDueDate(_ db: Database, id: Int, dueDate: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET due_date = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [dueDate, id]
        )
    }

    static func updateOwnership(_ db: Database, id: Int, ownership: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET ownership = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [ownership, id]
        )
    }

    static func updateBlocking(_ db: Database, id: Int, blocking: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET blocking = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [blocking, id]
        )
    }

    static func updateBallOn(_ db: Database, id: Int, ballOn: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET ball_on = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [ballOn, id]
        )
    }

    static func updatePriority(_ db: Database, id: Int, priority: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [priority, id]
        )
    }

    static func updateStatus(_ db: Database, id: Int, status: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [status, id]
        )
    }

    static func updateSubItems(_ db: Database, id: Int, subItems: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET sub_items = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [subItems, id]
        )
    }

    static func snooze(_ db: Database, id: Int, until: String) throws {
        try db.execute(
            sql: """
                UPDATE tasks SET status = 'snoozed', snooze_until = ?,
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [until, id]
        )
    }

    // MARK: - Delete

    static func delete(_ db: Database, id: Int) throws {
        try db.execute(sql: "DELETE FROM tasks WHERE id = ?", arguments: [id])
    }

    // MARK: - Helpers

    static func todayDateString() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: Date())
    }
}
