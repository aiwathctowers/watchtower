import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TrackHistoryTests: XCTestCase {

    private func makeEntry(event: String, oldValue: String = "", newValue: String = "", createdAt: String = "2025-01-15T10:30:00Z") -> TrackHistoryEntry {
        let db = try! TestDatabase.create()
        try! db.write { db in
            try TestDatabase.insertTrack(db)
            try db.execute(sql: """
                INSERT INTO track_history (track_id, event, field, old_value, new_value, created_at)
                VALUES (1, ?, '', ?, ?, ?)
                """, arguments: [event, oldValue, newValue, createdAt])
        }
        return try! db.read { try TrackQueries.fetchHistory($0, trackID: 1) }.first!
    }

    // MARK: - displayText

    func testDisplayTextCreated() {
        XCTAssertEqual(makeEntry(event: "created").displayText, "Created")
    }

    func testDisplayTextCreatedFromDigest() {
        XCTAssertEqual(makeEntry(event: "created", newValue: "from_digest").displayText, "Created from digest")
    }

    func testDisplayTextStatusChanged() {
        let entry = makeEntry(event: "status_changed", oldValue: "inbox", newValue: "active")
        XCTAssertEqual(entry.displayText, "Status: inbox → active")
    }

    func testDisplayTextAccepted() {
        XCTAssertEqual(makeEntry(event: "accepted").displayText, "Accepted — moved to active")
    }

    func testDisplayTextReopened() {
        XCTAssertEqual(makeEntry(event: "reopened").displayText, "Reopened — moved back to inbox")
    }

    func testDisplayTextSnoozed() {
        XCTAssertEqual(makeEntry(event: "snoozed").displayText, "Snoozed")
    }

    func testDisplayTextSnoozedWithDuration() {
        XCTAssertEqual(makeEntry(event: "snoozed", newValue: "until tomorrow").displayText, "Snoozed until tomorrow")
    }

    func testDisplayTextReactivated() {
        XCTAssertEqual(makeEntry(event: "reactivated").displayText, "Reactivated — snooze expired")
    }

    func testDisplayTextPriorityChanged() {
        let entry = makeEntry(event: "priority_changed", oldValue: "medium", newValue: "high")
        XCTAssertEqual(entry.displayText, "Priority: medium → high")
    }

    func testDisplayTextContextUpdated() {
        XCTAssertEqual(makeEntry(event: "context_updated").displayText, "Context updated")
    }

    func testDisplayTextDueDateSet() {
        XCTAssertEqual(makeEntry(event: "due_date_changed").displayText, "Due date set")
    }

    func testDisplayTextDueDateChanged() {
        XCTAssertEqual(makeEntry(event: "due_date_changed", oldValue: "old").displayText, "Due date changed")
    }

    func testDisplayTextReExtracted() {
        XCTAssertEqual(makeEntry(event: "re_extracted").displayText, "Re-extracted — new data from Slack")
    }

    func testDisplayTextDecisionEvolved() {
        XCTAssertEqual(makeEntry(event: "decision_evolved").displayText, "Decision updated")
    }

    func testDisplayTextDigestLinked() {
        XCTAssertEqual(makeEntry(event: "digest_linked", newValue: "#42").displayText, "Digest #42")
    }

    func testDisplayTextDigestLinkedNoValue() {
        XCTAssertEqual(makeEntry(event: "digest_linked").displayText, "Linked to digest")
    }

    func testDisplayTextSubItemsUpdated() {
        XCTAssertEqual(makeEntry(event: "sub_items_updated", newValue: "2/3 done").displayText, "Checklist: 2/3 done")
    }

    func testDisplayTextUpdateDetected() {
        XCTAssertEqual(makeEntry(event: "update_detected").displayText, "New activity in thread")
    }

    func testDisplayTextUpdateRead() {
        XCTAssertEqual(makeEntry(event: "update_read").displayText, "Update marked as read")
    }

    func testDisplayTextUnknownEvent() {
        XCTAssertEqual(makeEntry(event: "some_new_event").displayText, "Some New Event")
    }

    // MARK: - detailText

    func testDetailTextContextUpdated() {
        let entry = makeEntry(event: "context_updated", newValue: "More info added")
        XCTAssertEqual(entry.detailText, "More info added")
    }

    func testDetailTextContextUpdatedEmpty() {
        let entry = makeEntry(event: "context_updated", newValue: "")
        XCTAssertNil(entry.detailText)
    }

    func testDetailTextDueDateChangedBothValues() {
        let entry = makeEntry(event: "due_date_changed", oldValue: "Jan 1", newValue: "Jan 15")
        XCTAssertEqual(entry.detailText, "Jan 1 → Jan 15")
    }

    func testDetailTextStatusChanged() {
        let entry = makeEntry(event: "status_changed", oldValue: "inbox", newValue: "active")
        XCTAssertNil(entry.detailText)
    }

    // MARK: - createdDate

    func testCreatedDateISO8601() {
        let entry = makeEntry(event: "created", createdAt: "2025-01-15T10:30:00Z")
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        let expected = fmt.date(from: "2025-01-15T10:30:00Z")!
        XCTAssertEqual(entry.createdDate.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1)
    }

    func testCreatedDateISO8601Fractional() {
        let entry = makeEntry(event: "created", createdAt: "2025-01-15T10:30:00.123Z")
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let expected = fmt.date(from: "2025-01-15T10:30:00.123Z")!
        XCTAssertEqual(entry.createdDate.timeIntervalSince1970, expected.timeIntervalSince1970, accuracy: 1)
    }
}
