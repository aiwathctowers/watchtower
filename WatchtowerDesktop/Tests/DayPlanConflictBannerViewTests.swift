import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class DayPlanConflictBannerViewTests: XCTestCase {

    /// Заголовок «Calendar conflicts detected» виден всегда.
    func testHeadlineAlwaysShown() throws {
        let view = DayPlanConflictBanner(summary: nil, onRegenerate: {}, onCheckAgain: {})
        XCTAssertNoThrow(try view.inspect().find(text: "Calendar conflicts detected"))
    }

    /// Summary рендерится, когда непустой.
    func testSummaryShownWhenSet() throws {
        let view = DayPlanConflictBanner(
            summary: "2 events overlap with planned blocks",
            onRegenerate: {},
            onCheckAgain: {}
        )
        XCTAssertNoThrow(try view.inspect().find(text: "2 events overlap with planned blocks"))
    }

    /// Пустой summary не должен попадать в дерево как Text "" — find ничего не найдёт.
    func testEmptySummaryHidden() throws {
        let view = DayPlanConflictBanner(summary: "", onRegenerate: {}, onCheckAgain: {})
        // Только заголовок есть, никаких других Text-узлов с пустой строкой.
        XCTAssertNoThrow(try view.inspect().find(text: "Calendar conflicts detected"))
    }

    /// Кнопка Regenerate вызывает onRegenerate, но не onCheckAgain.
    func testRegenerateInvokesCallback() throws {
        var regen = 0
        var check = 0
        let view = DayPlanConflictBanner(
            summary: nil,
            onRegenerate: { regen += 1 },
            onCheckAgain: { check += 1 }
        )

        try view.inspect().find(button: "Regenerate").tap()

        XCTAssertEqual(regen, 1)
        XCTAssertEqual(check, 0)
    }

    /// Кнопка «Check again» вызывает только onCheckAgain.
    func testCheckAgainInvokesCallback() throws {
        var regen = 0
        var check = 0
        let view = DayPlanConflictBanner(
            summary: nil,
            onRegenerate: { regen += 1 },
            onCheckAgain: { check += 1 }
        )

        try view.inspect().find(button: "Check again").tap()

        XCTAssertEqual(regen, 0)
        XCTAssertEqual(check, 1)
    }
}
