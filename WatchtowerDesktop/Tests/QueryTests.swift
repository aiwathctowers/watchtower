import XCTest
import GRDB
@testable import WatchtowerDesktop

final class ChannelQueryTests: XCTestCase {

    func testFetchAllChannels() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "alpha")
            try TestDatabase.insertChannel(db, id: "C002", name: "beta")
            try TestDatabase.insertChannel(db, id: "C003", name: "gamma")
        }
        let channels = try db.read { try ChannelQueries.fetchAll($0) }
        XCTAssertEqual(channels.count, 3)
        // Should be sorted by name ASC
        XCTAssertEqual(channels.map(\.name), ["alpha", "beta", "gamma"])
    }

    func testFetchAllByType() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general", type: "public")
            try TestDatabase.insertChannel(db, id: "C002", name: "secret", type: "private")
            try TestDatabase.insertChannel(db, id: "C003", name: "dm", type: "dm")
        }
        let publicChannels = try db.read { try ChannelQueries.fetchAll($0, type: "public") }
        XCTAssertEqual(publicChannels.count, 1)
        XCTAssertEqual(publicChannels[0].name, "general")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C123", name: "test-channel")
        }
        let channel = try db.read { try ChannelQueries.fetchByID($0, id: "C123") }
        XCTAssertNotNil(channel)
        XCTAssertEqual(channel?.name, "test-channel")
    }

    func testFetchByIDNotFound() throws {
        let db = try TestDatabase.create()
        let channel = try db.read { try ChannelQueries.fetchByID($0, id: "NONEXISTENT") }
        XCTAssertNil(channel)
    }

    func testFetchByName() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "engineering")
        }
        let channel = try db.read { try ChannelQueries.fetchByName($0, name: "engineering") }
        XCTAssertNotNil(channel)
        XCTAssertEqual(channel?.id, "C001")
    }

    func testFetchWatched() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "alpha")
            try TestDatabase.insertChannel(db, id: "C002", name: "beta")
            try TestDatabase.insertChannel(db, id: "C003", name: "gamma")
            try TestDatabase.insertWatchItem(db, entityType: "channel", entityID: "C001")
            try TestDatabase.insertWatchItem(db, entityType: "channel", entityID: "C003")
        }
        let watched = try db.read { try ChannelQueries.fetchWatched($0) }
        XCTAssertEqual(watched.count, 2)
        XCTAssertEqual(Set(watched.map(\.name)), ["alpha", "gamma"])
    }
}

final class MessageQueryTests: XCTestCase {

    func testFetchByChannel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            for i in 0..<5 {
                try TestDatabase.insertMessage(db, channelID: "C001", ts: "170000000\(i).000100", text: "Message \(i)")
            }
        }
        let messages = try db.read { try MessageQueries.fetchByChannel($0, channelID: "C001", limit: 3) }
        XCTAssertEqual(messages.count, 3)
        // Should be ordered by ts_unix ASC (display order)
        XCTAssertTrue(messages[0].tsUnix <= messages[1].tsUnix)
    }

    func testFetchByChannelOffset() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            for i in 0..<10 {
                try TestDatabase.insertMessage(db, channelID: "C001", ts: "170000000\(i).000100")
            }
        }
        let page1 = try db.read { try MessageQueries.fetchByChannel($0, channelID: "C001", limit: 5, offset: 0) }
        let page2 = try db.read { try MessageQueries.fetchByChannel($0, channelID: "C001", limit: 5, offset: 5) }
        XCTAssertEqual(page1.count, 5)
        XCTAssertEqual(page2.count, 5)
        // Pages should not overlap
        let ids1 = Set(page1.map(\.ts))
        let ids2 = Set(page2.map(\.ts))
        XCTAssertTrue(ids1.isDisjoint(with: ids2))
    }

    func testFetchByTimeRange() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000000.000100", text: "Before")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700050000.000100", text: "During")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700100000.000100", text: "After")
        }
        let messages = try db.read {
            try MessageQueries.fetchByTimeRange($0, channelID: "C001", from: 1700040000, to: 1700060000)
        }
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages[0].text, "During")
    }

    func testFetchThreadReplies() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000000.000100", text: "Parent", threadTS: nil, replyCount: 2)
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", text: "Reply 1", threadTS: "1700000000.000100")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", text: "Reply 2", threadTS: "1700000000.000100")
        }
        let replies = try db.read {
            try MessageQueries.fetchThreadReplies($0, channelID: "C001", threadTS: "1700000000.000100")
        }
        XCTAssertEqual(replies.count, 2)
        XCTAssertEqual(replies[0].text, "Reply 1")
        XCTAssertEqual(replies[1].text, "Reply 2")
    }

    func testCountByChannel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000003.000100")
        }
        let count = try db.read { try MessageQueries.countByChannel($0, channelID: "C001") }
        XCTAssertEqual(count, 3)
    }

    func testFetchRecentWatched() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice")
            try TestDatabase.insertWatchItem(db, entityType: "channel", entityID: "C001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", userID: "U001", text: "Hello")
        }
        let messages = try db.read {
            try MessageQueries.fetchRecentWatched($0, sinceUnix: 1700000000)
        }
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages[0].channelName, "general")
        XCTAssertEqual(messages[0].userName, "Alice")
    }
}

