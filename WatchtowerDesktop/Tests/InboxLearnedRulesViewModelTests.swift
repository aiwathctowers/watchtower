import XCTest
import GRDB
@testable import WatchtowerDesktop

final class InboxLearnedRulesViewModelTests: XCTestCase {
    private var pool: DatabasePool!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        do {
            dbPath = NSTemporaryDirectory() + "learned_rules_vm_\(UUID().uuidString).db"
            pool = try DatabasePool(path: dbPath)
            try pool.write { db in
                try db.execute(sql: TestDatabase.schema)
            }
        } catch {
            XCTFail("setUp failed: \(error)")
        }
    }

    override func tearDown() {
        pool = nil
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    // MARK: - testListsSeparatesMutesAndBoosts

    @MainActor
    func testListsSeparatesMutesAndBoosts() async throws {
        try await pool.write { db in
            try TestDatabase.insertLearnedRule(db, scopeKey: "sender:U1", weight: -0.5, source: "implicit", ruleType: "source_mute")
            try TestDatabase.insertLearnedRule(db, scopeKey: "sender:U2", weight: 0.8, source: "explicit_feedback", ruleType: "source_boost")
            try TestDatabase.insertLearnedRule(db, scopeKey: "channel:C1", weight: -0.7, source: "user_rule", ruleType: "source_mute")
        }

        let vm = InboxLearnedRulesViewModel(db: pool)
        await vm.load()

        XCTAssertEqual(vm.mutes.count, 2, "Expected 2 mutes (weight < 0)")
        XCTAssertTrue(vm.mutes.allSatisfy { $0.weight < 0 }, "All mutes should have negative weight")
        XCTAssertEqual(vm.boosts.count, 1, "Expected 1 boost (weight > 0)")
        XCTAssertTrue(vm.boosts.allSatisfy { $0.weight > 0 }, "All boosts should have positive weight")
    }

    // MARK: - testAddManualRule

    @MainActor
    func test_INBOX_05_add_manual_rule() async throws {
        // BEHAVIOR INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab adds a manual rule that surfaces immediately.
        // Do not weaken or remove without explicit owner approval.
        let vm = InboxLearnedRulesViewModel(db: pool)
        await vm.addRule(ruleType: "source_mute", scopeKey: "sender:U9", weight: -0.5)

        // Verify DB row has source='user_rule'
        let rows = try await pool.read { db -> [Row] in
            try Row.fetchAll(db, sql: "SELECT * FROM inbox_learned_rules WHERE scope_key = 'sender:U9'")
        }
        XCTAssertEqual(rows.count, 1)
        let sourceValue: String = rows[0]["source"]
        XCTAssertEqual(sourceValue, "user_rule")
        let weightValue: Double = rows[0]["weight"]
        XCTAssertEqual(weightValue, -0.5)

        // ViewModel should reflect the new mute
        XCTAssertEqual(vm.mutes.count, 1)
        XCTAssertEqual(vm.mutes[0].scopeKey, "sender:U9")
    }

    // MARK: - testRemoveRule

    @MainActor
    func test_INBOX_05_remove_rule() async throws {
        // BEHAVIOR INBOX-05 — see docs/inventory/inbox-pulse.md
        // Learned tab removes a rule, persisted to DB.
        // Do not weaken or remove without explicit owner approval.
        try await pool.write { db in
            try TestDatabase.insertLearnedRule(db, scopeKey: "sender:U1", weight: -0.5, source: "implicit", ruleType: "source_mute")
        }

        let vm = InboxLearnedRulesViewModel(db: pool)
        await vm.load()
        XCTAssertEqual(vm.mutes.count, 1)

        let rule = vm.mutes[0]
        await vm.remove(rule)

        // Row should be deleted
        let count = try await pool.read { db -> Int in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_learned_rules") ?? 0
        }
        XCTAssertEqual(count, 0)
        XCTAssertTrue(vm.mutes.isEmpty)
    }

    // MARK: - testEmptyInitialState

    @MainActor
    func testEmptyInitialState() async {
        let vm = InboxLearnedRulesViewModel(db: pool)
        XCTAssertTrue(vm.mutes.isEmpty)
        XCTAssertTrue(vm.boosts.isEmpty)
    }

    // MARK: - testLoadReflectsUpdatedDB

    @MainActor
    func testLoadReflectsUpdatedDB() async throws {
        let vm = InboxLearnedRulesViewModel(db: pool)
        await vm.load()
        XCTAssertTrue(vm.mutes.isEmpty)

        try await pool.write { db in
            try TestDatabase.insertLearnedRule(db, scopeKey: "channel:C99", weight: -0.3, source: "implicit", ruleType: "source_mute")
        }

        await vm.load()
        XCTAssertEqual(vm.mutes.count, 1)
        XCTAssertEqual(vm.mutes[0].scopeKey, "channel:C99")
    }
}
