import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class ErrorViewTests: XCTestCase {

    /// Заголовок и сообщение видны.
    func testTitleAndMessageRendered() throws {
        let view = ErrorView(title: "Oops", message: "Something broke")

        XCTAssertNoThrow(try view.inspect().find(text: "Oops"))
        XCTAssertNoThrow(try view.inspect().find(text: "Something broke"))
    }

    /// Без actionTitle/action — кнопки не существует.
    func testNoButtonWhenActionMissing() throws {
        let view = ErrorView(title: "Err", message: "x")
        XCTAssertThrowsError(try view.inspect().find(ViewType.Button.self))
    }

    /// С action — кнопка отрисована, тап вызывает обработчик.
    func testActionButtonTriggersCallback() throws {
        var tapped = false
        let view = ErrorView(
            title: "Err",
            message: "x",
            actionTitle: "Retry",
            action: { tapped = true }
        )

        try view.inspect().find(button: "Retry").tap()
        XCTAssertTrue(tapped)
    }
}
