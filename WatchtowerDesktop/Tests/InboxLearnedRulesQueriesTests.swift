import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - InboxLearnedRulesQueries Tests

final class InboxLearnedRulesQueriesTests: XCTestCase {

    private let nowISO = "2026-04-23T10:00:00Z"

    private func makePool() throws -> DatabasePool {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        return manager.dbPool
    }

    // MARK: - listAll

    func test_INBOX_05_list_rules_ordered_by_weight() throws {
        // BEHAVIOR INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab lists rules ordered so the most impactful are visible first.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        try pool.write { db in
            try db.execute(
                sql: """
                    INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                    VALUES ('source_mute', 'a', -0.9, 'user_rule', 0, ?)
                    """,
                arguments: [nowISO]
            )
            try db.execute(
                sql: """
                    INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                    VALUES ('source_boost', 'b', 0.5, 'implicit', 5, ?)
                    """,
                arguments: [nowISO]
            )
        }
        let out = try q.listAll()
        XCTAssertEqual(out.count, 2)
        XCTAssertEqual(out[0].scopeKey, "a")  // ABS(-0.9) = 0.9 > ABS(0.5) = 0.5
        XCTAssertEqual(out[1].scopeKey, "b")
    }

    func testListAllEmptyWhenNoRules() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        let out = try q.listAll()
        XCTAssertTrue(out.isEmpty)
    }

    // MARK: - upsertManual

    func testUpsertManualCreatesNewRule() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.9)
        let all = try q.listAll()
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all[0].ruleType, "source_mute")
        XCTAssertEqual(all[0].scopeKey, "sender:U1")
        XCTAssertEqual(all[0].weight, -0.9)
        XCTAssertEqual(all[0].source, "user_rule")
    }

    func testUpsertManualUpdatesExistingRule() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.5)
        try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.9)
        let all = try q.listAll()
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all[0].weight, -0.9)
        XCTAssertEqual(all[0].source, "user_rule")
    }

    func test_INBOX_06_manual_rule_overrides_implicit() throws {
        // BEHAVIOR INBOX-06 — see docs/inventory/inbox-pulse.md
        // Manual rule upsert overrides an existing implicit rule on the same scope.
        // Do not weaken or remove without explicit owner approval.
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        // Insert an implicit rule first
        try pool.write { db in
            try db.execute(
                sql: """
                    INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                    VALUES ('source_mute', 'sender:U1', -0.3, 'implicit', 3, ?)
                    """,
                arguments: [nowISO]
            )
        }
        // Now upsert manual — should override source to 'user_rule'
        try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.9)
        let all = try q.listAll()
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all[0].source, "user_rule")
        XCTAssertEqual(all[0].weight, -0.9)
    }

    // MARK: - delete

    func testDeleteRule() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        try q.upsertManual(ruleType: "source_mute", scopeKey: "sender:U1", weight: -0.9)
        XCTAssertEqual(try q.listAll().count, 1)
        try q.delete(ruleType: "source_mute", scopeKey: "sender:U1")
        XCTAssertEqual(try q.listAll().count, 0)
    }

    func testDeleteNonExistentRuleIsNoop() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        XCTAssertNoThrow(try q.delete(ruleType: "source_mute", scopeKey: "sender:X"))
        XCTAssertEqual(try q.listAll().count, 0)
    }

    // MARK: - observeAll

    func testObserveAllReturnsValueObservation() throws {
        let pool = try makePool()
        let q = InboxLearnedRulesQueries(dbPool: pool)
        try pool.write { db in
            try db.execute(
                sql: """
                    INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                    VALUES ('source_mute', 'sender:U1', -0.9, 'user_rule', 0, ?)
                    """,
                arguments: [nowISO]
            )
        }
        var receivedRules: [InboxLearnedRule] = []
        let exp = expectation(description: "observation fires")
        let obs = q.observeAll()
        let cancellable = obs.start(
            in: pool,
            scheduling: .immediate
        ) { error in
            XCTFail("Observation error: \(error)")
        } onChange: { rules in
            receivedRules = rules
            exp.fulfill()
        }
        wait(for: [exp], timeout: 2.0)
        XCTAssertEqual(receivedRules.count, 1)
        XCTAssertEqual(receivedRules[0].scopeKey, "sender:U1")
        _ = cancellable
    }
}

