import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - InboxViewModel Pinned/Feed Split Tests

final class InboxViewModelPinnedFeedTests: XCTestCase {

    // MARK: - Helpers

    private func makeDB() throws -> (DatabaseManager, String) {
        try TestDatabase.createDatabaseManager()
    }

    private func insertInboxItem(
        _ pool: DatabasePool,
        messageTS: String,
        status: String = "pending",
        priority: String = "medium",
        pinned: Bool = false
    ) throws {
        try pool.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, arguments: [
                    "C1", messageTS, "U1", "mention",
                    status, priority, pinned ? 1 : 0,
                    "Snippet \(messageTS)",
                    "2026-04-23T10:00:00Z",
                    "2026-04-23T10:00:00Z"
                ])
        }
    }

    // MARK: - Pinned / Feed Split

    @MainActor
    func testViewModelSplitsPinnedAndFeed() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        // One pinned item
        try insertInboxItem(dbManager.dbPool, messageTS: "1.0", priority: "high", pinned: true)
        // Two feed items (not pinned)
        try insertInboxItem(dbManager.dbPool, messageTS: "2.0", priority: "medium", pinned: false)
        try insertInboxItem(dbManager.dbPool, messageTS: "3.0", priority: "low", pinned: false)

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.pinnedItems.count, 1)
        XCTAssertEqual(vm.feedItems.count, 2)
    }

    @MainActor
    func testPinnedItemsAreActuallyPinned() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0", pinned: true)
        try insertInboxItem(dbManager.dbPool, messageTS: "2.0", pinned: false)

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.pinnedItems.allSatisfy(\.pinned))
        XCTAssertTrue(vm.feedItems.allSatisfy { !$0.pinned })
    }

    @MainActor
    func testEmptyDBYieldsEmptyLists() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.pinnedItems.isEmpty)
        XCTAssertTrue(vm.feedItems.isEmpty)
        XCTAssertFalse(vm.hasHighPriorityPinned)
    }

    // MARK: - hasHighPriorityPinned

    @MainActor
    func testSidebarBadgeReflectsHighPriorityPinned() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0", priority: "high", pinned: true)

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.hasHighPriorityPinned)
    }

    @MainActor
    func testHasHighPriorityPinnedFalseWhenOnlyMedium() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0", priority: "medium", pinned: true)

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertFalse(vm.hasHighPriorityPinned)
    }

    // MARK: - loadMore (pagination)

    @MainActor
    func testLoadMoreAppendsToFeed() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        // Insert 5 feed items
        for i in 1...5 {
            try insertInboxItem(dbManager.dbPool, messageTS: "\(i).0", pinned: false)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.feedPageSize = 3
        vm.load()

        XCTAssertEqual(vm.feedItems.count, 3)

        vm.loadMore()

        XCTAssertEqual(vm.feedItems.count, 5)
    }

    @MainActor
    func testLoadMoreDoesNotDuplicateItems() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        for i in 1...4 {
            try insertInboxItem(dbManager.dbPool, messageTS: "\(i).0", pinned: false)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.feedPageSize = 2
        vm.load()
        vm.loadMore()

        let ids = vm.feedItems.map(\.id)
        XCTAssertEqual(ids.count, Set(ids).count, "No duplicates expected")
    }

    // MARK: - markSeen

    @MainActor
    func testMarkAsSeenOnScroll() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0")

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        guard let item = vm.feedItems.first else {
            XCTFail("Expected feed item")
            return
        }
        XCTAssertTrue(item.readAt.isEmpty, "Item should start unread")

        vm.markSeen(item)

        let updated = try dbManager.dbPool.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [item.id])
        }
        XCTAssertFalse(try XCTUnwrap(updated).readAt.isEmpty, "read_at should be set after markSeen")
    }

    @MainActor
    func testMarkSeenIsNoOpIfAlreadyRead() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        let originalReadAt = "2026-04-20T08:00:00Z"
        try dbManager.dbPool.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, read_at, created_at, updated_at)
                VALUES ('C1','5.0','U1','mention','pending','medium',0,?,?,?)
                """, arguments: [originalReadAt, "2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }

        let item = try dbManager.dbPool.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        }!

        let vm = InboxViewModel(dbManager: dbManager)
        vm.markSeen(item)

        let updated = try dbManager.dbPool.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [item.id])
        }
        XCTAssertEqual(try XCTUnwrap(updated).readAt, originalReadAt, "read_at should not change for already-read item")
    }

    // MARK: - submitFeedback

    @MainActor
    func testSubmitFeedbackUpdatesDB() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0")

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        guard let item = vm.feedItems.first else {
            XCTFail("Expected feed item")
            return
        }

        vm.submitFeedback(item, rating: -1, reason: "never_show")

        // Check that inbox_learned_rules got a source_mute row
        let ruleCount = try dbManager.dbPool.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_learned_rules WHERE rule_type = 'source_mute'")
        }
        XCTAssertEqual(ruleCount, 1, "Expected one source_mute rule after never_show feedback")
    }

    @MainActor
    func testSubmitFeedbackTriggersReload() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0")

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        let countBefore = vm.feedItems.count
        guard let item = vm.feedItems.first else {
            XCTFail("Expected feed item"); return
        }

        vm.submitFeedback(item, rating: 1, reason: "useful")

        // After reload the count should stay consistent (item still pending, feedback doesn't change status)
        XCTAssertEqual(vm.feedItems.count, countBefore)
    }

    // MARK: - Backward-compat: allItems still populated

    @MainActor
    func testLegacyAllItemsStillPopulated() throws {
        let (dbManager, path) = try makeDB()
        defer { TestDatabase.cleanup(path: path) }

        try insertInboxItem(dbManager.dbPool, messageTS: "1.0")
        try insertInboxItem(dbManager.dbPool, messageTS: "2.0", pinned: true)

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertFalse(vm.allItems.isEmpty, "allItems backward-compat property should still be populated")
    }
}
