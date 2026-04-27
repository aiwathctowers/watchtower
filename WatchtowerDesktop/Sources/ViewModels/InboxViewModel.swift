import Foundation
import GRDB

@MainActor
@Observable
final class InboxViewModel {
    var allItems: [InboxItem] = []
    var senderGroups: [SenderGroup] = []
    var pendingCount: Int = 0
    var unreadCount: Int = 0
    var highPriorityCount: Int = 0
    var isLoading = false
    var errorMessage: String?

    // Pinned / Feed split (Task 20)
    var pinnedItems: [InboxItem] = []
    var feedItems: [InboxItem] = []
    var hasHighPriorityPinned: Bool = false
    var feedPageSize: Int = 50
    private var feedOffset: Int = 0

    // Filters
    var priorityFilter: String?
    var triggerTypeFilter: String?
    var showResolved: Bool = false

    // Name caches
    private(set) var senderNames: [String: String] = [:]
    private(set) var channelNames: [String: String] = [:]
    private(set) var workspaceDomain: String?
    private(set) var workspaceTeamID: String?

    private let dbManager: DatabaseManager
    private let feedbackQueries: InboxFeedbackQueries
    private var observationTask: Task<Void, Never>?
    private var pollTask: Task<Void, Never>?

    /// Interval for the safety-net poll. GRDB ValueObservation cannot see writes
    /// from the Go daemon (separate process, separate SQLite update hooks), so
    /// the feed needs a periodic reload to surface daemon-inserted items.
    private let pollInterval: Duration = .seconds(30)

    struct SenderGroup: Identifiable {
        let senderUserID: String
        let senderName: String
        let items: [InboxItem]
        let highestPriority: String
        let unreadCount: Int
        let urgentCount: Int

        var id: String { senderUserID }
        var hasUrgent: Bool { urgentCount > 0 }

        var priorityOrder: Int {
            switch highestPriority {
            case "high": return 0
            case "medium": return 1
            case "low": return 2
            default: return 1
            }
        }

