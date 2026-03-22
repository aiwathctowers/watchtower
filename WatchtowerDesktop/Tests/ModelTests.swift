import XCTest
import GRDB
@testable import WatchtowerDesktop

final class ModelTests: XCTestCase {

    // MARK: - User.bestName

    func testBestNameUsesDisplayName() throws {
        let user = try makeUser(displayName: "Display", realName: "Real", name: "handle")
        XCTAssertEqual(user.bestName, "Display")
    }

    func testBestNameFallsBackToRealName() throws {
        let user = try makeUser(displayName: "", realName: "Real Name", name: "handle")
        XCTAssertEqual(user.bestName, "Real Name")
    }

    func testBestNameFallsBackToName() throws {
        let user = try makeUser(displayName: "", realName: "", name: "handle")
        XCTAssertEqual(user.bestName, "handle")
    }

    func testBestNameFallsBackToID() throws {
        let user = try makeUser(displayName: "", realName: "", name: "")
        XCTAssertEqual(user.bestName, "U001")
    }

    // MARK: - Decision

    func testDecisionResolvedImportanceWithValue() {
        let decision = Decision(text: "Do X", by: nil, messageTS: nil, importance: "high")
        XCTAssertEqual(decision.resolvedImportance, "high")
    }

    func testDecisionResolvedImportanceNilDefaultsMedium() {
        let decision = Decision(text: "Do X", by: nil, messageTS: nil, importance: nil)
        XCTAssertEqual(decision.resolvedImportance, "medium")
    }

    func testDecisionEquality() {
        let d1 = Decision(text: "Do X", by: "Alice", messageTS: "123", importance: "high")
        let d2 = Decision(text: "Do X", by: "Alice", messageTS: "123", importance: "high")
        XCTAssertEqual(d1, d2)
    }

    func testDecisionJSONDecoding() throws {
        let json = #"{"text":"Deploy v2","by":"Alice","message_ts":"123.456","importance":"high"}"#
        let decision = try JSONDecoder().decode(Decision.self, from: try XCTUnwrap(json.data(using: .utf8)))
        XCTAssertEqual(decision.text, "Deploy v2")
        XCTAssertEqual(decision.by, "Alice")
        XCTAssertEqual(decision.messageTS, "123.456")
        XCTAssertEqual(decision.importance, "high")
    }

    func testDecisionJSONDecodingMinimal() throws {
        let json = #"{"text":"Do something"}"#
        let decision = try JSONDecoder().decode(Decision.self, from: try XCTUnwrap(json.data(using: .utf8)))
        XCTAssertEqual(decision.text, "Do something")
        XCTAssertNil(decision.by)
        XCTAssertNil(decision.messageTS)
        XCTAssertNil(decision.importance)
    }

    // MARK: - DigestTrack (inline JSON from digest)

    func testDigestTrackJSONDecoding() throws {
        let json = #"{"text":"Write tests","assignee":"Bob","status":"pending"}"#
        let track = try JSONDecoder().decode(DigestTrack.self, from: try XCTUnwrap(json.data(using: .utf8)))
        XCTAssertEqual(track.text, "Write tests")
        XCTAssertEqual(track.assignee, "Bob")
        XCTAssertEqual(track.status, "pending")
    }

    func testDigestTrackMinimal() throws {
        let json = #"{"text":"TBD"}"#
        let track = try JSONDecoder().decode(DigestTrack.self, from: try XCTUnwrap(json.data(using: .utf8)))
        XCTAssertEqual(track.text, "TBD")
        XCTAssertNil(track.assignee)
    }

    // MARK: - Digest parsed fields

