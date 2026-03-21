import XCTest
import GRDB
@testable import WatchtowerDesktop

final class ModelTests: XCTestCase {

    // MARK: - User.bestName

    func testBestNameUsesDisplayName() {
        let user = makeUser(displayName: "Display", realName: "Real", name: "handle")
        XCTAssertEqual(user.bestName, "Display")
    }

    func testBestNameFallsBackToRealName() {
        let user = makeUser(displayName: "", realName: "Real Name", name: "handle")
        XCTAssertEqual(user.bestName, "Real Name")
    }

    func testBestNameFallsBackToName() {
        let user = makeUser(displayName: "", realName: "", name: "handle")
        XCTAssertEqual(user.bestName, "handle")
    }

    func testBestNameFallsBackToID() {
        let user = makeUser(displayName: "", realName: "", name: "")
        XCTAssertEqual(user.bestName, "U001")
    }

    // MARK: - Decision

    func testDecisionResolvedImportanceWithValue() {
        let d = Decision(text: "Do X", by: nil, messageTS: nil, importance: "high")
        XCTAssertEqual(d.resolvedImportance, "high")
    }

    func testDecisionResolvedImportanceNilDefaultsMedium() {
        let d = Decision(text: "Do X", by: nil, messageTS: nil, importance: nil)
        XCTAssertEqual(d.resolvedImportance, "medium")
    }

    func testDecisionEquality() {
        let d1 = Decision(text: "Do X", by: "Alice", messageTS: "123", importance: "high")
        let d2 = Decision(text: "Do X", by: "Alice", messageTS: "123", importance: "high")
        XCTAssertEqual(d1, d2)
    }

    func testDecisionJSONDecoding() throws {
        let json = #"{"text":"Deploy v2","by":"Alice","message_ts":"123.456","importance":"high"}"#
        let d = try JSONDecoder().decode(Decision.self, from: json.data(using: .utf8)!)
        XCTAssertEqual(d.text, "Deploy v2")
        XCTAssertEqual(d.by, "Alice")
        XCTAssertEqual(d.messageTS, "123.456")
        XCTAssertEqual(d.importance, "high")
    }

    func testDecisionJSONDecodingMinimal() throws {
        let json = #"{"text":"Do something"}"#
        let d = try JSONDecoder().decode(Decision.self, from: json.data(using: .utf8)!)
        XCTAssertEqual(d.text, "Do something")
        XCTAssertNil(d.by)
        XCTAssertNil(d.messageTS)
        XCTAssertNil(d.importance)
    }

    // MARK: - DigestTrack (inline JSON from digest)

    func testDigestTrackJSONDecoding() throws {
        let json = #"{"text":"Write tests","assignee":"Bob","status":"pending"}"#
        let a = try JSONDecoder().decode(DigestTrack.self, from: json.data(using: .utf8)!)
        XCTAssertEqual(a.text, "Write tests")
        XCTAssertEqual(a.assignee, "Bob")
        XCTAssertEqual(a.status, "pending")
    }

    func testDigestTrackMinimal() throws {
        let json = #"{"text":"TBD"}"#
        let a = try JSONDecoder().decode(DigestTrack.self, from: json.data(using: .utf8)!)
        XCTAssertEqual(a.text, "TBD")
        XCTAssertNil(a.assignee)
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
        let digest = try db.read { try DigestQueries.fetchAll($0).first! }
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
        let digest = try db.read { try DigestQueries.fetchAll($0).first! }
        XCTAssertTrue(digest.parsedDecisions.isEmpty)
    }

