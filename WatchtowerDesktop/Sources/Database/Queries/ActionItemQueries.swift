import GRDB

enum ActionItemQueries {
    static func fetchAll(
        _ db: Database,
        assigneeUserID: String? = nil,
        status: String? = nil,
        statuses: [String]? = nil,
        channelID: String? = nil,
        priority: String? = nil,
        limit: Int = 200
    ) throws -> [ActionItem] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let assigneeUserID {
            conditions.append("assignee_user_id = ?")
            args.append(assigneeUserID)
        }
        if let statuses, !statuses.isEmpty {
            let placeholders = statuses.map { _ in "?" }.joined(separator: ", ")
            conditions.append("status IN (\(placeholders))")
            for s in statuses { args.append(s) }
        } else if let status {
            conditions.append("status = ?")
            args.append(status)
        }
        if let channelID {
            conditions.append("channel_id = ?")
            args.append(channelID)
        }
        if let priority {
            conditions.append("priority = ?")
            args.append(priority)
        }

        var sql = "SELECT * FROM action_items"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += " ORDER BY CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END, created_at DESC"
        sql += " LIMIT ?"
        args.append(limit)

        return try ActionItem.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchOpenCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(db, sql: """
            SELECT COUNT(*) FROM action_items
            WHERE assignee_user_id = ? AND status IN ('inbox', 'active')
            """, arguments: [assigneeUserID]) ?? 0
    }

    static func fetchInboxCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(db, sql: """
            SELECT COUNT(*) FROM action_items
            WHERE assignee_user_id = ? AND status = 'inbox'
            """, arguments: [assigneeUserID]) ?? 0
    }

    static func fetchUpdatedCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(db, sql: """
            SELECT COUNT(*) FROM action_items
            WHERE assignee_user_id = ? AND has_updates = 1 AND status IN ('inbox', 'active')
            """, arguments: [assigneeUserID]) ?? 0
    }

    static func updateStatus(_ db: Database, id: Int, status: String) throws {
        if status == "done" || status == "dismissed" {
            try db.execute(sql: """
                UPDATE action_items SET status = ?, completed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """, arguments: [status, id])
        } else {
            try db.execute(sql: """
                UPDATE action_items SET status = ?, completed_at = NULL
                WHERE id = ?
                """, arguments: [status, id])
        }
    }

    static func acceptItem(_ db: Database, id: Int) throws {
        try db.execute(sql: """
            UPDATE action_items SET status = 'active', completed_at = NULL WHERE id = ? AND status = 'inbox'
            """, arguments: [id])
        try db.execute(sql: """
            INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
            VALUES (?, 'accepted', 'status', 'inbox', 'active')
            """, arguments: [id])
    }

    static func snoozeItem(_ db: Database, id: Int, until: Double) throws {
        let currentStatus = try String.fetchOne(db, sql: "SELECT status FROM action_items WHERE id = ?", arguments: [id]) ?? "inbox"
        try db.execute(sql: """
            UPDATE action_items SET status = 'snoozed', snooze_until = ?, pre_snooze_status = ? WHERE id = ?
            """, arguments: [until, currentStatus, id])
        try db.execute(sql: """
            INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
            VALUES (?, 'snoozed', 'status', ?, 'snoozed')
            """, arguments: [id, currentStatus])
    }

    static func markUpdateRead(_ db: Database, id: Int) throws {
        try db.execute(sql: "UPDATE action_items SET has_updates = 0 WHERE id = ?", arguments: [id])
        try db.execute(sql: """
            INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
            VALUES (?, 'update_read', '', '', '')
            """, arguments: [id])
    }

    static func fetchByID(_ db: Database, id: Int) throws -> ActionItem? {
        try ActionItem.fetchOne(db, sql: "SELECT * FROM action_items WHERE id = ?", arguments: [id])
    }

    static func fetchHistory(_ db: Database, actionItemID: Int) throws -> [ActionItemHistoryEntry] {
        guard try db.tableExists("action_item_history") else { return [] }
        return try ActionItemHistoryEntry.fetchAll(db, sql: """
            SELECT * FROM action_item_history
            WHERE action_item_id = ? ORDER BY created_at ASC
            """, arguments: [actionItemID])
    }

    static func updateSubItems(_ db: Database, id: Int, subItemsJSON: String) throws {
        try db.execute(sql: "UPDATE action_items SET sub_items = ? WHERE id = ?", arguments: [subItemsJSON, id])
    }

    static func fetchStatusCounts(_ db: Database, assigneeUserID: String) throws -> [String: Int] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT status, COUNT(*) as cnt FROM action_items
            WHERE assignee_user_id = ?
            GROUP BY status
            """, arguments: [assigneeUserID])
        var result: [String: Int] = [:]
        for row in rows {
            let status: String = row["status"]
            let cnt: Int = row["cnt"]
            result[status] = cnt
        }
        return result
    }

    static func fetchTotalCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(db, sql: """
            SELECT COUNT(*) FROM action_items WHERE assignee_user_id = ?
            """, arguments: [assigneeUserID]) ?? 0
    }

    static func fetchCurrentUserID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
    }

    /// Insert a manually-created action item (e.g. from a digest) and log history.
    @discardableResult
    static func insertActionItem(
        _ db: Database,
        channelID: String,
        assigneeUserID: String,
        assigneeRaw: String,
        text: String,
        context: String,
        sourceMessageTS: String,
        sourceChannelName: String,
        priority: String,
        dueDate: Double?,
        periodFrom: Double,
        periodTo: Double,
        model: String,
        inputTokens: Int,
        outputTokens: Int,
        costUSD: Double,
        participants: String,
        sourceRefs: String,
        requesterName: String,
        requesterUserID: String,
        category: String,
        blocking: String,
        tags: String,
        decisionSummary: String,
        decisionOptions: String,
        relatedDigestIDs: String,
        subItems: String
    ) throws -> Int {
        try db.execute(sql: """
            INSERT INTO action_items (
                channel_id, assignee_user_id, assignee_raw, text, context,
                source_message_ts, source_channel_name, status, priority,
                due_date, period_from, period_to, model, input_tokens,
                output_tokens, cost_usd, participants, source_refs,
                requester_name, requester_user_id, category, blocking,
                tags, decision_summary, decision_options, related_digest_ids, sub_items
            ) VALUES (
                ?, ?, ?, ?, ?,
                ?, ?, 'inbox', ?,
                ?, ?, ?, ?, ?,
                ?, ?, ?, ?,
                ?, ?, ?, ?,
                ?, ?, ?, ?, ?
            )
            """, arguments: [
                channelID, assigneeUserID, assigneeRaw, text, context,
                sourceMessageTS, sourceChannelName, priority,
                dueDate, periodFrom, periodTo, model, inputTokens,
                outputTokens, costUSD, participants, sourceRefs,
                requesterName, requesterUserID, category, blocking,
                tags, decisionSummary, decisionOptions, relatedDigestIDs, subItems
            ])
        let rowID = Int(db.lastInsertedRowID)
        try db.execute(sql: """
            INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
            VALUES (?, 'created', 'source', '', 'from_digest')
            """, arguments: [rowID])
        return rowID
    }
}
