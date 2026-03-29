import Foundation
import GRDB

enum InboxQueries {

    // MARK: - Fetch

    static func fetchAll(
        _ db: Database,
        status: String? = nil,
        priority: String? = nil,
        triggerType: String? = nil,
        includeResolved: Bool = false,
        limit: Int = 200
    ) throws -> [InboxItem] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let status {
            conditions.append("status = ?")
            args.append(status)
        } else if !includeResolved {
            conditions.append("status NOT IN ('resolved', 'dismissed')")
        }

        if let priority {
            conditions.append("priority = ?")
            args.append(priority)
        }

        if let triggerType {
            conditions.append("trigger_type = ?")
            args.append(triggerType)
        }

        var sql = "SELECT * FROM inbox_items"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += """
             ORDER BY \
            CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 1 END, \
            created_at DESC
            """
        sql += " LIMIT ?"
        args.append(limit)

        return try InboxItem.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchByID(_ db: Database, id: Int) throws -> InboxItem? {
        try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [id])
    }

    // MARK: - Counts

    static func fetchCounts(_ db: Database) throws -> (pending: Int, unread: Int, highPriority: Int) {
        let pending = try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM inbox_items WHERE status = 'pending'"
        ) ?? 0
        let unread = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM inbox_items
                WHERE status = 'pending' AND (read_at IS NULL OR read_at = '')
                """
        ) ?? 0
        let highPriority = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM inbox_items
                WHERE status = 'pending' AND priority = 'high'
                """
        ) ?? 0
        return (pending, unread, highPriority)
    }

    // MARK: - Status Updates

    static func resolve(_ db: Database, id: Int, reason: String = "Manually resolved") throws {
        try db.execute(
            sql: """
                UPDATE inbox_items SET status = 'resolved', resolved_reason = ?,
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [reason, id]
        )
    }

    static func dismiss(_ db: Database, id: Int) throws {
        try db.execute(
            sql: """
                UPDATE inbox_items SET status = 'dismissed',
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [id]
        )
    }

    static func snooze(_ db: Database, id: Int, until: String) throws {
        try db.execute(
            sql: """
                UPDATE inbox_items SET status = 'snoozed', snooze_until = ?,
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [until, id]
        )
    }

    // MARK: - Read

    static func markRead(_ db: Database, id: Int) throws {
        try db.execute(
            sql: """
                UPDATE inbox_items SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ? AND (read_at IS NULL OR read_at = '')
                """,
            arguments: [id]
        )
    }

    // MARK: - Task

    static func linkTask(_ db: Database, inboxID: Int, taskID: Int) throws {
        try db.execute(
            sql: """
                UPDATE inbox_items SET task_id = ?,
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [taskID, inboxID]
        )
    }

    @discardableResult
    static func createTask(_ db: Database, from item: InboxItem) throws -> Int64 {
        let text = item.snippet.isEmpty ? "Follow up on message" : item.snippet
        let taskID = try TaskQueries.create(
            db,
            text: text,
            sourceType: "inbox",
            sourceID: String(item.id)
        )
        try linkTask(db, inboxID: item.id, taskID: Int(taskID))
        return taskID
    }
}
