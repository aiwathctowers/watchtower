import XCTest
import GRDB
@testable import WatchtowerDesktop

final class BriefingModelTests: XCTestCase {

    // MARK: - Model Parsing

    func testBriefingDecodesFromDB() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(
                db,
                attention: """
                [{"text":"PR needs review","source_type":"track","source_id":5,"priority":"high","reason":"Blocking release"}]
                """,
                whatHappened: """
                [{"text":"Active discussion","digest_id":1,"channel_name":"general","item_type":"summary","importance":"high"}]
                """
            )
        }

        let briefing = try db.read { db in
            try BriefingQueries.fetchLatest(db)
        }

        XCTAssertNotNil(briefing)
        XCTAssertEqual(briefing?.date, "2024-01-15")
        XCTAssertEqual(briefing?.role, "engineer")
        XCTAssertFalse(briefing?.isRead ?? true)

        let attention = briefing?.parsedAttention ?? []
        XCTAssertEqual(attention.count, 1)
        XCTAssertEqual(attention.first?.priority, "high")
        XCTAssertEqual(attention.first?.sourceType, "track")
        XCTAssertEqual(attention.first?.sourceID, "5")
        XCTAssertEqual(attention.first?.reason, "Blocking release")

        let happened = briefing?.parsedWhatHappened ?? []
        XCTAssertEqual(happened.count, 1)
        XCTAssertEqual(happened.first?.channelName, "general")
        XCTAssertEqual(happened.first?.digestID, 1)
    }

    func testBriefingParsesYourDay() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(
                db,
                yourDay: """
                [{"text":"Review PR #42","track_id":7,"due_date":"2024-01-16","priority":"high","status":"active","ownership":"mine"}]
                """
            )
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        let items = briefing?.parsedYourDay ?? []
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items.first?.text, "Review PR #42")
        XCTAssertEqual(items.first?.trackID, 7)
        XCTAssertEqual(items.first?.dueDate, "2024-01-16")
        XCTAssertEqual(items.first?.ownership, "mine")
    }

    func testBriefingParsesTeamPulse() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(
                db,
                teamPulse: """
                [{"text":"Alice volume dropped","user_id":"U002","signal_type":"volume_drop","detail":"50% fewer messages"}]
                """
            )
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        let items = briefing?.parsedTeamPulse ?? []
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items.first?.userID, "U002")
        XCTAssertEqual(items.first?.signalType, "volume_drop")
        XCTAssertEqual(items.first?.detail, "50% fewer messages")
    }

    func testBriefingParsesCoaching() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(
                db,
                coaching: """
                [{"text":"Consider async standups","related_user_id":"U001","category":"process"}]
                """
            )
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        let items = briefing?.parsedCoaching ?? []
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items.first?.text, "Consider async standups")
        XCTAssertEqual(items.first?.category, "process")
        XCTAssertEqual(items.first?.relatedUserID, "U001")
    }

    func testAttentionSuggestTrack() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(
                db,
                attention: """
                [{"text":"Track suggestion","source_type":"track","source_id":"3","suggest_track":true},{"text":"Normal item","priority":"low"}]
                """
            )
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        let items = briefing?.parsedAttention ?? []
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].suggestTrack, true)
        XCTAssertNil(items[1].suggestTrack)
    }

    func testBriefingEmptyJSONReturnsEmptyArrays() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db)
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        XCTAssertNotNil(briefing)
        XCTAssertTrue(briefing?.parsedAttention.isEmpty ?? false)
        XCTAssertTrue(briefing?.parsedYourDay.isEmpty ?? false)
        XCTAssertTrue(briefing?.parsedWhatHappened.isEmpty ?? false)
        XCTAssertTrue(briefing?.parsedTeamPulse.isEmpty ?? false)
        XCTAssertTrue(briefing?.parsedCoaching.isEmpty ?? false)
    }

    func testDateLabel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-03-15")
        }

        let briefing = try db.read { db in try BriefingQueries.fetchLatest(db) }
        XCTAssertFalse(briefing?.dateLabel.isEmpty ?? true)
        XCTAssertTrue(briefing?.dateLabel.contains("2024") ?? false)
    }
}

// MARK: - Query Tests

final class BriefingQueryTests: XCTestCase {

    func testFetchRecent() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
            try TestDatabase.insertBriefing(db, date: "2024-01-16")
        }

        let briefings = try db.read { db in
            try BriefingQueries.fetchRecent(db)
        }
        XCTAssertEqual(briefings.count, 2)
    }

    func testFetchRecentWithLimit() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
            try TestDatabase.insertBriefing(db, date: "2024-01-16")
            try TestDatabase.insertBriefing(db, date: "2024-01-17")
        }

        let briefings = try db.read { db in
            try BriefingQueries.fetchRecent(db, limit: 2)
        }
        XCTAssertEqual(briefings.count, 2)
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
        }

        let briefing = try db.read { db in
            try BriefingQueries.fetchByID(db, id: 1)
        }
        XCTAssertEqual(briefing?.date, "2024-01-15")

        let missing = try db.read { db in
            try BriefingQueries.fetchByID(db, id: 999)
        }
        XCTAssertNil(missing)
    }

    func testMarkRead() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db)
        }

        let before = try db.read { db in try BriefingQueries.fetchByID(db, id: 1) }
        XCTAssertFalse(before?.isRead ?? true)

        try db.write { db in
            try BriefingQueries.markRead(db, id: 1)
        }

        let after = try db.read { db in try BriefingQueries.fetchByID(db, id: 1) }
        XCTAssertTrue(after?.isRead ?? false)
    }

    func testUnreadCount() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
            try TestDatabase.insertBriefing(db, date: "2024-01-16")
            try TestDatabase.insertBriefing(
                db,
                date: "2024-01-17",
                readAt: "2024-01-17T08:00:00Z"
            )
        }

        let count = try db.read { db in
            try BriefingQueries.unreadCount(db)
        }
        XCTAssertEqual(count, 2)
    }

    func testMaxID() throws {
        let db = try TestDatabase.create()

        let emptyMax = try db.read { db in try BriefingQueries.maxID(db) }
        XCTAssertEqual(emptyMax, 0)

        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
            try TestDatabase.insertBriefing(db, date: "2024-01-16")
        }

        let maxID = try db.read { db in try BriefingQueries.maxID(db) }
        XCTAssertEqual(maxID, 2)
    }

    func testFetchNewSince() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertBriefing(db, date: "2024-01-15")
            try TestDatabase.insertBriefing(db, date: "2024-01-16")
        }

        let newOnes = try db.read { db in
            try BriefingQueries.fetchNewSince(db, afterID: 1)
        }
        XCTAssertEqual(newOnes.count, 1)
        XCTAssertEqual(newOnes.first?.date, "2024-01-16")
    }
}
