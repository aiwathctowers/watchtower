import XCTest
@testable import WatchtowerDesktop

final class SlackTextParserTests: XCTestCase {

    // MARK: - Links

    func testLinkWithDisplayText() {
        let input = "Check <https://example.com|this link> out"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Check this link out")
    }

    func testBareLink() {
        let input = "Visit <https://example.com/page>"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Visit https://example.com/page")
    }

    func testMultipleLinks() {
        let input = "<https://a.com|A> and <https://b.com>"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "A and https://b.com")
    }

    // MARK: - Mentions

    func testUserMention() {
        let input = "Hey <@U12345ABC>!"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Hey @U12345ABC!")
    }

    func testChannelMention() {
        let input = "Post in <#C12345ABC|general>"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Post in #general")
    }

    func testSpecialMentionHere() {
        let input = "<!here> heads up"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "@here heads up")
    }

    func testSpecialMentionChannel() {
        let input = "<!channel> important"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "@channel important")
    }

    func testSpecialMentionEveryone() {
        let input = "<!everyone> announcement"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "@everyone announcement")
    }

    // MARK: - Formatting (plain text stripping)

    func testBoldStripped() {
        let input = "This is *bold text* here"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "This is bold text here")
    }

    func testItalicStripped() {
        let input = "This is _italic text_ here"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "This is italic text here")
    }

    func testStrikethroughStripped() {
        let input = "This is ~struck text~ here"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "This is struck text here")
    }

    func testInlineCodeStripped() {
        let input = "Run `npm install` first"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Run npm install first")
    }

    func testCodeBlockStripped() {
        let input = "Here:\n```\nfunc main() {\n    print(\"hi\")\n}\n```\nDone"
        let result = SlackTextParser.toPlainText(input)
        XCTAssertTrue(result.contains("func main()"))
        XCTAssertFalse(result.contains("```"))
    }

    func testBlockquoteStripped() {
        let input = "&gt; This is a quote"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "This is a quote")
    }

    // MARK: - Emoji resolution

    func testEmojiInText() {
        let input = "Great work :thumbsup: :fire:"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Great work 👍 🔥")
    }

    func testUnknownEmojiPreserved() {
        let input = "Custom :custom_emoji: here"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Custom :custom_emoji: here")
    }

    // MARK: - Combined

    func testComplexMessage() {
        let input = "*Important:* <@U123> posted in <#C456|dev> — check <https://pr.com/123|PR #123> :rocket:"
        let result = SlackTextParser.toPlainText(input)
        XCTAssertEqual(result, "Important: @U123 posted in #dev — check PR #123 🚀")
    }

    func testEmptyString() {
        XCTAssertEqual(SlackTextParser.toPlainText(""), "")
    }

    func testPlainTextPassthrough() {
        let input = "Just a normal message"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Just a normal message")
    }

    // MARK: - Date templates

    func testDateTemplateWithFallback() {
        let input = "Today-<!date^1776981600^{date_long}|Friday, April 24, 2026>"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Today-Friday, April 24, 2026")
    }

    func testDateTemplateWithoutFallbackFormatsTimestamp() {
        let input = "At <!date^1700000000^{date_short}>"
        let result = SlackTextParser.toPlainText(input)
        XCTAssertTrue(result.hasPrefix("At "))
        XCTAssertFalse(result.contains("<!date"))
        XCTAssertFalse(result.contains("1700000000"))
    }

    // MARK: - User-name resolution

    func testUserMentionResolvesViaLookup() {
        let input = "Ping <@U09H4EMS85U> about this"
        let result = SlackTextParser.toPlainText(input, userNames: ["U09H4EMS85U": "Roman Olifir"])
        XCTAssertEqual(result, "Ping @Roman Olifir about this")
    }

    func testUserMentionFallsBackToIDWhenUnknown() {
        let input = "Ping <@U09H4EMS85U> about this"
        XCTAssertEqual(SlackTextParser.toPlainText(input), "Ping @U09H4EMS85U about this")
    }

    func testUserMentionWithInlineNameIgnoresLookup() {
        let input = "Hey <@U09H4EMS85U|roman>"
        let result = SlackTextParser.toPlainText(input, userNames: ["U09H4EMS85U": "Roman Olifir"])
        XCTAssertEqual(result, "Hey @roman")
    }

    // MARK: - AttributedString

    func testAttributedStringDoesNotCrash() {
        let input = "*bold* _italic_ ~strike~ `code` <https://test.com|link> :fire:"
        let result = SlackTextParser.toAttributedString(input)
        XCTAssertFalse(String(result.characters).isEmpty)
    }

    func testAttributedStringPreservesContent() {
        let input = "Hello world"
        let result = SlackTextParser.toAttributedString(input)
        XCTAssertEqual(String(result.characters), "Hello world")
    }

    func testAttributedStringAutolinksBareURLs() {
        let input = "See https://whitebit-dev.atlassian.net/browse/REG-4 now"
        let result = SlackTextParser.toAttributedString(input)
        var foundLink = false
        for run in result.runs where run.link != nil {
            foundLink = true
            XCTAssertEqual(run.link?.absoluteString, "https://whitebit-dev.atlassian.net/browse/REG-4")
        }
        XCTAssertTrue(foundLink, "Bare URL should become a clickable link")
    }

    func testAttributedStringResolvesUserMentionViaLookup() {
        let input = "Hey <@U09H4EMS85U>"
        let result = SlackTextParser.toAttributedString(input, userNames: ["U09H4EMS85U": "Roman Olifir"])
        XCTAssertEqual(String(result.characters), "Hey @Roman Olifir")
    }
}
