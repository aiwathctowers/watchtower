import XCTest
import GRDB
@testable import WatchtowerDesktop

final class ChannelStatsTests: XCTestCase {

    // MARK: - ChannelSettings Model

    func testChannelSettingsRoundTrip() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            let settings = ChannelSettings(channelID: "C001", isMutedForLLM: true, isFavorite: false)
            try settings.insert(db)
            let fetched = try XCTUnwrap(ChannelSettings.fetchOne(db, key: "C001"))
            XCTAssertEqual(fetched.channelID, "C001")
            XCTAssertTrue(fetched.isMutedForLLM)
            XCTAssertFalse(fetched.isFavorite)
        }
    }

    // MARK: - ChannelStatsQueries.fetchAll

    func testFetchAllReturnsChannelStats() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertWorkspace(db, id: "T001")
            try TestDatabase.insertChannel(db, id: "C001", name: "general", numMembers: 10)
            try TestDatabase.insertUser(db, id: "U001", name: "alice", isBot: false)
            try TestDatabase.insertUser(db, id: "U002", name: "bot", isBot: true)
            // U001 messages (current user)
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", userID: "U001", text: "hello")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", userID: "U001", text: "hey <@U001>")
            // Bot message
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000003.000100", userID: "U002", text: "bot msg")
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats.count, 1)

        let stat = stats[0]
        XCTAssertEqual(stat.id, "C001")
        XCTAssertEqual(stat.name, "general")
        XCTAssertEqual(stat.totalMessages, 3)
        XCTAssertEqual(stat.userMessages, 2) // messages from U001
        XCTAssertEqual(stat.botMessages, 1)
        XCTAssertEqual(stat.mentionCount, 1) // only <@U001> mentions
        XCTAssertTrue(stat.isMember)
        XCTAssertFalse(stat.isWatched)
        XCTAssertFalse(stat.isMutedForLLM)
        XCTAssertFalse(stat.isFavorite)
    }

    func testFetchAllUserMessagesOnlyCountsCurrentUser() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertUser(db, id: "U001", name: "alice", isBot: false)
            try TestDatabase.insertUser(db, id: "U003", name: "bob", isBot: false)
            // U001 messages
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", userID: "U001", text: "hi")
            // U003 messages (different user)
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", userID: "U003", text: "hello")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000003.000100", userID: "U003", text: "hey")
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats[0].userMessages, 1) // only U001's messages
        XCTAssertEqual(stats[0].totalMessages, 3)
    }

    func testFetchAllMentionsOnlyCountsCurrentUser() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertUser(db, id: "U001", name: "alice", isBot: false)
            // Mention of U001
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", userID: "U001", text: "hey <@U001>")
            // Mention of someone else
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", userID: "U001", text: "cc <@U999>")
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats[0].mentionCount, 1) // only <@U001>
    }

    func testFetchAllIncludesWatchedStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "watched-chan")
            try TestDatabase.insertWatchItem(db, entityType: "channel", entityID: "C001")
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertTrue(stats[0].isWatched)
    }

    func testFetchAllIncludesSettings() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "noisy")
            try db.execute(sql: """
                INSERT INTO channel_settings (channel_id, is_muted_for_llm, is_favorite)
                VALUES ('C001', 1, 1)
                """)
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats.count, 1)
        XCTAssertTrue(stats[0].isMutedForLLM)
        XCTAssertTrue(stats[0].isFavorite)
    }

    // MARK: - Recommendations (matching Go thresholds)

    func testMuteHighBotRatio() {
        // Go: botRatio >= 0.70
        let stat = makeStat(totalMessages: 100, botMessages: 75, botRatio: 0.75)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertEqual(recs.count, 1)
        XCTAssertEqual(recs[0].action, .mute)
        XCTAssertEqual(recs[0].channelID, "C001")
        XCTAssertTrue(recs[0].reason.contains("75%"))
    }

    func testMuteHighVolumeNoParticipation() {
        // Go: total>=50 AND userMsgs==0 AND mentions==0
        let stat = makeStat(totalMessages: 60, userMessages: 0, botRatio: 0.3, mentionCount: 0)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertEqual(recs.count, 1)
        XCTAssertEqual(recs[0].action, .mute)
        XCTAssertTrue(recs[0].reason.contains("no participation"))
    }

    func testMuteSkippedWhenAlreadyMuted() {
        let stat = makeStat(totalMessages: 100, botRatio: 0.9, isMutedForLLM: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .mute })
    }

    func testMuteSkippedWhenFavorite() {
        // Go: skip if already favorite
        let stat = makeStat(totalMessages: 100, botRatio: 0.9, isFavorite: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .mute })
    }

    func testLeaveNoUserMessages() {
        // Go: userMsgs==0, not favorite, not watched, not DM, is member
        let stat = makeStat(type: "public", isMember: true, userMessages: 0)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        let leaveRecs = recs.filter { $0.action == .leave }
        XCTAssertEqual(leaveRecs.count, 1)
        XCTAssertTrue(leaveRecs[0].reason.contains("no messages from you"))
    }

    func testLeaveInactiveChannel() {
        // Go: lastUserActivity > 0 && lastUserActivity < thirtyDaysAgo
        let oldTs = Date().timeIntervalSince1970 - 40 * 86400 // 40 days ago
        let stat = makeStat(type: "public", isMember: true, userMessages: 5, lastUserActivity: oldTs)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        let leaveRecs = recs.filter { $0.action == .leave }
        XCTAssertEqual(leaveRecs.count, 1)
        XCTAssertTrue(leaveRecs[0].reason.contains("inactive"))
    }

    func testLeaveSkippedForDM() {
        let stat = makeStat(type: "dm", isMember: true, userMessages: 0)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .leave })
    }

    func testLeaveSkippedForWatched() {
        let stat = makeStat(type: "public", isMember: true, userMessages: 0, isWatched: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .leave })
    }

    func testLeaveSkippedForFavorite() {
        let stat = makeStat(type: "public", isMember: true, userMessages: 0, isFavorite: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .leave })
    }

    func testFavoriteHighEngagement() {
        // Go: userMsgs>=10 AND mentions>=3
        let stat = makeStat(userMessages: 15, mentionCount: 5)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        let favRecs = recs.filter { $0.action == .favorite }
        XCTAssertEqual(favRecs.count, 1)
        XCTAssertTrue(favRecs[0].reason.contains("engagement"))
    }

    func testFavoriteWatchedChannel() {
        // Go: isWatched AND !favorite
        let stat = makeStat(isWatched: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        let favRecs = recs.filter { $0.action == .favorite }
        XCTAssertEqual(favRecs.count, 1)
        XCTAssertTrue(favRecs[0].reason.contains("watched"))
    }

    func testFavoriteSkippedWhenAlreadyFavorite() {
        let stat = makeStat(userMessages: 15, mentionCount: 5, isFavorite: true)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        XCTAssertFalse(recs.contains { $0.action == .favorite })
    }

    func testNoDoubleRecommendation() {
        // Go: continue after mute → no leave for same channel
        let stat = makeStat(isMember: true, totalMessages: 100, userMessages: 0, botRatio: 0.8)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat])
        // Should only get mute, not also leave
        XCTAssertEqual(recs.count, 1)
        XCTAssertEqual(recs[0].action, .mute)
    }

    // MARK: - Toggle Actions

    func testToggleMuteForLLM() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "test")
            try ChannelStatsQueries.toggleMuteForLLM(db, channelID: "C001", muted: true)
        }
        let settings = try XCTUnwrap(db.read { try ChannelSettings.fetchOne($0, key: "C001") })
        XCTAssertTrue(settings.isMutedForLLM)
        XCTAssertFalse(settings.isFavorite)

        try db.write { try ChannelStatsQueries.toggleMuteForLLM($0, channelID: "C001", muted: false) }
        let updated = try XCTUnwrap(db.read { try ChannelSettings.fetchOne($0, key: "C001") })
        XCTAssertFalse(updated.isMutedForLLM)
    }

    func testToggleFavorite() throws {
        let db = try TestDatabase.create()
        try db.write { try ChannelStatsQueries.toggleFavorite($0, channelID: "C001", favorite: true) }
        let settings = try XCTUnwrap(db.read { try ChannelSettings.fetchOne($0, key: "C001") })
        XCTAssertTrue(settings.isFavorite)
    }

    func testTogglePreservesOtherSetting() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try ChannelStatsQueries.toggleMuteForLLM(db, channelID: "C001", muted: true)
            try ChannelStatsQueries.toggleFavorite(db, channelID: "C001", favorite: true)
        }
        let settings = try XCTUnwrap(db.read { try ChannelSettings.fetchOne($0, key: "C001") })
        XCTAssertTrue(settings.isMutedForLLM)
        XCTAssertTrue(settings.isFavorite)
    }

    // MARK: - Workspace / Current User

    func testFetchWorkspaceTeamID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertWorkspace($0, id: "T123") }
        let teamID = try db.read { try ChannelStatsQueries.fetchWorkspaceTeamID($0) }
        XCTAssertEqual(teamID, "T123")
    }

    func testFetchCurrentUserID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO workspace (id, name, domain, current_user_id)
                VALUES ('T001', 'Test', 'test', 'U042')
                """)
        }
        let uid = try db.read { try ChannelStatsQueries.fetchCurrentUserID($0) }
        XCTAssertEqual(uid, "U042")
    }

    // MARK: - ChannelStat Model

    func testLastActivityDaysAgo() {
        let recentTs = Date().timeIntervalSince1970 - 86400 * 3.5
        let stat = makeStat(lastActivity: recentTs)
        XCTAssertEqual(stat.lastActivityDaysAgo, 3)

        let noActivity = makeStat(lastActivity: 0)
        XCTAssertNil(noActivity.lastActivityDaysAgo)
    }

    // MARK: - ChannelRecommendation Model

    func testChannelRecommendationIdentifiable() {
        let rec = ChannelRecommendation(
            channelID: "C001",
            channelName: "alerts",
            action: .mute,
            reason: "High bot ratio"
        )
        XCTAssertEqual(rec.id, "mute-C001")
        XCTAssertEqual(rec.channelID, "C001")
    }

    // MARK: - Digest Info

    func testFetchAllIncludesDigestInfo() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertWorkspace(db, id: "T001")
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertUser(db, id: "U001", name: "alice", isBot: false)
            // Messages with known timestamps
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100", userID: "U001", text: "hi")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000002.000100", userID: "U001", text: "hey")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000003.000100", userID: "U001", text: "yo")
            // A channel digest covering first two messages
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, created_at)
                VALUES ('C001', 1700000000, 1700000002.5, 'channel', 'test summary', 2, '2024-01-15T10:00:00Z')
                """)
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats.count, 1)
        XCTAssertEqual(stats[0].digestCount, 1)
        XCTAssertEqual(stats[0].lastDigestAt, "2024-01-15T10:00:00Z")
        XCTAssertEqual(stats[0].messagesSinceDigest, 1) // ts 1700000003 > period_to 1700000002.5
    }

    func testFetchAllNoDigests() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
        }
        let stats = try db.read { try ChannelStatsQueries.fetchAll($0, currentUserID: "U001") }
        XCTAssertEqual(stats[0].digestCount, 0)
        XCTAssertNil(stats[0].lastDigestAt)
        XCTAssertEqual(stats[0].messagesSinceDigest, 0)
    }

    // MARK: - Digest Status

    func testDigestStatusUpToDate() {
        let stat = makeStat(totalMessages: 10, digestCount: 3, messagesSinceDigest: 0)
        XCTAssertEqual(stat.digestStatus.label, "Up to date")
    }

    func testDigestStatusPending() {
        let stat = makeStat(totalMessages: 10, digestCount: 1, messagesSinceDigest: 5)
        XCTAssertEqual(stat.digestStatus.label, "5 pending")
    }

    func testDigestStatusMuted() {
        let stat = makeStat(totalMessages: 10, isMutedForLLM: true, digestCount: 0)
        XCTAssertEqual(stat.digestStatus.label, "Muted")
    }

    func testDigestStatusTooFewMessages() {
        let stat = makeStat(totalMessages: 3, digestCount: 0)
        XCTAssertEqual(stat.digestStatus.label, "Too few msgs")
    }

    func testDigestStatusNeverProcessed() {
        let stat = makeStat(totalMessages: 20, digestCount: 0)
        XCTAssertEqual(stat.digestStatus.label, "Not processed")
    }

    // MARK: - Value Signals

    func testFetchValueSignals_Empty() throws {
        let db = try TestDatabase.create()
        let signals = try db.read { try ChannelStatsQueries.fetchValueSignals($0) }
        XCTAssertTrue(signals.isEmpty)
    }

    func testFetchValueSignals_Basic() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertWorkspace(db, id: "T001")
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            // Digest with topic that has decisions
            try TestDatabase.insertDigest(
                db,
                channelID: "C001",
                periodFrom: Date().timeIntervalSince1970 - 86400,
                periodTo: Date().timeIntervalSince1970
            )
            try db.execute(sql: """
                INSERT INTO digest_topics (digest_id, idx, title, decisions)
                VALUES (1, 0, 'topic', '[{"text":"decide X"}]')
                """)
            // Track linked to C001
            try TestDatabase.insertTrack(db, channelIDs: "[\"C001\"]")
            // Task via digest
            try TestDatabase.insertTask(db, status: "todo", sourceType: "digest", sourceID: "1")
            // Pending inbox item
            try TestDatabase.insertInboxItem(db, channelID: "C001", status: "pending")
        }
        let signals = try db.read { try ChannelStatsQueries.fetchValueSignals($0) }
        XCTAssertEqual(signals.count, 1)
        let vs = try XCTUnwrap(signals["C001"])
        XCTAssertEqual(vs.decisionCount, 1)
        XCTAssertEqual(vs.activeTrackCount, 1)
        XCTAssertEqual(vs.taskCount, 1)
        XCTAssertEqual(vs.pendingInboxCount, 1)
    }

    func testFetchValueSignals_InboxOnlyPending() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertInboxItem(db, channelID: "C001", messageTS: "1700000001.000100", status: "pending")
            try TestDatabase.insertInboxItem(db, channelID: "C001", messageTS: "1700000002.000100", status: "resolved")
            try TestDatabase.insertInboxItem(db, channelID: "C001", messageTS: "1700000003.000100", status: "dismissed")
        }
        let signals = try db.read { try ChannelStatsQueries.fetchValueSignals($0) }
        XCTAssertEqual(signals["C001"]?.pendingInboxCount, 1)
    }

    func testFetchValueSignals_TrackMultiChannel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "ch1")
            try TestDatabase.insertChannel(db, id: "C002", name: "ch2")
            try TestDatabase.insertTrack(db, channelIDs: "[\"C001\",\"C002\"]")
        }
        let signals = try db.read { try ChannelStatsQueries.fetchValueSignals($0) }
        XCTAssertEqual(signals["C001"]?.activeTrackCount, 1)
        XCTAssertEqual(signals["C002"]?.activeTrackCount, 1)
    }

    // MARK: - Recommendations with Value Signals

    func testMuteBlockedByInbox() {
        let stat = makeStat(totalMessages: 60, userMessages: 0, botRatio: 0.8)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(pendingInboxCount: 1)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        XCTAssertTrue(recs.filter { $0.action == .mute }.isEmpty, "mute should be blocked by pending inbox")
    }

    func testMuteBlockedByTask() {
        let stat = makeStat(totalMessages: 60, userMessages: 0, botRatio: 0.8)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(taskCount: 2)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        XCTAssertTrue(recs.filter { $0.action == .mute }.isEmpty, "mute should be blocked by active tasks")
    }

    func testLeaveBlockedByTrack() {
        let stat = makeStat(type: "public", isMember: true, userMessages: 0)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(activeTrackCount: 1)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        XCTAssertTrue(recs.filter { $0.action == .leave }.isEmpty, "leave should be blocked by active track")
    }

    func testLeaveBlockedByDecisions() {
        let stat = makeStat(type: "public", isMember: true, userMessages: 0)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(decisionCount: 3)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        XCTAssertTrue(recs.filter { $0.action == .leave }.isEmpty, "leave should be blocked by >=3 decisions")
    }

    func testFavoriteBoostedByDecisions() {
        let stat = makeStat(totalMessages: 20, userMessages: 2, mentionCount: 0)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(decisionCount: 5)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        let favRecs = recs.filter { $0.action == .favorite }
        XCTAssertEqual(favRecs.count, 1)
        XCTAssertTrue(favRecs[0].reason.contains("high-value"))
    }

    func testFavoriteBoostedByTracks() {
        let stat = makeStat(totalMessages: 20, userMessages: 2, mentionCount: 0)
        let signals: [String: ChannelValueSignals] = ["C001": makeSignals(activeTrackCount: 2)]
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: signals)
        let favRecs = recs.filter { $0.action == .favorite }
        XCTAssertEqual(favRecs.count, 1)
        XCTAssertTrue(favRecs[0].reason.contains("high-value"))
    }

    func testRecommendationsWithEmptySignals() {
        // Empty signals dict (not nil) — same behavior as before
        let stat = makeStat(totalMessages: 60, userMessages: 0, botRatio: 1.0)
        let recs = ChannelStatsQueries.computeRecommendations(from: [stat], signals: [:])
        XCTAssertEqual(recs.count, 1)
        XCTAssertEqual(recs[0].action, .mute)
    }

    // MARK: - Helpers

    private func makeStat(
        id: String = "C001",
        name: String = "test",
        type: String = "public",
        isArchived: Bool = false,
        isMember: Bool = false,
        numMembers: Int = 5,
        totalMessages: Int = 0,
        userMessages: Int = 0,
        botMessages: Int = 0,
        botRatio: Double = 0,
        mentionCount: Int = 0,
        lastActivity: Double = 0,
        lastUserActivity: Double = 0,
        isMutedForLLM: Bool = false,
        isFavorite: Bool = false,
        isWatched: Bool = false,
        digestCount: Int = 0,
        lastDigestAt: String? = nil,
        messagesSinceDigest: Int = 0
    ) -> ChannelStat {
        var json: [String: Any] = [
            "id": id,
            "name": name,
            "type": type,
            "is_archived": isArchived,
            "is_member": isMember,
            "num_members": numMembers,
            "total_messages": totalMessages,
            "user_messages": userMessages,
            "bot_messages": botMessages,
            "bot_ratio": botRatio,
            "mention_count": mentionCount,
            "last_activity": lastActivity,
            "last_user_activity": lastUserActivity,
            "is_muted_for_llm": isMutedForLLM,
            "is_favorite": isFavorite,
            "is_watched": isWatched,
            "digest_count": digestCount,
            "messages_since_digest": messagesSinceDigest
        ]
        if let digestAt = lastDigestAt {
            json["last_digest_at"] = digestAt
        }
        // swiftlint:disable:next force_try
        let data = try! JSONSerialization.data(withJSONObject: json)
        // swiftlint:disable:next force_try
        return try! JSONDecoder().decode(ChannelStat.self, from: data)
    }

    private func makeSignals(
        channelID: String = "C001",
        decisionCount: Int = 0,
        activeTrackCount: Int = 0,
        taskCount: Int = 0,
        pendingInboxCount: Int = 0
    ) -> ChannelValueSignals {
        let json: [String: Any] = [
            "channel_id": channelID,
            "decision_count": decisionCount,
            "active_track_count": activeTrackCount,
            "task_count": taskCount,
            "pending_inbox_count": pendingInboxCount
        ]
        // swiftlint:disable:next force_try
        let data = try! JSONSerialization.data(withJSONObject: json)
        // swiftlint:disable:next force_try
        return try! JSONDecoder().decode(ChannelValueSignals.self, from: data)
    }
}
