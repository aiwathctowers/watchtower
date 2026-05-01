import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class SearchResultRowViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeResult(
        channelName: String? = "general",
        userName: String? = "alice",
        userID: String = "U1",
        text: String = "Hello world",
        snippet: String? = nil
    ) -> SearchResult {
        let json = """
        {
          "channel_id": "C1",
          "ts": "1.0",
          "user_id": "\(userID)",
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
          "user_name": \(userName.map { "\"\($0)\"" } ?? "null"),
          "snippet": \(snippet.map { "\"\($0)\"" } ?? "null")
        }
        """
        return try! JSONDecoder().decode(SearchResult.self, from: Data(json.utf8))
    }

    // MARK: - Tests

    /// Имя канала с префиксом '#'.
    func testChannelNameRenderedWithHash() throws {
        let view = SearchResultRow(result: makeResult(channelName: "general"))
        XCTAssertNoThrow(try view.inspect().find(text: "#general"))
    }

    /// channelName=nil → fallback "#unknown".
    func testChannelFallbackToUnknown() throws {
        let view = SearchResultRow(result: makeResult(channelName: nil))
        XCTAssertNoThrow(try view.inspect().find(text: "#unknown"))
    }

    /// userName, если задан, идёт в Text.
    func testUserNameRendered() throws {
        let view = SearchResultRow(result: makeResult(userName: "alice"))
        XCTAssertNoThrow(try view.inspect().find(text: "alice"))
    }

    /// userName=nil → fallback на userID.
    func testUserNameFallsBackToUserID() throws {
        let view = SearchResultRow(result: makeResult(userName: nil, userID: "U_RAW"))
        XCTAssertNoThrow(try view.inspect().find(text: "U_RAW"))
    }

    /// Snippet рендерится с убранными `<mark>`-обёртками.
    func testSnippetMarkTagsStripped() throws {
        let view = SearchResultRow(
            result: makeResult(snippet: "hello <mark>world</mark> here")
        )
        XCTAssertNoThrow(try view.inspect().find(text: "hello world here"))
    }

    /// Без snippet — рендерится result.text (через SlackTextParser).
    /// Проверяем по подстроке, что Text появляется.
    func testTextShownWhenNoSnippet() throws {
        let view = SearchResultRow(result: makeResult(text: "Plain message", snippet: nil))
        XCTAssertNoThrow(try view.inspect().find(text: "Plain message"))
    }

    /// slackChannelURL=nil → нет Link, имя канала рендерится как обычный Text.
    func testNoLinkWithoutSlackURL() throws {
        let view = SearchResultRow(result: makeResult(), slackChannelURL: nil)
        XCTAssertThrowsError(try view.inspect().find(ViewType.Link.self))
    }

    /// slackChannelURL задан → Link с этим URL присутствует.
    func testLinkPresentWithSlackURL() throws {
        let url = URL(string: "slack://channel?id=C1")!
        let view = SearchResultRow(result: makeResult(), slackChannelURL: url)

        let link = try view.inspect().find(ViewType.Link.self)
        XCTAssertEqual(try link.url(), url)
    }
}
