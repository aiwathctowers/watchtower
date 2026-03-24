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
    private(set) var userNameCache: [String: String] = [:]
    var availableChannels: [(id: String, name: String)] = []
    var starredOnly: Bool = false
    private(set) var starredChannelIDs: Set<String> = []

    // Pagination
    private(set) var hasMoreItems = true
    private var itemsOffset = 0
    var isLoadingMore = false
    private let pageSize = 50

    private(set) var workspaceDomain: String?
    private(set) var workspaceTeamID: String?
    private let dbManager: DatabaseManager
    private var currentUserID: String?
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    /// Start observing the tracks table for live updates.
    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tracks") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
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
                    ownership: ownershipFilter,
                    limit: pageSize
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
            itemsOffset = loadedItems.count
            hasMoreItems = result.3.count >= pageSize
            openCount = result.4
            inboxCount = result.5
            updatedCount = result.6
            statusCounts = result.7
            totalCount = result.8
            ownershipCounts = result.9
            availableChannels = loadAvailableChannels()
            refreshUserNameCache()
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

    func setStatus(_ item: Track, to status: String) {
        updateStatus(item, to: status)
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

    func updatePriority(_ item: Track, to priority: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updatePriority(db, id: item.id, priority: priority)
            }
            load()
        } catch {
            errorMessage = "Failed to update priority: \(error.localizedDescription)"
        }
    }

    func updateCategory(_ item: Track, to category: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateCategory(db, id: item.id, category: category)
            }
            load()
        } catch {
            errorMessage = "Failed to update category: \(error.localizedDescription)"
        }
    }

    func updateOwnership(_ item: Track, to ownership: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateOwnership(db, id: item.id, ownership: ownership)
            }
            load()
        } catch {
            errorMessage = "Failed to update ownership: \(error.localizedDescription)"
        }
    }

    // MARK: - Pagination

    func loadMore() {
        guard hasMoreItems, !isLoadingMore else { return }
        isLoadingMore = true
        do {
            let effectiveStatus: String? = statusFilter == nil ? "inbox" : statusFilter
            let batch = try dbManager.dbPool.read { db in
                try TrackQueries.fetchAll(
                    db,
                    assigneeUserID: currentUserID,
                    status: effectiveStatus == "all" ? nil : effectiveStatus,
                    statuses: nil,
                    channelID: channelFilter,
                    priority: priorityFilter,
                    ownership: ownershipFilter,
                    limit: pageSize,
                    offset: itemsOffset
                )
            }
            var newItems = batch
            if starredOnly, !starredChannelIDs.isEmpty {
                newItems = newItems.filter { starredChannelIDs.contains($0.channelID) }
            }
            items.append(contentsOf: newItems)
            itemsOffset += batch.count
            hasMoreItems = batch.count >= pageSize
        } catch {
            print("Failed to load more tracks: \(error)")
        }
        isLoadingMore = false
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

    private static let userIDPattern = try! NSRegularExpression(pattern: "U[A-Z0-9]{8,11}")

    /// Resolve Slack user IDs found in track texts to display names.
    private func refreshUserNameCache() {
        var allText = items.flatMap { [$0.text, $0.context, $0.blocking, $0.requesterName] }
        allText.append(contentsOf: items.flatMap { $0.decodedSubItems.map(\.text) })
        allText.append(contentsOf: items.flatMap { $0.decodedParticipants.map(\.name) })
        let joined = allText.joined(separator: " ")

        let range = NSRange(joined.startIndex..., in: joined)
        let matches = Self.userIDPattern.matches(in: joined, range: range)
        var userIDs = Set<String>()
        for match in matches {
            if let idRange = Range(match.range, in: joined) {
                userIDs.insert(String(joined[idRange]))
            }
        }
        // Only look up IDs not already cached
        let newIDs = userIDs.subtracting(userNameCache.keys)
        guard !newIDs.isEmpty else { return }

        do {
            let map = try dbManager.dbPool.read { db in
                var result: [String: String] = [:]
                for uid in newIDs {
                    let name = try UserQueries.fetchDisplayName(db, forID: uid)
                    if name != uid { result[uid] = name }
                }
                return result
            }
            userNameCache.merge(map) { _, new in new }
        } catch {}
    }

    func resolveUserIDs(_ text: String) -> String {
        guard !userNameCache.isEmpty else { return text }
        let pattern = try! NSRegularExpression(pattern: "\\(?(U[A-Z0-9]{8,11})\\)?")
        let range = NSRange(text.startIndex..., in: text)
        var result = text
        let matches = pattern.matches(in: text, range: range).reversed()
        for match in matches {
            let fullRange = Range(match.range, in: result)!
            let idRange = Range(match.range(at: 1), in: result)!
            let userID = String(result[idRange])
            if let name = userNameCache[userID] {
                let fullMatch = String(result[fullRange])
                let hasParens = fullMatch.hasPrefix("(") && fullMatch.hasSuffix(")")
                result.replaceSubrange(fullRange, with: hasParens ? "(\(name))" : name)
            }
        }
        return result
    }

    /// Unique channel names for filter picker, refreshed on each load().
    private func loadAvailableChannels() -> [(id: String, name: String)] {
        guard let uid = currentUserID else { return [] }
        do {
            return try dbManager.dbPool.read { db in
                let rows = try Row.fetchAll(
                    db,
                    sql: """
                        SELECT DISTINCT channel_id, source_channel_name FROM tracks
                        WHERE assignee_user_id = ?
                        ORDER BY source_channel_name
                        """,
                    arguments: [uid]
                )
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
