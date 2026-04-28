import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TargetPrefillBuilderTests: XCTestCase {

    // MARK: - fromSubItem

    func testFromSubItem_BasicShape() {
        let parent = Self.makeTarget(
            id: 42,
            text: "Ship the rewrite",
            intent: "Unblock Q2 launch",
            subItems: [
                TargetSubItem(text: "Draft RFC", done: false),
                TargetSubItem(text: "Review with team", done: false),
                TargetSubItem(text: "Implement", done: false)
            ]
        )
        let subItem = parent.decodedSubItems[0]
        let prefill = TargetPrefillBuilder.fromSubItem(parent: parent, subItem: subItem, index: 0)

        XCTAssertEqual(prefill.text, "Draft RFC")
        XCTAssertEqual(prefill.sourceType, "promoted_subitem")
        XCTAssertEqual(prefill.sourceID, "42:0")
        XCTAssertEqual(prefill.parentID, 42)
        XCTAssertTrue(prefill.intent.contains("Sub-target of #42"))
        XCTAssertTrue(prefill.intent.contains("«Ship the rewrite»"))
        XCTAssertTrue(prefill.intent.contains("Unblock Q2 launch"))
        XCTAssertTrue(prefill.intent.contains("Review with team"))
        XCTAssertTrue(prefill.intent.contains("Implement"))
        XCTAssertTrue(prefill.secondaryLinks.isEmpty)
    }

    func testFromSubItem_NoSiblings_NoIntent() {
        let parent = Self.makeTarget(
            id: 7,
            text: "Lone target",
            intent: "",
            subItems: [TargetSubItem(text: "Only one", done: false)]
        )
        let subItem = parent.decodedSubItems[0]
        let prefill = TargetPrefillBuilder.fromSubItem(parent: parent, subItem: subItem, index: 0)

        XCTAssertTrue(prefill.intent.contains("Sub-target of #7 «Lone target»."))
        XCTAssertFalse(prefill.intent.contains("Parent context:"))
        XCTAssertFalse(prefill.intent.contains("Sibling sub-items:"))
    }

    // MARK: - fromTrack

    func testFromTrack_HappyPath() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertChannel(db, id: "C002", name: "engineering")
            try TestDatabase.insertTrack(
                db,
                text: "Migrate auth service",
                context: "We need to swap the legacy IDP for the new one.",
                priority: "high",
                channelIDs: #"["C001","C002"]"#,
                blocking: "Waiting on infra",
                decisionSummary: "Going with provider X"
            )
        }
        let track = try await mgr.dbPool.read { db in
            try XCTUnwrap(try Track.fetchOne(db, sql: "SELECT * FROM tracks WHERE id = 1"))
        }
        let prefill = try await TargetPrefillBuilder.fromTrack(track, db: mgr)

        XCTAssertEqual(prefill.text, "Migrate auth service")
        XCTAssertEqual(prefill.sourceType, "track")
        XCTAssertEqual(prefill.sourceID, "1")
        XCTAssertNil(prefill.parentID)
        XCTAssertTrue(prefill.intent.contains("We need to swap the legacy IDP"))
        XCTAssertTrue(prefill.intent.contains("Decision: Going with provider X"))
        XCTAssertTrue(prefill.intent.contains("Blocking: Waiting on infra"))
        XCTAssertTrue(prefill.intent.contains("In channels: #general, #engineering"))
        XCTAssertEqual(prefill.secondaryLinks.count, 2)
        XCTAssertEqual(prefill.secondaryLinks[0].externalRef, "slack:C001")
        XCTAssertEqual(prefill.secondaryLinks[0].relation, "related")
    }

    func testFromTrack_UnknownChannelFallsBackToID() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertTrack(
                db,
                text: "Track with orphan channel",
                channelIDs: #"["C999"]"#
            )
        }
        let track = try await mgr.dbPool.read { db in
            try XCTUnwrap(try Track.fetchOne(db, sql: "SELECT * FROM tracks WHERE id = 1"))
        }
        let prefill = try await TargetPrefillBuilder.fromTrack(track, db: mgr)
        XCTAssertTrue(prefill.intent.contains("In channels: #C999"))
        XCTAssertEqual(prefill.secondaryLinks.first?.externalRef, "slack:C999")
    }

    // MARK: - fromDigest

    func testFromDigest_WithTopic() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertChannel(db, id: "C100", name: "deals")
            try TestDatabase.insertDigest(
                db,
                channelID: "C100",
                summary: "Channel-level summary"
            )
            try TestDatabase.insertDigestTopic(
                db,
                digestID: 1,
                idx: 0,
                title: "Q2 pipeline review",
                summary: "Three deals at risk; Acme is committed.",
                keyMessages: #"["Acme signed the NDA","Beta wants a 10% discount"]"#
            )
        }
        let digest = try await mgr.dbPool.read { db in
            try XCTUnwrap(try Digest.fetchOne(db, sql: "SELECT * FROM digests WHERE id = 1"))
        }
        let topic = try await mgr.dbPool.read { db in
            try XCTUnwrap(try DigestTopic.fetchOne(db, sql: "SELECT * FROM digest_topics WHERE id = 1"))
        }

        let prefill = try await TargetPrefillBuilder.fromDigest(digest, topic: topic, db: mgr)
        XCTAssertEqual(prefill.text, "Q2 pipeline review")
        XCTAssertEqual(prefill.sourceType, "digest")
        XCTAssertEqual(prefill.sourceID, "1")
        XCTAssertTrue(prefill.intent.contains("From digest in #deals"))
        XCTAssertTrue(prefill.intent.contains("Three deals at risk"))
        XCTAssertTrue(prefill.intent.contains("Acme signed the NDA"))
        XCTAssertEqual(prefill.secondaryLinks, [
            TargetPrefillLink(externalRef: "slack:C100", relation: "related")
        ])
    }

    func testFromDigest_NoTopic_FallsBackToSummary() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertChannel(db, id: "C200", name: "ops")
            try TestDatabase.insertDigest(db, channelID: "C200", summary: "Plain summary")
        }
        let digest = try await mgr.dbPool.read { db in
            try XCTUnwrap(try Digest.fetchOne(db, sql: "SELECT * FROM digests WHERE id = 1"))
        }
        let prefill = try await TargetPrefillBuilder.fromDigest(digest, topic: nil, db: mgr)
        XCTAssertTrue(prefill.text.contains("Plain summary"))
        XCTAssertTrue(prefill.intent.contains("From digest in #ops"))
        XCTAssertTrue(prefill.intent.contains("Plain summary"))
        XCTAssertFalse(prefill.intent.contains("Key messages:"))
    }

    func testFromDigest_UnknownChannelFallsBackToID() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertDigest(db, channelID: "C404", summary: "Orphan digest")
        }
        let digest = try await mgr.dbPool.read { db in
            try XCTUnwrap(try Digest.fetchOne(db, sql: "SELECT * FROM digests WHERE id = 1"))
        }
        let prefill = try await TargetPrefillBuilder.fromDigest(digest, topic: nil, db: mgr)
        XCTAssertTrue(prefill.intent.contains("From digest in #C404"))
    }

    // MARK: - fromInbox

    func testFromInbox_HappyPath() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertUser(db, id: "U010", name: "vlad", displayName: "Vlad", realName: "Vlad K.")
            try TestDatabase.insertChannel(db, id: "C300", name: "team")
            try TestDatabase.insertInboxItem(
                db,
                channelID: "C300",
                senderUserID: "U010",
                triggerType: "mention",
                snippet: "Need your call on the API contract",
                permalink: "https://slack.com/archives/C300/p123",
                aiReason: "Direct ask, blocking external commitment"
            )
        }
        let item = try await mgr.dbPool.read { db in
            try XCTUnwrap(try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1"))
        }

        let prefill = try await TargetPrefillBuilder.fromInbox(item, db: mgr)
        XCTAssertEqual(prefill.text, "Need your call on the API contract")
        XCTAssertEqual(prefill.sourceType, "inbox")
        XCTAssertEqual(prefill.sourceID, String(item.id))
        XCTAssertTrue(prefill.intent.contains("From @Vlad in #team (mention):"))
        XCTAssertTrue(prefill.intent.contains("\"Need your call on the API contract\""))
        XCTAssertTrue(prefill.intent.contains("Why it matters: Direct ask, blocking external commitment"))
        XCTAssertEqual(prefill.secondaryLinks, [
            TargetPrefillLink(externalRef: "slack:https://slack.com/archives/C300/p123", relation: "related")
        ])
    }

    func testFromInbox_NoPermalink_NoAIReason() async throws {
        let mgr = try Self.makeManagerSeededWith { db in
            try TestDatabase.insertUser(db, id: "U011", name: "jane", displayName: "")
            try TestDatabase.insertChannel(db, id: "C301", name: "design")
            try TestDatabase.insertInboxItem(
                db,
                channelID: "C301",
                senderUserID: "U011",
                triggerType: "dm",
                snippet: "ping",
                permalink: ""
            )
        }
        let item = try await mgr.dbPool.read { db in
            try XCTUnwrap(try InboxItem.fetchOne(db, sql: "SELECT * FROM inbox_items LIMIT 1"))
        }
        let prefill = try await TargetPrefillBuilder.fromInbox(item, db: mgr)
        XCTAssertTrue(prefill.secondaryLinks.isEmpty)
        XCTAssertFalse(prefill.intent.contains("Why it matters:"))
    }

    // MARK: - Helpers

    /// Creates a file-backed `DatabaseManager` (DatabasePool requires a path),
    /// applies the schema, runs the seed closure. The OS reaps the temp file.
    static func makeManagerSeededWith(_ seed: (Database) throws -> Void) throws -> DatabaseManager {
        let path = NSTemporaryDirectory() + "twtest_\(UUID().uuidString).db"
        let pool = try DatabasePool(path: path)
        try pool.write { db in
            try db.execute(sql: TestDatabase.schema)
            try seed(db)
        }
        return DatabaseManager(pool: pool)
    }

    static func makeTarget(
        id: Int,
        text: String,
        intent: String = "",
        level: String = "week",
        priority: String = "medium",
        ownership: String = "mine",
        dueDate: String = "",
        subItems: [TargetSubItem] = []
    ) -> Target {
        let subItemsJSON: String = {
            guard !subItems.isEmpty,
                  let data = try? JSONEncoder().encode(subItems),
                  let json = String(data: data, encoding: .utf8) else { return "[]" }
            return json
        }()
        let row: Row = [
            "id": id,
            "text": text,
            "intent": intent,
            "level": level,
            "priority": priority,
            "ownership": ownership,
            "due_date": dueDate,
            "sub_items": subItemsJSON,
            "period_start": "2026-04-20",
            "period_end": "2026-04-26",
            "status": "todo",
            "ball_on": "",
            "snooze_until": "",
            "blocking": "",
            "tags": "[]",
            "notes": "[]",
            "progress": 0.0,
            "source_type": "manual",
            "source_id": "",
            "custom_label": "",
            "created_at": "2026-04-20T00:00:00Z",
            "updated_at": "2026-04-20T00:00:00Z"
        ]
        return Target(row: row)
    }
}
