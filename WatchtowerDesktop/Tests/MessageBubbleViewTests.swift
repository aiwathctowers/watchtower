import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class MessageBubbleViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeMessage(
        role: ChatMessage.Role,
        text: String,
        isStreaming: Bool = false
    ) -> ChatMessage {
        ChatMessage(
            id: UUID(),
            role: role,
            text: text,
            timestamp: Date(),
            isStreaming: isStreaming
        )
    }

    // MARK: - Tests

    /// User message — текст рендерится, есть corner copy button.
    func testUserMessageRendersText() throws {
        let view = MessageBubble(message: makeMessage(role: .user, text: "what's up"))
        XCTAssertNoThrow(try view.inspect().find(text: "what's up"))
    }

    /// User-message с непустым текстом → есть кнопка help "Copy message".
    func testUserMessageHasCopyButton() throws {
        let view = MessageBubble(message: makeMessage(role: .user, text: "hi"))
        let buttons = try view.inspect().findAll(ViewType.Button.self)
        let helps = buttons.compactMap { try? $0.help().string() }
        XCTAssertTrue(helps.contains("Copy message"),
                      "expected a 'Copy message' button, got \(helps)")
    }

    /// Пустое user-сообщение → corner copy button не рисуется (блок `if !message.text.isEmpty`).
    func testEmptyUserMessageHidesCopyButton() throws {
        let view = MessageBubble(message: makeMessage(role: .user, text: ""))
        let buttons = try view.inspect().findAll(ViewType.Button.self)
        let helps = buttons.compactMap { try? $0.help().string() }
        XCTAssertFalse(helps.contains("Copy message"),
                       "empty text should not render the corner copy button")
    }

    /// System message — текст виден, никаких кнопок copy.
    func testSystemMessageRendersTextOnly() throws {
        let view = MessageBubble(message: makeMessage(role: .system, text: "system note"))
        XCTAssertNoThrow(try view.inspect().find(text: "system note"))
        let buttons = try view.inspect().findAll(ViewType.Button.self)
        XCTAssertTrue(buttons.isEmpty, "system role should render only text")
    }

    /// Streaming assistant message → StreamingIndicator в дереве (через `Text` нельзя
    /// гарантированно проверить, поэтому ищем сам тип).
    func testStreamingAssistantHasIndicator() throws {
        let view = MessageBubble(
            message: makeMessage(role: .assistant, text: "thinking", isStreaming: true)
        )
        XCTAssertNoThrow(try view.inspect().find(StreamingIndicator.self))
    }

    /// Не-streaming assistant message → StreamingIndicator отсутствует.
    func testNonStreamingAssistantHasNoIndicator() throws {
        let view = MessageBubble(
            message: makeMessage(role: .assistant, text: "done", isStreaming: false)
        )
        XCTAssertThrowsError(try view.inspect().find(StreamingIndicator.self))
    }
}
