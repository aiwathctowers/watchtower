import Foundation
#if canImport(AppKit)
import AppKit
#endif

/// Parses Slack mrkdwn into plain text or AttributedString.
enum SlackTextParser {

    // MARK: - Compiled regex patterns

    // swiftformat:disable all
    private static let linkPattern = try? NSRegularExpression(pattern: #"<(https?://[^|>]+)\|([^>]+)>"#)
    private static let bareLinkPattern = try? NSRegularExpression(pattern: #"<(https?://[^>]+)>"#)
    private static let userMentionWithNamePattern = try? NSRegularExpression(pattern: #"<@U[A-Z0-9]+\|([^>]+)>"#)
    private static let userMentionPattern = try? NSRegularExpression(pattern: #"<@(U[A-Z0-9]+)>"#)
    private static let channelMentionPattern = try? NSRegularExpression(pattern: #"<#C[A-Z0-9]+\|([^>]+)>"#)
    private static let dateTemplatePattern = try? NSRegularExpression(pattern: #"<!date\^(\d+)\^[^|>]*(?:\|([^>]*))?>"#)
    private static let specialMentionPattern = try? NSRegularExpression(pattern: #"<!(\w+)(?:\|[^>]+)?>"#)
    private static let bareURLPattern = try? NSRegularExpression(pattern: #"(?<![<(\[])\b(https?://[^\s<>)\]]+)"#)
    private static let codeBlockRegex = try? NSRegularExpression(pattern: #"```[\s\S]*?```"#, options: [.dotMatchesLineSeparators])
    private static let inlineCodeRegex = try? NSRegularExpression(pattern: #"`([^`]+)`"#)
    private static let boldRegex = try? NSRegularExpression(pattern: #"(?<!\w)\*([^\*]+)\*(?!\w)"#)
    private static let italicRegex = try? NSRegularExpression(pattern: #"(?<!\w)_([^_]+)_(?!\w)"#)
    private static let strikeRegex = try? NSRegularExpression(pattern: #"(?<!\w)~([^~]+)~(?!\w)"#)
    private static let blockquoteRegex = try? NSRegularExpression(pattern: #"(?m)^&gt;\s?"#)
    private static let codeBlockCaptureRegex = try? NSRegularExpression(pattern: #"```([\s\S]*?)```"#, options: [.dotMatchesLineSeparators])
    // swiftformat:enable all

    // MARK: - Plain text (strips all formatting)

    /// Convert Slack mrkdwn to plain text.
    /// - Parameter userNames: optional `userID -> displayName` map used to resolve bare `<@U…>` mentions.
    static func toPlainText(_ input: String, userNames: [String: String] = [:]) -> String {
        var text = resolveEntities(input, userNames: userNames)
        text = SlackEmoji.resolve(text)
        text = stripFormatting(text)
        return text
    }

    // MARK: - Rich text (AttributedString)

    /// Convert Slack mrkdwn to AttributedString with formatting preserved.
    /// - Parameter userNames: optional `userID -> displayName` map used to resolve bare `<@U…>` mentions.
    static func toAttributedString(_ input: String, userNames: [String: String] = [:]) -> AttributedString {
        var text = resolveEntities(input, userNames: userNames)
        text = SlackEmoji.resolve(text)
        text = slackToMarkdown(text)
        text = autolinkBareURLs(text)

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

    /// Resolve Slack entities: links, user mentions, channel mentions, date templates.
    /// - Parameter userNames: optional `userID -> displayName` map; bare `<@U…>` mentions are
    ///   replaced with `@DisplayName` when a match is found, otherwise fall back to the user ID.
    static func resolveEntities(_ input: String, userNames: [String: String] = [:]) -> String {
        var text = input

        // Date templates: <!date^TS^format|fallback> → fallback (or formatted TS if no fallback)
        if let pattern = dateTemplatePattern {
            text = replaceMatches(in: text, pattern: pattern) { match, source in
                let fallback = capturedString(match, at: 2, in: source)
                if let fallback, !fallback.isEmpty { return fallback }
                if let tsStr = capturedString(match, at: 1, in: source),
                   let ts = TimeInterval(tsStr) {
                    let date = Date(timeIntervalSince1970: ts)
                    return Self.fallbackDateFormatter.string(from: date)
                }
                return ""
            }
        }

        // Resolve links: <https://url|display text> → display text, <https://url> → url
        if let pattern = linkPattern {
            text = pattern.stringByReplacingMatches(
                in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "$2"
            )
        }

        if let pattern = bareLinkPattern {
            text = pattern.stringByReplacingMatches(
                in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "$1"
            )
        }

        // User mentions with display name: <@U123ABC|Name> → @Name
        if let pattern = userMentionWithNamePattern {
            text = pattern.stringByReplacingMatches(
                in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "@$1"
            )
        }

        // User mentions without display name: <@U123ABC> → @DisplayName if known, else @U123ABC
        if let pattern = userMentionPattern {
            text = replaceMatches(in: text, pattern: pattern) { match, source in
                guard let uid = capturedString(match, at: 1, in: source) else { return "" }
                if let name = userNames[uid], !name.isEmpty {
                    return "@\(name)"
                }
                return "@\(uid)"
            }
        }

        // Channel mentions: <#C123ABC|channel-name> → #channel-name
        if let pattern = channelMentionPattern {
            text = pattern.stringByReplacingMatches(
                in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "#$1"
            )
        }

        // Special commands: <!here>, <!channel>, <!everyone>
        if let pattern = specialMentionPattern {
            text = pattern.stringByReplacingMatches(
                in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "@$1"
            )
        }

        return text
    }

    // MARK: - Regex helpers

    private static let fallbackDateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .short
        return fmt
    }()

    private static func capturedString(
        _ match: NSTextCheckingResult,
        at group: Int,
        in source: String
    ) -> String? {
        guard group < match.numberOfRanges else { return nil }
        let range = match.range(at: group)
        guard range.location != NSNotFound, let swiftRange = Range(range, in: source) else { return nil }
        return String(source[swiftRange])
    }

    private static func replaceMatches(
        in source: String,
        pattern: NSRegularExpression,
        transform: (NSTextCheckingResult, String) -> String
    ) -> String {
        let fullRange = NSRange(source.startIndex..., in: source)
        let matches = pattern.matches(in: source, range: fullRange)
        guard !matches.isEmpty else { return source }

        var result = source
        for match in matches.reversed() {
            guard let range = Range(match.range, in: result) else { continue }
            let replacement = transform(match, result)
            result.replaceSubrange(range, with: replacement)
        }
        return result
    }

    /// Wrap bare URLs in `<…>` so `AttributedString(markdown:)` renders them as autolinks.
    private static func autolinkBareURLs(_ text: String) -> String {
        guard let pattern = bareURLPattern else { return text }
        return replaceMatches(in: text, pattern: pattern) { match, source in
            guard let url = capturedString(match, at: 1, in: source) else { return "" }
            return "<\(url)>"
        }
    }

    /// Strip all formatting marks for plain text output.
    private static func stripFormatting(_ text: String) -> String {
        var result = text

        // Remove code blocks (```...```)
        if let regex = codeBlockRegex {
            let matches = regex.matches(in: result, range: NSRange(result.startIndex..., in: result))
            for match in matches.reversed() {
                guard let range = Range(match.range, in: result) else { continue }
                var content = String(result[range])
                content = content.replacingOccurrences(of: "```", with: "")
                result.replaceSubrange(range, with: content.trimmingCharacters(in: .whitespacesAndNewlines))
            }
        }

        // Remove inline code
        if let regex = inlineCodeRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
            )
        }

        // Remove bold markers
        if let regex = boldRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
            )
        }

        // Remove italic markers
        if let regex = italicRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
            )
        }

        // Remove strikethrough markers
        if let regex = strikeRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1"
            )
        }

        // Remove blockquote markers
        if let regex = blockquoteRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: ""
            )
        }

        return result
    }

    /// Convert Slack mrkdwn formatting to standard Markdown for AttributedString parsing.
    static func slackToMarkdown(_ text: String) -> String {
        var result = text

        // Handle code blocks first — protect them from further processing
        var codeBlocks: [String] = []
        let codeBlockMatches = (codeBlockCaptureRegex?.matches(
            in: result, range: NSRange(result.startIndex..., in: result)
        )) ?? []
        for (i, match) in codeBlockMatches.reversed().enumerated() {
            guard let range = Range(match.range, in: result),
                  let contentRange = Range(match.range(at: 1), in: result) else { continue }
            let content = String(result[contentRange]).trimmingCharacters(in: .whitespacesAndNewlines)
            let placeholder = "⟪CODEBLOCK\(codeBlockMatches.count - 1 - i)⟫"
            codeBlocks.insert(content, at: 0)
            result.replaceSubrange(range, with: placeholder)
        }

        // Handle inline code — protect from further processing
        var inlineCodes: [String] = []
        let inlineMatches = (inlineCodeRegex?.matches(
            in: result, range: NSRange(result.startIndex..., in: result)
        )) ?? []
        for (i, match) in inlineMatches.reversed().enumerated() {
            guard let range = Range(match.range, in: result),
                  let contentRange = Range(match.range(at: 1), in: result) else { continue }
            let content = String(result[contentRange])
            let placeholder = "⟪INLINE\(inlineMatches.count - 1 - i)⟫"
            inlineCodes.insert(content, at: 0)
            result.replaceSubrange(range, with: placeholder)
        }

        // Convert Slack bold *text* → Markdown **text**
        if let regex = boldRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "**$1**"
            )
        }

        // Slack _italic_ → Markdown _italic_ (same syntax, no change needed)

        // Convert Slack ~strikethrough~ → Markdown ~~strikethrough~~
        if let regex = strikeRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "~~$1~~"
            )
        }

        // Convert blockquotes: &gt; → >
        if let regex = blockquoteRegex {
            result = regex.stringByReplacingMatches(
                in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "> "
            )
        }

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
