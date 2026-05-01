import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class DayPlanItemRowViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeView(
        item: DayPlanItem,
        onToggle: @escaping () -> Void = {},
        onDelete: @escaping () -> Void = {},
        onNavigateSource: @escaping () -> Void = {}
    ) -> DayPlanItemRow {
        DayPlanItemRow(
            item: item,
            onToggle: onToggle,
            onDelete: onDelete,
            onNavigateSource: onNavigateSource
        )
    }

    // MARK: - Tests

    /// Title рендерится.
    func testTitleRendered() throws {
        let view = makeView(item: .stub(title: "Review PR #42"))
        XCTAssertNoThrow(try view.inspect().find(text: "Review PR #42"))
    }

    /// Tap чекбокс вызывает onToggle (это первая кнопка в строке).
    func testCheckboxToggleInvokesCallback() throws {
        var toggled = 0
        let view = makeView(
            item: .stub(),
            onToggle: { toggled += 1 }
        )

        // Первая кнопка в иерархии — чекбокс.
        let buttons = try view.inspect().findAll(ViewType.Button.self)
        try buttons.first?.tap()

        XCTAssertEqual(toggled, 1)
    }

    /// Rationale рендерится, если задано.
    func testRationaleShownWhenSet() throws {
        let view = makeView(item: .stub(rationale: "follow-up from briefing"))
        XCTAssertNoThrow(try view.inspect().find(text: "follow-up from briefing"))
    }

    /// Source-badge для типа .task выводит "task:<id>".
    func testTaskSourceBadgeLabel() throws {
        let view = makeView(item: .stub(sourceType: .task, sourceId: "99"))
        XCTAssertNoThrow(try view.inspect().find(text: "task:99"))
    }

    /// Source-type .manual не выводит source badge — find бросает.
    func testManualSourceHasNoBadge() throws {
        let view = makeView(item: .stub(sourceType: .manual))
        // Нет ни "task:", ни "briefing", ни "focus" в дереве.
        XCTAssertThrowsError(try view.inspect().find(text: "briefing"))
        XCTAssertThrowsError(try view.inspect().find(text: "focus"))
    }

    /// Priority badge показывает заглавный вариант ("High", "Medium", ...).
    func testPriorityBadgeRendered() throws {
        let view = makeView(item: .stub(priority: "high"))
        XCTAssertNoThrow(try view.inspect().find(text: "High"))
    }

    /// nil priority — никакого badge нет.
    func testNoPriorityBadgeWhenNil() throws {
        let view = makeView(item: .stub(priority: nil))
        XCTAssertThrowsError(try view.inspect().find(text: "High"))
        XCTAssertThrowsError(try view.inspect().find(text: "Medium"))
        XCTAssertThrowsError(try view.inspect().find(text: "Low"))
    }
}
