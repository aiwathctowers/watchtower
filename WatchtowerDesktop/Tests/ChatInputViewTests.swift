import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class ChatInputViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeView(
        text: String = "",
        isStreaming: Bool = false,
        onSend: @escaping () -> Void = {},
        onStop: (() -> Void)? = nil,
        placeholder: String = "Ask about your workspace..."
    ) -> ChatInput {
        var stored = text
        return ChatInput(
            text: Binding(get: { stored }, set: { stored = $0 }),
            isStreaming: isStreaming,
            onSend: onSend,
            onStop: onStop,
            placeholder: placeholder
        )
    }

    // MARK: - Tests

    /// Пустой text → виден placeholder.
    func testPlaceholderShownWhenEmpty() throws {
        let view = makeView(text: "", placeholder: "Type here…")
        XCTAssertNoThrow(try view.inspect().find(text: "Type here…"))
    }

    /// Непустой text → placeholder в дерево не вставляется.
    func testPlaceholderHiddenWhenTextPresent() throws {
        let view = makeView(text: "hello", placeholder: "Type here…")
        XCTAssertThrowsError(try view.inspect().find(text: "Type here…"))
    }

    /// Не streaming + пустой text → кнопка отключена.
    func testSendButtonDisabledWhenTextEmpty() throws {
        let view = makeView(text: "", isStreaming: false)
        let button = try view.inspect().find(ViewType.Button.self)
        XCTAssertTrue(try button.isDisabled())
    }

    /// Не streaming + непустой text → кнопка активна, тап вызывает onSend.
    func testSendButtonInvokesOnSendWhenTextPresent() throws {
        var sent = 0
        let view = makeView(text: "hi", isStreaming: false, onSend: { sent += 1 })

        let button = try view.inspect().find(ViewType.Button.self)
        XCTAssertFalse(try button.isDisabled())
        try button.tap()

        XCTAssertEqual(sent, 1)
    }

    /// Streaming + onStop=nil → кнопка отключена, onSend не вызывается.
    func testStreamingWithoutOnStopDisablesButton() throws {
        var sent = 0
        let view = makeView(text: "x", isStreaming: true, onSend: { sent += 1 }, onStop: nil)

        let button = try view.inspect().find(ViewType.Button.self)
        XCTAssertTrue(try button.isDisabled())
        XCTAssertEqual(sent, 0)
    }

    /// Streaming + onStop задан → тап вызывает onStop, не onSend.
    func testStreamingWithOnStopInvokesOnStop() throws {
        var sent = 0
        var stopped = 0
        let view = makeView(
            text: "anything",
            isStreaming: true,
            onSend: { sent += 1 },
            onStop: { stopped += 1 }
        )

        try view.inspect().find(ViewType.Button.self).tap()

        XCTAssertEqual(stopped, 1)
        XCTAssertEqual(sent, 0)
    }
}