final class DigestQueryTests: XCTestCase {

    func testFetchAll() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, type: "channel")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily")
        }
        let digests = try db.read { try DigestQueries.fetchAll($0) }
        XCTAssertEqual(digests.count, 2)
    }

    func testFetchByType() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, type: "channel")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700086400, periodTo: 1700172800, type: "weekly")
        }
        let daily = try db.read { try DigestQueries.fetchAll($0, type: "daily") }
        XCTAssertEqual(daily.count, 1)
        XCTAssertEqual(daily[0].type, "daily")
    }

    func testFetchByChannelID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400)
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700000000, periodTo: 1700086400)
        }
        let digests = try db.read { try DigestQueries.fetchAll($0, channelID: "C001") }
        XCTAssertEqual(digests.count, 1)
        XCTAssertEqual(digests[0].channelID, "C001")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, summary: "First digest")
        }
        let digest = try db.read { try DigestQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(digest)
        XCTAssertEqual(digest?.summary, "First digest")
    }

    func testFetchLatest() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // Use explicit created_at to ensure ordering
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model, created_at)
                VALUES ('C001', 1700000000, 1700086400, 'channel', 'Old', 10, 'haiku', '2025-01-01T00:00:00Z')
                """)
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model, created_at)
                VALUES ('C002', 1700086400, 1700172800, 'channel', 'New', 10, 'haiku', '2025-01-02T00:00:00Z')
                """)
        }
        let latest = try db.read { try DigestQueries.fetchLatest($0, type: "channel") }
        XCTAssertEqual(latest?.summary, "New")
    }

    func testFetchWithDecisions() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, decisions: "[]")
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700000000, periodTo: 1700086400,
                                          decisions: #"[{"text":"Use Go"}]"#)
        }
        let withDecisions = try db.read { try DigestQueries.fetchWithDecisions($0) }
        XCTAssertEqual(withDecisions.count, 1)
        XCTAssertEqual(withDecisions[0].channelID, "C002")
    }

    func testFetchNewSince() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400,
                                          decisions: #"[{"text":"Old"}]"#)
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700086400, periodTo: 1700172800,
                                          decisions: #"[{"text":"New"}]"#)
        }
        let newer = try db.read { try DigestQueries.fetchNewSince($0, afterID: 1) }
        XCTAssertEqual(newer.count, 1)
        XCTAssertEqual(newer[0].channelID, "C002")
    }

    func testMaxID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400)
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700000000, periodTo: 1700086400)
            try TestDatabase.insertDigest(db, channelID: "C003", periodFrom: 1700000000, periodTo: 1700086400)
        }
        let maxID = try db.read { try DigestQueries.maxID($0) }
        XCTAssertEqual(maxID, 3)
    }

    func testMaxIDEmpty() throws {
        let db = try TestDatabase.create()
        let maxID = try db.read { try DigestQueries.maxID($0) }
        XCTAssertEqual(maxID, 0)
    }
}

final class UserQueryTests: XCTestCase {

    func testFetchAllActiveOnly() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "active")
            try TestDatabase.insertUser(db, id: "U002", name: "deleted", isDeleted: true)
        }
        let active = try db.read { try UserQueries.fetchAll($0, activeOnly: true) }
        XCTAssertEqual(active.count, 1)
        XCTAssertEqual(active[0].id, "U001")
    }

    func testFetchAllIncludingDeleted() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "active")
            try TestDatabase.insertUser(db, id: "U002", name: "deleted", isDeleted: true)
        }
        let all = try db.read { try UserQueries.fetchAll($0, activeOnly: false) }
        XCTAssertEqual(all.count, 2)
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: "U123", name: "alice", displayName: "Alice")
        }
        let user = try db.read { try UserQueries.fetchByID($0, id: "U123") }
        XCTAssertEqual(user?.displayName, "Alice")
    }

    func testFetchByName() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "alice")
        }
        let user = try db.read { try UserQueries.fetchByName($0, name: "alice") }
        XCTAssertNotNil(user)
    }

    func testFetchDisplayName() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice Wonder")
        }
        let name = try db.read { try UserQueries.fetchDisplayName($0, forID: "U001") }
        XCTAssertEqual(name, "Alice Wonder")
    }

    func testFetchDisplayNameFallback() throws {
        let db = try TestDatabase.create()
        let name = try db.read { try UserQueries.fetchDisplayName($0, forID: "U999") }
        XCTAssertEqual(name, "U999") // Falls back to ID
    }
}

