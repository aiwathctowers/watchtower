import XCTest
import SwiftUI
import GRDB
import ViewInspector
@testable import WatchtowerDesktop

// MARK: - InboxFeedbackSheet — пример UI-теста через ViewInspector
//
// ViewInspector обходит дерево SwiftUI без рендера — работает прямо
// в `swift test`, без Xcode-проекта и без .xcodeproj.
//
// Покрывает:
//   - заголовок "Why is this not helpful?"
//   - наличие radio-Picker с четырьмя вариантами
//   - тап Cancel → onSubmit(0, "")
//   - тап Apply  → onSubmit(-1, выбранная_причина)

@MainActor
final class InboxFeedbackSheetViewTests: XCTestCase {

    // MARK: - Helpers

    /// `InboxItem` имеет только `init(row: Row)` (GRDB), поэтому фикстуру собираем
    /// из словаря — все недостающие поля заполняются дефолтами в инициализаторе.
    private func makeItem() -> InboxItem {
        let row: Row = [
            "id": 1,
            "channel_id": "C1",
            "message_ts": "1.0",
            "sender_user_id": "U1",
            "trigger_type": "mention",
            "snippet": "hello",
            "status": "pending",
            "priority": "medium",
            "created_at": "2026-04-23T10:00:00Z",
            "updated_at": "2026-04-23T10:00:00Z"
        ]
        return InboxItem(row: row)
    }

    // MARK: - Tests

    /// Заголовок sheet виден пользователю.
    func testHeadlineVisible() throws {
        let sheet = InboxFeedbackSheet(item: makeItem()) { _, _ in }

        let headline = try sheet.inspect().find(text: "Why is this not helpful?")
        XCTAssertEqual(try headline.string(), "Why is this not helpful?")
    }

    /// Все четыре причины присутствуют в Picker'е.
    func testAllReasonsPresent() throws {
        let sheet = InboxFeedbackSheet(item: makeItem()) { _, _ in }
        let inspected = try sheet.inspect()

        for label in [
            "Source usually noise",
            "Wrong priority",
            "Wrong class",
            "Never show me this"
        ] {
            XCTAssertNoThrow(try inspected.find(text: label),
                             "Reason '\(label)' must be visible")
        }
    }

    /// Тап Cancel вызывает onSubmit с rating=0 и пустой причиной.
    func testCancelSubmitsZeroRating() async throws {
        let expectation = expectation(description: "onSubmit called")
        var received: (rating: Int, reason: String)?

        let sheet = InboxFeedbackSheet(item: makeItem()) { rating, reason in
            received = (rating, reason)
            expectation.fulfill()
        }

        try sheet.inspect().find(button: "Cancel").tap()

        await fulfillment(of: [expectation], timeout: 1.0)
        XCTAssertEqual(received?.rating, 0)
        XCTAssertEqual(received?.reason, "")
    }

    /// Тап Apply вызывает onSubmit(-1, дефолтная_причина).
    func testApplySubmitsDefaultReason() async throws {
        let expectation = expectation(description: "onSubmit called")
        var received: (rating: Int, reason: String)?

        let sheet = InboxFeedbackSheet(item: makeItem()) { rating, reason in
            received = (rating, reason)
            expectation.fulfill()
        }

        try sheet.inspect().find(button: "Apply").tap()

        await fulfillment(of: [expectation], timeout: 1.0)
        XCTAssertEqual(received?.rating, -1)
        // По умолчанию выбран "source_noise"
        XCTAssertEqual(received?.reason, "source_noise")
    }
}
