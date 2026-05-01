import XCTest
import SwiftUI
import GRDB
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class WorkloadPersonDetailViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeEntry(
        slackUserID: String = "U1",
        displayName: String = "Alice",
        openIssues: Int = 5,
        inProgressCount: Int = 2,
        testingCount: Int = 1,
        overdueCount: Int = 0,
        blockedCount: Int = 0,
        avgCycleTimeDays: Double = 3.5,
        signal: WorkloadViewModel.WorkloadSignal = .normal
    ) -> WorkloadViewModel.WorkloadEntry {
        WorkloadViewModel.WorkloadEntry(
            slackUserID: slackUserID,
            displayName: displayName,
            openIssues: openIssues,
            inProgressCount: inProgressCount,
            testingCount: testingCount,
            overdueCount: overdueCount,
            blockedCount: blockedCount,
            avgCycleTimeDays: avgCycleTimeDays,
            signal: signal
        )
    }

    private func makeView(
        entry: WorkloadViewModel.WorkloadEntry? = nil,
        onClose: (() -> Void)? = nil
    ) throws -> (WorkloadPersonDetailView, String) {
        let (db, path) = try TestDatabase.createDatabaseManager()
        return (
            WorkloadPersonDetailView(
                entry: entry ?? makeEntry(),
                dbManager: db,
                onClose: onClose
            ),
            path
        )
    }

    // MARK: - Tests

    /// Header: displayName, slackUserID видны.
    func testHeaderShowsNameAndID() throws {
        let (view, path) = try makeView(entry: makeEntry(slackUserID: "U_RAW", displayName: "Bob"))
        defer { TestDatabase.cleanup(path: path) }

        let inspected = try view.inspect()
        XCTAssertNoThrow(try inspected.find(text: "Bob"))
        XCTAssertNoThrow(try inspected.find(text: "U_RAW"))
    }

    /// Signal badge: emoji + label видны.
    func testSignalBadgeRendered() throws {
        let (view, path) = try makeView(entry: makeEntry(signal: .overload))
        defer { TestDatabase.cleanup(path: path) }

        let inspected = try view.inspect()
        XCTAssertNoThrow(try inspected.find(text: "Overload"))
    }

    /// statsGrid: 6 заголовков карточек.
    func testStatsGridLabelsRendered() throws {
        let (view, path) = try makeView()
        defer { TestDatabase.cleanup(path: path) }

        let inspected = try view.inspect()
        for label in ["Open Issues", "In Progress", "Testing", "Overdue", "Blocked", "Cycle Time"] {
            XCTAssertNoThrow(try inspected.find(text: label),
                             "stat card label '\(label)' must be visible")
        }
    }

    /// statsGrid: значения подставляются.
    func testStatsGridValuesRendered() throws {
        let (view, path) = try makeView(entry: makeEntry(
            openIssues: 7,
            inProgressCount: 3,
            testingCount: 2,
            overdueCount: 1,
            blockedCount: 4,
            avgCycleTimeDays: 4.2
        ))
        defer { TestDatabase.cleanup(path: path) }

        let inspected = try view.inspect()
        XCTAssertNoThrow(try inspected.find(text: "7"))
        XCTAssertNoThrow(try inspected.find(text: "3"))
        XCTAssertNoThrow(try inspected.find(text: "4"))
        XCTAssertNoThrow(try inspected.find(text: "4.2d"))
    }

    /// Issues section: загрузка → "Issues (0)" + ProgressView.
    /// `.onAppear` ViewInspector не триггерит, поэтому isLoading=true стабилен.
    func testIssuesSectionInLoadingState() throws {
        let (view, path) = try makeView()
        defer { TestDatabase.cleanup(path: path) }

        let inspected = try view.inspect()
        XCTAssertNoThrow(try inspected.find(text: "Issues (0)"))
        XCTAssertNoThrow(try inspected.find(ViewType.ProgressView.self))
        // Empty-state не показывается, т.к. isLoading=true.
        XCTAssertThrowsError(try inspected.find(text: "No issues found"))
    }

    /// onClose=nil → нет кнопки закрытия в дереве.
    func testCloseButtonHiddenWithoutCallback() throws {
        let (view, path) = try makeView(onClose: nil)
        defer { TestDatabase.cleanup(path: path) }

        XCTAssertThrowsError(try view.inspect().find(ViewType.Button.self))
    }

    /// onClose задан → кнопка есть и тап срабатывает.
    func testCloseButtonInvokesCallback() throws {
        var closed = 0
        let (view, path) = try makeView(onClose: { closed += 1 })
        defer { TestDatabase.cleanup(path: path) }

        try view.inspect().find(ViewType.Button.self).tap()
        XCTAssertEqual(closed, 1)
    }
}
