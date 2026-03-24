import Foundation
import GRDB

/// Resolves emoji shortcodes using both standard Unicode mapping and custom workspace emojis.
/// Standard emojis are replaced with Unicode characters inline.
/// Custom emojis are identified and returned as references for image rendering.
@MainActor
final class EmojiResolver: Observable {
    private static let emojiPattern = try? NSRegularExpression(pattern: #":([a-zA-Z0-9_+\-]+):"#)

    /// Map of custom emoji name → image URL
    private(set) var customEmojiMap: [String: String] = [:]

    private let dbPool: DatabasePool

    init(dbPool: DatabasePool) {
        self.dbPool = dbPool
    }

    /// Reload custom emoji map from database.
    func reload() {
        do {
            customEmojiMap = try dbPool.read { db in
                try CustomEmojiQueries.fetchEmojiMap(db)
            }
        } catch {
            // Non-fatal: custom emoji just won't render
        }
    }

    /// Resolve standard emoji shortcodes in text (returns String with Unicode replacements).
    /// Custom emoji shortcodes are left as-is for the view layer to handle.
    func resolveStandard(_ text: String) -> String {
        SlackEmoji.resolve(text)
    }

    /// Parse text and return segments: plain text parts and custom emoji references.
    func parse(_ text: String) -> [MessageSegment] {
        // First resolve standard emoji
        let withStandard = SlackEmoji.resolve(text)

        guard let pattern = Self.emojiPattern else { return [.text(SlackEmoji.resolve(text))] }
        let nsText = withStandard as NSString
        let matches = pattern.matches(in: withStandard, range: NSRange(location: 0, length: nsText.length))

        guard !matches.isEmpty else {
            return [.text(withStandard)]
        }

        var segments: [MessageSegment] = []
        var lastEnd = 0

        for match in matches {
            let shortcode = nsText.substring(with: match.range(at: 1))

            // Add text before this match
            if match.range.location > lastEnd {
                let textRange = NSRange(location: lastEnd, length: match.range.location - lastEnd)
                segments.append(.text(nsText.substring(with: textRange)))
            }

            if let url = customEmojiMap[shortcode] {
                segments.append(.customEmoji(name: shortcode, url: url))
            } else {
                // Unknown emoji — leave as-is
                segments.append(.text(nsText.substring(with: match.range)))
            }

            lastEnd = match.range.location + match.range.length
        }

        // Add remaining text
        if lastEnd < nsText.length {
            segments.append(.text(nsText.substring(from: lastEnd)))
        }

        return segments
    }
}

/// A segment of a parsed message — either plain text or a custom emoji reference.
enum MessageSegment: Equatable {
    case text(String)
    case customEmoji(name: String, url: String)
}
