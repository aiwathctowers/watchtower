import SwiftUI

/// Renders markdown text with proper block structure (headers, lists, code blocks, paragraphs).
/// Uses block-level parsing + inline markdown rendering to correctly preserve line breaks.
struct MarkdownText: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(parseBlocks().enumerated()), id: \.offset) { _, block in
                renderBlock(block)
            }
        }
        .textSelection(.enabled)
    }

    // MARK: - Block types

    private enum Block {
        case paragraph(String)
        case header(Int, String)
        case codeBlock(String)
        case bulletList([String])
        case numberedList([String])
        case blockquote(String)
        case divider
    }

    // MARK: - Helpers

    /// Returns header level (1-6) and content if the line is a header, nil otherwise.
    private static func parseHeader(_ line: String) -> (Int, String)? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        var level = 0
        for ch in trimmed {
            if ch == "#" { level += 1 } else { break }
        }
        guard level >= 1 && level <= 6 else { return nil }
        let rest = trimmed.dropFirst(level)
        guard rest.hasPrefix(" ") else { return nil }
        return (level, String(rest.dropFirst()).trimmingCharacters(in: .whitespaces))
    }

    /// Returns true if the line starts a numbered list item (e.g. "1. " or "2) ").
    private static func isNumberedListItem(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard let first = trimmed.first, first.isNumber else { return false }
        var i = trimmed.startIndex
        while i < trimmed.endIndex && trimmed[i].isNumber { i = trimmed.index(after: i) }
        guard i < trimmed.endIndex else { return false }
        let sep = trimmed[i]
        guard sep == "." || sep == ")" else { return false }
        let after = trimmed.index(after: i)
        guard after < trimmed.endIndex && trimmed[after] == " " else { return false }
        return true
    }

    /// Extracts the text after the "N. " or "N) " prefix of a numbered list item.
    private static func numberedListContent(_ line: String) -> String {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard let dotIdx = trimmed.firstIndex(where: { $0 == "." || $0 == ")" }) else { return trimmed }
        let afterDot = trimmed.index(after: dotIdx)
        guard afterDot < trimmed.endIndex else { return "" }
        return String(trimmed[afterDot...]).trimmingCharacters(in: .whitespaces)
    }

    /// Returns true if the line is a block-level start (not a paragraph continuation).
    private static func isBlockStart(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("```") { return true }
        if parseHeader(t) != nil { return true }
        if t.hasPrefix("- ") || t.hasPrefix("* ") { return true }
        if isNumberedListItem(t) { return true }
        if t.hasPrefix("> ") { return true }
        if t == "---" || t == "***" || t == "___" { return true }
        return false
    }

    // MARK: - Parser

    private func parseBlocks() -> [Block] {
        var blocks: [Block] = []
        let lines = text.components(separatedBy: "\n")
        var i = 0

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            // Skip blank lines
            if trimmed.isEmpty {
                i += 1
                continue
            }

            // Horizontal rule
            if trimmed == "---" || trimmed == "***" || trimmed == "___" {
                blocks.append(.divider)
                i += 1
                continue
            }

            // Fenced code block
            if trimmed.hasPrefix("```") {
                var codeLines: [String] = []
                i += 1
                while i < lines.count {
                    if lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                        i += 1
                        break
                    }
                    codeLines.append(lines[i])
                    i += 1
                }
                blocks.append(.codeBlock(codeLines.joined(separator: "\n")))
                continue
            }

            // Header
            if let (level, content) = Self.parseHeader(trimmed) {
                blocks.append(.header(level, content))
                i += 1
                continue
            }

            // Blockquote
            if trimmed.hasPrefix("> ") {
                var quoteLines: [String] = []
                while i < lines.count {
                    let l = lines[i].trimmingCharacters(in: .whitespaces)
                    if l.hasPrefix("> ") {
                        quoteLines.append(String(l.dropFirst(2)))
                    } else if l.isEmpty || Self.isBlockStart(l) {
                        break
                    } else {
                        break
                    }
                    i += 1
                }
                blocks.append(.blockquote(quoteLines.joined(separator: "\n")))
                continue
            }

            // Bullet list (- or *)
            if trimmed.hasPrefix("- ") || trimmed.hasPrefix("* ") {
                var items: [String] = []
                while i < lines.count {
                    let l = lines[i].trimmingCharacters(in: .whitespaces)
                    if l.hasPrefix("- ") {
                        items.append(String(l.dropFirst(2)))
                    } else if l.hasPrefix("* ") {
                        items.append(String(l.dropFirst(2)))
                    } else if l.isEmpty {
                        break
                    } else if !items.isEmpty && !Self.isBlockStart(l) {
                        // Continuation of last item
                        items[items.count - 1] += " " + l
                    } else {
                        break
                    }
                    i += 1
                }
                blocks.append(.bulletList(items))
                continue
            }

            // Numbered list
            if Self.isNumberedListItem(trimmed) {
                var items: [String] = []
                while i < lines.count {
                    let l = lines[i].trimmingCharacters(in: .whitespaces)
                    if Self.isNumberedListItem(l) {
                        items.append(Self.numberedListContent(l))
                    } else if l.isEmpty {
                        break
                    } else if !items.isEmpty && !Self.isBlockStart(l) {
                        items[items.count - 1] += " " + l
                    } else {
                        break
                    }
                    i += 1
                }
                blocks.append(.numberedList(items))
                continue
            }

            // Regular paragraph — collect lines until a blank line or block element
            var paraLines: [String] = []
            while i < lines.count {
                let l = lines[i]
                let t = l.trimmingCharacters(in: .whitespaces)
                if t.isEmpty || Self.isBlockStart(t) { break }
                paraLines.append(l)
                i += 1
            }
            if !paraLines.isEmpty {
                blocks.append(.paragraph(paraLines.joined(separator: "\n")))
            }
        }

        return blocks
    }

    // MARK: - Rendering

    @ViewBuilder
    private func renderBlock(_ block: Block) -> some View {
        switch block {
        case .paragraph(let text):
            inlineMarkdown(text)

        case .header(let level, let text):
            inlineMarkdown(text)
                .font(headerFont(level))
                .fontWeight(.bold)

        case .codeBlock(let code):
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.textBackgroundColor).opacity(0.6), in: RoundedRectangle(cornerRadius: 6))

        case .bulletList(let items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .top, spacing: 6) {
                        Text("\u{2022}")
                            .foregroundStyle(.secondary)
                        inlineMarkdown(item)
                    }
                }
            }

        case .numberedList(let items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { idx, item in
                    HStack(alignment: .top, spacing: 6) {
                        Text("\(idx + 1).")
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                        inlineMarkdown(item)
                    }
                }
            }

        case .blockquote(let text):
            HStack(spacing: 0) {
                Rectangle()
                    .fill(Color.accentColor.opacity(0.4))
                    .frame(width: 3)
                inlineMarkdown(text)
                    .foregroundStyle(.secondary)
                    .padding(.leading, 8)
            }

        case .divider:
            Divider()
                .padding(.vertical, 4)
        }
    }

    private func inlineMarkdown(_ text: String) -> Text {
        // inlineOnlyPreservingWhitespace keeps \n as actual line breaks and renders bold/italic/code/links
        let options = AttributedString.MarkdownParsingOptions(
            interpretedSyntax: .inlineOnlyPreservingWhitespace
        )
        if let attr = try? AttributedString(markdown: text, options: options) {
            return Text(attr)
        }
        return Text(text)
    }

    private func headerFont(_ level: Int) -> Font {
        switch level {
        case 1: .title
        case 2: .title2
        case 3: .title3
        default: .headline
        }
    }
}
