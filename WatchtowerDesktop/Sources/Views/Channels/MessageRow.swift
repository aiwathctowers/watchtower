import SwiftUI

struct MessageRow: View {
    let message: Message
    let dbManager: DatabaseManager?
    let isGrouped: Bool
    let onThreadTap: (() -> Void)?
    var customEmojiMap: [String: String] = [:]
    var emojiImageCache: EmojiImageCache?

    @State private var userName: String?
    @State private var isHovered = false

    init(
        message: Message,
        dbManager: DatabaseManager?,
        isGrouped: Bool = false,
        onThreadTap: (() -> Void)? = nil,
        customEmojiMap: [String: String] = [:],
        emojiImageCache: EmojiImageCache? = nil
    ) {
        self.message = message
        self.dbManager = dbManager
        self.isGrouped = isGrouped
        self.onThreadTap = onThreadTap
        self.customEmojiMap = customEmojiMap
        self.emojiImageCache = emojiImageCache
    }

    private var displayName: String {
        userName ?? (message.userID.isEmpty ? "Unknown" : message.userID)
    }

    var body: some View {
        if isGrouped {
            groupedRow
        } else {
            headerRow
        }
    }

    // Full row with avatar + name + timestamp
    private var headerRow: some View {
        HStack(alignment: .top, spacing: 8) {
            AvatarView(name: displayName, userID: message.userID)

            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(displayName)
                        .font(.subheadline)
                        .fontWeight(.semibold)

                    Text(TimeFormatting.relativeTimeFromUnix(message.tsUnix))
                        .font(.caption2)
                        .foregroundStyle(.secondary)

                    Spacer()

                    threadButton
                }

                messageText
            }
        }
        .padding(.vertical, 4)
        .padding(.horizontal, 4)
        .background(hoverBackground)
        .onHover { isHovered = $0 }
        .task { await resolveUser() }
    }

    // Continuation row — no avatar/name, just indented text
    private var groupedRow: some View {
        HStack(alignment: .top, spacing: 8) {
            // Invisible spacer matching avatar width
            Color.clear
                .frame(width: 32, height: 1)
                .overlay(alignment: .center) {
                    if isHovered {
                        Text(TimeFormatting.shortTime(message.tsUnix))
                            .font(.system(size: 9))
                            .foregroundStyle(.tertiary)
                            .frame(width: 32)
                    }
                }

            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    messageText
                    Spacer()
                    threadButton
                }
            }
        }
        .padding(.vertical, 1)
        .padding(.horizontal, 4)
        .background(hoverBackground)
        .onHover { isHovered = $0 }
        .task { await resolveUser() }
    }

    @ViewBuilder
    private var messageText: some View {
        if !message.text.isEmpty {
            if let cache = emojiImageCache, !customEmojiMap.isEmpty {
                MessageTextView(rawText: message.text, customEmojiMap: customEmojiMap, imageCache: cache)
            } else {
                Text(SlackTextParser.toAttributedString(message.text))
                    .font(.body)
                    .textSelection(.enabled)
            }
        }
    }

    @ViewBuilder
    private var threadButton: some View {
        if message.replyCount > 0 {
            Button {
                onThreadTap?()
            } label: {
                HStack(spacing: 3) {
                    Image(systemName: "bubble.left.and.bubble.right")
                        .font(.caption2)
                    Text("\(message.replyCount)")
                        .font(.caption)
                }
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color.accentColor)
        }
    }

    private var hoverBackground: some View {
        RoundedRectangle(cornerRadius: 4)
            .fill(isHovered ? Color.primary.opacity(0.04) : Color.clear)
    }

    private func resolveUser() async {
        guard let db = dbManager, !message.userID.isEmpty else { return }
        do {
            let name = try await Task.detached {
                try db.dbPool.read { dbConn in
                    try UserQueries.fetchDisplayName(dbConn, forID: message.userID)
                }
            }.value
            userName = name
        } catch {}
    }
}

// MARK: - Avatar

struct AvatarView: View {
    let name: String
    let userID: String

    private var initial: String {
        String(name.prefix(1)).uppercased()
    }

    private var color: Color {
        let colors: [Color] = [
            .red, .orange, .yellow, .green, .mint, .teal,
            .cyan, .blue, .indigo, .purple, .pink, .brown
        ]
        let hash = userID.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }
        return colors[abs(hash) % colors.count]
    }

    var body: some View {
        ZStack {
            Circle()
                .fill(color.gradient)
                .frame(width: 32, height: 32)

            Text(initial)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(.white)
        }
    }
}
