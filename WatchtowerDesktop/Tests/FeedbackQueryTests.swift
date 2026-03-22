import XCTest
import GRDB
@testable import WatchtowerDesktop

final class FeedbackQueryTests: XCTestCase {

    func testAddAndGetFeedback() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "1", rating: 1, comment: "Great")
        }
        let feedback = try db.read { try FeedbackQueries.getFeedback($0, entityType: "digest", entityID: "1") }
        XCTAssertNotNil(feedback)
        XCTAssertEqual(feedback?.rating, 1)
        XCTAssertEqual(feedback?.comment, "Great")
        XCTAssertTrue(try XCTUnwrap(feedback).isPositive)
    }

    func testGetFeedbackReturnsLatest() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // Insert with explicit created_at to guarantee ordering
            try db.execute(sql: """
                INSERT INTO feedback (entity_type, entity_id, rating, comment, created_at)
                VALUES ('digest', '1', 1, 'Good', '2025-01-01T00:00:00Z')
                """)
            try db.execute(sql: """
                INSERT INTO feedback (entity_type, entity_id, rating, comment, created_at)
                VALUES ('digest', '1', -1, 'Actually bad', '2025-01-02T00:00:00Z')
                """)
        }
        let feedback = try db.read { try FeedbackQueries.getFeedback($0, entityType: "digest", entityID: "1") }
        XCTAssertEqual(feedback?.rating, -1)
        XCTAssertFalse(try XCTUnwrap(feedback).isPositive)
    }

    func testGetFeedbackNotFound() throws {
        let db = try TestDatabase.create()
        let feedback = try db.read { try FeedbackQueries.getFeedback($0, entityType: "digest", entityID: "999") }
        XCTAssertNil(feedback)
    }

    func testGetStats() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "1", rating: 1)
            try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "2", rating: 1)
            try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "3", rating: -1)
            try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "1", rating: -1)
        }
        let stats = try db.read { try FeedbackQueries.getStats($0) }
        XCTAssertEqual(stats.count, 2)

        let digestStats = try XCTUnwrap(stats.first { $0.entityType == "digest" })
        XCTAssertEqual(digestStats.positive, 2)
        XCTAssertEqual(digestStats.negative, 1)
        XCTAssertEqual(digestStats.total, 3)
        XCTAssertEqual(digestStats.positivePercent, 66) // 2*100/3

        let trackStats = try XCTUnwrap(stats.first { $0.entityType == "track" })
        XCTAssertEqual(trackStats.positive, 0)
        XCTAssertEqual(trackStats.negative, 1)
        XCTAssertEqual(trackStats.total, 1)
        XCTAssertEqual(trackStats.positivePercent, 0)
    }

    func testGetStatsEmpty() throws {
        let db = try TestDatabase.create()
        let stats = try db.read { try FeedbackQueries.getStats($0) }
        XCTAssertTrue(stats.isEmpty)
    }

    func testGetAllFeedback() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "1", rating: 1)
            try FeedbackQueries.addFeedback(db, entityType: "track", entityID: "2", rating: -1)
            try FeedbackQueries.addFeedback(db, entityType: "decision", entityID: "3", rating: 1)
        }
        let all = try db.read { try FeedbackQueries.getAllFeedback($0) }
        XCTAssertEqual(all.count, 3)
    }

    func testGetAllFeedbackWithLimit() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            for i in 0..<5 {
                try FeedbackQueries.addFeedback(db, entityType: "digest", entityID: "\(i)", rating: 1)
            }
        }
        let limited = try db.read { try FeedbackQueries.getAllFeedback($0, limit: 2) }
        XCTAssertEqual(limited.count, 2)
    }

    func testFeedbackStatsPositivePercentZeroTotal() {
        let stats = FeedbackStats(entityType: "test", positive: 0, negative: 0, total: 0)
        XCTAssertEqual(stats.positivePercent, 0)
    }
}
