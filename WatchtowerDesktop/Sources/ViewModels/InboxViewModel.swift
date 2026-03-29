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

    // Filters
    var priorityFilter: String?
    var triggerTypeFilter: String?
    var showResolved: Bool = false

    // Name caches
    private(set) var senderNames: [String: String] = [:]
    private(set) var channelNames: [String: String] = [:]
    private(set) var workspaceTeamID: String?

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

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
                return (ws?.id, all, counts)
            }

            workspaceTeamID = result.0
            let items = result.1
            let counts = result.2
            allItems = items
            pendingCount = counts.pending
            unreadCount = counts.unread
            highPriorityCount = counts.highPriority

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
            errorMessage = error.localizedDescription
        }
        isLoading = false
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
