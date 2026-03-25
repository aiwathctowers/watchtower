import Foundation
import GRDB

enum TrackQueries {

    // MARK: - Fetch

    static func fetchAll(
        _ db: Database,
        priority: String? = nil,
        hasUpdates: Bool? = nil, // swiftlint:disable:this discouraged_optional_boolean
        channelID: String? = nil,
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

    // MARK: - Mark read

    /// Mark a track as read: set read_at=now, has_updates=0, and cascade-mark linked digests.
    static func markRead(_ db: Database, id: Int) throws {
        try db.execute(sql: """
            UPDATE tracks SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), has_updates = 0
            WHERE id = ?
            """, arguments: [id])

        // Cascade: mark linked digests read via source_refs JSON
        let json = try String.fetchOne(
            db, sql: "SELECT source_refs FROM tracks WHERE id = ?", arguments: [id]
        )
        guard let json, !json.isEmpty, json != "[]",
              let data = json.data(using: .utf8),
              let refs = try? JSONDecoder().decode([TrackSourceRef].self, from: data)
        else { return }
        let digestIDs = Set(refs.map(\.digestID).filter { $0 > 0 })
        for digestID in digestIDs {
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

    // MARK: - Workspace helper

    static func fetchCurrentUserID(_ db: Database) throws -> String? {
        try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
    }
}
