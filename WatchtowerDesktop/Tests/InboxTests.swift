import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - InboxItem Model Tests

final class InboxModelTests: XCTestCase {

    func testDefaultValues() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.channelID, "C001")
        XCTAssertEqual(item.senderUserID, "U002")
        XCTAssertEqual(item.triggerType, "mention")
        XCTAssertEqual(item.status, "pending")
        XCTAssertEqual(item.priority, "medium")
        XCTAssertTrue(item.isPending)
        XCTAssertTrue(item.isUnread)
        XCTAssertTrue(item.isMention)
        XCTAssertFalse(item.isDM)
    }

    func testIsPending() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "pending") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.isPending)
    }

    func testIsResolved() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "resolved") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.isResolved)
        XCTAssertFalse(item.isPending)
    }

    func testIsDismissed() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "dismissed") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.isDismissed)
    }

    func testIsSnoozed() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "snoozed") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.isSnoozed)
    }

    func testIsUnreadWhenNoReadAt() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, readAt: nil) }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.isUnread)
        XCTAssertTrue(item.readAt.isEmpty)
    }

    func testIsReadWhenReadAtSet() throws {
        let db = try TestDatabase.create()
        try db.write {
            try TestDatabase.insertInboxItem($0, readAt: "2026-03-26T10:00:00Z")
        }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertFalse(item.isUnread)
        XCTAssertFalse(item.readAt.isEmpty)
    }

    func testPriorityOrder() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, priority: "high") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.priorityOrder, 0)
    }

    func testPriorityOrderLow() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, priority: "low") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.priorityOrder, 2)
    }

    func testTriggerIconMention() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, triggerType: "mention") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.triggerIcon, "at")
    }

    func testTriggerIconDM() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, triggerType: "dm") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.triggerIcon, "envelope")
        XCTAssertTrue(item.isDM)
    }

    func testStatusIconPending() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "pending") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.statusIcon, "tray.full")
    }

    func testStatusIconResolved() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, status: "resolved") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.statusIcon, "checkmark.circle.fill")
    }

    func testHasTask() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, taskID: 42) }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.hasLinkedTarget)
        XCTAssertEqual(item.targetID, 42)
    }

    func testNoTask() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertFalse(item.hasLinkedTarget)
        XCTAssertNil(item.targetID)
    }

    func testPriorityColor() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, priority: "low") }
        let item = try XCTUnwrap(db.read {
            try InboxItem.fetchOne($0, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.priorityColor, "secondary")
    }
}

// MARK: - InboxQueries Tests

final class InboxQueryTests: XCTestCase {

    // MARK: - fetchAll

    func testFetchAllDefaultExcludesResolved() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", status: "pending")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", status: "resolved")
            try TestDatabase.insertInboxItem(db, messageTS: "1.3", status: "dismissed")
            try TestDatabase.insertInboxItem(db, messageTS: "1.4", status: "snoozed")
        }
        let items = try db.read { try InboxQueries.fetchAll($0) }
        XCTAssertEqual(items.count, 2)
        let statuses = Set(items.map(\.status))
        XCTAssertTrue(statuses.contains("pending"))
        XCTAssertTrue(statuses.contains("snoozed"))
    }

    func testFetchAllIncludeResolved() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", status: "pending")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", status: "resolved")
        }
        let items = try db.read { try InboxQueries.fetchAll($0, includeResolved: true) }
        XCTAssertEqual(items.count, 2)
    }

    func testFetchAllFilterByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", priority: "high")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", priority: "low")
        }
        let items = try db.read { try InboxQueries.fetchAll($0, priority: "high") }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].priority, "high")
    }

    func testFetchAllFilterByTriggerType() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", triggerType: "mention")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", triggerType: "dm")
        }
        let items = try db.read { try InboxQueries.fetchAll($0, triggerType: "dm") }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].triggerType, "dm")
    }

    func testFetchAllOrderByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", snippet: "Low", priority: "low")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", snippet: "High", priority: "high")
            try TestDatabase.insertInboxItem(db, messageTS: "1.3", snippet: "Med", priority: "medium")
        }
        let items = try db.read { try InboxQueries.fetchAll($0) }
        XCTAssertEqual(items[0].snippet, "High")
        XCTAssertEqual(items[1].snippet, "Med")
        XCTAssertEqual(items[2].snippet, "Low")
    }

    // MARK: - fetchByID

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, snippet: "Test item") }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.snippet, "Test item")
    }

    func testFetchByIDNotFound() throws {
        let db = try TestDatabase.create()
        let item = try db.read { try InboxQueries.fetchByID($0, id: 999) }
        XCTAssertNil(item)
    }

    // MARK: - fetchCounts

    func testFetchCounts() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertInboxItem(
                db,
                messageTS: "1.1",
                status: "pending",
                priority: "high"
            )
            try TestDatabase.insertInboxItem(
                db,
                messageTS: "1.2",
                status: "pending",
                priority: "medium"
            )
            try TestDatabase.insertInboxItem(
                db,
                messageTS: "1.3",
                status: "pending",
                priority: "low",
                readAt: "2026-03-26T10:00:00Z"
            )
            try TestDatabase.insertInboxItem(db, messageTS: "1.4", status: "resolved")
        }
        let counts = try db.read { try InboxQueries.fetchCounts($0) }
        XCTAssertEqual(counts.pending, 3)
        XCTAssertEqual(counts.unread, 2)
        XCTAssertEqual(counts.highPriority, 1)
    }

    // MARK: - resolve

    func testResolve() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.resolve($0, id: 1) }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.status, "resolved")
        XCTAssertEqual(item.resolvedReason, "Manually resolved")
    }

    func testResolveWithCustomReason() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.resolve($0, id: 1, reason: "Answered in thread") }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.status, "resolved")
        XCTAssertEqual(item.resolvedReason, "Answered in thread")
    }

    // MARK: - linkTask

    func testLinkTarget() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.linkTarget($0, inboxID: 1, targetID: 42) }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.targetID, 42)
        XCTAssertTrue(item.hasLinkedTarget)
    }

    // MARK: - dismiss

    func testDismiss() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.dismiss($0, id: 1) }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.status, "dismissed")
    }

    // MARK: - snooze

    func testSnooze() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.snooze($0, id: 1, until: "2026-04-01") }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(item.status, "snoozed")
        XCTAssertEqual(item.snoozeUntil, "2026-04-01")
    }

    // MARK: - markRead

    func testMarkRead() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0) }
        try db.write { try InboxQueries.markRead($0, id: 1) }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertFalse(item.isUnread)
        XCTAssertFalse(item.readAt.isEmpty)
    }

    // MARK: - createTask

    func testCreateTask() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertInboxItem($0, snippet: "Please review PR") }
        let item = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        let taskID = try db.write {
            try InboxQueries.createTask($0, from: item)
        }
        XCTAssertGreaterThan(taskID, 0)

        // Verify target was created
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: Int(taskID)) })
        XCTAssertEqual(target.text, "Please review PR")
        XCTAssertEqual(target.sourceType, "inbox")
        XCTAssertEqual(target.sourceID, "1")

        // Verify inbox item was linked
        let updated = try XCTUnwrap(db.read { try InboxQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(updated.targetID, Int(taskID))
        XCTAssertTrue(updated.hasLinkedTarget)
    }
}

