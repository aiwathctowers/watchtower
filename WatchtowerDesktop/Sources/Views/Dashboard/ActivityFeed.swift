import SwiftUI

struct ActivityFeed: View {
    let messages: [MessageWithContext]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Recent Activity")
                .font(.headline)

            if messages.isEmpty {
                emptyState
            } else {
                messageGroups
            }
        }
    }

    private var emptyState: some View {
        GroupBox {
            VStack(spacing: 8) {
                Image(systemName: "eye.slash")
                    .font(.title2)
                    .foregroundStyle(.secondary)
                Text("No watched channels")
                    .font(.subheadline)
                Text("Add channels to your watch list to see activity here.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity)
            .padding()
        }
    }

    private var messageGroups: some View {
        let grouped = Dictionary(grouping: messages) { $0.channelName ?? "unknown" }
        return ForEach(grouped.keys.sorted(), id: \.self) { channelName in
            Section {
                ForEach(grouped[channelName] ?? []) { msg in
                    ActivityRow(message: msg)
                }
            } header: {
                Text("#\(channelName)")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .foregroundStyle(Color.accentColor)
            }
        }
    }
}

private struct ActivityRow: View {
    let message: MessageWithContext

    var body: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(message.userName ?? (message.userID.isEmpty ? "Unknown" : message.userID))
                        .font(.subheadline)
                        .fontWeight(.medium)
                    Spacer()
                    Text(TimeFormatting.relativeTimeFromUnix(message.tsUnix))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                // H10: apply SlackTextParser
                Text(SlackTextParser.toAttributedString(message.text))
                    .font(.subheadline)
                    .lineLimit(2)
                    .foregroundStyle(.secondary)
            }
        }
    }
}