    func testDigestParsedDecisions() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(
                db,
                decisions: #"[{"text":"Use Go","by":"Alice","importance":"high"},{"text":"Deploy Friday"}]"#
            )
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        let decisions = digest.parsedDecisions
        XCTAssertEqual(decisions.count, 2)
        XCTAssertEqual(decisions[0].text, "Use Go")
        XCTAssertEqual(decisions[0].by, "Alice")
        XCTAssertEqual(decisions[1].text, "Deploy Friday")
    }

    func testDigestParsedDecisionsEmptyArray() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, decisions: "[]")
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertTrue(digest.parsedDecisions.isEmpty)
    }

    func testDigestParsedDecisionsInvalidJSON() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, decisions: "not json")
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertTrue(digest.parsedDecisions.isEmpty)
    }

    func testDigestParsedTopics() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, topics: #"["API design","Testing"]"#)
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertEqual(digest.parsedTopics, ["API design", "Testing"])
    }

    func testDigestParsedTracks() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, tracksJSON: #"[{"text":"Write docs","assignee":"Bob"}]"#)
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertEqual(digest.parsedTracks.count, 1)
        XCTAssertEqual(digest.parsedTracks[0].text, "Write docs")
    }

    // MARK: - RunningSummary

    func testRunningSummaryDecoding() throws {
        let json = """
        {
          "active_topics": [
            {
              "topic": "Migration to PostgreSQL",
              "status": "in_progress",
              "started": "2026-03-18",
              "last_update": "2026-03-21",
              "key_participants": ["U123", "U456"],
              "summary": "POC in progress, blocker on legacy API"
            }
          ],
          "recent_decisions": [
            {
              "decision": "Use pgx instead of database/sql",
              "date": "2026-03-20",
              "by": "U123",
              "status": "active"
            }
          ],
          "channel_dynamics": "Active channel",
          "open_questions": ["Need separate service?"],
          "meta": {
            "generated_at": "2026-03-21T10:00:00Z",
            "digest_id": "abc-123",
            "message_count": 47,
            "period": "2026-03-21"
          }
        }
        """
        let data = try XCTUnwrap(json.data(using: .utf8))
        let rs = try JSONDecoder().decode(RunningSummary.self, from: data)
        XCTAssertEqual(rs.activeTopics?.count, 1)
        XCTAssertEqual(rs.activeTopics?[0].topic, "Migration to PostgreSQL")
        XCTAssertEqual(rs.activeTopics?[0].status, "in_progress")
        XCTAssertEqual(rs.activeTopics?[0].keyParticipants, ["U123", "U456"])
        XCTAssertEqual(rs.recentDecisions?.count, 1)
        XCTAssertEqual(rs.recentDecisions?[0].decision, "Use pgx instead of database/sql")
        XCTAssertEqual(rs.channelDynamics, "Active channel")
        XCTAssertEqual(rs.openQuestions, ["Need separate service?"])
        XCTAssertEqual(rs.meta?.messageCount, 47)
    }

    func testRunningSummaryMinimal() throws {
        let json = #"{}"#
        let data = try XCTUnwrap(json.data(using: .utf8))
        let rs = try JSONDecoder().decode(RunningSummary.self, from: data)
        XCTAssertNil(rs.activeTopics)
        XCTAssertNil(rs.recentDecisions)
        XCTAssertNil(rs.channelDynamics)
    }

    func testActiveTopicIdentifiable() throws {
        let json = #"{"topic":"Test","status":"in_progress","started":"2026-03-18"}"#
        let data = try XCTUnwrap(json.data(using: .utf8))
        let topic = try JSONDecoder().decode(ActiveTopic.self, from: data)
        XCTAssertEqual(topic.id, "Test")
        XCTAssertEqual(topic.status, "in_progress")
        XCTAssertEqual(topic.started, "2026-03-18")
    }

    func testDigestParsedRunningSummary() throws {
        let db = try TestDatabase.create()
        let summaryJSON = #"{"active_topics":[{"topic":"Deploy v3","status":"in_progress"}],"channel_dynamics":"Busy"}"#
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model, running_summary)
                VALUES ('C001', 1700000000, 1700086400, 'channel', 'Test', 10, 'haiku', ?)
                """, arguments: [summaryJSON])
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        let rs = try XCTUnwrap(digest.parsedRunningSummary)
        XCTAssertEqual(rs.activeTopics?.count, 1)
        XCTAssertEqual(rs.activeTopics?[0].topic, "Deploy v3")
        XCTAssertEqual(rs.channelDynamics, "Busy")
    }

    func testDigestParsedRunningSummaryNil() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db)
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertNil(digest.parsedRunningSummary)
    }

    func testDigestParsedRunningSummaryInvalidJSON() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model, running_summary)
                VALUES ('C001', 1700000000, 1700086400, 'channel', 'Test', 10, 'haiku', 'not valid json')
                """)
        }
        let digest = try XCTUnwrap(db.read { try DigestQueries.fetchAll($0).first })
        XCTAssertNil(digest.parsedRunningSummary)
    }

    // MARK: - UserAnalysis

    func testUserAnalysisParsedRedFlags() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, redFlags: #"["Low engagement","Missed deadlines"]"#)
        }
        let analysis = try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }
        XCTAssertEqual(analysis.parsedRedFlags, ["Low engagement", "Missed deadlines"])
        XCTAssertTrue(analysis.hasRedFlags)
    }

    func testUserAnalysisNoRedFlags() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, redFlags: "[]")
        }
        let analysis = try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }
        XCTAssertTrue(analysis.parsedRedFlags.isEmpty)
        XCTAssertFalse(analysis.hasRedFlags)
    }

    func testUserAnalysisParsedHighlights() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, highlights: #"["Great teamwork","Quick responses"]"#)
        }
        let analysis = try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }
        XCTAssertEqual(analysis.parsedHighlights, ["Great teamwork", "Quick responses"])
    }

    func testUserAnalysisParsedActiveHours() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, activeHoursJSON: #"{"9":12,"10":8,"14":15}"#)
        }
        let analysis = try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }
        XCTAssertEqual(analysis.parsedActiveHours["9"], 12)
        XCTAssertEqual(analysis.parsedActiveHours["14"], 15)
    }

    func testUserAnalysisStyleEmoji() throws {
        XCTAssertEqual(try styleEmoji("driver"), "🚀")
        XCTAssertEqual(try styleEmoji("collaborator"), "🤝")
        XCTAssertEqual(try styleEmoji("executor"), "⚡")
        XCTAssertEqual(try styleEmoji("observer"), "👀")
        XCTAssertEqual(try styleEmoji("facilitator"), "🎯")
        XCTAssertEqual(try styleEmoji("unknown"), "💬")
        XCTAssertEqual(try styleEmoji("Driver"), "🚀") // case insensitive
    }

    func testUserAnalysisPeriodDates() throws {
        let db = try TestDatabase.create()
        let from: Double = 1700000000
        let to: Double = 1700604800
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, periodFrom: from, periodTo: to)
        }
        let analysis = try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }
        XCTAssertEqual(analysis.periodFromDate.timeIntervalSince1970, from)
        XCTAssertEqual(analysis.periodToDate.timeIntervalSince1970, to)
    }

    // MARK: - Message.id

    func testMessageID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db)
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000000.000100")
        }
        let msg = try db.read { db in
            try XCTUnwrap(Message.fetchOne(db, sql: "SELECT * FROM messages LIMIT 1"))
        }
        XCTAssertEqual(msg.id, "C001_1700000000.000100")
    }

    // MARK: - Channel types

    func testChannelFromDB() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general", type: "public", isMember: true, numMembers: 42)
        }
        let channel = try db.read { db in
            try XCTUnwrap(Channel.fetchOne(db, sql: "SELECT * FROM channels LIMIT 1"))
        }
        XCTAssertEqual(channel.id, "C001")
        XCTAssertEqual(channel.name, "general")
        XCTAssertEqual(channel.type, "public")
        XCTAssertTrue(channel.isMember)
        XCTAssertEqual(channel.numMembers, 42)
    }

    // MARK: - UserProfile

    func testUserProfileFromDB() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertProfile(
                db,
                slackUserID: "U123",
                role: "EM",
                team: "Platform",
                reports: #"["U1","U2"]"#,
                manager: "U000"
            )
        }
        let profile = try db.read { db in
            try XCTUnwrap(UserProfile.fetchOne(db, sql: "SELECT * FROM user_profile LIMIT 1"))
        }
        XCTAssertEqual(profile.slackUserID, "U123")
        XCTAssertEqual(profile.role, "EM")
        XCTAssertEqual(profile.team, "Platform")
        XCTAssertEqual(profile.decodedReports, ["U1", "U2"])
        XCTAssertEqual(profile.manager, "U000")
    }

    func testUserProfileDefaults() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertProfile(db, slackUserID: "U_MIN")
        }
        let profile = try db.read { db in
            try XCTUnwrap(UserProfile.fetchOne(db, sql: "SELECT * FROM user_profile LIMIT 1"))
        }
        XCTAssertEqual(profile.role, "")
        XCTAssertEqual(profile.decodedReports, [])
        XCTAssertEqual(profile.decodedPeers, [])
        XCTAssertEqual(profile.decodedStarredChannels, [])
        XCTAssertFalse(profile.onboardingDone)
    }

    func testUserProfileInitWithValues() {
        let profile = UserProfile(
            slackUserID: "U123",
            role: "IC",
            reports: #"["U1"]"#,
            starredChannels: #"["C1","C2"]"#
        )
        XCTAssertEqual(profile.slackUserID, "U123")
        XCTAssertEqual(profile.role, "IC")
        XCTAssertEqual(profile.decodedReports, ["U1"])
        XCTAssertEqual(profile.decodedStarredChannels, ["C1", "C2"])
        XCTAssertEqual(profile.decodedPeers, [])
    }

    // MARK: - PeopleCard

    func testPeopleCardFromDB() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertPeopleCard(
                db,
                userID: "U001",
                summary: "Active contributor",
                communicationStyle: "driver",
                redFlags: #"["Low engagement"]"#,
                highlights: #"["Great leadership"]"#,
                accomplishments: #"["Shipped v2"]"#,
                tactics: #"["Be direct"]"#,
                status: "active"
            )
        }
        let card = try db.read { db in
            try XCTUnwrap(PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards LIMIT 1"))
        }
        XCTAssertEqual(card.userID, "U001")
        XCTAssertEqual(card.summary, "Active contributor")
        XCTAssertEqual(card.communicationStyle, "driver")
        XCTAssertEqual(card.parsedRedFlags, ["Low engagement"])
        XCTAssertTrue(card.hasRedFlags)
        XCTAssertEqual(card.parsedHighlights, ["Great leadership"])
        XCTAssertEqual(card.parsedAccomplishments, ["Shipped v2"])
        XCTAssertEqual(card.parsedTactics, ["Be direct"])
        XCTAssertFalse(card.isInsufficientData)
        XCTAssertEqual(card.styleEmoji, "🚀")
    }

    func testPeopleCardInsufficientData() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertPeopleCard(db, status: "insufficient_data")
        }
        let card = try db.read { db in
            try XCTUnwrap(PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards LIMIT 1"))
        }
        XCTAssertTrue(card.isInsufficientData)
    }

    func testPeopleCardStyleEmojis() throws {
        let db = try TestDatabase.create()
        let cases: [(String, String)] = [
            ("driver", "🚀"), ("collaborator", "🤝"), ("executor", "⚡"),
            ("observer", "👀"), ("facilitator", "🎯"), ("other", "💬")
        ]
        for (i, (style, emoji)) in cases.enumerated() {
            try db.write { db in
                try TestDatabase.insertPeopleCard(
                    db, userID: "U\(i)", communicationStyle: style
                )
            }
            let card = try db.read { db in
                try XCTUnwrap(PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards WHERE user_id = ?", arguments: ["U\(i)"]))
            }
            XCTAssertEqual(card.styleEmoji, emoji, "Style \(style) should have emoji \(emoji)")
        }
    }

    // MARK: - PeopleCardSummary

    func testPeopleCardSummaryFromDB() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertPeopleCardSummary(
                db,
                summary: "Team is doing great",
                attention: #"["Alice is overloaded","Bob missing meetings"]"#,
                tips: #"["Redistribute tasks","Schedule 1-on-1s"]"#
            )
        }
        let cs = try db.read { db in
            try XCTUnwrap(PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1"))
        }
        XCTAssertEqual(cs.summary, "Team is doing great")
        XCTAssertEqual(cs.parsedAttention, ["Alice is overloaded", "Bob missing meetings"])
        XCTAssertEqual(cs.parsedTips, ["Redistribute tasks", "Schedule 1-on-1s"])
        XCTAssertEqual(cs.model, "haiku")
        XCTAssertEqual(cs.inputTokens, 500)
        XCTAssertEqual(cs.outputTokens, 200)
    }

    func testPeopleCardSummaryEmptyArrays() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertPeopleCardSummary(db, attention: "[]", tips: "[]")
        }
        let cs = try db.read { db in
            try XCTUnwrap(PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1"))
        }
        XCTAssertTrue(cs.parsedAttention.isEmpty)
        XCTAssertTrue(cs.parsedTips.isEmpty)
    }

    func testPeopleCardSummaryInvalidJSON() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertPeopleCardSummary(db, attention: "not json", tips: "invalid")
        }
        let cs = try db.read { db in
            try XCTUnwrap(PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1"))
        }
        XCTAssertTrue(cs.parsedAttention.isEmpty)
        XCTAssertTrue(cs.parsedTips.isEmpty)
    }

    // MARK: - Helpers

    private func makeUser(
        id: String = "U001",
        displayName: String = "",
        realName: String = "",
        name: String = ""
    ) throws -> User {
        // Use GRDB's row-based init via in-memory DB
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUser(db, id: id, name: name, displayName: displayName, realName: realName)
        }
        return try db.read { db in
            try XCTUnwrap(User.fetchOne(db, sql: "SELECT * FROM users LIMIT 1"))
        }
    }

    private func styleEmoji(_ style: String) throws -> String {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, communicationStyle: style)
        }
        return try db.read { db in
            try XCTUnwrap(UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1"))
        }.styleEmoji
    }
}
