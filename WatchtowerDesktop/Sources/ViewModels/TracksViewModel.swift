import Foundation
import GRDB

@MainActor
@Observable
final class TracksViewModel {
    var items: [Track] = []
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
    var ownershipFilter: String?
    var ownershipCounts: [String: Int] = [:]
    var availableChannels: [(id: String, name: String)] = []
    var starredOnly: Bool = false
    private(set) var starredChannelIDs: Set<String> = []

    private(set) var workspaceDomain: String?
    private(set) var workspaceTeamID: String?
    private let dbManager: DatabaseManager
    private var currentUserID: String?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    var inboxItems: [Track] {
        items.filter { $0.isInbox }
    }

    var activeItems: [Track] {
        items.filter { $0.isActive }
    }

    func load() {
        isLoading = true
        do {
            // When statusFilter is nil, default to showing inbox
            let effectiveStatus: String? = statusFilter == nil ? "inbox" : statusFilter
            let result = try dbManager.dbPool.read { db in
                let uid = try TrackQueries.fetchCurrentUserID(db)
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let all = try TrackQueries.fetchAll(
                    db,
                    assigneeUserID: uid,
                    status: effectiveStatus == "all" ? nil : effectiveStatus,
                    statuses: nil,
                    channelID: channelFilter,
                    priority: priorityFilter,
                    ownership: ownershipFilter
                )
                let count = try uid.map { try TrackQueries.fetchOpenCount(db, assigneeUserID: $0) } ?? 0
                let inbox = try uid.map { try TrackQueries.fetchInboxCount(db, assigneeUserID: $0) } ?? 0
                let updated = try uid.map { try TrackQueries.fetchUpdatedCount(db, assigneeUserID: $0) } ?? 0
                let sCounts = try uid.map { try TrackQueries.fetchStatusCounts(db, assigneeUserID: $0) } ?? [:]
                let total = try uid.map { try TrackQueries.fetchTotalCount(db, assigneeUserID: $0) } ?? 0
                let oCounts = try uid.map { try TrackQueries.fetchOwnershipCounts(db, assigneeUserID: $0) } ?? [:]
                let profile = try ProfileQueries.fetchCurrentProfile(db)
                let starred = Set(profile?.decodedStarredChannels ?? [])
                return (uid, ws?.domain, ws?.id, all, count, inbox, updated, sCounts, total, oCounts, starred)
            }
            currentUserID = result.0
            workspaceDomain = result.1
            workspaceTeamID = result.2
            var loadedItems = result.3
            let starredCh = result.10
            starredChannelIDs = starredCh
            if starredOnly && !starredCh.isEmpty {
                loadedItems = loadedItems.filter { starredCh.contains($0.channelID) }
            }
            items = loadedItems
            openCount = result.4
            inboxCount = result.5
            updatedCount = result.6
            statusCounts = result.7
            totalCount = result.8
            ownershipCounts = result.9
            availableChannels = loadAvailableChannels()
            errorMessage = nil
        } catch {
            items = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func markDone(_ item: Track) {
        updateStatus(item, to: "done")
    }

    func dismiss(_ item: Track) {
        updateStatus(item, to: "dismissed")
    }

    func reopen(_ item: Track) {
        updateStatus(item, to: "inbox")
    }

    func accept(_ item: Track) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.acceptItem(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to accept: \(error.localizedDescription)"
        }
    }

    func snooze(_ item: Track, until: Date) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.snoozeItem(db, id: item.id, until: until.timeIntervalSince1970)
            }
            load()
        } catch {
            errorMessage = "Failed to snooze: \(error.localizedDescription)"
        }
    }

    func toggleSubItem(_ item: Track, subItemIndex: Int) {
        var subs = item.decodedSubItems
        guard subItemIndex < subs.count else { return }
        subs[subItemIndex].status = subs[subItemIndex].isDone ? "open" : "done"
        guard let data = try? JSONEncoder().encode(subs),
              let json = String(data: data, encoding: .utf8) else { return }
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateSubItems(db, id: item.id, subItemsJSON: json)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-item: \(error.localizedDescription)"
        }
    }

    func markUpdateRead(_ item: Track) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.markUpdateRead(db, id: item.id)
            }
            load()
        } catch {
            errorMessage = "Failed to mark read: \(error.localizedDescription)"
        }
    }

    private func updateStatus(_ item: Track, to status: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateStatus(db, id: item.id, status: status)
            }
            load()
        } catch {
            errorMessage = "Failed to update: \(error.localizedDescription)"
        }
    }

    func itemByID(_ id: Int) -> Track? {
        do {
            return try dbManager.dbPool.read { db in
                try TrackQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func fetchHistory(for itemID: Int) -> [TrackHistoryEntry] {
        do {
            return try dbManager.dbPool.read { db in
                try TrackQueries.fetchHistory(db, trackID: itemID)
            }
        } catch {
            return []
        }
    }

    func slackChannelURL(channelID: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)")
    }

    func slackMessageURL(channelID: String, messageTS: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)&message=\(messageTS)")
    }

    /// Unique channel names for filter picker, refreshed on each load().
    private func loadAvailableChannels() -> [(id: String, name: String)] {
        guard let uid = currentUserID else { return [] }
        do {
            return try dbManager.dbPool.read { db in
                let rows = try Row.fetchAll(db, sql: """
                    SELECT DISTINCT channel_id, source_channel_name FROM tracks
                    WHERE assignee_user_id = ?
                    ORDER BY source_channel_name
                    """, arguments: [uid])
                return rows.map { row in
                    let chID: String = row["channel_id"]
                    let name: String = row["source_channel_name"] ?? chID
                    return (id: chID, name: name.isEmpty ? chID : name)
                }
            }
        } catch {
            return []
        }
    }
}
