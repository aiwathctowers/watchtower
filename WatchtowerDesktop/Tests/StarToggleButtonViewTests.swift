import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class StarToggleButtonViewTests: XCTestCase {

    /// Tap on the star button должен дёргать переданный action ровно один раз.
    func testTapInvokesAction() throws {
        var tapCount = 0
        let view = StarToggleButton(isStarred: false) { tapCount += 1 }

        try view.inspect().button().tap()

        XCTAssertEqual(tapCount, 1)
    }

    /// Help-текст зависит от `isStarred`: "Star" для пустой звезды.
    func testHelpTextWhenNotStarred() throws {
        let view = StarToggleButton(isStarred: false) {}
        let help = try view.inspect().button().help()
        XCTAssertEqual(try help.string(), "Star")
    }

    /// Help-текст для отмеченной — "Unstar".
    func testHelpTextWhenStarred() throws {
        let view = StarToggleButton(isStarred: true) {}
        let help = try view.inspect().button().help()
        XCTAssertEqual(try help.string(), "Unstar")
    }
}
