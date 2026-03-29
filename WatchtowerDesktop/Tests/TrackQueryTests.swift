import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackQueryTests: XCTestCase {

    // MARK: - Fetch

    func testFetchAllDefaultOrder() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "Low", priority: "low")
            try TestDatabase.insertTrack(db, text: "High", priority: "high", hasUpdates: true)
            try TestDatabase.insertTrack(db, text: "Medium", priority: "medium")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0) }
        // has_updates DESC, updated_at DESC — "High" has updates so comes first
        XCTAssertEqual(tracks[0].text, "High")
    }

    func testFetchAllByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "A", priority: "high")
            try TestDatabase.insertTrack(db, text: "B", priority: "low")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, priority: "high") }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].text, "A")
    }

    func testFetchAllByHasUpdates() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "Updated", hasUpdates: true)
            try TestDatabase.insertTrack(db, text: "Stable")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, hasUpdates: true) }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].text, "Updated")
    }

    func testFetchAllByChannelID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "In C001", channelIDs: #"["C001"]"#)
            try TestDatabase.insertTrack(db, text: "In C002", channelIDs: #"["C002"]"#)
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, channelID: "C002") }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].text, "In C002")
    }

    func testFetchAllByOwnership() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "Mine", ownership: "mine")
            try TestDatabase.insertTrack(db, text: "Watching", ownership: "watching")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, ownership: "watching") }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].text, "Watching")
    }

    func testFetchUpdatedTracks() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "Updated", hasUpdates: true)
            try TestDatabase.insertTrack(db, text: "Stable")
        }
        let tracks = try db.read { try TrackQueries.fetchUpdatedTracks($0) }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].text, "Updated")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, text: "Find me") }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(track)
        XCTAssertEqual(track?.text, "Find me")
    }

    func testFetchByIDs() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, text: "First")
            try TestDatabase.insertTrack(db, text: "Second")
            try TestDatabase.insertTrack(db, text: "Third")
        }
        let tracks = try db.read { try TrackQueries.fetchByIDs($0, ids: [1, 3]) }
        XCTAssertEqual(tracks.count, 2)
        let texts = Set(tracks.map(\.text))
        XCTAssertTrue(texts.contains("First"))
        XCTAssertTrue(texts.contains("Third"))
    }

    func testFetchByIDsEmpty() throws {
        let db = try TestDatabase.create()
        let tracks = try db.read { try TrackQueries.fetchByIDs($0, ids: []) }
        XCTAssertTrue(tracks.isEmpty)
    }

    // MARK: - Counts

    func testFetchCounts() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, hasUpdates: true)
            try TestDatabase.insertTrack(db, hasUpdates: true)
            try TestDatabase.insertTrack(db)
        }
        let counts = try db.read { try TrackQueries.fetchCounts($0) }
        XCTAssertEqual(counts.total, 3)
        XCTAssertEqual(counts.updated, 2)
    }

    func testFetchOwnershipCounts() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, ownership: "mine")
            try TestDatabase.insertTrack(db, ownership: "mine")
            try TestDatabase.insertTrack(db, ownership: "delegated")
        }
        let counts = try db.read { try TrackQueries.fetchOwnershipCounts($0) }
        XCTAssertEqual(counts["mine"], 2)
        XCTAssertEqual(counts["delegated"], 1)
    }

    // MARK: - Mark read

    func testMarkRead() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, hasUpdates: true)
            try TrackQueries.markRead(db, id: 1)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertTrue(try XCTUnwrap(track).isRead)
        XCTAssertFalse(try XCTUnwrap(track).hasUpdates)
    }

    func testMarkReadCascadesToDigests() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db)
            try TestDatabase.insertTrack(db, hasUpdates: true, relatedDigestIDs: "[1]")
            try TrackQueries.markRead(db, id: 1)
        }
        let readAt = try db.read { try String.fetchOne($0, sql: "SELECT read_at FROM digests WHERE id = 1") }
        XCTAssertNotNil(readAt)
    }

    // MARK: - Priority

    func testUpdatePriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, priority: "medium")
            try TrackQueries.updatePriority(db, id: 1, priority: "high")
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.priority, "high")
    }

    // MARK: - Ownership

    func testUpdateOwnership() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, ownership: "mine")
            try TrackQueries.updateOwnership(db, id: 1, ownership: "delegated")
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(track?.ownership, "delegated")
    }

    // MARK: - Sub-items

    func testUpdateSubItems() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db)
            let items = [TrackSubItem(text: "Do thing", status: "done")]
            try TrackQueries.updateSubItems(db, id: 1, subItems: items)
        }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        let items = try XCTUnwrap(track).decodedSubItems
        XCTAssertEqual(items.count, 1)
        XCTAssertTrue(items[0].isDone)
    }

    // MARK: - Current user

    func testFetchCurrentUserID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U123'")
        }
        let uid = try db.read { try TrackQueries.fetchCurrentUserID($0) }
        XCTAssertEqual(uid, "U123")
    }
}