// MARK: - InboxFeedbackQueries Tests

final class InboxFeedbackQueriesTests: XCTestCase {

    private func makePool() throws -> (DatabasePool, InboxItem) {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        let pool = manager.dbPool
        // Insert a prerequisite inbox item
        try pool.write { db in
            try TestDatabase.insertInboxItem(
                db,
                channelID: "C001",
                messageTS: "1700000000.000100",
                senderUserID: "U002",
                triggerType: "mention"
            )
        }
        let item = try pool.read { db in
            try XCTUnwrap(InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1"))
        }
        return (pool, item)
    }

    // MARK: - record — basic row insertion

    func testRecordInsertsFeedbackRow() throws {
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: 1, reason: "")
        let rows = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_feedback")
        }
        XCTAssertEqual(rows.count, 1)
        XCTAssertEqual(rows[0]["rating"] as Int, 1)
        XCTAssertEqual(rows[0]["inbox_item_id"] as Int, item.id)
    }

    // MARK: - record — rule derivation: never_show

    func test_INBOX_04_record_never_show_creates_user_rule() throws {
        // BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, never_show) creates source='user_rule' weight=-1.0 instantly.
        // Do not weaken or remove without explicit owner approval.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "never_show")
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0]["rule_type"] as String, "source_mute")
        XCTAssertEqual(rules[0]["scope_key"] as String, "sender:U002")
        XCTAssertEqual(rules[0]["weight"] as Double, -1.0)
        XCTAssertEqual(rules[0]["source"] as String, "user_rule")
    }

    // MARK: - record — rule derivation: source_noise

    func test_INBOX_04_record_source_noise_does_not_create_rule() throws {
        // BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, source_noise) is audit-only; no immediate rule write.
        // Do not weaken or remove without explicit owner approval.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "source_noise")
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertTrue(rules.isEmpty)
    }

    // MARK: - record — rule derivation: wrong_class

    func test_INBOX_04_record_wrong_class_flips_item_no_rule() throws {
        // BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, wrong_class) flips item_class to ambient but writes no learned rule.
        // Do not weaken or remove without explicit owner approval.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "wrong_class")

        // item_class should be updated to 'ambient'
        let updatedItem = try pool.read { db in
            try XCTUnwrap(InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [item.id]))
        }
        XCTAssertEqual(updatedItem.itemClass, .ambient)

        // No rule should be written
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertTrue(rules.isEmpty)
    }

    // MARK: - record — rule derivation: wrong_priority

    func test_INBOX_04_record_wrong_priority_does_not_create_rule() throws {
        // BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
        // (-1, wrong_priority) is audit-only; no immediate rule write.
        // Do not weaken or remove without explicit owner approval.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "wrong_priority")
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertTrue(rules.isEmpty)
    }

    // MARK: - record — rule derivation: positive rating

    func test_INBOX_04_record_positive_does_not_create_rule() throws {
        // BEHAVIOR INBOX-04 — see docs/inventory/inbox-pulse.md
        // (1, "") positive rating is audit-only; no immediate rule write.
        // Do not weaken or remove without explicit owner approval.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: 1, reason: "")
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertTrue(rules.isEmpty)
    }

    // MARK: - record — evidence_count increments on repeat never_show

    func testRecordEvidenceCountIncrementsOnRepeat() throws {
        // never_show still upserts, so evidence_count should increment on repeated calls.
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "never_show")
        try q.record(item: item, rating: -1, reason: "never_show")
        let rules = try pool.read { db in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules")
        }
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0]["evidence_count"] as Int, 2)
    }

    // MARK: - record — atomic transaction (feedback + rule in one write)

    func testRecordIsAtomicFeedbackAndRule() throws {
        let (pool, item) = try makePool()
        let q = InboxFeedbackQueries(dbPool: pool)
        try q.record(item: item, rating: -1, reason: "never_show")

        let feedbackCount = try pool.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_feedback") ?? 0
        }
        let ruleCount = try pool.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_learned_rules") ?? 0
        }
        XCTAssertEqual(feedbackCount, 1)
        XCTAssertEqual(ruleCount, 1)
    }
}