// MARK: - InboxViewModel Tests

final class InboxViewModelTests: XCTestCase {

    @MainActor
    func testLoadGroupsBySender() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.1", snippet: "Urgent", priority: "high"
            )
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.2", snippet: "Normal", priority: "medium"
            )
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.3", snippet: "Low", priority: "low"
            )
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        // All from same sender U002 → one group
        XCTAssertEqual(vm.senderGroups.count, 1)
        XCTAssertEqual(vm.senderGroups[0].items.count, 3)
        XCTAssertEqual(vm.senderGroups[0].highestPriority, "high")
        XCTAssertEqual(vm.pendingCount, 3)
    }

    @MainActor
    func testResolve() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()
        XCTAssertEqual(vm.pendingCount, 1)

        let item = try XCTUnwrap(vm.allItems.first)
        vm.resolve(item)
        XCTAssertEqual(vm.pendingCount, 0)
    }

    @MainActor
    func testDismiss() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        let item = try XCTUnwrap(vm.allItems.first)
        vm.dismiss(item)
        XCTAssertEqual(vm.pendingCount, 0)
    }

    @MainActor
    func testSnooze() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        let item = try XCTUnwrap(vm.allItems.first)
        vm.snooze(item, until: "2026-04-01")
        XCTAssertEqual(vm.pendingCount, 0)
    }

    @MainActor
    func testMarkRead() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db)
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()
        XCTAssertEqual(vm.unreadCount, 1)

        let item = try XCTUnwrap(vm.allItems.first)
        vm.markRead(item)
        XCTAssertEqual(vm.unreadCount, 0)
    }

    @MainActor
    func testCreateTask() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db, snippet: "Review this")
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        let item = try XCTUnwrap(vm.allItems.first)
        vm.createTask(from: item)

        let updated = try XCTUnwrap(vm.itemByID(item.id))
        XCTAssertTrue(updated.hasLinkedTarget)
    }

    @MainActor
    func testHighPriorityCount() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.1", priority: "high"
            )
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.2", priority: "high"
            )
            try TestDatabase.insertInboxItem(
                db, messageTS: "1.3", priority: "medium"
            )
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.highPriorityCount, 2)
    }

    @MainActor
    func testShowResolvedFilter() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertInboxItem(db, messageTS: "1.1", status: "pending")
            try TestDatabase.insertInboxItem(db, messageTS: "1.2", status: "resolved")
        }

        let vm = InboxViewModel(dbManager: dbManager)
        vm.showResolved = true
        vm.load()

        let statuses = Set(vm.allItems.map(\.status))
        XCTAssertTrue(statuses.contains("resolved"))
        XCTAssertTrue(statuses.contains("pending"))
    }
}