final class WatchQueryTests: XCTestCase {

    func testAddAndFetch() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try WatchQueries.add(db, entityType: "channel", entityID: "C001", entityName: "general")
        }
        let items = try db.read { try WatchQueries.fetchAll($0) }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].entityType, "channel")
        XCTAssertEqual(items[0].entityID, "C001")
        XCTAssertEqual(items[0].entityName, "general")
    }

    func testIsWatched() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try WatchQueries.add(db, entityType: "channel", entityID: "C001", entityName: "general")
        }
        let watched = try db.read { try WatchQueries.isWatched($0, entityType: "channel", entityID: "C001") }
        let notWatched = try db.read { try WatchQueries.isWatched($0, entityType: "channel", entityID: "C999") }
        XCTAssertTrue(watched)
        XCTAssertFalse(notWatched)
    }

    func testRemove() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try WatchQueries.add(db, entityType: "channel", entityID: "C001", entityName: "general")
            try WatchQueries.remove(db, entityType: "channel", entityID: "C001")
        }
        let watched = try db.read { try WatchQueries.isWatched($0, entityType: "channel", entityID: "C001") }
        XCTAssertFalse(watched)
    }

    func testAddReplace() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try WatchQueries.add(db, entityType: "channel", entityID: "C001", entityName: "general", priority: "normal")
            try WatchQueries.add(db, entityType: "channel", entityID: "C001", entityName: "general", priority: "high")
        }
        let items = try db.read { try WatchQueries.fetchAll($0) }
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].priority, "high")
    }
}

final class StatsQueryTests: XCTestCase {

    func testDashboardStats() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertChannel(db, id: "C002")
            try TestDatabase.insertUser(db, id: "U001")
            try TestDatabase.insertUser(db, id: "U002", isDeleted: true) // Should not count
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100")
            try TestDatabase.insertDigest(db)
        }
        let stats = try db.read { try StatsQueries.fetchDashboardStats($0) }
        XCTAssertEqual(stats.channelCount, 2)
        XCTAssertEqual(stats.userCount, 1) // Only non-deleted
        XCTAssertEqual(stats.messageCount, 1)
        XCTAssertEqual(stats.digestCount, 1)
    }

    func testTopChannels() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "busy")
            try TestDatabase.insertChannel(db, id: "C002", name: "quiet")
            for i in 0..<5 {
                try TestDatabase.insertMessage(db, channelID: "C001", ts: "170000000\(i).000100")
            }
            try TestDatabase.insertMessage(db, channelID: "C002", ts: "1700000010.000100")
        }
        let top = try db.read { try StatsQueries.fetchTopChannels($0, limit: 2) }
        XCTAssertEqual(top.count, 2)
        XCTAssertEqual(top[0].name, "busy")
        XCTAssertEqual(top[0].count, 5)
        XCTAssertEqual(top[1].name, "quiet")
        XCTAssertEqual(top[1].count, 1)
    }

    func testSyncSummary() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", isMember: true)
            try TestDatabase.insertChannel(db, id: "C002", isMember: true)
            try TestDatabase.insertChannel(db, id: "C003", isMember: false) // Not a member
            try TestDatabase.insertSyncState(db, channelID: "C001", isInitialSyncComplete: true, messagesSynced: 100)
            try TestDatabase.insertSyncState(db, channelID: "C002", isInitialSyncComplete: false, messagesSynced: 20)
        }
        let summary = try db.read { try StatsQueries.fetchSyncSummary($0) }
        XCTAssertEqual(summary.total, 2) // Only members
        XCTAssertEqual(summary.synced, 1) // Only completed
        XCTAssertEqual(summary.totalMessages, 120) // Sum of all
    }
}

final class UserAnalysisQueryTests: XCTestCase {

