import Foundation
import GRDB

@MainActor
@Observable
final class TracksViewModel {
    var updatedTracks: [Track] = []
    var allTracks: [Track] = []
    var isLoading = false
    var errorMessage: String?
    var totalCount: Int = 0
    var updatedCount: Int = 0
    var trackTaskCounts: [Int: Int] = [:]

    // Jira data
    var isJiraConnected: Bool = false
    var jiraSiteURL: String?
    var trackJiraIssues: [Int: [JiraIssue]] = [:]

    // Filters
    var priorityFilter: String?
    var channelFilter: String?
    var tagFilter: String?
    var ownershipFilter: String?
    var jiraFilter: JiraFilter = .all
    var showRead: Bool = false
    var showDismissed: Bool = false

    enum JiraFilter: String, CaseIterable {
        case all = "All"
        case withJira = "With Jira"
        case withoutJira = "Without Jira"
    }

    private(set) var workspaceDomain: String?
    private(set) var workspaceTeamID: String?
    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    // User name cache for resolving Slack user IDs
    private(set) var userNameCache: [String: String] = [:]
    // swiftlint:disable:next force_try
    private static let userIDPattern = try! NSRegularExpression(
        pattern: "U[A-Z0-9]{8,11}"
    )

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
        refreshJiraStatus()
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

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db in
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let counts = try TrackQueries.fetchCounts(db)
                let all = try TrackQueries.fetchAll(
                    db,
                    priority: self.priorityFilter,
                    channelID: self.channelFilter,
                    ownership: self.ownershipFilter,
                    includeDismissed: self.showDismissed
                )
                let taskCounts = try TaskQueries.fetchActiveCountsBySourceTrack(db)
                return (ws?.domain, ws?.id, all, counts, taskCounts)
            }
            workspaceDomain = result.0
            workspaceTeamID = result.1
            trackTaskCounts = result.4

            var tracks = result.2

            // Load Jira issues for all tracks in a single read
            if isJiraConnected {
                loadJiraData(trackIDs: tracks.map(\.id))
            }

            // Apply tag filter in memory (tags is JSON array)
            if let tagFilter, !tagFilter.isEmpty {
                tracks = tracks.filter { $0.decodedTags.contains(tagFilter) }
            }

            // Apply Jira filter
            switch jiraFilter {
            case .all:
                break
            case .withJira:
                tracks = tracks.filter { !(trackJiraIssues[$0.id]?.isEmpty ?? true) }
            case .withoutJira:
                tracks = tracks.filter { trackJiraIssues[$0.id]?.isEmpty ?? true }
            }

            updatedTracks = tracks.filter { $0.hasUpdates }
            let rest = tracks.filter { !$0.hasUpdates }
            // Hide read tracks unless showRead is enabled
            allTracks = showRead ? rest : rest.filter { $0.isUnread }
            totalCount = result.3.total
            updatedCount = result.3.updated
            refreshUserNameCache(tracks: tracks)
            errorMessage = nil
        } catch {
            updatedTracks = []
            allTracks = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func taskCount(for trackID: Int) -> Int {
        trackTaskCounts[trackID] ?? 0
    }

    func markRead(_ track: Track) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.markRead(db, id: track.id)
            }
            load()
        } catch {
            errorMessage = "Failed to mark read: \(error.localizedDescription)"
        }
    }

    func updatePriority(_ track: Track, to priority: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updatePriority(db, id: track.id, priority: priority)
            }
            load()
        } catch {
            errorMessage = "Failed to update priority: \(error.localizedDescription)"
        }
    }

    func updateOwnership(_ track: Track, to ownership: String) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateOwnership(db, id: track.id, ownership: ownership)
            }
            load()
        } catch {
            errorMessage = "Failed to update ownership: \(error.localizedDescription)"
        }
    }

    func toggleSubItem(_ track: Track, at index: Int) {
        var items = track.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].status = items[index].isDone ? "open" : "done"
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.updateSubItems(db, id: track.id, subItems: items)
            }
            load()
        } catch {
            errorMessage = "Failed to toggle sub-item: \(error.localizedDescription)"
        }
    }

    func dismissTrack(_ track: Track) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.dismiss(db, id: track.id)
            }
            load()
        } catch {
            errorMessage = "Failed to dismiss: \(error.localizedDescription)"
        }
    }

    func restoreTrack(_ track: Track) {
        do {
            try dbManager.dbPool.write { db in
                try TrackQueries.restore(db, id: track.id)
            }
            load()
        } catch {
            errorMessage = "Failed to restore: \(error.localizedDescription)"
        }
    }

    func fetchDigest(id: Int) -> Digest? {
        do {
            return try dbManager.dbPool.read { db in
                try DigestQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func channelName(for channelID: String) -> String? {
        guard !channelID.isEmpty else { return nil }
        do {
            return try dbManager.dbPool.read { db in
                try ChannelQueries.fetchByID(db, id: channelID)?.name
            }
        } catch {
            return nil
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

    func slackChannelURL(channelID: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)")
    }

    func slackMessageURL(channelID: String, messageTS: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)&message=\(messageTS)")
    }

    func submitFeedback(trackID: Int, rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                try FeedbackQueries.addFeedback(
                    db,
                    entityType: "track",
                    entityID: "\(trackID)",
                    rating: rating,
                    comment: ""
                )
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // MARK: - Jira

    func refreshJiraStatus() {
        isJiraConnected = JiraQueries.isConnected()
        if isJiraConnected {
            let auth = JiraAuthService()
            jiraSiteURL = auth.siteURL
        } else {
            jiraSiteURL = nil
            trackJiraIssues = [:]
        }
    }

    func jiraIssues(for trackID: Int) -> [JiraIssue] {
        trackJiraIssues[trackID] ?? []
    }

    private func loadJiraData(trackIDs: [Int]) {
        do {
            let issueMap = try dbManager.dbPool.read { db in
                try JiraQueries.fetchIssuesForTracks(db, trackIDs: trackIDs)
            }
            trackJiraIssues = issueMap
        } catch {
            trackJiraIssues = [:]
        }
    }

    // MARK: - User name resolution

    func resolveUserIDs(_ text: String) -> String {
        guard !userNameCache.isEmpty else { return text }
        guard let pattern = try? NSRegularExpression(pattern: "\\(?(U[A-Z0-9]{8,11})\\)?") else {
            return text
        }
        let range = NSRange(text.startIndex..., in: text)
        var result = text
        let matches = pattern.matches(in: text, range: range).reversed()
        for match in matches {
            guard let fullRange = Range(match.range, in: result),
                  let idRange = Range(match.range(at: 1), in: result) else { continue }
            let userID = String(result[idRange])
            if let name = userNameCache[userID] {
                let fullMatch = String(result[fullRange])
                let hasParens = fullMatch.hasPrefix("(") && fullMatch.hasSuffix(")")
                result.replaceSubrange(
                    fullRange, with: hasParens ? "(\(name))" : name
                )
            }
        }
        return result
    }

    private func refreshUserNameCache(tracks: [Track]) {
        let allText = tracks.flatMap {
            [$0.text, $0.context, $0.blocking, $0.participants, $0.requesterName]
        }
        let joined = allText.joined(separator: " ")
        let range = NSRange(joined.startIndex..., in: joined)
        let matches = Self.userIDPattern.matches(in: joined, range: range)
        var userIDs = Set<String>()
        for match in matches {
            if let idRange = Range(match.range, in: joined) {
                userIDs.insert(String(joined[idRange]))
            }
        }
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
}
