import XCTest
import GRDB
@testable import WatchtowerDesktop

/// Guard tests for TRACKS-06 — track state history.
/// See docs/inventory/tracks.md.
final class TrackStateQueriesTests: XCTestCase {

    // BEHAVIOR TRACKS-06: fetchByTrackID returns rows in DESC order, newest first.
    func test_TRACKS_06_fetchByTrackID_returnsDescendingOrder() throws {
        let dbq = try TestDatabase.create()
        try dbq.write { db in
            try TestDatabase.insertTrack(db, text: "task")
            try insertTrackState(db, trackID: 1, text: "v1", priority: "low",
                                 source: "manual", createdAt: "2026-04-01T10:00:00Z")
            try insertTrackState(db, trackID: 1, text: "v2", priority: "medium",
                                 source: "extraction", createdAt: "2026-04-02T10:00:00Z")
            try insertTrackState(db, trackID: 1, text: "v3", priority: "high",
                                 source: "manual", createdAt: "2026-04-03T10:00:00Z")
        }

        let states = try dbq.read { db in
            try TrackStateQueries.fetchByTrackID(db, trackID: 1)
        }
        XCTAssertEqual(states.count, 3)
        XCTAssertEqual(states[0].text, "v3")
        XCTAssertEqual(states[1].text, "v2")
        XCTAssertEqual(states[2].text, "v1")
    }

    // BEHAVIOR TRACKS-06: a track without history returns an empty array, not nil.
    func test_TRACKS_06_fetchByTrackID_emptyForNewTrack() throws {
        let dbq = try TestDatabase.create()
        try dbq.write { db in
            try TestDatabase.insertTrack(db, text: "fresh")
        }
        let states = try dbq.read { db in
            try TrackStateQueries.fetchByTrackID(db, trackID: 1)
        }
        XCTAssertEqual(states, [])
    }

    // BEHAVIOR TRACKS-06: TrackState decodes all snapshot fields correctly.
    func test_TRACKS_06_fetchByTrackID_decodesAllFields() throws {
        let dbq = try TestDatabase.create()
        try dbq.write { db in
            try TestDatabase.insertTrack(db, text: "task")
            try db.execute(sql: """
                INSERT INTO track_states
                    (track_id, text, context, category, ownership, ball_on,
                     owner_user_id, requester_name, requester_user_id, blocking,
                     decision_summary, decision_options, sub_items, participants,
                     tags, priority, due_date, source, model, prompt_version, created_at)
                VALUES (1, 'task text', 'task context', 'task', 'mine', 'U_ball',
                        'U_owner', 'Bob', 'U_bob', 'blocking note',
                        'decide it', '[{"option":"A","supporters":[],"pros":"","cons":""}]',
                        '[{"text":"step","status":"open"}]',
                        '[{"name":"alice","user_id":"U1","stance":"driver"}]',
                        '["urgent"]', 'high', 1714000000.0, 'extraction', 'haiku-4-5', 7,
                        '2026-04-15T10:00:00Z')
                """)
        }
        let states = try dbq.read { db in
            try TrackStateQueries.fetchByTrackID(db, trackID: 1)
        }
        XCTAssertEqual(states.count, 1)
        let s = states[0]
        XCTAssertEqual(s.trackID, 1)
        XCTAssertEqual(s.text, "task text")
        XCTAssertEqual(s.context, "task context")
        XCTAssertEqual(s.category, "task")
        XCTAssertEqual(s.ownership, "mine")
        XCTAssertEqual(s.ballOn, "U_ball")
        XCTAssertEqual(s.ownerUserID, "U_owner")
        XCTAssertEqual(s.requesterName, "Bob")
        XCTAssertEqual(s.requesterUserID, "U_bob")
        XCTAssertEqual(s.blocking, "blocking note")
        XCTAssertEqual(s.decisionSummary, "decide it")
        XCTAssertEqual(s.priority, "high")
        XCTAssertEqual(s.dueDate, 1714000000.0)
        XCTAssertEqual(s.source, "extraction")
        XCTAssertEqual(s.model, "haiku-4-5")
        XCTAssertEqual(s.promptVersion, 7)
        XCTAssertTrue(s.isExtraction)
        XCTAssertFalse(s.isManual)
        XCTAssertEqual(s.decodedTags, ["urgent"])
        XCTAssertEqual(s.decodedSubItems.count, 1)
        XCTAssertEqual(s.decodedSubItems.first?.text, "step")
    }

    // MARK: - Helpers

    private func insertTrackState(
        _ db: Database,
        trackID: Int,
        text: String,
        priority: String,
        source: String,
        createdAt: String
    ) throws {
        try db.execute(sql: """
            INSERT INTO track_states
                (track_id, text, category, ownership, priority, source, created_at)
            VALUES (?, ?, 'task', 'mine', ?, ?, ?)
            """, arguments: [trackID, text, priority, source, createdAt])
    }
}