    func testFetchForWindow() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200, messageCount: 30)
            try TestDatabase.insertUserAnalysis(db, userID: "U003", periodFrom: 200, periodTo: 300, messageCount: 10) // Different window
        }
        let analyses = try db.read { try UserAnalysisQueries.fetchForWindow($0, periodFrom: 100, periodTo: 200) }
        XCTAssertEqual(analyses.count, 2)
        // Ordered by message_count DESC
        XCTAssertEqual(analyses[0].userID, "U001")
        XCTAssertEqual(analyses[1].userID, "U002")
    }

    func testFetchLatestWindow() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 200, periodTo: 300)
        }
        let window = try db.read { try UserAnalysisQueries.fetchLatestWindow($0) }
        XCTAssertNotNil(window)
        XCTAssertEqual(window?.from, 200)
        XCTAssertEqual(window?.to, 300)
    }

    func testFetchLatestWindowEmpty() throws {
        let db = try TestDatabase.create()
        let window = try db.read { try UserAnalysisQueries.fetchLatestWindow($0) }
        XCTAssertNil(window)
    }

    func testFetchLatest() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 200, periodTo: 300)
            try TestDatabase.insertUserAnalysis(db, userID: "U003", periodFrom: 200, periodTo: 300)
        }
        let latest = try db.read { try UserAnalysisQueries.fetchLatest($0) }
        XCTAssertEqual(latest.count, 2)
        XCTAssertTrue(latest.allSatisfy { $0.periodFrom == 200 && $0.periodTo == 300 })
    }

    func testFetchByUser() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 200, periodTo: 300)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200)
        }
        let analyses = try db.read { try UserAnalysisQueries.fetchByUser($0, userID: "U001") }
        XCTAssertEqual(analyses.count, 2)
        XCTAssertTrue(analyses.allSatisfy { $0.userID == "U001" })
    }

    func testFetchAvailableWindows() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200) // Same window
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 200, periodTo: 300)
        }
        let windows = try db.read { try UserAnalysisQueries.fetchAvailableWindows($0) }
        XCTAssertEqual(windows.count, 2) // DISTINCT
        // Ordered by period_to DESC
        XCTAssertEqual(windows[0].to, 300)
        XCTAssertEqual(windows[1].to, 200)
    }

    func testCountRedFlags() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200,
                                                 redFlags: #"["Low engagement"]"#)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200,
                                                 redFlags: "[]")
            try TestDatabase.insertUserAnalysis(db, userID: "U003", periodFrom: 100, periodTo: 200,
                                                 redFlags: #"["Issue 1","Issue 2"]"#)
        }
        let count = try db.read { try UserAnalysisQueries.countRedFlags($0) }
        XCTAssertEqual(count, 2) // U001 and U003 have red flags
    }

    func testCountRedFlagsEmpty() throws {
        let db = try TestDatabase.create()
        let count = try db.read { try UserAnalysisQueries.countRedFlags($0) }
        XCTAssertEqual(count, 0)
    }
}

final class WorkspaceQueryTests: XCTestCase {

    func testFetchWorkspace() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertWorkspace(db, name: "My Corp")
        }
        let workspace = try db.read { try WorkspaceQueries.fetchWorkspace($0) }
        XCTAssertNotNil(workspace)
        XCTAssertEqual(workspace?.name, "My Corp")
    }

    func testFetchWorkspaceEmpty() throws {
        let db = try TestDatabase.create()
        let workspace = try db.read { try WorkspaceQueries.fetchWorkspace($0) }
        XCTAssertNil(workspace)
    }

    func testFetchStats() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertUser(db, id: "U001")
        }
        let stats = try db.read { try WorkspaceQueries.fetchStats($0) }
        XCTAssertEqual(stats.channelCount, 1)
        XCTAssertEqual(stats.userCount, 1)
        XCTAssertEqual(stats.messageCount, 0)
    }
}

final class SearchQueryTests: XCTestCase {

    func testSearchFTS5Available() throws {
        // Verify FTS5 trigger populates the index
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", text: "Hello world from the team")
        }
        let count = try db.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM messages_fts")
        }
        XCTAssertEqual(count, 1)
    }

    func testSearchFTSDeletedNotIndexed() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", text: "Visible message")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", text: "Deleted message", isDeleted: true)
        }
        let count = try db.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM messages_fts")
        }
        XCTAssertEqual(count, 1) // Only non-deleted is indexed
    }

    func testSearchFTSEmptyTextNotIndexed() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", text: "")
        }
        let count = try db.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM messages_fts")
        }
        XCTAssertEqual(count, 0) // Empty text not indexed
    }

    func testSearchEmptyQuery() throws {
        let db = try TestDatabase.create()
        let results = try db.read { try SearchQueries.search($0, query: "") }
        XCTAssertTrue(results.isEmpty)
    }

    func testSearchOnlyOperators() throws {
        let db = try TestDatabase.create()
        let results = try db.read { try SearchQueries.search($0, query: "AND OR NOT") }
        XCTAssertTrue(results.isEmpty) // Sanitized to empty
    }
}

