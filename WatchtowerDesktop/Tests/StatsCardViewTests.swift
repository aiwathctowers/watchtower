import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class StatsCardViewTests: XCTestCase {

    /// title и value рендерятся как текст в карточке.
    func testTitleAndValueRendered() throws {
        let view = StatsCard(title: "Messages", value: "42", icon: "message")
        let inspected = try view.inspect()
        XCTAssertNoThrow(try inspected.find(text: "Messages"))
        XCTAssertNoThrow(try inspected.find(text: "42"))
    }

    /// Пустой value-string всё равно рендерится без падения.
    func testEmptyValueDoesNotCrash() throws {
        let view = StatsCard(title: "X", value: "", icon: "circle")
        XCTAssertNoThrow(try view.inspect().find(text: "X"))
    }

    /// Большое числовое значение с monospacedDigit отображается как-есть.
    func testLargeNumericValue() throws {
        let view = StatsCard(title: "Total", value: "1,234,567", icon: "number")
        XCTAssertNoThrow(try view.inspect().find(text: "1,234,567"))
    }
}
