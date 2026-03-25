import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackQueryTests: XCTestCase {

    // MARK: - Fetch

    func testFetchAllDefaultOrder() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "Low", priority: "low")
            try TestDatabase.insertTrack(db, title: "High", priority: "high", hasUpdates: true)
            try TestDatabase.insertTrack(db, title: "Medium", priority: "medium")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0) }
        // has_updates DESC, updated_at DESC — "High" has updates so comes first
        XCTAssertEqual(tracks[0].title, "High")
    }

    func testFetchAllByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "A", priority: "high")
            try TestDatabase.insertTrack(db, title: "B", priority: "low")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, priority: "high") }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].title, "A")
    }

    func testFetchAllByHasUpdates() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "Updated", hasUpdates: true)
            try TestDatabase.insertTrack(db, title: "Stable")
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, hasUpdates: true) }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].title, "Updated")
    }

    func testFetchAllByChannelID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "In C001", channelIDs: #"["C001"]"#)
            try TestDatabase.insertTrack(db, title: "In C002", channelIDs: #"["C002"]"#)
        }
        let tracks = try db.read { try TrackQueries.fetchAll($0, channelID: "C002") }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].title, "In C002")
    }

    func testFetchUpdatedTracks() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "Updated", hasUpdates: true)
            try TestDatabase.insertTrack(db, title: "Stable")
        }
        let tracks = try db.read { try TrackQueries.fetchUpdatedTracks($0) }
        XCTAssertEqual(tracks.count, 1)
        XCTAssertEqual(tracks[0].title, "Updated")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, title: "Find me") }
        let track = try db.read { try TrackQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(track)
        XCTAssertEqual(track?.title, "Find me")
    }

    func testFetchByIDs() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db, title: "First")
            try TestDatabase.insertTrack(db, title: "Second")
            try TestDatabase.insertTrack(db, title: "Third")
        }
        let tracks = try db.read { try TrackQueries.fetchByIDs($0, ids: [1, 3]) }
        XCTAssertEqual(tracks.count, 2)
        let titles = Set(tracks.map(\.title))
        XCTAssertTrue(titles.contains("First"))
        XCTAssertTrue(titles.contains("Third"))
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
            let sourceRefs = #"[{"digest_id":1}]"#
            try TestDatabase.insertTrack(db, sourceRefs: sourceRefs, hasUpdates: true)
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