// MARK: - ChatConversationQueries

final class ChatConversationQueryTests: XCTestCase {

    func testEnsureTableAndCreate() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try ChatConversationQueries.ensureTable(db)
            let conv = try ChatConversationQueries.create(db, title: "My Chat")
            XCTAssertEqual(conv.title, "My Chat")
            XCTAssertNil(conv.sessionID)
            XCTAssertTrue(conv.createdAt > 0)
        }
    }

    func testFetchAll() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try ChatConversationQueries.ensureTable(db)
            try ChatConversationQueries.create(db, title: "First")
            try ChatConversationQueries.create(db, title: "Second")
        }
        let all = try db.read { try ChatConversationQueries.fetchAll($0) }
        XCTAssertEqual(all.count, 2)
        // Ordered by updated_at DESC — Second was created last
        XCTAssertEqual(all[0].title, "Second")
        XCTAssertEqual(all[1].title, "First")
    }

    func testSearch() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try ChatConversationQueries.ensureTable(db)
            try ChatConversationQueries.create(db, title: "Slack discussion")
            try ChatConversationQueries.create(db, title: "Meeting notes")
        }
        let results = try db.read { try ChatConversationQueries.search($0, query: "slack") }
        XCTAssertEqual(results.count, 1)
        XCTAssertEqual(results[0].title, "Slack discussion")
    }

    func testUpdateTitle() throws {
        let db = try TestDatabase.create()
        let conv = try db.write { db -> ChatConversation in
            try ChatConversationQueries.ensureTable(db)
            return try ChatConversationQueries.create(db, title: "Old Title")
        }
        try db.write { db in
            try ChatConversationQueries.updateTitle(db, id: conv.id, title: "New Title")
        }
        let updated = try db.read { try ChatConversationQueries.fetchByID($0, id: conv.id) }
        XCTAssertEqual(updated?.title, "New Title")
    }

    func testUpdateSessionID() throws {
        let db = try TestDatabase.create()
        let conv = try db.write { db -> ChatConversation in
            try ChatConversationQueries.ensureTable(db)
            return try ChatConversationQueries.create(db, title: "Test")
        }
        XCTAssertNil(conv.sessionID)

        try db.write { db in
            try ChatConversationQueries.updateSessionID(db, id: conv.id, sessionID: "sess-123")
        }
        let updated = try db.read { try ChatConversationQueries.fetchByID($0, id: conv.id) }
        XCTAssertEqual(updated?.sessionID, "sess-123")
    }

    func testDelete() throws {
        let db = try TestDatabase.create()
        let conv = try db.write { db -> ChatConversation in
            try ChatConversationQueries.ensureTable(db)
            return try ChatConversationQueries.create(db, title: "Doomed")
        }
        try db.write { db in
            try ChatConversationQueries.delete(db, id: conv.id)
        }
        let all = try db.read { try ChatConversationQueries.fetchAll($0) }
        XCTAssertTrue(all.isEmpty)
    }

    func testTouch() throws {
        let db = try TestDatabase.create()
        let conv = try db.write { db -> ChatConversation in
            try ChatConversationQueries.ensureTable(db)
            return try ChatConversationQueries.create(db, title: "Test")
        }
        let originalUpdated = conv.updatedAt

        // Small delay to ensure timestamp differs
        Thread.sleep(forTimeInterval: 0.05)

        try db.write { db in
            try ChatConversationQueries.touch(db, id: conv.id)
        }
        let updated = try db.read { try ChatConversationQueries.fetchByID($0, id: conv.id) }
        XCTAssertTrue(updated!.updatedAt > originalUpdated)
    }

    func testDisplayTitle() throws {
        let db = try TestDatabase.create()
        let conv = try db.write { db -> ChatConversation in
            try ChatConversationQueries.ensureTable(db)
            return try ChatConversationQueries.create(db, title: "")
        }
        XCTAssertEqual(conv.displayTitle, "New Chat")

        let conv2 = try db.write { db -> ChatConversation in
            return try ChatConversationQueries.create(db, title: "My Chat")
        }
        XCTAssertEqual(conv2.displayTitle, "My Chat")
    }
}
