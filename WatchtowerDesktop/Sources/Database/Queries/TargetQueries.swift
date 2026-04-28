import Foundation
import GRDB

// MARK: - Supporting Types

struct TargetFilter {
    var level: String? = nil
    var status: String? = nil
    var priority: String? = nil
    var ownership: String? = nil
    var periodStart: String? = nil     // filter targets whose period overlaps this date range start
    var periodEnd: String? = nil       // filter targets whose period overlaps this date range end
    var search: String? = nil
    var includeDone: Bool = false
    var parentID: Int? = nil
    var limit: Int = 200
}

struct TargetCounts {
    let active: Int
    let overdue: Int
    let dueToday: Int
    let highPriority: Int
}

enum LinkDirection {
    case inbound    // target_target_id = targetID
    case outbound   // source_target_id = targetID
    case both
}

// MARK: - TargetQueries

enum TargetQueries {

    // MARK: - Fetch

    static func fetchAll(
        _ db: Database,
        filter: TargetFilter = TargetFilter()
    ) throws -> [Target] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let level = filter.level {
            conditions.append("level = ?")
            args.append(level)
        }

        if let status = filter.status {
            conditions.append("status = ?")
            args.append(status)
        } else if !filter.includeDone {
            conditions.append("status NOT IN ('done', 'dismissed')")
        }

        if let priority = filter.priority {
            conditions.append("priority = ?")
            args.append(priority)
        }

        if let ownership = filter.ownership {
            conditions.append("ownership = ?")
            args.append(ownership)
        }

        if let parentID = filter.parentID {
            conditions.append("parent_id = ?")
            args.append(parentID)
        }

        // Period overlap: period_start <= periodEnd AND period_end >= periodStart
        if let ps = filter.periodStart {
            conditions.append("period_end >= ?")
            args.append(ps)
        }
        if let pe = filter.periodEnd {
            conditions.append("period_start <= ?")
            args.append(pe)
        }

        if let search = filter.search, !search.isEmpty {
            conditions.append("(text LIKE ? OR intent LIKE ?)")
            let pattern = "%\(search)%"
            args.append(pattern)
            args.append(pattern)
        }

        var sql = "SELECT * FROM targets"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += """
             ORDER BY \
            CASE level WHEN 'quarter' THEN 0 WHEN 'month' THEN 1 WHEN 'week' THEN 2 WHEN 'day' THEN 3 ELSE 4 END, \
            period_start ASC, \
            CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 1 END, \
            created_at DESC
            """
        sql += " LIMIT ?"
        args.append(filter.limit)

        return try Target.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchByID(_ db: Database, id: Int) throws -> Target? {
        try Target.fetchOne(db, sql: "SELECT * FROM targets WHERE id = ?", arguments: [id])
    }

    static func fetchBySourceRef(
        _ db: Database,
        sourceType: String,
        sourceID: String
    ) throws -> [Target] {
        try Target.fetchAll(
            db,
            sql: "SELECT * FROM targets WHERE source_type = ? AND source_id = ? ORDER BY created_at DESC",
            arguments: [sourceType, sourceID]
        )
    }

    // MARK: - Counts

