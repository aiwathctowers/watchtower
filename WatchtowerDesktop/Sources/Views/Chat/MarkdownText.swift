import SwiftUI

struct MarkdownText: View {
    let text: String

    var body: some View {
        // Convert lone \n to hard line breaks (two trailing spaces + \n)
        // so Markdown renders them as actual line breaks instead of ignoring them.
        let processed = text.replacingOccurrences(
            of: "(?<!\n)\n(?!\n)",
            with: "  \n",
            options: .regularExpression
        )
        if let attrStr = try? AttributedString(markdown: processed, options: .init(interpretedSyntax: .full)) {
            Text(attrStr)
                .textSelection(.enabled)
        } else {
            Text(text)
                .textSelection(.enabled)
        }
    }
}
