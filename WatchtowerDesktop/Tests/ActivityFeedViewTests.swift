import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class ActivityFeedViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeMessage(
        channelID: String = "C1",
        channelName: String? = "general",
        userName: String? = "alice",
        text: String = "hello"
    ) -> MessageWithContext {
        let json = """
        {
          "channel_id": "\(channelID)",
          "ts": "1.0",
          "user_id": "U1",
          "text": "\(text)",
          "thread_ts": null,
          "reply_count": 0,
          "is_edited": false,
          "is_deleted": false,
          "subtype": "",
          "permalink": "",
          "ts_unix": 1714572000.0,
          "raw_json": "{}",
          "channel_name": \(channelName.map { "\"\($0)\"" } ?? "null"),
          "user_name": \(userName.map { "\"\($0)\"" } ?? "null")
        }
        """
        return try! JSONDecoder().decode(MessageWithContext.self, from: Data(json.utf8))
    }

    // MARK: - Tests

    /// Заголовок секции виден всегда.
    func testHeadlineRendered() throws {
        let view = ActivityFeed(messages: [])
        XCTAssertNoThrow(try view.inspect().find(text: "Recent Activity"))
    }

    /// Empty state — текст «No watched channels» и подсказка ниже.
    func testEmptyStateShownWhenNoMessages() throws {
        let view = ActivityFeed(messages: [])
        XCTAssertNoThrow(try view.inspect().find(text: "No watched channels"))
        XCTAssertNoThrow(try view.inspect().find(
            text: "Add channels to your watch list to see activity here."
        ))
    }

    /// Группировка по каналу — заголовок «#alpha» появляется, empty state — нет.
    func testChannelHeaderRendered() throws {
        let view = ActivityFeed(messages: [
            makeMessage(channelName: "alpha"),
        ])
        XCTAssertNoThrow(try view.inspect().find(text: "#alpha"))
        XCTAssertThrowsError(try view.inspect().find(text: "No watched channels"))
    }

    /// channelName=nil → fallback "#unknown".
    func testUnknownChannelFallback() throws {
        let view = ActivityFeed(messages: [makeMessage(channelName: nil)])
        XCTAssertNoThrow(try view.inspect().find(text: "#unknown"))
    }

    /// slackChannelURL вернул URL → дерево содержит Link.
    func testLinkRenderedWhenURLProvided() throws {
        let url = URL(string: "slack://channel?id=C1")!
        let view = ActivityFeed(
            messages: [makeMessage(channelName: "alpha")],
            slackChannelURL: { _ in url }
        )

        let link = try view.inspect().find(ViewType.Link.self)
        XCTAssertEqual(try link.url(), url)
    }
}
