import XCTest
import GRDB
@testable import WatchtowerDesktop

final class InboxItemTests: XCTestCase {

    // MARK: - New field decoding

    func testDecodesNewFields() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, item_class, pinned, created_at, updated_at)
                VALUES ('C1','1.0','U1','mention','pending','high','actionable',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let item = try XCTUnwrap(db.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.itemClass, .actionable)
        XCTAssertTrue(item.pinned)
        XCTAssertNil(item.archivedAt)
        XCTAssertEqual(item.archiveReason, "")
    }

    func testDecodesAmbientClass() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, item_class, pinned, created_at, updated_at)
                VALUES ('C1','2.0','U1','mention','pending','low','ambient',0,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let item = try XCTUnwrap(db.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertEqual(item.itemClass, .ambient)
        XCTAssertFalse(item.pinned)
    }

    func testDefaultsToAmbientWhenClassMissing() throws {
        let db = try TestDatabase.create()
        // Insert without item_class — should default to ambient
        try db.write { db in
            try TestDatabase.insertInboxItem(db)
        }
        let item = try XCTUnwrap(db.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        // Default column value is 'ambient', so ItemClass should decode to .ambient
        XCTAssertEqual(item.itemClass, .ambient)
    }

    func testDecodesArchivedAt() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, archived_at, archive_reason, created_at, updated_at)
                VALUES ('C1','3.0','U1','mention','resolved','low',?,?,?,?)
            """, arguments: [
                "2026-04-23T12:00:00Z",
                "auto-resolved",
                "2026-04-23T10:00:00Z",
                "2026-04-23T10:00:00Z"
            ])
        }
        let item = try XCTUnwrap(db.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertNotNil(item.archivedAt)
        XCTAssertEqual(item.archiveReason, "auto-resolved")
    }

    func testIsPinnedPredicateTrue() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type,
                    status, priority, pinned, created_at, updated_at)
                VALUES ('C1','4.0','U1','mention','pending','high',1,?,?)
            """, arguments: ["2026-04-23T10:00:00Z", "2026-04-23T10:00:00Z"])
        }
        let item = try XCTUnwrap(db.read { db in
            try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1")
        })
        XCTAssertTrue(item.pinned)
    }
}