        var latestDate: Date {
            items.first?.createdDate ?? Date.distantPast
        }
    }

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
        self.feedbackQueries = InboxFeedbackQueries(dbPool: dbManager.dbPool)
    }

    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_items") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
        startPolling()
    }

    /// Force an immediate reload from disk. Called on view-appear so daemon
    /// inserts surface even when the user takes no action in the inbox.
    func refresh() {
        load()
    }

    private func startPolling() {
        guard pollTask == nil else { return }
        let interval = pollInterval
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard !Task.isCancelled else { break }
                self?.load()
            }
        }
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db in
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let counts = try InboxQueries.fetchCounts(db)
                let all = try InboxQueries.fetchAll(
                    db,
                    priority: self.priorityFilter,
                    triggerType: self.triggerTypeFilter,
                    includeResolved: self.showResolved
                )
                let pinned = try InboxQueries.fetchPinned(db)
                let feed = try InboxQueries.fetchFeed(db, limit: self.feedPageSize, offset: 0)
                let highPriorityPinned = try InboxQueries.hasHighPriorityPinned(db)
                return (ws?.domain, ws?.id, all, counts, pinned, feed, highPriorityPinned)
            }

            workspaceDomain = result.0
            workspaceTeamID = result.1
            let items = result.2
            let counts = result.3
            allItems = items
            pendingCount = counts.pending
            unreadCount = counts.unread
            highPriorityCount = counts.highPriority
            pinnedItems = result.4
            feedItems = result.5
            feedOffset = result.5.count
            hasHighPriorityPinned = result.6

            // Resolve sender and channel names
            let senderIDs = Set(items.map(\.senderUserID).filter { !$0.isEmpty })
            let channelIDs = Set(items.map(\.channelID).filter { !$0.isEmpty })
            let (sNames, cNames) = try dbManager.dbPool.read { db -> ([String: String], [String: String]) in
                var sn: [String: String] = [:]
                for uid in senderIDs {
                    let name = try UserQueries.fetchDisplayName(db, forID: uid)
                    sn[uid] = name
                }
                var cn: [String: String] = [:]
                for cid in channelIDs {
                    if let name = try String.fetchOne(db, sql: "SELECT name FROM channels WHERE id = ?", arguments: [cid]) {
                        cn[cid] = name
                    }
                }
                return (sn, cn)
            }
            senderNames = sNames
            channelNames = cNames

            // Build sender groups
            buildSenderGroups(from: items)

            errorMessage = nil
        } catch {
            allItems = []
            senderGroups = []
            pinnedItems = []
            feedItems = []
            hasHighPriorityPinned = false
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    /// Appends the next page of feed items (infinite scroll).
    func loadMore() {
        do {
            let next = try dbManager.dbPool.read { db in
                try InboxQueries.fetchFeed(db, limit: feedPageSize, offset: feedOffset)
            }
            feedItems.append(contentsOf: next)
            feedOffset += next.count
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Marks an inbox item as seen (sets read_at) if it hasn't been seen before.
    func markSeen(_ item: InboxItem) {
        guard item.readAt.isEmpty else { return }
        do {
            try dbManager.dbPool.write { db in
                try InboxQueries.markSeen(db, itemID: Int64(item.id))
            }
        } catch {
            errorMessage = "Failed to mark seen: \(error.localizedDescription)"
        }
    }

    /// Records thumbs-up/down feedback for an item and derives a learned rule, then reloads.
    func submitFeedback(_ item: InboxItem, rating: Int, reason: String) {
        do {
            try feedbackQueries.record(item: item, rating: rating, reason: reason)
            load()
        } catch {
            errorMessage = "Failed to submit feedback: \(error.localizedDescription)"
        }
    }

    func resolve(_ item: InboxItem, reason: String = "Manually resolved") {
        do {
            try dbManager.dbPool.write { db in
                try InboxQueries.resolve(db, id: item.id, reason: reason)
            }
            load()
        } catch {
            errorMessage = "Failed to resolve: \(error.localizedDescription)"
        }
    }

    func dismiss(_ item: InboxItem) {
        do {
            try dbManager.dbPool.write { db in
                try InboxQueries.dismiss(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to dismiss: \(error.localizedDescription)"
        }
    }

    func snooze(_ item: InboxItem, until: String) {
        do {
            try dbManager.dbPool.write { db in
                try InboxQueries.snooze(db, id: item.id, until: until)
            }
            load()
        } catch {
            errorMessage = "Failed to snooze: \(error.localizedDescription)"
        }
    }

    func markRead(_ item: InboxItem) {
        do {
            try dbManager.dbPool.write { db in
                try InboxQueries.markRead(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to mark read: \(error.localizedDescription)"
        }
    }

    func createTask(from item: InboxItem) {
        do {
            _ = try dbManager.dbPool.write { db in
                try InboxQueries.createTask(db, from: item)
            }
            load()
        } catch {
            errorMessage = "Failed to create task: \(error.localizedDescription)"
        }
    }

    func senderName(for item: InboxItem) -> String {
        senderNames[item.senderUserID] ?? (item.senderUserID.isEmpty ? "Unknown" : item.senderUserID)
    }

    func userName(for userID: String) -> String {
        senderNames[userID] ?? userID
    }

    func channelName(for item: InboxItem) -> String {
        if item.isDM {
            // For DMs, show sender name instead of channel ID
            return "DM"
        }
        return channelNames[item.channelID] ?? item.channelID
    }

    func itemByID(_ id: Int) -> InboxItem? {
        do {
            return try dbManager.dbPool.read { db in
                try InboxQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    /// Loads the live conversation around an inbox item from the local `messages` table.
    /// For thread-rooted items returns the full ordered thread; for top-level items returns
    /// a 10-before / trigger / 10-after window. Empty result means the local DB hasn't synced
    /// those messages yet — the caller should fall back to the stored `item.context` snapshot.
    func loadConversation(for item: InboxItem) -> [InboxConversationMessage] {
        do {
            return try dbManager.dbPool.read { db -> [InboxConversationMessage] in
                let messages: [Message]
                if !item.threadTS.isEmpty {
                    messages = try MessageQueries.fetchInboxThread(
                        db,
                        channelID: item.channelID,
                        threadTS: item.threadTS
                    )
                } else {
                    messages = try MessageQueries.fetchInboxChannelWindow(
                        db,
                        channelID: item.channelID,
                        aroundTS: item.messageTS
                    )
                }

                let userIDs = Set(messages.map(\.userID).filter { !$0.isEmpty })
                var nameByID: [String: String] = [:]
                for uid in userIDs {
                    nameByID[uid] = try UserQueries.fetchDisplayName(db, forID: uid)
                }

                return messages.compactMap { msg in
                    let cleaned = SlackTextParser.toPlainText(msg.text)
                    guard !cleaned.isEmpty else { return nil }
                    let name = nameByID[msg.userID] ?? (msg.userID.isEmpty ? "Unknown" : msg.userID)
                    return InboxConversationMessage(
                        id: msg.ts,
                        author: name,
                        text: cleaned,
                        isTrigger: msg.ts == item.messageTS,
                        date: Date(timeIntervalSince1970: msg.tsUnix)
                    )
                }
            }
        } catch {
            errorMessage = "Failed to load conversation: \(error.localizedDescription)"
            return []
        }
    }

    func slackMessageURL(for item: InboxItem) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        let ts = item.threadTS.isEmpty ? item.messageTS : item.threadTS
        return URL(string: "slack://channel?team=\(teamID)&id=\(item.channelID)&message=\(ts)")
    }

    // MARK: - Grouping

    private func buildSenderGroups(from items: [InboxItem]) {
        var grouped: [String: [InboxItem]] = [:]
        for item in items {
            let key = item.senderUserID.isEmpty ? "_unknown" : item.senderUserID
            grouped[key, default: []].append(item)
        }

        senderGroups = grouped.map { (senderID, senderItems) in
            let sorted = senderItems.sorted { $0.priorityOrder < $1.priorityOrder }
            let name = senderNames[senderID] ?? (senderID == "_unknown" ? "Unknown" : senderID)
            let highest = sorted.first?.priority ?? "medium"
            let unread = sorted.filter(\.isUnread).count
            let urgent = sorted.filter { $0.isPending && $0.priority == "high" }.count
            return SenderGroup(
                senderUserID: senderID,
                senderName: name,
                items: sorted,
                highestPriority: highest,
                unreadCount: unread,
                urgentCount: urgent
            )
        }
        .sorted { a, b in
            if a.priorityOrder != b.priorityOrder { return a.priorityOrder < b.priorityOrder }
            return a.latestDate > b.latestDate
        }
    }
}
