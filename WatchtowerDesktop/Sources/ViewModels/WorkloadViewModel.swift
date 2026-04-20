import Foundation
import GRDB

@MainActor
@Observable
final class WorkloadViewModel {
    var entries: [WorkloadEntry] = []
    var isLoading = true
    var errorMessage: String?
    var searchText: String = ""
    var signalFilter: WorkloadSignal?

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    // MARK: - Types

    struct WorkloadEntry: Identifiable {
        var id: String { slackUserID }
        var slackUserID: String
        var displayName: String
        var openIssues: Int
        var inProgressCount: Int
        var testingCount: Int
        var overdueCount: Int
        var blockedCount: Int
        var avgCycleTimeDays: Double
        var signal: WorkloadSignal
    }

    var filteredEntries: [WorkloadEntry] {
        var result = entries
        if !searchText.isEmpty {
            let q = searchText.lowercased()
            result = result.filter { $0.displayName.lowercased().contains(q) }
        }
        if let filter = signalFilter {
            result = result.filter { $0.signal == filter }
        }
        return result
    }

    enum WorkloadSignal: String {
        case normal, watch, overload, low

        var label: String {
            switch self {
            case .normal: "Normal"
            case .watch: "Watch"
            case .overload: "Overload"
            case .low: "Low"
            }
        }

        var emoji: String {
            switch self {
            case .normal: "✅"
            case .watch: "⚠️"
            case .overload: "🔴"
            case .low: "💤"
            }
        }

        /// Sort priority (overload first).
        var sortOrder: Int {
            switch self {
            case .overload: 0
            case .watch: 1
            case .low: 2
            case .normal: 3
            }
        }
    }

    // MARK: - Init

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    // MARK: - Observation

    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM jira_issues") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }

    // MARK: - Load

    func load() {
        isLoading = true
        Task {
            do {
                let result = try await Task.detached { [dbManager] in
                    let testingStatuses: Set<String> = try dbManager.dbPool.read { db in
                        let boards = try JiraBoard.filter(Column("is_selected") == true).fetchAll(db)
                        var statuses = Set<String>()
                        for board in boards where !board.llmProfileJSON.isEmpty {
                            if let data = board.llmProfileJSON.data(using: .utf8) {
                                let decoder = JSONDecoder()
                                decoder.keyDecodingStrategy = .convertFromSnakeCase
                                if let profile = try? decoder.decode(BoardProfileDisplay.self, from: data) {
                                    for stage in profile.workflowStages where stage.phase == "testing" {
                                        statuses.formUnion(stage.originalStatuses.map { $0.lowercased() })
                                    }
                                }
                            }
                        }
                        return statuses
                    }

                    let rows = try dbManager.dbPool.read { db in
                        try JiraQueries.fetchTeamWorkload(db)
                    }

                    var entries: [WorkloadEntry] = []
                    for row in rows {
                        let (inProg, testing) = try dbManager.dbPool.read { db -> (Int, Int) in
                            let issues = try JiraQueries.fetchIssuesByAssignee(db, slackID: row.slackUserID)
                            let inP = issues.filter { $0.statusCategory == "in_progress" || $0.statusCategory == "indeterminate" }.count
                            let test = issues.filter { testingStatuses.contains($0.status.lowercased()) }.count
                            return (inP, test)
                        }

                        let signal = WorkloadViewModel.computeSignal(
                            openIssues: row.openIssues,
                            overdueCount: row.overdueCount,
                            blockedCount: row.blockedCount
                        )

                        entries.append(WorkloadEntry(
                            slackUserID: row.slackUserID,
                            displayName: row.displayName.isEmpty ? row.slackUserID : row.displayName,
                            openIssues: row.openIssues,
                            inProgressCount: inProg,
                            testingCount: testing,
                            overdueCount: row.overdueCount,
                            blockedCount: row.blockedCount,
                            avgCycleTimeDays: (row.avgCycleTimeDays * 100).rounded() / 100,
                            signal: signal
                        ))
                    }

                    entries.sort { $0.signal.sortOrder < $1.signal.sortOrder }
                    return entries
                }.value
                entries = result
                errorMessage = nil
            } catch {
                entries = []
                errorMessage = error.localizedDescription
            }
            isLoading = false
        }
    }

    // MARK: - Signal Logic (matches Go computeSignal)

    nonisolated static func computeSignal(
        openIssues: Int,
        overdueCount: Int,
        blockedCount: Int
    ) -> WorkloadSignal {
        if overdueCount > 2 || blockedCount > 3 || openIssues > 15 {
            return .overload
        }
        if overdueCount > 0 || blockedCount > 1 || openIssues > 10 {
            return .watch
        }
        if openIssues == 0 {
            return .low
        }
        return .normal
    }
}
