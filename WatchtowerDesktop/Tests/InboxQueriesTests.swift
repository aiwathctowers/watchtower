import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - InboxQueries Extended Method Tests

final class InboxQueriesTests: XCTestCase {

    // MARK: - fetchPinned

    func testFetchPinnedReturnsPinnedPendingItems() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // pinned=1, status=pending, archived_at=NULL → should be returned
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            // pinned=0 → should not be returned
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','pending','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchPinned($0) }
        XCTAssertEqual(items.count, 1)
        XCTAssertTrue(items[0].pinned)
    }

    func testFetchPinnedExcludesResolvedStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // pinned=1 but status=resolved → should not be returned
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','resolved','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchPinned($0) }
        XCTAssertTrue(items.isEmpty)
    }

    func testFetchPinnedExcludesArchivedItems() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // pinned=1 but archived_at set → should not be returned
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, archived_at, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high',1,?,?,?)
            """, arguments: ["2026-04-23T12:00:00Z", "2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchPinned($0) }
        XCTAssertTrue(items.isEmpty)
    }

    func testFetchPinnedOrdersByPriorityThenCreatedAtDesc() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','low',1,'Low',?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','pending','high',1,'High',?,?)
            """, arguments: ["2026-04-23T09:00:00Z", "2026-04-23T09:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchPinned($0) }
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].snippet, "High")
        XCTAssertEqual(items[1].snippet, "Low")
    }

    // MARK: - fetchFeed

    func testFetchFeedReturnsPendingUnpinnedItems() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // pinned=0, status=pending → should be returned
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,'Feed item',?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            // pinned=1 → should not appear in feed
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','pending','high',1,'Pinned item',?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchFeed($0, limit: 50, offset: 0) }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].snippet, "Feed item")
    }

    func testFetchFeedExcludesResolvedDismissedSnoozed() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // pending → included
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            // resolved → excluded
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','resolved','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            // dismissed → excluded
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','3.0','U1','mention','dismissed','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
            // snoozed → excluded
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','4.0','U1','mention','snoozed','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchFeed($0, limit: 50, offset: 0) }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].status, "pending")
    }

    func testFetchFeedExcludesArchivedItems() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, archived_at, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,?,?,?)
            """, arguments: ["2026-04-23T12:00:00Z", "2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchFeed($0, limit: 50, offset: 0) }
        XCTAssertTrue(items.isEmpty)
    }

    func testFetchFeedPagination() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            for i in 1...5 {
                try db.execute(sql: """
                    INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                        status, priority, pinned, snippet, created_at, updated_at)
                    VALUES ('C1',?,      'U1','mention','pending','medium',0,?,?,?)
                """, arguments: ["\(i).0", "Item \(i)",
                                 "2026-04-23T\(String(format: "%02d", i)):00:00Z",
                                 "2026-04-23T\(String(format: "%02d", i)):00:00Z"])
            }
        }
        let page1 = try db.read { try InboxQueries.fetchFeed($0, limit: 2, offset: 0) }
        let page2 = try db.read { try InboxQueries.fetchFeed($0, limit: 2, offset: 2) }
        XCTAssertEqual(page1.count, 2)
        XCTAssertEqual(page2.count, 2)
        // Items should not overlap
        let page1IDs = Set(page1.map(\.id))
        let page2IDs = Set(page2.map(\.id))
        XCTAssertTrue(page1IDs.isDisjoint(with: page2IDs))
    }

    func testFetchFeedOrdersByCreatedAtDesc() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,'Older',?,?)
            """, arguments: ["2026-04-22T10:00:00Z", "2026-04-22T10:00:00Z"])
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, snippet, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','pending','medium',0,'Newer',?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let items = try db.read { try InboxQueries.fetchFeed($0, limit: 10, offset: 0) }
        XCTAssertEqual(items[0].snippet, "Newer")
        XCTAssertEqual(items[1].snippet, "Older")
    }

    // MARK: - hasHighPriorityPinned

    func testHasHighPriorityPinnedReturnsTrueWhenExists() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let result = try db.read { try InboxQueries.hasHighPriorityPinned($0) }
        XCTAssertTrue(result)
    }

    func testHasHighPriorityPinnedReturnsFalseWhenNone() throws {
        let db = try TestDatabase.create()
        // No pinned items
        let result = try db.read { try InboxQueries.hasHighPriorityPinned($0) }
        XCTAssertFalse(result)
    }

    func testHasHighPriorityPinnedReturnsFalseForMediumPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let result = try db.read { try InboxQueries.hasHighPriorityPinned($0) }
        XCTAssertFalse(result)
    }

    func testHasHighPriorityPinnedReturnsFalseWhenNotPending() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // high priority but resolved → false
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','resolved','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let result = try db.read { try InboxQueries.hasHighPriorityPinned($0) }
        XCTAssertFalse(result)
    }

    // MARK: - observePinned

    func testObservePinnedReturnsValueObservation() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }

        let observation = InboxQueries.observePinned()
        var receivedItems: [InboxItem] = []
        let expectation = XCTestExpectation(description: "observation fires")
        let cancellable = observation.start(
            in: dbManager.dbPool,
            scheduling: .immediate
        ) { error in
            XCTFail("Observation error: \(error)")
        } onChange: { items in
            receivedItems = items
            expectation.fulfill()
        }
        wait(for: [expectation], timeout: 2)
        XCTAssertEqual(receivedItems.count, 1)
        XCTAssertTrue(receivedItems[0].pinned)
        _ = cancellable
    }

    // MARK: - markSeen

    func testMarkSeenSetsReadAt() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let id = try db.read { db -> Int64 in
            try Int64.fetchOne(db, sql: "SELECT id FROM inbox_items LIMIT 1") ?? 0
        }
        try db.write { try InboxQueries.markSeen($0, itemID: id) }
        let item = try db.read { try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [id]) }
        XCTAssertFalse(try XCTUnwrap(item).readAt.isEmpty)
    }

    func testMarkSeenDoesNotOverwriteExistingReadAt() throws {
        let db = try TestDatabase.create()
        let originalReadAt = "2026-04-20T08:00:00Z"
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, read_at, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,?,?,?)
            """, arguments: [originalReadAt, "2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let id = try db.read { db -> Int64 in
            try Int64.fetchOne(db, sql: "SELECT id FROM inbox_items LIMIT 1") ?? 0
        }
        try db.write { try InboxQueries.markSeen($0, itemID: id) }
        let item = try db.read { try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items WHERE id = ?", arguments: [id]) }
        XCTAssertEqual(try XCTUnwrap(item).readAt, originalReadAt)
    }

    func testMarkSeenIsIdempotentForUnread() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','medium',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let id = try db.read { db -> Int64 in
            try Int64.fetchOne(db, sql: "SELECT id FROM inbox_items LIMIT 1") ?? 0
        }
        // Mark seen twice — second call should be a no-op, no crash
        try db.write { try InboxQueries.markSeen($0, itemID: id) }
        let firstReadAt = try db.read { try String.fetchOne($0, sql: "SELECT read_at FROM inbox_items WHERE id = ?", arguments: [id]) ?? "" }
        try db.write { try InboxQueries.markSeen($0, itemID: id) }
        let secondReadAt = try db.read { try String.fetchOne($0, sql: "SELECT read_at FROM inbox_items WHERE id = ?", arguments: [id]) ?? "" }
        XCTAssertEqual(firstReadAt, secondReadAt)
        XCTAssertFalse(firstReadAt.isEmpty)
    }
}
