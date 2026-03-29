import Foundation
import GRDB

@MainActor
@Observable
final class ChannelStatsViewModel {
    enum Filter: String, CaseIterable {
        case all = "All"
        case muted = "Muted"
        case favorites = "Favorites"
        case hasRecommendations = "Recommendations"
    }

    enum SortOrder: String, CaseIterable {
        case messageCount = "Messages"
        case name = "Name"
        case botRatio = "Bot %"
        case lastActivity = "Activity"
    }

    var stats: [ChannelStat] = []
    var recommendations: [ChannelRecommendation] = []
    var filter: Filter = .all
    var sortOrder: SortOrder = .messageCount
    var searchText = ""
    var minMessages: Int = 5
    var showAllChannels = false
    var isLoading = false
    var errorMessage: String?
    var teamID: String?

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM channels") ?? 0
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
        let dbPool = dbManager.dbPool
        Task.detached { [weak self] in
            let result: ([ChannelStat], [ChannelRecommendation], String?)
            do {
                result = try await dbPool.read { db in
                    let uid = try ChannelStatsQueries.fetchCurrentUserID(db)
                    guard let uid, !uid.isEmpty else {
                        return ([ChannelStat](), [ChannelRecommendation](), String?.none)
                    }
                    let allStats = try ChannelStatsQueries.fetchAll(db, currentUserID: uid)
                    let signals = try ChannelStatsQueries.fetchValueSignals(db)
                    let recs = ChannelStatsQueries.computeRecommendations(from: allStats, signals: signals)
                    let tid = try ChannelStatsQueries.fetchWorkspaceTeamID(db)
                    return (allStats, recs, tid)
                }
            } catch {
                await MainActor.run { [weak self] in
                    self?.stats = []
                    self?.recommendations = []
                    self?.errorMessage = error.localizedDescription
                    self?.isLoading = false
                }
                return
            }
            await MainActor.run { [weak self] in
                self?.stats = result.0
                self?.recommendations = result.1
                self?.teamID = result.2
                self?.errorMessage = nil
                self?.isLoading = false
            }
        }
    }

    var filteredStats: [ChannelStat] {
        var result = stats

        // Apply minimum messages filter unless showing all
        if !showAllChannels {
            result = result.filter { $0.totalMessages >= minMessages }
        }

        switch filter {
        case .all:
            break
        case .muted:
            result = result.filter(\.isMutedForLLM)
        case .favorites:
            result = result.filter(\.isFavorite)
        case .hasRecommendations:
            let recChannelIDs = Set(recommendations.map(\.channelID))
            result = result.filter { recChannelIDs.contains($0.id) }
        }

        if !searchText.isEmpty {
            let query = searchText.lowercased()
            result = result.filter { $0.name.lowercased().contains(query) }
        }

        switch sortOrder {
        case .messageCount:
            result.sort { $0.totalMessages > $1.totalMessages }
        case .name:
            result.sort { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
        case .botRatio:
            result.sort { $0.botRatio > $1.botRatio }
        case .lastActivity:
            result.sort { $0.lastActivity > $1.lastActivity }
        }

        return result
    }

    var recommendationCount: Int {
        recommendations.count
    }

    /// Number of channels hidden by min messages filter.
    var hiddenChannelCount: Int {
        guard !showAllChannels else { return 0 }
        return stats.filter { $0.totalMessages < minMessages }.count
    }

    // MARK: - Summary stats

    var totalChannels: Int { stats.count }
    var activeChannels: Int { stats.filter { ($0.lastActivityDaysAgo ?? 999) <= 7 }.count }
    var mutedChannels: Int { stats.filter(\.isMutedForLLM).count }
    var favoriteChannels: Int { stats.filter(\.isFavorite).count }
    var totalMessages: Int { stats.reduce(0) { $0 + $1.totalMessages } }
    var digestedChannels: Int { stats.filter(\.hasDigests).count }
    var pendingDigestChannels: Int {
        stats.filter { if case .pending = $0.digestStatus { return true }; return false }.count
    }

    func toggleMute(channelID: String) {
        guard let stat = stats.first(where: { $0.id == channelID }) else { return }
        let newValue = !stat.isMutedForLLM
        do {
            try dbManager.dbPool.write { db in
                try ChannelStatsQueries.toggleMuteForLLM(db, channelID: channelID, muted: newValue)
            }
            load()
        } catch {
            errorMessage = "Failed to toggle mute: \(error.localizedDescription)"
        }
    }

    func toggleFavorite(channelID: String) {
        guard let stat = stats.first(where: { $0.id == channelID }) else { return }
        let newValue = !stat.isFavorite
        do {
            try dbManager.dbPool.write { db in
                try ChannelStatsQueries.toggleFavorite(db, channelID: channelID, favorite: newValue)
            }
            load()
        } catch {
            errorMessage = "Failed to toggle favorite: \(error.localizedDescription)"
        }
    }

    func slackURL(for channelID: String) -> URL? {
        guard let tid = teamID else { return nil }
        return URL(string: "slack://channel?team=\(tid)&id=\(channelID)")
    }

    func applyRecommendation(_ rec: ChannelRecommendation) {
        switch rec.action {
        case .mute:
            toggleMute(channelID: rec.channelID)
        case .leave:
            toggleMute(channelID: rec.channelID)
        case .favorite:
            toggleFavorite(channelID: rec.channelID)
        }
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }
}
