import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - InboxLearnedRule Model Tests

final class InboxLearnedRuleTests: XCTestCase {

    func testInboxLearnedRuleFetches() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                VALUES ('source_mute','sender:U1',-0.7,'implicit',10,'2026-04-23T10:00:00Z')
            """)
        }
        let rules = try db.read { db in try InboxLearnedRule.fetchAll(db) }
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0].scopeKey, "sender:U1")
        XCTAssertEqual(rules[0].weight, -0.7)
    }

    func testInboxLearnedRuleFields() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertLearnedRule($0) }
        let rule = try XCTUnwrap(db.read {
            try InboxLearnedRule.fetchOne($0, sql: "SELECT * FROM inbox_learned_rules LIMIT 1")
        })
        XCTAssertEqual(rule.ruleType, "source_mute")
        XCTAssertEqual(rule.scopeKey, "sender:U1")
        XCTAssertEqual(rule.weight, -0.5)
        XCTAssertEqual(rule.source, "implicit")
        XCTAssertEqual(rule.evidenceCount, 3)
        XCTAssertEqual(rule.lastUpdated, "2026-04-23T10:00:00Z")
    }

    func testInboxLearnedRuleInsertAndFetch() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            var rule = InboxLearnedRule(
                id: nil,
                ruleType: "source_boost",
                scopeKey: "sender:U2",
                weight: 0.8,
                source: "explicit_feedback",
                evidenceCount: 1,
                lastUpdated: "2026-04-23T12:00:00Z"
            )
            try rule.insert(db)
        }
        let rules = try db.read { try InboxLearnedRule.fetchAll($0) }
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0].ruleType, "source_boost")
        XCTAssertEqual(rules[0].source, "explicit_feedback")
        XCTAssertNotNil(rules[0].id)
    }

    func testInboxLearnedRuleEquatable() throws {
        let r1 = InboxLearnedRule(
            id: 1, ruleType: "source_mute", scopeKey: "sender:U1",
            weight: -0.5, source: "implicit", evidenceCount: 3,
            lastUpdated: "2026-04-23T10:00:00Z"
        )
        let r2 = InboxLearnedRule(
            id: 1, ruleType: "source_mute", scopeKey: "sender:U1",
            weight: -0.5, source: "implicit", evidenceCount: 3,
            lastUpdated: "2026-04-23T10:00:00Z"
        )
        XCTAssertEqual(r1, r2)
    }

    func testInboxLearnedRuleMultipleRows() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertLearnedRule(db, scopeKey: "sender:U1", weight: -0.5)
            try TestDatabase.insertLearnedRule(db, scopeKey: "sender:U2", weight: 0.8, source: "explicit_feedback", ruleType: "source_boost")
        }
        let rules = try db.read { try InboxLearnedRule.fetchAll($0) }
        XCTAssertEqual(rules.count, 2)
    }
}

// MARK: - InboxFeedback Model Tests

final class InboxFeedbackTests: XCTestCase {

    func testInboxFeedbackFields() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertFeedbackRecord($0) }
        let fb = try XCTUnwrap(db.read {
            try InboxFeedback.fetchOne($0, sql: "SELECT * FROM inbox_feedback LIMIT 1")
        })
        XCTAssertEqual(fb.inboxItemId, 1)
        XCTAssertEqual(fb.rating, 1)
        XCTAssertEqual(fb.reason, "useful")
        XCTAssertFalse(fb.createdAt.isEmpty)
    }

    func testInboxFeedbackInsertAndFetch() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            var fb = InboxFeedback(
                id: nil,
                inboxItemId: 42,
                rating: -1,
                reason: "never_show",
                createdAt: "2026-04-23T10:00:00Z"
            )
            try fb.insert(db)
        }
        let all = try db.read { try InboxFeedback.fetchAll($0) }
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all[0].inboxItemId, 42)
        XCTAssertEqual(all[0].rating, -1)
        XCTAssertEqual(all[0].reason, "never_show")
        XCTAssertNotNil(all[0].id)
    }

    func testInboxFeedbackEquatable() throws {
        let f1 = InboxFeedback(id: 1, inboxItemId: 1, rating: 1, reason: "useful", createdAt: "2026-04-23T10:00:00Z")
        let f2 = InboxFeedback(id: 1, inboxItemId: 1, rating: 1, reason: "useful", createdAt: "2026-04-23T10:00:00Z")
        XCTAssertEqual(f1, f2)
    }

    func testInboxFeedbackMultipleForSameItem() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertFeedbackRecord(db, inboxItemId: 5, rating: 1, reason: "useful")
            try TestDatabase.insertFeedbackRecord(db, inboxItemId: 5, rating: -1, reason: "wrong_priority")
        }
        let all = try db.read { try InboxFeedback.fetchAll($0) }
        XCTAssertEqual(all.count, 2)
        XCTAssertTrue(all.allSatisfy { $0.inboxItemId == 5 })
    }
}
