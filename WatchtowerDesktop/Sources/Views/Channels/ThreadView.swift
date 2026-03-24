import SwiftUI

struct ThreadView: View {
    let channelID: String
    let threadTS: String
    let dbManager: DatabaseManager
    var customEmojiMap: [String: String] = [:]
    var emojiImageCache: EmojiImageCache?

    @State private var replies: [Message] = []
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Thread")
                    .font(.headline)
                Spacer()
                Button("Close") {
                    dismiss()
                }
                .keyboardShortcut(.escape)
            }
            .padding()

            Divider()

            if replies.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "bubble.left.and.bubble.right")
                        .font(.title)
                        .foregroundStyle(.secondary)
                    Text("No replies synced yet")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(replies.enumerated()), id: \.element.id) { index, msg in
                            let isGrouped = isGroupedWithPrevious(index: index)

                            if !isGrouped && index > 0 {
                                Divider()
                                    .padding(.leading, 44)
                                    .padding(.vertical, 4)
                            }

                            MessageRow(
                                message: msg,
                                dbManager: dbManager,
                                isGrouped: isGrouped,
                                onThreadTap: nil,
                                customEmojiMap: customEmojiMap,
                                emojiImageCache: emojiImageCache
                            )
                        }
                    }
                    .padding()
                }
            }
        }
        .frame(minWidth: 400, minHeight: 300)
        .onAppear { load() }
    }

    private func isGroupedWithPrevious(index: Int) -> Bool {
        guard index > 0 else { return false }
        let prev = replies[index - 1]
        let curr = replies[index]
        guard prev.userID == curr.userID, !curr.userID.isEmpty else { return false }
        return abs(curr.tsUnix - prev.tsUnix) < 300
    }

    private func load() {
        Task {
            do {
                let r = try await Task.detached { [dbManager, channelID, threadTS] in
                    try dbManager.dbPool.read { db in
                        try MessageQueries.fetchThreadReplies(db, channelID: channelID, threadTS: threadTS)
                    }
                }.value
                replies = r
            } catch {
                print("[ThreadView] load error: \(error.localizedDescription)")
            }
        }
    }
}
