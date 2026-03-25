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

    // MARK: - JSON decoders

    func testDecodedParticipants() throws {
        let db = try TestDatabase.create()
        let json = #"[{"name":"Alice","user_id":"U001","role":"driver"},{"name":"Bob"}]"#
        try db.write { try TestDatabase.insertTrack($0, participants: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.decodedParticipants.count, 2)
        XCTAssertEqual(track.decodedParticipants[0].name, "Alice")
        XCTAssertEqual(track.decodedParticipants[0].userID, "U001")
        XCTAssertEqual(track.decodedParticipants[0].role, "driver")
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

    func testDecodedTimeline() throws {
        let db = try TestDatabase.create()
        let json = ##"[{"date":"2026-03-01","event":"Discussion started","channel":"#general"}]"##
        try db.write { try TestDatabase.insertTrack($0, timeline: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let events = track.decodedTimeline
        XCTAssertEqual(events.count, 1)
        XCTAssertEqual(events[0].event, "Discussion started")
        XCTAssertEqual(events[0].channel, "#general")
    }

    func testDecodedKeyMessages() throws {
        let db = try TestDatabase.create()
        let json = ##"[{"ts":"1700000000.000100","author":"Alice","text":"Let's do it","channel":"#general"}]"##
        try db.write { try TestDatabase.insertTrack($0, keyMessages: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let msgs = track.decodedKeyMessages
        XCTAssertEqual(msgs.count, 1)
        XCTAssertEqual(msgs[0].author, "Alice")
        XCTAssertEqual(msgs[0].text, "Let's do it")
    }

    func testDecodedChannelIDs() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, channelIDs: #"["C001","C002"]"#) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.decodedChannelIDs, ["C001", "C002"])
    }

    func testDecodedSourceRefs() throws {
        let db = try TestDatabase.create()
        let json = #"[{"digest_id":5,"topic_id":42,"channel_id":"C001"}]"#
        try db.write { try TestDatabase.insertTrack($0, sourceRefs: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let refs = track.decodedSourceRefs
        XCTAssertEqual(refs.count, 1)
        XCTAssertEqual(refs[0].digestID, 5)
        XCTAssertEqual(refs[0].topicID, 42)
        XCTAssertEqual(refs[0].channelID, "C001")
    }

    func testLinkedDigestIDs() throws {
        let db = try TestDatabase.create()
        let json = #"[{"digest_id":1},{"digest_id":5},{"digest_id":1}]"#
        try db.write { try TestDatabase.insertTrack($0, sourceRefs: json) }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        let ids = track.linkedDigestIDs.sorted()
        XCTAssertEqual(ids, [1, 5])
    }

    // MARK: - Priority helpers

    func testPriorityOrder() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, priority: "high") }
        let track = try XCTUnwrap(db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1") })
        XCTAssertEqual(track.priorityOrder, 0)
    }
}
