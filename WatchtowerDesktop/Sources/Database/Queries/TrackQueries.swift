import GRDB

enum TrackQueries {
    static func fetchAll(
        _ db: Database,
        assigneeUserID: String? = nil,
        status: String? = nil,
        statuses: [String]? = nil,
        channelID: String? = nil,
        priority: String? = nil,
        ownership: String? = nil,
        limit: Int = 200,
        offset: Int = 0
    ) throws -> [Track] {
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
        if let ownership {
            conditions.append("ownership = ?")
            args.append(ownership)
        }

        var sql = """
            SELECT t.*,
              CASE
                WHEN t.source_channel_name = '' OR t.source_channel_name = t.channel_id THEN
                  COALESCE(
                    (SELECT 'DM: ' || COALESCE(NULLIF(u.display_name, ''), u.name)
                     FROM channels c JOIN users u ON c.dm_user_id = u.id
                     WHERE c.id = t.channel_id AND c.type IN ('dm', 'im')),
                    CASE WHEN t.source_channel_name = '' THEN t.channel_id ELSE t.source_channel_name END
                  )
                ELSE t.source_channel_name
              END as source_channel_name
            FROM tracks t
            """
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += " ORDER BY CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END, CASE WHEN source_message_ts != '' THEN source_message_ts ELSE created_at END DESC"
        sql += " LIMIT ? OFFSET ?"
        args.append(limit)
        args.append(offset)

        return try Track.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchOpenCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM tracks
                WHERE assignee_user_id = ? AND status IN ('inbox', 'active')
                """,
            arguments: [assigneeUserID]
        ) ?? 0
    }

    static func fetchInboxCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM tracks
                WHERE assignee_user_id = ? AND status = 'inbox'
                """,
            arguments: [assigneeUserID]
        ) ?? 0
    }

    static func fetchUpdatedCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM tracks
                WHERE assignee_user_id = ? AND has_updates = 1 AND status IN ('inbox', 'active')
                """,
            arguments: [assigneeUserID]
        ) ?? 0
    }

    static func updateStatus(_ db: Database, id: Int, status: String) throws {
        // Get old status for history logging.
        let oldStatus = try String.fetchOne(db, sql: "SELECT status FROM tracks WHERE id = ?", arguments: [id])

        if status == "done" || status == "dismissed" {
            try db.execute(sql: """
                UPDATE tracks SET status = ?, completed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                WHERE id = ?
                """, arguments: [status, id])
        } else {
            try db.execute(sql: """
                UPDATE tracks SET status = ?, completed_at = NULL
                WHERE id = ?
                """, arguments: [status, id])
        }

        // Log history if status actually changed.
        if let oldStatus, oldStatus != status {
            let event = (oldStatus != "inbox" && oldStatus != "active" && (status == "inbox" || status == "active"))
                ? "reopened" : "status_changed"
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value)
                VALUES (?, ?, 'status', ?, ?)
                """, arguments: [id, event, oldStatus, status])

            // Implicit feedback for Training
            if status == "dismissed" {
                try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "\(id)", rating: -1,
                                                comment: "dismissed")
            } else if status == "done" || (status == "active" && oldStatus == "inbox") {
                try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "\(id)", rating: 1,
                                                comment: status == "done" ? "completed" : "accepted")
            }
        }
    }

    static func acceptItem(_ db: Database, id: Int) throws {
        try db.execute(sql: """
            UPDATE tracks SET status = 'active', completed_at = NULL WHERE id = ? AND status = 'inbox'
            """, arguments: [id])
        // Only log history if a row was actually updated (item was in inbox).
        if db.changesCount > 0 {
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value)
                VALUES (?, 'accepted', 'status', 'inbox', 'active')
                """, arguments: [id])
        }
    }

    static func snoozeItem(_ db: Database, id: Int, until: Double) throws {
        let currentStatus = try String.fetchOne(db, sql: "SELECT status FROM tracks WHERE id = ?", arguments: [id]) ?? "inbox"
        try db.execute(sql: """
            UPDATE tracks SET status = 'snoozed', snooze_until = ?, pre_snooze_status = ? WHERE id = ?
            """, arguments: [until, currentStatus, id])
        // Only log history if a row was actually updated.
        guard db.changesCount > 0 else { return }
        try db.execute(sql: """
            INSERT INTO track_history (track_id, event, field, old_value, new_value)
            VALUES (?, 'snoozed', 'status', ?, 'snoozed')
            """, arguments: [id, currentStatus])
    }

    static func markUpdateRead(_ db: Database, id: Int) throws {
        try db.execute(sql: "UPDATE tracks SET has_updates = 0 WHERE id = ?", arguments: [id])
        try db.execute(sql: """
            INSERT INTO track_history (track_id, event, field, old_value, new_value)
            VALUES (?, 'update_read', '', '', '')
            """, arguments: [id])
    }

    static func updatePriority(_ db: Database, id: Int, priority: String) throws {
        let old = try String.fetchOne(db, sql: "SELECT priority FROM tracks WHERE id = ?", arguments: [id])
        try db.execute(sql: "UPDATE tracks SET priority = ? WHERE id = ?", arguments: [priority, id])
        if let old, old != priority {
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value)
                VALUES (?, 'priority_changed', 'priority', ?, ?)
                """, arguments: [id, old, priority])
            // Implicit negative feedback: AI assigned wrong priority
            try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "\(id)", rating: -1,
                                            comment: "priority corrected: \(old) → \(priority)")
        }
    }

    static func updateCategory(_ db: Database, id: Int, category: String) throws {
        let old = try String.fetchOne(db, sql: "SELECT category FROM tracks WHERE id = ?", arguments: [id])
        try db.execute(sql: "UPDATE tracks SET category = ? WHERE id = ?", arguments: [category, id])
        if let old, old != category {
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value)
                VALUES (?, 'category_changed', 'category', ?, ?)
                """, arguments: [id, old, category])
            // Implicit negative feedback: AI assigned wrong category
            try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "\(id)", rating: -1,
                                            comment: "category corrected: \(old) → \(category)")
        }
    }

    static func updateOwnership(_ db: Database, id: Int, ownership: String) throws {
        let old = try String.fetchOne(db, sql: "SELECT ownership FROM tracks WHERE id = ?", arguments: [id])
        try db.execute(sql: "UPDATE tracks SET ownership = ? WHERE id = ?", arguments: [ownership, id])
        if let old, old != ownership {
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value)
                VALUES (?, 'ownership_changed', 'ownership', ?, ?)
                """, arguments: [id, old, ownership])
            try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "\(id)", rating: -1,
                                            comment: "ownership corrected: \(old) → \(ownership)")
        }
    }

    static func fetchByID(_ db: Database, id: Int) throws -> Track? {
        try Track.fetchOne(db, sql: "SELECT * FROM tracks WHERE id = ?", arguments: [id])
    }

    static func fetchHistory(_ db: Database, trackID: Int) throws -> [TrackHistoryEntry] {
        guard try db.tableExists("track_history") else { return [] }
        return try TrackHistoryEntry.fetchAll(
            db,
            sql: """
                SELECT * FROM track_history
                WHERE track_id = ? ORDER BY created_at ASC
                """,
            arguments: [trackID]
        )
    }

    static func updateSubItems(_ db: Database, id: Int, subItemsJSON: String) throws {
        try db.execute(sql: "UPDATE tracks SET sub_items = ? WHERE id = ?", arguments: [subItemsJSON, id])
        guard try db.tableExists("track_history") else { return }
        try db.execute(sql: """
            INSERT INTO track_history (track_id, event, field, new_value, created_at)
            VALUES (?, 'sub_items_updated', 'sub_items', ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
            """, arguments: [id, subItemsJSON])
    }

    static func fetchStatusCounts(_ db: Database, assigneeUserID: String) throws -> [String: Int] {
        let rows = try Row.fetchAll(
            db,
            sql: """
                SELECT status, COUNT(*) as cnt FROM tracks
                WHERE assignee_user_id = ?
                GROUP BY status
                """,
            arguments: [assigneeUserID]
        )
        var result: [String: Int] = [:]
        for row in rows {
            let status: String = row["status"]
            let cnt: Int = row["cnt"]
            result[status] = cnt
        }
        return result
    }

    static func fetchOwnershipCounts(_ db: Database, assigneeUserID: String) throws -> [String: Int] {
        let rows = try Row.fetchAll(
            db,
            sql: """
                SELECT ownership, COUNT(*) as cnt FROM tracks
                WHERE assignee_user_id = ? AND status IN ('inbox', 'active')
                GROUP BY ownership
                """,
            arguments: [assigneeUserID]
        )
        var result: [String: Int] = [:]
        for row in rows {
            let ownership: String = row["ownership"]
            let cnt: Int = row["cnt"]
            result[ownership] = cnt
        }
        return result
    }

    static func fetchTotalCount(_ db: Database, assigneeUserID: String) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) FROM tracks WHERE assignee_user_id = ?
                """,
            arguments: [assigneeUserID]
        ) ?? 0
    }

    static func fetchCurrentUserID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
    }

    /// Insert a manually-created track (e.g. from a digest) and log history.
    @discardableResult
    static func insertTrack(_ db: Database, data: TrackInsertData) throws -> Int {
        try db.execute(sql: """
            INSERT INTO tracks (
                channel_id, assignee_user_id, assignee_raw, text, context,
                source_message_ts, source_channel_name, status, priority,
                due_date, period_from, period_to, model, input_tokens,
                output_tokens, cost_usd, participants, source_refs,
                requester_name, requester_user_id, category, blocking,
                tags, decision_summary, decision_options, related_digest_ids, sub_items,
                ownership, ball_on, owner_user_id
            ) VALUES (
                ?, ?, ?, ?, ?,
                ?, ?, 'inbox', ?,
                ?, ?, ?, ?, ?,
                ?, ?, ?, ?,
                ?, ?, ?, ?,
                ?, ?, ?, ?, ?,
                ?, ?, ?
            )
            """, arguments: [
                data.channelID, data.assigneeUserID, data.assigneeRaw, data.text, data.context,
                data.sourceMessageTS, data.sourceChannelName, data.priority,
                data.dueDate, data.periodFrom, data.periodTo, data.model, data.inputTokens,
                data.outputTokens, data.costUSD, data.participants, data.sourceRefs,
                data.requesterName, data.requesterUserID, data.category, data.blocking,
                data.tags, data.decisionSummary, data.decisionOptions, data.relatedDigestIDs, data.subItems,
                data.ownership, data.ballOn, data.ownerUserID
            ])
        let rowID = Int(db.lastInsertedRowID)
        try db.execute(sql: """
            INSERT INTO track_history (track_id, event, field, old_value, new_value)
            VALUES (?, 'created', 'source', '', 'from_digest')
            """, arguments: [rowID])
        return rowID
    }
}

struct TrackInsertData {
    var channelID: String
    var assigneeUserID: String
    var assigneeRaw: String = ""
    var text: String
    var context: String = ""
    var sourceMessageTS: String = ""
    var sourceChannelName: String = ""
    var priority: String = "medium"
    var dueDate: Double?
    var periodFrom: Double = 0
    var periodTo: Double = 0
    var model: String = ""
    var inputTokens: Int = 0
    var outputTokens: Int = 0
    var costUSD: Double = 0
    var participants: String = "[]"
    var sourceRefs: String = "[]"
    var requesterName: String = ""
    var requesterUserID: String = ""
    var category: String = ""
    var blocking: String = ""
    var tags: String = "[]"
    var decisionSummary: String = ""
    var decisionOptions: String = "[]"
    var relatedDigestIDs: String = "[]"
    var subItems: String = "[]"
    var ownership: String = "mine"
    var ballOn: String = ""
    var ownerUserID: String = ""
}