    static func fetchCounts(_ db: Database) throws -> TargetCounts {
        let active = try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM targets WHERE status IN ('todo', 'in_progress', 'blocked')"
        ) ?? 0
        let now = nowDatetimeString()
        let overdue = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM targets
                WHERE status IN ('todo', 'in_progress', 'blocked')
                AND due_date != '' AND due_date < ?
                """,
            arguments: [now]
        ) ?? 0
        let today = todayDateString()
        let dueToday = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM targets
                WHERE status IN ('todo', 'in_progress', 'blocked')
                AND due_date != '' AND due_date >= ? AND due_date < ?
                """,
            arguments: [today, today + "T23:59"]
        ) ?? 0
        let highPriority = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM targets
                WHERE status IN ('todo', 'in_progress', 'blocked')
                AND priority = 'high'
                """
        ) ?? 0
        return TargetCounts(active: active, overdue: overdue, dueToday: dueToday, highPriority: highPriority)
    }

    // MARK: - Create

    @discardableResult
    static func create(
        _ db: Database,
        text: String,
        intent: String = "",
        level: String = "day",
        customLabel: String = "",
        periodStart: String,
        periodEnd: String,
        parentId: Int? = nil,
        status: String = "todo",
        priority: String = "medium",
        ownership: String = "mine",
        ballOn: String = "",
        dueDate: String = "",
        snoozeUntil: String = "",
        blocking: String = "",
        tags: String = "[]",
        subItems: String = "[]",
        notes: String = "[]",
        progress: Double = 0.0,
        sourceType: String = "manual",
        sourceID: String = "",
        aiLevelConfidence: Double? = nil,
        secondaryLinks: [TargetPrefillLink] = []
    ) throws -> Int {
        try db.execute(sql: """
            INSERT INTO targets (text, intent, level, custom_label, period_start, period_end,
                parent_id, status, priority, ownership, ball_on, due_date, snooze_until,
                blocking, tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [text, intent, level, customLabel, periodStart, periodEnd,
                             parentId, status, priority, ownership, ballOn, dueDate, snoozeUntil,
                             blocking, tags, subItems, notes, progress, sourceType, sourceID, aiLevelConfidence])
        let newID = Int(db.lastInsertedRowID)

        for link in secondaryLinks {
            let ref = link.externalRef
            // Mirrors the Go-side allow-list `IsValidExternalRef`
            // (internal/targets/extractor.go:146): only "jira:" and "slack:" pass.
            guard ref.hasPrefix("jira:") || ref.hasPrefix("slack:") else { continue }
            try db.execute(
                sql: """
                    INSERT INTO target_links (source_target_id, target_target_id, external_ref, relation, created_by)
                    VALUES (?, NULL, ?, ?, 'user')
                    """,
                arguments: [newID, ref, link.relation]
            )
        }

        return newID
    }

    // MARK: - Update

    static func updateStatus(_ db: Database, id: Int, status: String) throws {
        try db.execute(
            sql: """
                UPDATE targets SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [status, id]
        )
    }

    static func updatePriority(_ db: Database, id: Int, priority: String) throws {
        try db.execute(
            sql: """
                UPDATE targets SET priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [priority, id]
        )
    }

    static func updateSubItems(_ db: Database, id: Int, subItems: [TargetSubItem]) throws {
        let data = try JSONEncoder().encode(subItems)
        let json = String(data: data, encoding: .utf8) ?? "[]"
        try db.execute(
            sql: """
                UPDATE targets SET sub_items = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [json, id]
        )
    }

    static func snooze(_ db: Database, id: Int, until: Date) throws {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        let dateStr = fmt.string(from: until)
        try db.execute(
            sql: """
                UPDATE targets SET status = 'snoozed', snooze_until = ?,
                    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """,
            arguments: [dateStr, id]
        )
    }

    // MARK: - Delete

    static func delete(_ db: Database, id: Int) throws {
        try db.execute(sql: "DELETE FROM targets WHERE id = ?", arguments: [id])
    }

    // MARK: - Links

    static func fetchLinks(
        _ db: Database,
        targetID: Int,
        direction: LinkDirection = .both
    ) throws -> [TargetLink] {
        switch direction {
        case .inbound:
            return try TargetLink.fetchAll(
                db,
                sql: "SELECT * FROM target_links WHERE target_target_id = ? ORDER BY created_at DESC",
                arguments: [targetID]
            )
        case .outbound:
            return try TargetLink.fetchAll(
                db,
                sql: "SELECT * FROM target_links WHERE source_target_id = ? ORDER BY created_at DESC",
                arguments: [targetID]
            )
        case .both:
            return try TargetLink.fetchAll(
                db,
                sql: """
                    SELECT * FROM target_links
                    WHERE source_target_id = ? OR target_target_id = ?
                    ORDER BY created_at DESC
                    """,
                arguments: [targetID, targetID]
            )
        }
    }

    // MARK: - Helpers

    static func todayDateString() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: Date())
    }

    static func nowDatetimeString() -> String {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd'T'HH:mm"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: Date())
    }
}
