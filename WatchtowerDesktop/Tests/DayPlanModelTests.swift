import XCTest
import GRDB
@testable import WatchtowerDesktop

final class DayPlanModelTests: XCTestCase {

    // MARK: - Enum raw values

    func testKindRawValues() {
        XCTAssertEqual(DayPlanItemKind.timeblock.rawValue, "timeblock")
        XCTAssertEqual(DayPlanItemKind.backlog.rawValue, "backlog")
    }

    func testSourceTypeRawValues() {
        XCTAssertEqual(DayPlanItemSourceType.task.rawValue, "task")
        XCTAssertEqual(DayPlanItemSourceType.briefingAttention.rawValue, "briefing_attention")
        XCTAssertEqual(DayPlanItemSourceType.jira.rawValue, "jira")
        XCTAssertEqual(DayPlanItemSourceType.calendar.rawValue, "calendar")
        XCTAssertEqual(DayPlanItemSourceType.manual.rawValue, "manual")
        XCTAssertEqual(DayPlanItemSourceType.focus.rawValue, "focus")
    }

    func testStatusRawValues() {
        XCTAssertEqual(DayPlanItemStatus.pending.rawValue, "pending")
        XCTAssertEqual(DayPlanItemStatus.done.rawValue, "done")
        XCTAssertEqual(DayPlanItemStatus.skipped.rawValue, "skipped")
    }

    // MARK: - isReadOnly

    func testIsReadOnlyForCalendar() {
        let calendarItem = DayPlanItem.stub(sourceType: .calendar)
        XCTAssertTrue(calendarItem.isCalendarEvent)
        XCTAssertTrue(calendarItem.isReadOnly)

        let manualItem = DayPlanItem.stub(sourceType: .manual)
        XCTAssertTrue(manualItem.isManual)
        XCTAssertFalse(manualItem.isReadOnly)
    }

    // MARK: - timeRange formatting

    func testTimeRangeFormatting() throws {
        var components = DateComponents()
        components.year = 2026
        components.month = 4
        components.day = 23
        components.hour = 9
        components.minute = 0
        components.second = 0
        components.timeZone = TimeZone(identifier: "UTC")
        let start = Calendar(identifier: .gregorian).date(from: components)!

        var endComponents = components
        endComponents.hour = 10
        endComponents.minute = 30
        let end = Calendar(identifier: .gregorian).date(from: endComponents)!

        let item = DayPlanItem.stub(startTime: start, endTime: end)
        let range = try XCTUnwrap(item.timeRange)
        XCTAssertFalse(range.isEmpty)
        XCTAssertTrue(range.contains("–"), "timeRange should contain en-dash separator")
    }

    func testTimeRangeNilWhenMissingTimes() {
        let item = DayPlanItem.stub(startTime: nil, endTime: nil)
        XCTAssertNil(item.timeRange)

        let itemOnlyStart = DayPlanItem.stub(startTime: Date(), endTime: nil)
        XCTAssertNil(itemOnlyStart.timeRange)
    }

    // MARK: - DayPlan computed properties

    func testParsedFeedbackHistoryEmpty() {
        let plan = DayPlan.stub(feedbackHistory: "[]")
        XCTAssertTrue(plan.parsedFeedbackHistory.isEmpty)
    }

    func testParsedFeedbackHistoryWithValues() {
        let plan = DayPlan.stub(feedbackHistory: #"["too long","missing task"]"#)
        XCTAssertEqual(plan.parsedFeedbackHistory, ["too long", "missing task"])
    }

    func testParsedFeedbackHistoryInvalidJSON() {
        let plan = DayPlan.stub(feedbackHistory: "not json")
        XCTAssertTrue(plan.parsedFeedbackHistory.isEmpty)
    }

    func testIsReadFalseWhenReadAtNil() {
        let plan = DayPlan.stub(readAt: nil)
        XCTAssertFalse(plan.isRead)
    }

    func testIsReadTrueWhenReadAtSet() {
        let plan = DayPlan.stub(readAt: Date())
        XCTAssertTrue(plan.isRead)
    }
}
