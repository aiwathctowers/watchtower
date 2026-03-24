import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackHistoryTests: XCTestCase {

    private func makeEntry(
        event: String,
        oldValue: String = "",
        newValue: String = "",
        createdAt: String = "2025-01-15T10:30:00Z"
    ) throws -> TrackHistoryEntry {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTrack(db)
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value, created_at)
                VALUES (1, ?, '', ?, ?, ?)
                """, arguments: [event, oldValue, newValue, createdAt])
        }
        return try XCTUnwrap(db.read { try TrackQueries.fetchHistory($0, trackID: 1) }.first)
    }

    // MARK: - displayText

    func testDisplayTextCreated() throws {
        XCTAssertEqual(try makeEntry(event: "created").displayText, "Created")
    }

    func testDisplayTextCreatedFromDigest() throws {
        XCTAssertEqual(try makeEntry(event: "created", newValue: "from_digest").displayText, "Created from digest")
    }

    func testDisplayTextStatusChanged() throws {
        let entry = try makeEntry(event: "status_changed", oldValue: "inbox", newValue: "active")
        XCTAssertEqual(entry.displayText, "Status: inbox → active")
    }

    func testDisplayTextAccepted() throws {
        XCTAssertEqual(try makeEntry(event: "accepted").displayText, "Accepted — moved to active")
    }

    func testDisplayTextReopened() throws {
        XCTAssertEqual(try makeEntry(event: "reopened").displayText, "Reopened — moved back to inbox")
    }

    func testDisplayTextSnoozed() throws {
        XCTAssertEqual(try makeEntry(event: "snoozed").displayText, "Snoozed")
    }

    func testDisplayTextSnoozedWithDuration() throws {
        XCTAssertEqual(try makeEntry(event: "snoozed", newValue: "until tomorrow").displayText, "Snoozed until tomorrow")
    }

    func testDisplayTextReactivated() throws {
        XCTAssertEqual(try makeEntry(event: "reactivated").displayText, "Reactivated — snooze expired")
    }

    func testDisplayTextPriorityChanged() throws {
        let entry = try makeEntry(event: "priority_changed", oldValue: "medium", newValue: "high")
        XCTAssertEqual(entry.displayText, "Priority: medium → high")
    }

    func testDisplayTextContextUpdated() throws {
        XCTAssertEqual(try makeEntry(event: "context_updated").displayText, "Context updated")
    }

    func testDisplayTextDueDateSet() throws {
        XCTAssertEqual(try makeEntry(event: "due_date_changed").displayText, "Due date set")
    }

    func testDisplayTextDueDateChanged() throws {
        XCTAssertEqual(try makeEntry(event: "due_date_changed", oldValue: "old").displayText, "Due date changed")
    }

    func testDisplayTextReExtracted() throws {
        XCTAssertEqual(try makeEntry(event: "re_extracted").displayText, "Re-extracted — new data from Slack")
    }

    func testDisplayTextDecisionEvolved() throws {
        XCTAssertEqual(try makeEntry(event: "decision_evolved").displayText, "Decision updated")
    }

    func testDisplayTextDigestLinked() throws {
        XCTAssertEqual(try makeEntry(event: "digest_linked", newValue: "#42").displayText, "Digest #42")
    }

    func testDisplayTextDigestLinkedNoValue() throws {
        XCTAssertEqual(try makeEntry(event: "digest_linked").displayText, "Linked to digest")
    }

    func testDisplayTextSubItemsUpdated() throws {
        XCTAssertEqual(try makeEntry(event: "sub_items_updated", newValue: "2/3 done").displayText, "Checklist: 2/3 done")
    }

    func testDisplayTextUpdateDetected() throws {
        XCTAssertEqual(try makeEntry(event: "update_detected").displayText, "New activity in thread")
    }

    func testDisplayTextUpdateRead() throws {
        XCTAssertEqual(try makeEntry(event: "update_read").displayText, "Update marked as read")
    }

    func testDisplayTextUnknownEvent() throws {
        XCTAssertEqual(try makeEntry(event: "some_new_event").displayText, "Some New Event")
    }

    // MARK: - detailText

    func testDetailTextContextUpdated() throws {
        let entry = try makeEntry(event: "context_updated", newValue: "More info added")
        XCTAssertEqual(entry.detailText, "More info added")
    }

    func testDetailTextContextUpdatedEmpty() throws {
        let entry = try makeEntry(event: "context_updated", newValue: "")
        XCTAssertNil(entry.detailText)
    }

    func testDetailTextDueDateChangedBothValues() throws {
        let entry = try makeEntry(event: "due_date_changed", oldValue: "Jan 1", newValue: "Jan 15")
        XCTAssertEqual(entry.detailText, "Jan 1 → Jan 15")
    }

    func testDetailTextStatusChanged() throws {
        let entry = try makeEntry(event: "status_changed", oldValue: "inbox", newValue: "active")
        XCTAssertNil(entry.detailText)
    }

    // MARK: - createdDate

    func testCreatedDateISO8601() throws {
        let entry = try makeEntry(event: "created", createdAt: "2025-01-15T10:30:00Z")
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        let expected = try XCTUnwrap(fmt.date(from: "2025-01-15T10:30:00Z"))
        XCTAssertEqual(entry.createdDate.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1)
    }

    func testCreatedDateISO8601Fractional() throws {
        let entry = try makeEntry(event: "created", createdAt: "2025-01-15T10:30:00.123Z")
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let expected = try XCTUnwrap(fmt.date(from: "2025-01-15T10:30:00.123Z"))
        XCTAssertEqual(entry.createdDate.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1)
    }
}