    func testDigestParsedDecisionsInvalidJSON() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, decisions: "not json")
        }
        let digest = try db.read { try DigestQueries.fetchAll($0).first! }
        XCTAssertTrue(digest.parsedDecisions.isEmpty)
    }

    func testDigestParsedTopics() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, topics: #"["API design","Testing"]"#)
        }
        let digest = try db.read { try DigestQueries.fetchAll($0).first! }
        XCTAssertEqual(digest.parsedTopics, ["API design", "Testing"])
    }

    func testDigestParsedTracks() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertDigest(db, tracksJSON: #"[{"text":"Write docs","assignee":"Bob"}]"#)
        }
        let digest = try db.read { try DigestQueries.fetchAll($0).first! }
        XCTAssertEqual(digest.parsedTracks.count, 1)
        XCTAssertEqual(digest.parsedTracks[0].text, "Write docs")
    }

    // MARK: - UserAnalysis

    func testUserAnalysisParsedRedFlags() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, redFlags: #"["Low engagement","Missed deadlines"]"#)
        }
        let analysis = try db.read { db in
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
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
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
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
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
        }
        XCTAssertEqual(analysis.parsedHighlights, ["Great teamwork", "Quick responses"])
    }

    func testUserAnalysisParsedActiveHours() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, activeHoursJSON: #"{"9":12,"10":8,"14":15}"#)
        }
        let analysis = try db.read { db in
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
        }
        XCTAssertEqual(analysis.parsedActiveHours["9"], 12)
        XCTAssertEqual(analysis.parsedActiveHours["14"], 15)
    }

    func testUserAnalysisStyleEmoji() {
        XCTAssertEqual(styleEmoji("driver"), "🚀")
        XCTAssertEqual(styleEmoji("collaborator"), "🤝")
        XCTAssertEqual(styleEmoji("executor"), "⚡")
        XCTAssertEqual(styleEmoji("observer"), "👀")
        XCTAssertEqual(styleEmoji("facilitator"), "🎯")
        XCTAssertEqual(styleEmoji("unknown"), "💬")
        XCTAssertEqual(styleEmoji("Driver"), "🚀") // case insensitive
    }

    func testUserAnalysisPeriodDates() throws {
        let db = try TestDatabase.create()
        let from: Double = 1700000000
        let to: Double = 1700604800
        try db.write { db in
            try TestDatabase.insertUserAnalysis(db, periodFrom: from, periodTo: to)
        }
        let analysis = try db.read { db in
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
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
            try Message.fetchOne(db, sql: "SELECT * FROM messages LIMIT 1")!
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
            try Channel.fetchOne(db, sql: "SELECT * FROM channels LIMIT 1")!
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
            try UserProfile.fetchOne(db, sql: "SELECT * FROM user_profile LIMIT 1")!
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
        let p = try db.read { db in
            try UserProfile.fetchOne(db, sql: "SELECT * FROM user_profile LIMIT 1")!
        }
        XCTAssertEqual(p.role, "")
        XCTAssertEqual(p.decodedReports, [])
        XCTAssertEqual(p.decodedPeers, [])
        XCTAssertEqual(p.decodedStarredChannels, [])
        XCTAssertFalse(p.onboardingDone)
    }

    func testUserProfileInitWithValues() {
        let p = UserProfile(
            slackUserID: "U123",
            role: "IC",
            reports: #"["U1"]"#,
            starredChannels: #"["C1","C2"]"#
        )
        XCTAssertEqual(p.slackUserID, "U123")
        XCTAssertEqual(p.role, "IC")
        XCTAssertEqual(p.decodedReports, ["U1"])
        XCTAssertEqual(p.decodedStarredChannels, ["C1", "C2"])
        XCTAssertEqual(p.decodedPeers, [])
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
            try PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards LIMIT 1")!
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
            try PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards LIMIT 1")!
        }
        XCTAssertTrue(card.isInsufficientData)
    }

    func testPeopleCardStyleEmojis() throws {
        let db = try TestDatabase.create()
        let cases: [(String, String)] = [
            ("driver", "🚀"), ("collaborator", "🤝"), ("executor", "⚡"),
            ("observer", "👀"), ("facilitator", "🎯"), ("other", "💬"),
        ]
        for (i, (style, emoji)) in cases.enumerated() {
            try db.write { db in
                try TestDatabase.insertPeopleCard(
                    db, userID: "U\(i)", communicationStyle: style
                )
            }
            let card = try db.read { db in
                try PeopleCard.fetchOne(db, sql: "SELECT * FROM people_cards WHERE user_id = ?", arguments: ["U\(i)"])!
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
            try PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1")!
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
            try PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1")!
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
            try PeopleCardSummary.fetchOne(db, sql: "SELECT * FROM people_card_summaries LIMIT 1")!
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
    ) -> User {
        // Use GRDB's row-based init via in-memory DB
        let db = try! TestDatabase.create()
        try! db.write { db in
            try TestDatabase.insertUser(db, id: id, name: name, displayName: displayName, realName: realName)
        }
        return try! db.read { db in
            try User.fetchOne(db, sql: "SELECT * FROM users LIMIT 1")!
        }
    }

    private func styleEmoji(_ style: String) -> String {
        let db = try! TestDatabase.create()
        try! db.write { db in
            try TestDatabase.insertUserAnalysis(db, communicationStyle: style)
        }
        return try! db.read { db in
            try UserAnalysis.fetchOne(db, sql: "SELECT * FROM user_analyses LIMIT 1")!
        }.styleEmoji
    }
}
