import Foundation
import GRDB

@MainActor
@Observable
final class UserStatsViewModel {
    enum Filter: String, CaseIterable {
        case all = "All"
        case humans = "Humans"
        case bots = "Bots"
        case deleted = "Deleted"
    }

    enum SortOrder: String, CaseIterable {
        case messageCount = "Messages"
        case name = "Name"
        case channels = "Channels"
        case lastActivity = "Activity"
    }

    var stats: [UserStat] = []
    var filter: Filter = .all
    var sortOrder: SortOrder = .messageCount
    var searchText = ""
    var showInactive = false
    var isLoading = false
    var errorMessage: String?

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
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM users") ?? 0
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
            let result: [UserStat]
            do {
                result = try await dbPool.read { db in
                    try UserStatsQueries.fetchAll(db)
                }
            } catch {
                await MainActor.run { [weak self] in
                    self?.stats = []
                    self?.errorMessage = error.localizedDescription
                    self?.isLoading = false
                }
                return
            }
            await MainActor.run { [weak self] in
                self?.stats = result
                self?.errorMessage = nil
                self?.isLoading = false
            }
        }
    }

    var filteredStats: [UserStat] {
        var result = stats

        if !showInactive {
            result = result.filter { $0.totalMessages > 0 || $0.effectiveIsBot }
        }

        switch filter {
        case .all:
            break
        case .humans:
            result = result.filter { !$0.effectiveIsBot && !$0.isDeleted }
        case .bots:
            result = result.filter(\.effectiveIsBot)
        case .deleted:
            result = result.filter(\.isDeleted)
        }

        if !searchText.isEmpty {
            let query = searchText.lowercased()
            result = result.filter {
                $0.name.lowercased().contains(query)
                || $0.displayName.lowercased().contains(query)
                || $0.realName.lowercased().contains(query)
            }
        }

        switch sortOrder {
        case .messageCount:
            result.sort { $0.totalMessages > $1.totalMessages }
        case .name:
            result.sort { $0.bestName.localizedCaseInsensitiveCompare($1.bestName) == .orderedAscending }
        case .channels:
            result.sort { $0.channelCount > $1.channelCount }
        case .lastActivity:
            result.sort { $0.lastActivity > $1.lastActivity }
        }

        return result
    }

    // MARK: - Summary stats

    var totalUsers: Int { stats.filter { !$0.isDeleted }.count }
    var totalBots: Int { stats.filter { $0.effectiveIsBot && !$0.isDeleted }.count }
    var totalHumans: Int { stats.filter { !$0.effectiveIsBot && !$0.isDeleted }.count }
    var activeUsers: Int { stats.filter { ($0.lastActivityDaysAgo ?? 999) <= 7 && !$0.effectiveIsBot }.count }
    var totalDeleted: Int { stats.filter(\.isDeleted).count }
    var inactiveCount: Int {
        guard !showInactive else { return 0 }
        return stats.filter { $0.totalMessages == 0 && !$0.effectiveIsBot }.count
    }
    var overriddenCount: Int { stats.filter { $0.isBotOverride != nil }.count }
    var mutedCount: Int { stats.filter(\.isMutedForLLM).count }

    // MARK: - Message aggregate stats

    var totalMessages: Int { stats.reduce(0) { $0 + $1.totalMessages } }
    var humanMessages: Int {
        stats.filter { !$0.effectiveIsBot }.reduce(0) { $0 + $1.totalMessages }
    }
    var botMessages: Int {
        stats.filter(\.effectiveIsBot).reduce(0) { $0 + $1.totalMessages }
    }

    func toggleMuteForLLM(userID: String) {
        guard let stat = stats.first(where: { $0.id == userID }) else { return }
        let newValue = !stat.isMutedForLLM
        do {
            try dbManager.dbPool.write { db in
                try UserStatsQueries.setMutedForLLM(db, userID: userID, muted: newValue)
            }
            load()
        } catch {
            errorMessage = "Failed to toggle mute: \(error.localizedDescription)"
        }
    }

    func toggleBotOverride(userID: String) {
        guard let stat = stats.first(where: { $0.id == userID }) else { return }
        let newValue: Bool? = stat.effectiveIsBot ? false : true
        // If the override would match the Slack value, clear it instead
        let finalValue: Bool?
        if let nv = newValue, nv == stat.isBot {
            finalValue = nil
        } else {
            finalValue = newValue
        }
        do {
            try dbManager.dbPool.write { db in
                try UserStatsQueries.setBotOverride(db, userID: userID, isBot: finalValue)
            }
            load()
        } catch {
            errorMessage = "Failed to toggle bot status: \(error.localizedDescription)"
        }
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }
}
