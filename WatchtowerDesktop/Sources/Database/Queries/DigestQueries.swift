import GRDB

enum DigestQueries {
    static func fetchAll(
        _ db: Database,
        type: String? = nil,
        channelID: String? = nil,
        limit: Int = 50
    ) throws -> [Digest] {
        var conditions: [String] = []
        var args: [any DatabaseValueConvertible] = []

        if let type {
            conditions.append("type = ?")
            args.append(type)
        }
        if let channelID {
            conditions.append("channel_id = ?")
            args.append(channelID)
        }

        var sql = "SELECT * FROM digests"
        if !conditions.isEmpty {
            sql += " WHERE " + conditions.joined(separator: " AND ")
        }
        sql += " ORDER BY created_at DESC LIMIT ?"
        args.append(limit)

        return try Digest.fetchAll(db, sql: sql, arguments: StatementArguments(args))
    }

    static func fetchByID(_ db: Database, id: Int) throws -> Digest? {
        try Digest.fetchOne(db, sql: "SELECT * FROM digests WHERE id = ?", arguments: [id])
    }

    static func fetchLatest(_ db: Database, type: String) throws -> Digest? {
        try Digest.fetchOne(db, sql: """
            SELECT * FROM digests WHERE type = ?
            ORDER BY created_at DESC LIMIT 1
            """, arguments: [type])
    }

    static func fetchWithDecisions(_ db: Database, limit: Int = 50) throws -> [Digest] {
        try Digest.fetchAll(db, sql: """
            SELECT * FROM digests
            WHERE decisions != '[]' AND decisions IS NOT NULL
            ORDER BY created_at DESC LIMIT ?
            """, arguments: [limit])
    }

    static func fetchNewSince(_ db: Database, afterID: Int) throws -> [Digest] {
        try Digest.fetchAll(db, sql: """
            SELECT * FROM digests
            WHERE id > ? AND decisions != '[]' AND decisions IS NOT NULL
            ORDER BY id ASC
            """, arguments: [afterID])
    }

    static func maxID(_ db: Database) throws -> Int {
        try Int.fetchOne(db, sql: "SELECT MAX(id) FROM digests") ?? 0
    }

    // MARK: - Read tracking

    static func markDigestRead(_ db: Database, id: Int) throws {
        let columns = try db.columns(in: "digests")
        guard columns.contains(where: { $0.name == "read_at" }) else { return }
        try db.execute(
            sql: "UPDATE digests SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ? AND read_at IS NULL",
            arguments: [id]
        )
    }

    static func markDecisionRead(_ db: Database, digestID: Int, decisionIdx: Int) throws {
        guard try db.tableExists("decision_reads") else { return }
        try db.execute(
            sql: """
                INSERT INTO decision_reads (digest_id, decision_idx)
                VALUES (?, ?)
                ON CONFLICT DO NOTHING
                """,
            arguments: [digestID, decisionIdx]
        )
    }

    static func readDecisionIndices(_ db: Database, digestIDs: [Int]) throws -> [Int: Set<Int>] {
        guard !digestIDs.isEmpty else { return [:] }
        guard try db.tableExists("decision_reads") else { return [:] }
        let placeholders = digestIDs.map { _ in "?" }.joined(separator: ",")
        let rows = try Row.fetchAll(
            db,
            sql: "SELECT digest_id, decision_idx FROM decision_reads WHERE digest_id IN (\(placeholders))",
            arguments: StatementArguments(digestIDs)
        )
        var result: [Int: Set<Int>] = [:]
        for row in rows {
            let digestID: Int = row["digest_id"]
            let idx: Int = row["decision_idx"]
            result[digestID, default: []].insert(idx)
        }
        return result
    }

    static func unreadDigestCount(_ db: Database) throws -> Int {
        // read_at column may not exist on older schema — treat all as read
        let columns = try db.columns(in: "digests")
        guard columns.contains(where: { $0.name == "read_at" }) else { return 0 }
        return try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM digests WHERE read_at IS NULL") ?? 0
    }

    static func unreadDecisionCount(_ db: Database, totalDecisionCount: Int) throws -> Int {
        guard try db.tableExists("decision_reads") else { return 0 }
        let readCount = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM decision_reads") ?? 0
        return max(0, totalDecisionCount - readCount)
    }
}
