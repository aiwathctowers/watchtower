import GRDB

enum ImportanceCorrectionQueries {

    // MARK: - Write

    /// Save or update an importance correction for a decision.
    static func upsert(
        _ db: Database,
        digestID: Int,
        decisionIdx: Int,
        decisionText: String,
        originalImportance: String,
        newImportance: String
    ) throws {
        guard try db.tableExists("decision_importance_corrections") else { return }
        try db.execute(
            sql: """
                INSERT OR REPLACE INTO decision_importance_corrections
                (digest_id, decision_idx, decision_text, original_importance, new_importance)
                VALUES (?, ?, ?, ?, ?)
                """,
            arguments: [digestID, decisionIdx, decisionText, originalImportance, newImportance]
        )
    }

    /// Delete a correction (user reverted to original importance).
    static func delete(_ db: Database, digestID: Int, decisionIdx: Int) throws {
        guard try db.tableExists("decision_importance_corrections") else { return }
        try db.execute(
            sql: "DELETE FROM decision_importance_corrections WHERE digest_id = ? AND decision_idx = ?",
            arguments: [digestID, decisionIdx]
        )
    }

    // MARK: - Read

    /// Get the corrected importance for a specific decision, if any.
    static func correctedImportance(
        _ db: Database,
        digestID: Int,
        decisionIdx: Int
    ) throws -> String? {
        guard try db.tableExists("decision_importance_corrections") else { return nil }
        return try String.fetchOne(db, sql: """
            SELECT new_importance FROM decision_importance_corrections
            WHERE digest_id = ? AND decision_idx = ?
            """, arguments: [digestID, decisionIdx])
    }

    /// Get all corrected importances as a map of "digestID:decisionIdx" -> newImportance.
    static func allCorrections(_ db: Database) throws -> [String: String] {
        guard try db.tableExists("decision_importance_corrections") else { return [:] }
        let rows = try Row.fetchAll(db, sql: """
            SELECT digest_id, decision_idx, new_importance
            FROM decision_importance_corrections
            """)
        var result: [String: String] = [:]
        for row in rows {
            let key = "\(row["digest_id"] as Int):\(row["decision_idx"] as Int)"
            result[key] = row["new_importance"]
        }
        return result
    }
}
