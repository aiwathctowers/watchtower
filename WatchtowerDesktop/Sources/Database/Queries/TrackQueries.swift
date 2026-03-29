import Foundation
import GRDB

enum TrackQueries {

    // MARK: - Fetch

    static func fetchAll(
        _ db: Database,
        priority: String? = nil,
        hasUpdates: Bool? = nil, // swiftlint:disable:this discouraged_optional_boolean
        channelID: String? = nil,
        ownership: String? = nil,
        limit: Int = 200
    ) throws -> [Track] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let priority {
            conditions.append("priority = ?")
            args.append(priority)
        }
        if let hasUpdates {
            conditions.append("has_updates = ?")
            args.append(hasUpdates ? 1 : 0)
        }
        if let channelID {
            conditions.append("channel_ids LIKE ?")
            args.append("%\(channelID)%")
        }
        if let ownership {
            conditions.append("ownership = ?")
            args.append(ownership)
        }

        var sql = "SELECT * FROM tracks"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += " ORDER BY has_updates DESC, updated_at DESC"
        sql += " LIMIT ?"
        args.append(limit)

        return try Track.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchUpdatedTracks(_ db: Database) throws -> [Track] {
        try Track.fetchAll(
            db,
            sql: "SELECT * FROM tracks WHERE has_updates = 1 ORDER BY updated_at DESC"
        )
    }

    static func fetchByID(_ db: Database, id: Int) throws -> Track? {
        try Track.fetchOne(db, sql: "SELECT * FROM tracks WHERE id = ?", arguments: [id])
    }

    static func fetchByIDs(_ db: Database, ids: [Int]) throws -> [Track] {
        guard !ids.isEmpty else { return [] }
        let placeholders = ids.map { _ in "?" }.joined(separator: ",")
        return try Track.fetchAll(
            db,
            sql: "SELECT * FROM tracks WHERE id IN (\(placeholders))",
            arguments: StatementArguments(ids)
        )
    }

    // MARK: - Counts

    static func fetchCounts(_ db: Database) throws -> (total: Int, updated: Int) {
        let total = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tracks") ?? 0
        let updated = try Int.fetchOne(
            db, sql: "SELECT COUNT(*) FROM tracks WHERE has_updates = 1"
        ) ?? 0
        return (total, updated)
    }

    static func fetchOwnershipCounts(_ db: Database) throws -> [String: Int] {
        var result: [String: Int] = [:]
        let rows = try Row.fetchAll(
            db, sql: "SELECT ownership, COUNT(*) as cnt FROM tracks GROUP BY ownership"
        )
        for row in rows {
            let key: String = row["ownership"]
            let count: Int = row["cnt"]
            result[key] = count
        }
        return result
    }

    // MARK: - Mark read

    /// Mark a track as read: set read_at=now, has_updates=0, and cascade-mark related digests.
    static func markRead(_ db: Database, id: Int) throws {
        try db.execute(sql: """
            UPDATE tracks SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), has_updates = 0
            WHERE id = ?
            """, arguments: [id])

        // Cascade: mark related digests read via related_digest_ids JSON
        let json = try String.fetchOne(
            db, sql: "SELECT related_digest_ids FROM tracks WHERE id = ?", arguments: [id]
        )
        guard let json, !json.isEmpty, json != "[]",
              let data = json.data(using: .utf8),
              let digestIDs = try? JSONDecoder().decode([Int].self, from: data)
        else { return }
        for digestID in digestIDs where digestID > 0 {
            try DigestQueries.markDigestRead(db, id: digestID)
            try DigestQueries.markAllDecisionsRead(db, digestID: digestID)
        }
    }

    // MARK: - Priority

    static func updatePriority(_ db: Database, id: Int, priority: String) throws {
        try db.execute(
            sql: "UPDATE tracks SET priority = ? WHERE id = ?",
            arguments: [priority, id]
        )
        try FeedbackQueries.addFeedback(
            db,
            entityType: "track",
            entityID: "\(id)",
            rating: -1,
            comment: "priority corrected to \(priority)"
        )
    }

    // MARK: - Ownership

    static func updateOwnership(_ db: Database, id: Int, ownership: String) throws {
        try db.execute(
            sql: "UPDATE tracks SET ownership = ? WHERE id = ?",
            arguments: [ownership, id]
        )
    }

    // MARK: - Sub-items

    static func updateSubItems(_ db: Database, id: Int, subItems: [TrackSubItem]) throws {
        let encoder = JSONEncoder()
        let data = try encoder.encode(subItems)
        let json = String(data: data, encoding: .utf8) ?? "[]"
        try db.execute(
            sql: "UPDATE tracks SET sub_items = ? WHERE id = ?",
            arguments: [json, id]
        )
    }

    // MARK: - Workspace helper

    static func fetchCurrentUserID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
    }
}
