import XCTest
import GRDB
@testable import WatchtowerDesktop

final class InteractionQueryTests: XCTestCase {

    private func insertInteraction(
        _ db: Database,
        userA: String,
        userB: String,
        periodFrom: Double = 100,
        periodTo: Double = 200,
        messagesTo: Int = 5,
        messagesFrom: Int = 3,
        sharedChannels: Int = 2,
        threadRepliesTo: Int = 1,
        threadRepliesFrom: Int = 1,
        sharedChannelIDs: String = #"["C001"]"#
    ) throws {
        try db.execute(sql: """
            INSERT INTO user_interactions
                (user_a, user_b, period_from, period_to, messages_to, messages_from,
                 shared_channels, thread_replies_to, thread_replies_from, shared_channel_ids)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [userA, userB, periodFrom, periodTo, messagesTo, messagesFrom,
                             sharedChannels, threadRepliesTo, threadRepliesFrom, sharedChannelIDs])
    }

    func testFetchForUser() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insertInteraction(db, userA: "U001", userB: "U002", messagesTo: 10, messagesFrom: 5)
            try insertInteraction(db, userA: "U001", userB: "U003", messagesTo: 3, messagesFrom: 2)
            try insertInteraction(db, userA: "U002", userB: "U003") // Different user_a
        }
        let interactions = try db.read {
            try InteractionQueries.fetchForUser($0, userID: "U001", periodFrom: 100, periodTo: 200)
        }
        XCTAssertEqual(interactions.count, 2)
        // Ordered by total volume DESC
        XCTAssertEqual(interactions[0].userB, "U002") // 15 total
        XCTAssertEqual(interactions[1].userB, "U003") // 5 total
    }

    func testFetchForUserDifferentWindow() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insertInteraction(db, userA: "U001", userB: "U002", periodFrom: 100, periodTo: 200)
            try insertInteraction(db, userA: "U001", userB: "U003", periodFrom: 200, periodTo: 300)
        }
        let interactions = try db.read {
            try InteractionQueries.fetchForUser($0, userID: "U001", periodFrom: 100, periodTo: 200)
        }
        XCTAssertEqual(interactions.count, 1)
        XCTAssertEqual(interactions[0].userB, "U002")
    }

    func testFetchTopInteractionsWithLimit() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            for i in 0..<5 {
                try insertInteraction(db, userA: "U001", userB: "U0\(10 + i)", messagesTo: 10 - i, messagesFrom: 5 - i)
            }
        }
        let top = try db.read {
            try InteractionQueries.fetchTopInteractions($0, userID: "U001", periodFrom: 100, periodTo: 200, limit: 3)
        }
        XCTAssertEqual(top.count, 3)
    }

    func testUserInteractionComputedProperties() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insertInteraction(
                db,
                userA: "U001",
                userB: "U002",
                messagesTo: 10,
                messagesFrom: 5,
                threadRepliesTo: 3,
                threadRepliesFrom: 2,
                sharedChannelIDs: #"["C001","C002"]"#
            )
        }
        let interaction = try XCTUnwrap(
            try db.read {
                try InteractionQueries.fetchForUser($0, userID: "U001", periodFrom: 100, periodTo: 200)
            }.first
        )

        XCTAssertEqual(interaction.totalMessages, 15)
        XCTAssertEqual(interaction.totalThreadReplies, 5)
        XCTAssertEqual(interaction.parsedSharedChannelIDs, ["C001", "C002"])
    }

    func testUserInteractionEmptyChannelIDs() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try insertInteraction(db, userA: "U001", userB: "U002", sharedChannelIDs: "[]")
        }
        let interaction = try XCTUnwrap(
            try db.read {
                try InteractionQueries.fetchForUser($0, userID: "U001", periodFrom: 100, periodTo: 200)
            }.first
        )
        XCTAssertTrue(interaction.parsedSharedChannelIDs.isEmpty)
    }
}
