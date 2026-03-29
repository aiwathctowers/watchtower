import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackModelTests: XCTestCase {

    // MARK: - Read predicates

    func testReadPredicates() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, hasUpdates: true) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertTrue(track.isUnread)
        XCTAssertFalse(track.isRead)
    }

    func testReadPredicatesMarkedRead() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db)
            try db.execute(sql: "UPDATE tracks SET read_at = '2025-01-01T00:00:00Z', has_updates = 0 WHERE id = 1")
        }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertTrue(track.isRead)
        XCTAssertFalse(track.isUnread)
    }

    // MARK: - Ownership predicates

    func testOwnershipPredicates() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "mine") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertTrue(track.isMine)
        XCTAssertFalse(track.isDelegated)
        XCTAssertFalse(track.isWatching)
    }

    func testDelegatedOwnership() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "delegated") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertTrue(track.isDelegated)
    }

    // MARK: - Labels

    func testCategoryLabel() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, category: "decision") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.categoryLabel, "Decision")
    }

    func testOwnershipLabel() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "watching") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.ownershipLabel, "Watching")
    }

    // MARK: - JSON decoders

    func testDecodedParticipants() throws {
        let db = try TestDatabase.create()
        let json = #"[{"name":"Alice","user_id":"U001","stance":"driver"},{"name":"Bob"}]"#
        try db.write { try TestDatabase.insertTrack($0, participants: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.decodedParticipants.count, 2)
        XCTAssertEqual(track.decodedParticipants[0].name, "Alice")
        XCTAssertEqual(track.decodedParticipants[0].userID, "U001")
        XCTAssertEqual(track.decodedParticipants[0].stance, "driver")
        XCTAssertNil(track.decodedParticipants[1].userID)
    }

    func testDecodedTags() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, tags: #"["urgent","backend"]"#) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.decodedTags, ["urgent", "backend"])
    }

    func testDecodedTagsEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertTrue(track.decodedTags.isEmpty)
    }

    func testDecodedSourceRefs() throws {
        let db = try TestDatabase.create()
        let json = #"[{"ts":"1700000000.000100","author":"Alice","text":"Let's do it"}]"#
        try db.write { try TestDatabase.insertTrack($0, sourceRefs: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let refs = track.decodedSourceRefs
        XCTAssertEqual(refs.count, 1)
        XCTAssertEqual(refs[0].author, "Alice")
        XCTAssertEqual(refs[0].text, "Let's do it")
        XCTAssertEqual(refs[0].ts, "1700000000.000100")
    }

    func testDecodedChannelIDs() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, channelIDs: #"["C001","C002"]"#) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.decodedChannelIDs, ["C001", "C002"])
    }

    func testDecodedRelatedDigestIDs() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, relatedDigestIDs: #"[1,5,1]"#) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let ids = track.decodedRelatedDigestIDs.sorted()
        XCTAssertEqual(ids, [1, 1, 5])
    }

    func testDecodedDecisionOptions() throws {
        let db = try TestDatabase.create()
        let json = #"[{"option":"Option A","supporters":["Alice"],"pros":"Fast","cons":"Risky"}]"#
        try db.write { try TestDatabase.insertTrack($0, decisionOptions: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let options = track.decodedDecisionOptions
        XCTAssertEqual(options.count, 1)
        XCTAssertEqual(options[0].option, "Option A")
        XCTAssertEqual(options[0].supporters, ["Alice"])
    }

    func testDecodedSubItems() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Do thing","status":"open"},{"text":"Done thing","status":"done"}]"#
        try db.write { try TestDatabase.insertTrack($0, subItems: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let items = track.decodedSubItems
        XCTAssertEqual(items.count, 2)
        XCTAssertFalse(items[0].isDone)
        XCTAssertTrue(items[1].isDone)
        let progress = track.subItemsProgress
        XCTAssertEqual(progress.done, 1)
        XCTAssertEqual(progress.total, 2)
    }

    // MARK: - Priority helpers

    func testPriorityOrder() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, priority: "high") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.priorityOrder, 0)
    }
}
