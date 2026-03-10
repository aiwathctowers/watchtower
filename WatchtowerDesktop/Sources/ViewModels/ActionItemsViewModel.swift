import Foundation
import GRDB

@MainActor
@Observable
final class ActionItemsViewModel {
    var items: [ActionItem] = []
    var isLoading = false
    var errorMessage: String?
    var openCount: Int = 0
    var inboxCount: Int = 0
    var updatedCount: Int = 0
    var statusCounts: [String: Int] = [:]
    var totalCount: Int = 0
    var statusFilter: String?
    var priorityFilter: String?
    var channelFilter: String?

    private(set) var workspaceDomain: String?
    private let dbManager: DatabaseManager
    private var currentUserID: String?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    var inboxItems: [ActionItem] {
        items.filter { $0.isInbox }
    }

    var activeItems: [ActionItem] {
        items.filter { $0.isActive }
    }

    func load() {
        isLoading = true
        do {
            // When statusFilter is nil, default to showing inbox
            let effectiveStatus: String? = statusFilter == nil ? "inbox" : statusFilter
            let effectiveStatuses: [String]? = nil
            let result = try dbManager.dbPool.read { db in
                let uid = try ActionItemQueries.fetchCurrentUserID(db)
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let all = try ActionItemQueries.fetchAll(
                    db,
                    assigneeUserID: uid,
                    status: effectiveStatus == "all" ? nil : effectiveStatus,
                    statuses: effectiveStatuses,
                    channelID: channelFilter,
                    priority: priorityFilter
                )
                let count = uid.map { try? ActionItemQueries.fetchOpenCount(db, assigneeUserID: $0) } ?? nil
                let inbox = uid.map { try? ActionItemQueries.fetchInboxCount(db, assigneeUserID: $0) } ?? nil
                let updated = uid.map { try? ActionItemQueries.fetchUpdatedCount(db, assigneeUserID: $0) } ?? nil
                let sCounts = uid.map { try? ActionItemQueries.fetchStatusCounts(db, assigneeUserID: $0) } ?? nil
                let total = uid.map { try? ActionItemQueries.fetchTotalCount(db, assigneeUserID: $0) } ?? nil
                return (uid, ws?.domain, all, count ?? 0, inbox ?? 0, updated ?? 0, sCounts ?? [:], total ?? 0)
            }
            currentUserID = result.0
            workspaceDomain = result.1
            items = result.2
            openCount = result.3
            inboxCount = result.4
            updatedCount = result.5
            statusCounts = result.6
            totalCount = result.7
            errorMessage = nil
        } catch {
            items = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func markDone(_ item: ActionItem) {
        updateStatus(item, to: "done")
    }

    func dismiss(_ item: ActionItem) {
        updateStatus(item, to: "dismissed")
    }

    func reopen(_ item: ActionItem) {
        updateStatus(item, to: "inbox")
    }

    func accept(_ item: ActionItem) {
        do {
            try dbManager.dbPool.write { db in
                try ActionItemQueries.acceptItem(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to accept: \(error.localizedDescription)"
        }
    }

    func snooze(_ item: ActionItem, until: Date) {
        do {
            try dbManager.dbPool.write { db in
                try ActionItemQueries.snoozeItem(db, id: item.id, until: until.timeIntervalSince1970)
            }
            load()
        } catch {
            errorMessage = "Failed to snooze: \(error.localizedDescription)"
        }
    }

    func toggleSubItem(_ item: ActionItem, subItemIndex: Int) {
        var subs = item.decodedSubItems
        guard subItemIndex < subs.count else { return }
        subs[subItemIndex].status = subs[subItemIndex].isDone ? "open" : "done"
        guard let data = try? JSONEncoder().encode(subs),
              let json = String(data: data, encoding: .utf8) else { return }
        do {
            try dbManager.dbPool.write { db in
                try ActionItemQueries.updateSubItems(db, id: item.id, subItemsJSON: json)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-item: \(error.localizedDescription)"
        }
    }

    func markUpdateRead(_ item: ActionItem) {
        do {
            try dbManager.dbPool.write { db in
                try ActionItemQueries.markUpdateRead(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to mark read: \(error.localizedDescription)"
        }
    }

    private func updateStatus(_ item: ActionItem, to status: String) {
        do {
            try dbManager.dbPool.write { db in
                try ActionItemQueries.updateStatus(db, id: item.id, status: status)
            }
            load()
        } catch {
            errorMessage = "Failed to update: \(error.localizedDescription)"
        }
    }

    func itemByID(_ id: Int) -> ActionItem? {
        do {
            return try dbManager.dbPool.read { db in
                try ActionItemQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func fetchHistory(for itemID: Int) -> [ActionItemHistoryEntry] {
        do {
            return try dbManager.dbPool.read { db in
                try ActionItemQueries.fetchHistory(db, actionItemID: itemID)
            }
        } catch {
            return []
        }
    }

    func slackMessageURL(channelID: String, messageTS: String) -> URL? {
        guard let domain = workspaceDomain, !domain.isEmpty else { return nil }
        let tsForURL = "p" + messageTS.replacingOccurrences(of: ".", with: "")
        return URL(string: "https://\(domain).slack.com/archives/\(channelID)/\(tsForURL)")
    }

    /// Unique channel names for filter picker
    var availableChannels: [(id: String, name: String)] {
        var seen = Set<String>()
        var result: [(id: String, name: String)] = []
        for item in items {
            if seen.insert(item.channelID).inserted {
                let name = item.sourceChannelName.isEmpty ? item.channelID : item.sourceChannelName
                result.append((id: item.channelID, name: name))
            }
        }
        return result.sorted { $0.name < $1.name }
    }
}
