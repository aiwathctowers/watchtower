import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackModelTests: XCTestCase {

    // MARK: - Status predicates

    func testStatusPredicates() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, status: "inbox") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }

        XCTAssertTrue(track.isInbox)
        XCTAssertFalse(track.isActive)
        XCTAssertFalse(track.isDone)
        XCTAssertFalse(track.isDismissed)
        XCTAssertFalse(track.isSnoozed)
    }

    func testOwnershipPredicates() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "delegated") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }

        XCTAssertFalse(track.isMine)
        XCTAssertTrue(track.isDelegated)
        XCTAssertFalse(track.isWatching)
        XCTAssertEqual(track.ownershipLabel, "Delegated")
    }

    func testOwnershipLabelMine() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "mine") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.ownershipLabel, "Mine")
    }

    func testOwnershipLabelWatching() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, ownership: "watching") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.ownershipLabel, "Watching")
    }

    // MARK: - Category labels

    func testCategoryLabels() {
        let cases: [(String, String)] = [
            ("code_review", "Review"),
            ("decision_needed", "Decision"),
            ("info_request", "Info"),
            ("task", "Task"),
            ("approval", "Approval"),
            ("follow_up", "Follow-up"),
            ("bug_fix", "Bug"),
            ("discussion", "Discussion"),
            ("unknown", ""),
        ]
        for (category, expected) in cases {
            let db = try! TestDatabase.create()
            try! db.write { db in
                try db.execute(sql: """
                    INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, category)
                    VALUES ('C001', 'U001', 'test', 100, 200, ?)
                    """, arguments: [category])
            }
            let track = try! db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
            XCTAssertEqual(track.categoryLabel, expected, "Failed for category: \(category)")
        }
    }

    // MARK: - JSON decoders

    func testDecodedParticipants() throws {
        let db = try TestDatabase.create()
        let json = #"[{"name":"Alice","user_id":"U001","stance":"for"},{"name":"Bob"}]"#
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, participants)
                VALUES ('C001', 'U001', 'test', 100, 200, ?)
                """, arguments: [json])
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.decodedParticipants.count, 2)
        XCTAssertEqual(track.decodedParticipants[0].name, "Alice")
        XCTAssertEqual(track.decodedParticipants[0].userID, "U001")
        XCTAssertEqual(track.decodedParticipants[0].stance, "for")
        XCTAssertNil(track.decodedParticipants[1].userID)
    }

    func testDecodedTags() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, tags)
                VALUES ('C001', 'U001', 'test', 100, 200, '["urgent","backend"]')
                """)
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.decodedTags, ["urgent", "backend"])
    }

    func testDecodedTagsEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0) }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertTrue(track.decodedTags.isEmpty)
    }

    func testDecodedSubItems() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Step 1","status":"done"},{"text":"Step 2","status":"open"}]"#
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, sub_items)
                VALUES ('C001', 'U001', 'test', 100, 200, ?)
                """, arguments: [json])
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        let subs = track.decodedSubItems
        XCTAssertEqual(subs.count, 2)
        XCTAssertTrue(subs[0].isDone)
        XCTAssertFalse(subs[1].isDone)
        let progress = track.subItemsProgress
        XCTAssertEqual(progress.done, 1)
        XCTAssertEqual(progress.total, 2)
    }

    func testDecodedRelatedDigestIDs() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, related_digest_ids)
                VALUES ('C001', 'U001', 'test', 100, 200, '[1,5,10]')
                """)
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.decodedRelatedDigestIDs, [1, 5, 10])
    }

    func testDecodedDecisionOptions() throws {
        let db = try TestDatabase.create()
        let json = #"[{"option":"Go with A","supporters":["Alice"],"pros":"Fast","cons":"Risky"}]"#
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, decision_options)
                VALUES ('C001', 'U001', 'test', 100, 200, ?)
                """, arguments: [json])
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        let options = track.decodedDecisionOptions
        XCTAssertEqual(options.count, 1)
        XCTAssertEqual(options[0].option, "Go with A")
        XCTAssertEqual(options[0].supporters, ["Alice"])
        XCTAssertEqual(options[0].pros, "Fast")
        XCTAssertEqual(options[0].cons, "Risky")
    }

    func testDecodedSourceRefs() throws {
        let db = try TestDatabase.create()
        let json = #"[{"ts":"1700000000.000100","author":"Alice","text":"Let's do it"}]"#
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO tracks (channel_id, assignee_user_id, text, period_from, period_to, source_refs)
                VALUES ('C001', 'U001', 'test', 100, 200, ?)
                """, arguments: [json])
        }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        let refs = track.decodedSourceRefs
        XCTAssertEqual(refs.count, 1)
        XCTAssertEqual(refs[0].author, "Alice")
    }

    // MARK: - Date helpers

    func testSourceDate() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, sourceMessageTS: "1700000000.000100") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertEqual(track.sourceDate.timeIntervalSince1970, 1700000000, accuracy: 1)
    }

    func testSourceDateFallsBackToCreatedDate() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, sourceMessageTS: "") }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        // sourceDate should fallback to createdDate (not epoch)
        XCTAssertTrue(track.sourceDate.timeIntervalSince1970 > 1700000000)
    }

    func testDueDateFormatted() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0, dueDate: 1700000000) }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertNotNil(track.dueDateFormatted)
    }

    func testDueDateFormattedNil() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTrack($0) }
        let track = try db.read { try Track.fetchOne($0, sql: "SELECT * FROM tracks LIMIT 1")! }
        XCTAssertNil(track.dueDateFormatted)
    }
}
