import SwiftUI

/// Renders Slack message text with standard emoji (Unicode) and custom emoji (inline images).
struct MessageTextView: View {
    let rawText: String
    let customEmojiMap: [String: String]
    let imageCache: EmojiImageCache
    var font: Font = .body

    var body: some View {
        let segments = buildSegments()
        buildText(from: segments)
            .font(font)
            .textSelection(.enabled)
    }

    /// Parse text into segments: resolve entities, standard emoji, then split on custom emoji.
    private func buildSegments() -> [Segment] {
        var text = SlackTextParser.resolveEntities(rawText)
        text = SlackEmoji.resolve(text)
        text = SlackTextParser.slackToMarkdown(text)
        return splitOnCustomEmoji(text)
    }

    /// Split text on :shortcodes: that match custom emoji, returning text and emoji segments.
    private func splitOnCustomEmoji(_ text: String) -> [Segment] {
        let pattern = try! NSRegularExpression(pattern: #":([a-zA-Z0-9_+\-]+):"#)
        let nsText = text as NSString
        let matches = pattern.matches(in: text, range: NSRange(location: 0, length: nsText.length))

        guard !matches.isEmpty else {
            return [.text(text)]
        }

        var segments: [Segment] = []
        var lastEnd = 0

        for match in matches {
            let shortcode = nsText.substring(with: match.range(at: 1))

            // Only treat as custom emoji if we have it in the map
            guard let url = customEmojiMap[shortcode] else {
                continue
            }

            // Add text before this match
            if match.range.location > lastEnd {
                let beforeRange = NSRange(location: lastEnd, length: match.range.location - lastEnd)
                segments.append(.text(nsText.substring(with: beforeRange)))
            }

            segments.append(.emoji(name: shortcode, url: url))
            lastEnd = match.range.location + match.range.length
        }

        // Add remaining text
        if lastEnd < nsText.length {
            segments.append(.text(nsText.substring(from: lastEnd)))
        }

        // If no custom emoji were found, return full text
        if segments.isEmpty {
            return [.text(text)]
        }

        return segments
    }

    /// Build a concatenated Text view from segments.
    @ViewBuilder
    private func buildText(from segments: [Segment]) -> some View {
        if segments.allSatisfy({ if case .text = $0 { return true } else { return false } }) {
            // No custom emoji — use AttributedString for markdown support
            let fullText = segments.map { if case .text(let s) = $0 { return s } else { return "" } }.joined()
            if let attr = try? AttributedString(markdown: fullText, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) {
                Text(attr)
            } else {
                Text(fullText)
            }
        } else {
            // Has custom emoji — build concatenated Text
            segments.reduce(Text("")) { result, segment in
                switch segment {
                case .text(let str):
                    if let attr = try? AttributedString(markdown: str, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) {
                        return result + Text(attr)
                    }
                    return result + Text(str)
                case .emoji(let name, let url):
                    if let nsImage = imageCache.image(for: name, url: url) {
                        let image = Image(nsImage: nsImage)
                        return result + Text(image)
                    }
                    // Not loaded yet — show shortcode as placeholder
                    return result + Text(":\(name):")
                        .foregroundColor(.secondary)
                }
            }
        }
    }
}

// MARK: - Segment

private enum Segment {
    case text(String)
    case emoji(name: String, url: String)
}
