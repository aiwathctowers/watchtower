import Foundation
#if canImport(AppKit)
import AppKit
#endif

/// Parses Slack mrkdwn into plain text or AttributedString.
enum SlackTextParser {

    // MARK: - Plain text (strips all formatting)

    /// Convert Slack mrkdwn to plain text
    static func toPlainText(_ input: String) -> String {
        var text = resolveEntities(input)
        text = SlackEmoji.resolve(text)
        text = stripFormatting(text)
        return text
    }

    // MARK: - Rich text (AttributedString)

    /// Convert Slack mrkdwn to AttributedString with formatting preserved.
    static func toAttributedString(_ input: String) -> AttributedString {
        var text = resolveEntities(input)
        text = SlackEmoji.resolve(text)
        text = slackToMarkdown(text)

        // Try parsing as Markdown; fall back to plain text
        if let attributed = try? AttributedString(
            markdown: text,
            options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        ) {
            return attributed
        }
        return AttributedString(text)
    }

    // MARK: - Internal helpers

    /// Resolve Slack entities: links, user mentions, channel mentions.
    static func resolveEntities(_ input: String) -> String {
        var text = input

        // Resolve links: <https://url|display text> → display text, <https://url> → url
        let linkPattern = try! NSRegularExpression(pattern: #"<(https?://[^|>]+)\|([^>]+)>"#)
        text = linkPattern.stringByReplacingMatches(
            in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "$2"
        )

        let bareLink = try! NSRegularExpression(pattern: #"<(https?://[^>]+)>"#)
        text = bareLink.stringByReplacingMatches(
            in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "$1"
        )

        // User mentions: <@U123ABC> → @U123ABC
        let userMention = try! NSRegularExpression(pattern: #"<@(U[A-Z0-9]+)>"#)
        text = userMention.stringByReplacingMatches(
            in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "@$1"
        )

        // Channel mentions: <#C123ABC|channel-name> → #channel-name
        let channelMention = try! NSRegularExpression(pattern: #"<#C[A-Z0-9]+\|([^>]+)>"#)
        text = channelMention.stringByReplacingMatches(
            in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "#$1"
        )

        // Special commands: <!here>, <!channel>, <!everyone>
        let specialMention = try! NSRegularExpression(pattern: #"<!(\w+)(\|[^>]+)?>"#)
        text = specialMention.stringByReplacingMatches(
            in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "@$1"
        )

        return text
    }

    /// Strip all formatting marks for plain text output.
    private static func stripFormatting(_ text: String) -> String {
        var result = text

        // Remove code blocks (```...```)
        let codeBlock = try! NSRegularExpression(pattern: #"```[\s\S]*?```"#, options: [.dotMatchesLineSeparators])
        let codeBlockMatches = codeBlock.matches(in: result, range: NSRange(result.startIndex..., in: result))
        for match in codeBlockMatches.reversed() {
            let range = Range(match.range, in: result)!
            var content = String(result[range])
            content = content.replacingOccurrences(of: "```", with: "")
            result.replaceSubrange(range, with: content.trimmingCharacters(in: .whitespacesAndNewlines))
        }

        // Remove inline code
        let inlineCode = try! NSRegularExpression(pattern: #"`([^`]+)`"#)
        result = inlineCode.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
        )

        // Remove bold markers
        let bold = try! NSRegularExpression(pattern: #"(?<!\w)\*([^\*]+)\*(?!\w)"#)
        result = bold.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
        )

        // Remove italic markers
        let italic = try! NSRegularExpression(pattern: #"(?<!\w)_([^_]+)_(?!\w)"#)
        result = italic.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
        )

        // Remove strikethrough markers
        let strike = try! NSRegularExpression(pattern: #"(?<!\w)~([^~]+)~(?!\w)"#)
        result = strike.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
        )

        // Remove blockquote markers
        let blockquote = try! NSRegularExpression(pattern: #"(?m)^&gt;\s?"#)
        result = blockquote.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: ""
        )

        return result
    }

    /// Convert Slack mrkdwn formatting to standard Markdown for AttributedString parsing.
    static func slackToMarkdown(_ text: String) -> String {
        var result = text

        // Handle code blocks first — protect them from further processing
        var codeBlocks: [String] = []
        let codeBlockPattern = try! NSRegularExpression(
            pattern: #"```([\s\S]*?)```"#, options: [.dotMatchesLineSeparators]
        )
        let codeBlockMatches = codeBlockPattern.matches(
            in: result, range: NSRange(result.startIndex..., in: result)
        )
        for (i, match) in codeBlockMatches.reversed().enumerated() {
            let range = Range(match.range, in: result)!
            let contentRange = Range(match.range(at: 1), in: result)!
            let content = String(result[contentRange]).trimmingCharacters(in: .whitespacesAndNewlines)
            let placeholder = "⟪CODEBLOCK\(codeBlockMatches.count - 1 - i)⟫"
            codeBlocks.insert(content, at: 0)
            result.replaceSubrange(range, with: placeholder)
        }

        // Handle inline code — protect from further processing
        var inlineCodes: [String] = []
        let inlineCodePattern = try! NSRegularExpression(pattern: #"`([^`]+)`"#)
        let inlineMatches = inlineCodePattern.matches(
            in: result, range: NSRange(result.startIndex..., in: result)
        )
        for (i, match) in inlineMatches.reversed().enumerated() {
            let range = Range(match.range, in: result)!
            let contentRange = Range(match.range(at: 1), in: result)!
            let content = String(result[contentRange])
            let placeholder = "⟪INLINE\(inlineMatches.count - 1 - i)⟫"
            inlineCodes.insert(content, at: 0)
            result.replaceSubrange(range, with: placeholder)
        }

        // Convert Slack bold *text* → Markdown **text**
        let bold = try! NSRegularExpression(pattern: #"(?<!\w)\*([^\*]+)\*(?!\w)"#)
        result = bold.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "**$1**"
        )

        // Slack _italic_ → Markdown _italic_ (same syntax, no change needed)

        // Convert Slack ~strikethrough~ → Markdown ~~strikethrough~~
        let strike = try! NSRegularExpression(pattern: #"(?<!\w)~([^~]+)~(?!\w)"#)
        result = strike.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "~~$1~~"
        )

        // Convert blockquotes: &gt; → >
        let blockquote = try! NSRegularExpression(pattern: #"(?m)^&gt;\s?"#)
        result = blockquote.stringByReplacingMatches(
            in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "> "
        )

        // Restore inline code
        for (i, code) in inlineCodes.enumerated() {
            result = result.replacingOccurrences(of: "⟪INLINE\(i)⟫", with: "`\(code)`")
        }

        // Restore code blocks (as inline code since AttributedString inline mode doesn't support blocks)
        for (i, code) in codeBlocks.enumerated() {
            result = result.replacingOccurrences(of: "⟪CODEBLOCK\(i)⟫", with: "`\(code)`")
        }

        return result
    }
}