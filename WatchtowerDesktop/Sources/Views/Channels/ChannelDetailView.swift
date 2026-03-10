import SwiftUI

private struct ThreadSelection: Identifiable {
    let id: String
}

struct ChannelDetailView: View {
    let channelID: String
    @Environment(AppState.self) private var appState
    @State private var channel: Channel?
    @State private var messages: [Message] = []
    @State private var offset = 0
    @State private var hasMore = false
    @State private var isLoadingOlder = false
    @State private var initialScrollDone = false
    @State private var selectedThread: ThreadSelection?
    @State private var errorMessage: String?
    @State private var dmUserName: String?

    private let pageSize = 50

    var body: some View {
        VStack(spacing: 0) {
            if let channel {
                channelHeader(channel)
                Divider()
            }

            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        // Lazy load trigger — only active after initial scroll to bottom
                        if hasMore {
                            ProgressView()
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 8)
                                .onAppear {
                                    guard initialScrollDone else { return }
                                    loadOlder()
                                }
                        }

                        ForEach(Array(messages.enumerated()), id: \.element.id) { index, msg in
                            let isGrouped = isGroupedWithPrevious(index: index)

                            if !isGrouped && index > 0 {
                                Divider()
                                    .padding(.leading, 44)
                                    .padding(.vertical, 4)
                            }

                            MessageRow(
                                message: msg,
                                dbManager: appState.databaseManager,
                                isGrouped: isGrouped,
                                onThreadTap: {
                                    selectedThread = ThreadSelection(id: msg.ts)
                                },
                                customEmojiMap: appState.customEmojiMap,
                                emojiImageCache: appState.emojiImageCache
                            )
                            .id(msg.id)
                        }

                        Color.clear
                            .frame(height: 1)
                            .id("bottom")
                    }
                    .padding()
                }
                .task {
                    await load()
                    // Wait for layout to complete before scrolling
                    try? await Task.sleep(for: .milliseconds(150))
                    proxy.scrollTo("bottom", anchor: .bottom)
                    try? await Task.sleep(for: .milliseconds(100))
                    initialScrollDone = true
                }
            }

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(8)
            }
        }
        .navigationTitle(displayTitle)
        .sheet(item: $selectedThread) { thread in
            if let db = appState.databaseManager {
                ThreadView(channelID: channelID, threadTS: thread.id, dbManager: db, customEmojiMap: appState.customEmojiMap, emojiImageCache: appState.emojiImageCache)
            }
        }
    }

    private var displayTitle: String {
        guard let channel else { return "Channel" }
        if channel.type == "dm" {
            return dmUserName ?? channel.name
        }
        return channel.name
    }

    private func channelHeader(_ ch: Channel) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                if ch.type == "dm" {
                    Text(dmUserName ?? ch.name)
                        .font(.headline)
                } else {
                    Text("#\(ch.name)")
                        .font(.headline)
                }

                Text(ch.type)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(.secondary.opacity(0.2), in: Capsule())

                if ch.type != "dm" {
                    Text("\(ch.numMembers) members")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()
            }

            if !ch.topic.isEmpty {
                Text(ch.topic)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .padding()
    }

    private func isGroupedWithPrevious(index: Int) -> Bool {
        guard index > 0 else { return false }
        let prev = messages[index - 1]
        let curr = messages[index]
        guard prev.userID == curr.userID, !curr.userID.isEmpty else { return false }
        return abs(curr.tsUnix - prev.tsUnix) < 300
    }

    private func load() async {
        guard let db = appState.databaseManager else { return }
        do {
            let (ch, msgs, total) = try await Task.detached {
                try db.dbPool.read { dbConn in
                    let ch = try ChannelQueries.fetchByID(dbConn, id: channelID)
                    let msgs = try MessageQueries.fetchByChannel(dbConn, channelID: channelID, limit: pageSize, offset: 0)
                    let total = try MessageQueries.countByChannel(dbConn, channelID: channelID)
                    return (ch, msgs, total)
                }
            }.value
            channel = ch
            messages = msgs
            offset = msgs.count
            hasMore = total > msgs.count
            errorMessage = nil

            // Resolve DM user name — dm_user_id may be empty, fall back to channel name
            if let ch, ch.type == "dm" {
                let uid = (ch.dmUserID?.isEmpty == false) ? ch.dmUserID! : ch.name
                await resolveDMUser(uid)
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func loadOlder() {
        guard !isLoadingOlder else { return }
        isLoadingOlder = true

        guard let db = appState.databaseManager else {
            isLoadingOlder = false
            return
        }
        let currentOffset = offset
        Task {
            do {
                let (more, total) = try await Task.detached {
                    try db.dbPool.read { dbConn in
                        let msgs = try MessageQueries.fetchByChannel(dbConn, channelID: channelID, limit: pageSize, offset: currentOffset)
                        let total = try MessageQueries.countByChannel(dbConn, channelID: channelID)
                        return (msgs, total)
                    }
                }.value
                messages.insert(contentsOf: more, at: 0)
                offset += more.count
                hasMore = total > offset
            } catch {
                errorMessage = error.localizedDescription
            }
            isLoadingOlder = false
        }
    }

    private func resolveDMUser(_ userID: String) async {
        guard let db = appState.databaseManager else { return }
        do {
            let name = try await Task.detached {
                try db.dbPool.read { dbConn in
                    try UserQueries.fetchDisplayName(dbConn, forID: userID)
                }
            }.value
            dmUserName = name
        } catch {}
    }
}
