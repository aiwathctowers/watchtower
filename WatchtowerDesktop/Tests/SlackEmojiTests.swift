import XCTest
@testable import WatchtowerDesktop

final class SlackEmojiTests: XCTestCase {

    func testResolveSingleEmoji() {
        XCTAssertEqual(SlackEmoji.resolve(":fire:"), "🔥")
    }

    func testResolveMultipleEmojis() {
        XCTAssertEqual(SlackEmoji.resolve(":thumbsup: :rocket:"), "👍 🚀")
    }

    func testUnknownEmojiPreserved() {
        XCTAssertEqual(SlackEmoji.resolve(":nonexistent_emoji:"), ":nonexistent_emoji:")
    }

    func testMixedKnownAndUnknown() {
        XCTAssertEqual(SlackEmoji.resolve(":fire: :unknown: :heart:"), "🔥 :unknown: ❤️")
    }

    func testNoEmojis() {
        XCTAssertEqual(SlackEmoji.resolve("plain text"), "plain text")
    }

    func testEmptyString() {
        XCTAssertEqual(SlackEmoji.resolve(""), "")
    }

    func testEmojiInSentence() {
        XCTAssertEqual(SlackEmoji.resolve("This is :100: percent"), "This is 💯 percent")
    }

    func testPlusOneAlias() {
        XCTAssertEqual(SlackEmoji.resolve(":+1:"), "👍")
    }

    func testMinusOneAlias() {
        XCTAssertEqual(SlackEmoji.resolve(":-1:"), "👎")
    }

    func testColonInMiddleOfWord() {
        // Should NOT match partial colons in non-emoji contexts
        let input = "time:10:30"
        let result = SlackEmoji.resolve(input)
        // The regex matches :10: but "10" is not in the map, so it stays
        XCTAssertEqual(result, "time:10:30")
    }

    func testStatusEmojis() {
        XCTAssertEqual(SlackEmoji.resolve(":warning:"), "⚠️")
        XCTAssertEqual(SlackEmoji.resolve(":white_check_mark:"), "✅")
        XCTAssertEqual(SlackEmoji.resolve(":x:"), "❌")
        XCTAssertEqual(SlackEmoji.resolve(":rotating_light:"), "🚨")
    }

    func testTechEmojis() {
        XCTAssertEqual(SlackEmoji.resolve(":bug:"), "🐛")
        XCTAssertEqual(SlackEmoji.resolve(":wrench:"), "🔧")
        XCTAssertEqual(SlackEmoji.resolve(":package:"), "📦")
        XCTAssertEqual(SlackEmoji.resolve(":computer:"), "💻")
    }

    func testFaceEmojis() {
        XCTAssertEqual(SlackEmoji.resolve(":thinking_face:"), "🤔")
        XCTAssertEqual(SlackEmoji.resolve(":joy:"), "😂")
        XCTAssertEqual(SlackEmoji.resolve(":skull:"), "💀")
    }
}
