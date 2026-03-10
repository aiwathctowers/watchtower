import SwiftUI

struct SearchResultRow: View {
    let result: SearchResult

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("#\(result.channelName ?? "unknown")")
                    .font(.caption)
                    .fontWeight(.medium)
                    .foregroundStyle(Color.accentColor)

                Text(result.userName ?? result.userID)
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Spacer()

                Text(TimeFormatting.relativeTimeFromUnix(result.tsUnix))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            if let snippet = result.snippet {
                // Strip <mark> tags for now — a future task will convert to AttributedString
                Text(snippet.replacingOccurrences(of: "<mark>", with: "").replacingOccurrences(of: "</mark>", with: ""))
                    .font(.subheadline)
                    .lineLimit(3)
            } else if !result.text.isEmpty {
                Text(SlackTextParser.toAttributedString(result.text))
                    .font(.subheadline)
                    .lineLimit(3)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}
