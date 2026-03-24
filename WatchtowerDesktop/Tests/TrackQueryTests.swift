import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackQueryTests: XCTestCase {

    /// Insert a track with unique sourceMessageTS to avoid PK conflicts
    private func insert(
        _ db: Database,
        text: String = "test",
        status: String = "inbox",
        priority: String = "medium",
        ownership: String = "mine",
        ts: String = "1700000000.000100",
        hasUpdates: Bool = false
    ) throws {
        try db.execute(sql: """
            INSERT INTO tracks (channel_id, assignee_user_id, text, source_message_ts,
                status, priority, period_from, period_to, ownership, has_updates)
            VALUES ('C001', 'U001', ?, ?, ?, ?, 100, 200, ?, ?)
            """, arguments: [text, ts, status, priority, ownership, hasUpdates ? 1 : 0])
    }

    // MARK: - Fetch

    func testFetchAllDefaultOrder() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, text: "Low", priority: "low", ts: "1700000000.000100")
            try insert(db, text: "High", priority: "high", ts: "1700000001.000100")
            try insert(db, text: "Medium", priority: "medium", ts: "1700000002.000100")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0) }
        XCTAssertEqual(tracks[0].priority, "high")
        XCTAssertEqual(tracks[1].priority, "medium")
        XCTAssertEqual(tracks[2].priority, "low")
    }

    func testFetchAllByStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, text: "A", status: "inbox", ts: "1700000000.000100")
            try insert(db, text: "B", status: "active", ts: "1700000001.000100")
            try insert(db, text: "C", status: "done", ts: "1700000002.000100")
        }
        let inbox = try db.read { try TrackQueries.fetchAll($0, status: "inbox") }
        XCTAssertEqual(inbox.count, 1)
        XCTAssertEqual(inbox[0].text, "A")
    }

    func testFetchAllByStatuses() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, text: "A", status: "inbox", ts: "1700000000.000100")
            try insert(db, text: "B", status: "active", ts: "1700000001.000100")
            try insert(db, text: "C", status: "done", ts: "1700000002.000100")
        }
        let open = try db.read { try TrackQueries.fetchAll($0, statuses: ["inbox", "active"]) }
        XCTAssertEqual(open.count, 2)
    }

    func testFetchAllByOwnership() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, text: "Mine", ownership: "mine", ts: "1700000000.000100")
            try insert(db, text: "Delegated", ownership: "delegated", ts: "1700000001.000100")
        }
        let mine = try db.read { try TrackQueries.fetchAll($0, ownership: "mine") }
        XCTAssertEqual(mine.count, 1)
        XCTAssertEqual(mine[0].text, "Mine")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, text: "Find me") }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(track)
        XCTAssertEqual(track?.text, "Find me")
    }

    // MARK: - Counts

    func testFetchOpenCount() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, status: "inbox", ts: "1700000000.000100")
            try insert(db, status: "active", ts: "1700000001.000100")
            try insert(db, status: "done", ts: "1700000002.000100")
        }
        let count = try db.read { try TrackQueries.fetchOpenCount($0, assigneeUserID: "U001") }
        XCTAssertEqual(count, 2)
    }

    func testFetchInboxCount() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, status: "inbox", ts: "1700000000.000100")
            try insert(db, status: "active", ts: "1700000001.000100")
        }
        let count = try db.read { try TrackQueries.fetchInboxCount($0, assigneeUserID: "U001") }
        XCTAssertEqual(count, 1)
    }

    func testFetchStatusCounts() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, status: "inbox", ts: "1700000000.000100")
            try insert(db, status: "inbox", ts: "1700000001.000100")
            try insert(db, status: "done", ts: "1700000002.000100")
        }
        let counts = try db.read { try TrackQueries.fetchStatusCounts($0, assigneeUserID: "U001") }
        XCTAssertEqual(counts["inbox"], 2)
        XCTAssertEqual(counts["done"], 1)
        XCTAssertNil(counts["active"])
    }

    func testFetchOwnershipCounts() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, status: "inbox", ownership: "mine", ts: "1700000000.000100")
            try insert(db, status: "active", ownership: "delegated", ts: "1700000001.000100")
            try insert(db, status: "done", ownership: "mine", ts: "1700000002.000100")
        }
        let counts = try db.read { try TrackQueries.fetchOwnershipCounts($0, assigneeUserID: "U001") }
        XCTAssertEqual(counts["mine"], 1) // Only inbox+active
        XCTAssertEqual(counts["delegated"], 1)
    }

    // MARK: - Status updates

    func testUpdateStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "inbox")
            try TrackQueries.updateStatus(db, id: 1, status: "active")
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.status, "active")
        XCTAssertNil(track?.completedAt)
    }

    func testUpdateStatusDoneSetsCompletedAt() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "active")
            try TrackQueries.updateStatus(db, id: 1, status: "done")
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.status, "done")
        XCTAssertNotNil(track?.completedAt)
    }

    func testUpdateStatusLogsHistory() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "inbox")
            try TrackQueries.updateStatus(db, id: 1, status: "active")
        }
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertEqual(history.count, 1)
        XCTAssertEqual(history[0].event, "status_changed")
        XCTAssertEqual(history[0].oldValue, "inbox")
        XCTAssertEqual(history[0].newValue, "active")
    }

    func testUpdateStatusReopenLogsReopened() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "done")
            try TrackQueries.updateStatus(db, id: 1, status: "inbox")
        }
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertEqual(history[0].event, "reopened")
    }

    func testAcceptItem() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "inbox")
            try TrackQueries.acceptItem(db, id: 1)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.status, "active")
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertEqual(history.count, 1)
        XCTAssertEqual(history[0].event, "accepted")
    }

    func testAcceptItemIgnoresNonInbox() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "done")
            try TrackQueries.acceptItem(db, id: 1)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.status, "done")
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertTrue(history.isEmpty)
    }

    func testSnoozeItem() throws {
        let db = try TestDatabase.create()
        let snoozeUntil = Date().timeIntervalSince1970 + 86400
        try db.write { db in
            try TestDatabase.insertTrack(db, status: "active")
            try TrackQueries.snoozeItem(db, id: 1, until: snoozeUntil)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.status, "snoozed")
        XCTAssertEqual(track?.snoozeUntil ?? 0, snoozeUntil, accuracy: 1)
        XCTAssertEqual(track?.preSnoozeStatus, "active")
    }

    func testMarkUpdateRead() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insert(db, hasUpdates: true)
            try TrackQueries.markUpdateRead(db, id: 1)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertFalse(try XCTUnwrap(track).hasUpdates)
    }

    // MARK: - Insert and history

    func testInsertTrackLogsCreatedHistory() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TrackQueries.insertTrack(
                db,
                data: TrackInsertData(
                    channelID: "C001",
                    assigneeUserID: "U001",
                    assigneeRaw: "alice",
                    text: "Do thing",
                    sourceChannelName: "general",
                    priority: "high",
                    periodFrom: 100,
                    periodTo: 200,
                    model: "haiku",
                    category: "task"
                )
            )
        }
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertEqual(history.count, 1)
        XCTAssertEqual(history[0].event, "created")
        XCTAssertEqual(history[0].newValue, "from_digest")
    }

    func testUpdateSubItems() throws {
        let db = try TestDatabase.create()
        let newJSON = #"[{"text":"Step 1","status":"done"}]"#
        try db.write { db in
            try TestDatabase.insertTrack(db)
            try TrackQueries.updateSubItems(db, id: 1, subItemsJSON: newJSON)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.subItems, newJSON)
        let history = try db.read { try TrackQueries.fetchHistory($0, trackID: 1) }
        XCTAssertTrue(history.contains { $0.event == "sub_items_updated" })
    }
}
