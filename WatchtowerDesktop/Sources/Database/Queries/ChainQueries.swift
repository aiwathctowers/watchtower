import Foundation
import GRDB

enum ChainQueries {
    /// Fetch top-level chains (no parent), ordered by last_seen DESC.
    static func fetchAll(
        _ db: Database,
        status: String? = nil,
        topLevelOnly: Bool = false,
        limit: Int = 100
    ) throws -> [Chain] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let status {
            conditions.append("status = ?")
            args.append(status)
        }
        if topLevelOnly {
            conditions.append("(parent_id IS NULL OR parent_id = 0)")
        }

        var sql = "SELECT * FROM chains"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += " ORDER BY last_seen DESC LIMIT ?"
        args.append(limit)

        return try Chain.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    /// Fetch a single chain by ID.
    static func fetchByID(_ db: Database, id: Int) throws -> Chain? {
        try Chain.fetchOne(db, sql: "SELECT * FROM chains WHERE id = ?", arguments: [id])
    }

    /// Fetch child chains of a parent.
    static func fetchChildren(_ db: Database, parentID: Int) throws -> [Chain] {
        try Chain.fetchAll(db, sql: """
            SELECT * FROM chains WHERE parent_id = ?
            ORDER BY last_seen DESC
            """, arguments: [parentID])
    }

    /// Fetch all refs for a chain, ordered by timestamp.
    static func fetchRefs(_ db: Database, chainID: Int) throws -> [ChainRef] {
        try ChainRef.fetchAll(db, sql: """
            SELECT * FROM chain_refs WHERE chain_id = ?
            ORDER BY timestamp ASC
            """, arguments: [chainID])
    }

    /// Count of active chains.
    static func fetchActiveCount(_ db: Database) throws -> Int {
        try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM chains WHERE status = 'active'") ?? 0
    }

    /// Count of unread active chains.
    static func fetchUnreadCount(_ db: Database) throws -> Int {
        try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM chains WHERE status = 'active' AND read_at IS NULL") ?? 0
    }

    /// Update chain status (e.g., archive).
    static func updateStatus(_ db: Database, id: Int, status: String) throws {
        try db.execute(sql: """
            UPDATE chains SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
            WHERE id = ?
            """, arguments: [status, id])
    }

    /// Mark a chain as read.
    static func markRead(_ db: Database, id: Int) throws {
        try db.execute(sql: """
            UPDATE chains SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
            WHERE id = ? AND read_at IS NULL
            """, arguments: [id])
    }

    /// Mark multiple chains as read.
    static func markReadBatch(_ db: Database, ids: Set<Int>) throws {
        guard !ids.isEmpty else { return }
        let placeholders = ids.map { _ in "?" }.joined(separator: ",")
        let args: [any DatabaseValueConvertible] = ids.map { $0 as any DatabaseValueConvertible }
        try db.execute(sql: """
            UPDATE chains SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
            WHERE id IN (\(placeholders)) AND read_at IS NULL
            """, arguments: StatementArguments(args))
    }

    /// Fetch chains that contain a specific digest's decisions or the digest itself.
    static func fetchChainsForDigest(_ db: Database, digestID: Int) throws -> [Chain] {
        try Chain.fetchAll(db, sql: """
            SELECT DISTINCT c.* FROM chains c
            JOIN chain_refs cr ON cr.chain_id = c.id
            WHERE cr.digest_id = ?
            ORDER BY c.last_seen DESC
            """, arguments: [digestID])
    }

    /// Fetch chains that contain a specific track.
    static func fetchChainsForTrack(_ db: Database, trackID: Int) throws -> [Chain] {
        try Chain.fetchAll(db, sql: """
            SELECT DISTINCT c.* FROM chains c
            JOIN chain_refs cr ON cr.chain_id = c.id
            WHERE cr.track_id = ? AND cr.ref_type = 'track'
            ORDER BY c.last_seen DESC
            """, arguments: [trackID])
    }
}
