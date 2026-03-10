import XCTest
@testable import WatchtowerDesktop

final class TimeFormattingTests: XCTestCase {

    // MARK: - parseISO

    func testParseISOWithFractionalSeconds() {
        let date = TimeFormatting.parseISO("2025-01-15T10:30:00.123Z")
        XCTAssertNotNil(date)
    }

    func testParseISOWithoutFractionalSeconds() {
        let date = TimeFormatting.parseISO("2025-01-15T10:30:00Z")
        XCTAssertNotNil(date)
    }

    func testParseISOInvalidString() {
        XCTAssertNil(TimeFormatting.parseISO("not-a-date"))
    }

    func testParseISOEmptyString() {
        XCTAssertNil(TimeFormatting.parseISO(""))
    }

    // MARK: - relativeTime

    func testRelativeTimeJustNow() {
        let date = Date().addingTimeInterval(-10) // 10 seconds ago
        XCTAssertEqual(TimeFormatting.relativeTime(from: date), "just now")
    }

    func testRelativeTimeMinutesAgo() {
        let date = Date().addingTimeInterval(-300) // 5 minutes ago
        XCTAssertEqual(TimeFormatting.relativeTime(from: date), "5m ago")
    }

    func testRelativeTimeHoursAgo() {
        let date = Date().addingTimeInterval(-7200) // 2 hours ago
        XCTAssertEqual(TimeFormatting.relativeTime(from: date), "2h ago")
    }

    func testRelativeTimeYesterday() {
        let date = Date().addingTimeInterval(-100000) // ~27.7 hours ago
        XCTAssertEqual(TimeFormatting.relativeTime(from: date), "yesterday")
    }

    func testRelativeTimeDaysAgo() {
        let date = Date().addingTimeInterval(-259200) // 3 days ago
        XCTAssertEqual(TimeFormatting.relativeTime(from: date), "3d ago")
    }

    func testRelativeTimeOldDate() {
        let date = Date().addingTimeInterval(-864000) // 10 days ago
        let result = TimeFormatting.relativeTime(from: date)
        // Should return formatted date like "Feb 26" not "Xd ago"
        XCTAssertFalse(result.hasSuffix("ago"))
        XCTAssertFalse(result == "just now")
    }

    func testRelativeTimeFromISO() {
        // Invalid ISO returns the original string
        XCTAssertEqual(TimeFormatting.relativeTime(from: "bad-string"), "bad-string")
    }

    func testRelativeTimeFromUnix() {
        let ts = Date().addingTimeInterval(-150).timeIntervalSince1970 // 2.5 minutes ago
        XCTAssertEqual(TimeFormatting.relativeTimeFromUnix(ts), "2m ago")
    }

    // MARK: - formatUnixTimestamp

    func testFormatUnixTimestamp() {
        let ts: Double = 1700000000 // Nov 14, 2023
        let result = TimeFormatting.formatUnixTimestamp(ts)
        // Should contain a date — exact format depends on locale
        XCTAssertFalse(result.isEmpty)
        XCTAssertTrue(result.contains("2023") || result.contains("14") || result.contains("Nov"))
    }

    // MARK: - shortTime

    func testShortTime() {
        let result = TimeFormatting.shortTime(1700000000)
        // Should be in HH:mm format
        XCTAssertTrue(result.count <= 5)
        XCTAssertTrue(result.contains(":"))
    }
}
