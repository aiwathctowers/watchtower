import Foundation
import GRDB

struct InboxLearnedRulesQueries {
    let dbPool: DatabasePool

    // MARK: - Read

    func listAll() throws -> [InboxLearnedRule] {
        try dbPool.read { db in
            try InboxLearnedRule.fetchAll(
                db,
                sql: "SELECT * FROM inbox_learned_rules ORDER BY ABS(weight) DESC, last_updated DESC"
            )
        }
    }

    func observeAll() -> ValueObservation<ValueReducers.Fetch<[InboxLearnedRule]>> {
        ValueObservation.tracking { db in
            try InboxLearnedRule.fetchAll(
                db,
                sql: "SELECT * FROM inbox_learned_rules ORDER BY ABS(weight) DESC, last_updated DESC"
            )
        }
    }

    // MARK: - Write

    func upsertManual(ruleType: String, scopeKey: String, weight: Double) throws {
        let now = ISO8601DateFormatter().string(from: Date())
        try dbPool.write { db in
            try db.execute(
                sql: """
                    INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                    VALUES (?, ?, ?, 'user_rule', 0, ?)
                    ON CONFLICT(rule_type, scope_key) DO UPDATE SET
                        weight = excluded.weight,
                        source = 'user_rule',
                        last_updated = excluded.last_updated
                    """,
                arguments: [ruleType, scopeKey, weight, now]
            )
        }
    }

    func delete(ruleType: String, scopeKey: String) throws {
        try dbPool.write { db in
            try db.execute(
                sql: "DELETE FROM inbox_learned_rules WHERE rule_type = ? AND scope_key = ?",
                arguments: [ruleType, scopeKey]
            )
        }
    }
}
