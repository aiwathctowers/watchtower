import Foundation
import GRDB

@MainActor
@Observable
final class ProjectMapViewModel {
    var epics: [EpicItem] = []
    var isLoading = false
    var errorMessage: String?
    var searchText: String = ""

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    // MARK: - Types

    struct EpicItem: Identifiable {
        var id: String { key }
        let key: String
        let name: String
        let ownerName: String?
        let ownerSlackID: String?
        let progressPct: Double
        let statusBadge: EpicStatusBadge
        let totalIssues: Int
        let doneIssues: Int
        let inProgressIssues: Int
        let staleCount: Int
        let blockedCount: Int
        let forecastWeeks: Double?
        let issues: [JiraIssue]
        let participants: [(slackID: String, name: String)]
        let pingTargets: [PingTargetItem]
        let createdAt: String?

        // MARK: - Gantt dates

        var startDate: Date? {
            guard let raw = createdAt, !raw.isEmpty else { return nil }
            return Self.parseDate(raw)
        }

        var endDate: Date? {
            guard let weeks = forecastWeeks, weeks > 0 else { return nil }
            // End = now + forecastWeeks
            return Calendar.current.date(
                byAdding: .day,
                value: Int(weeks * 7),
                to: Date()
            )
        }

        private static let isoFull: ISO8601DateFormatter = {
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            return f
        }()

        private static func parseDate(_ s: String) -> Date? {
            isoFull.date(from: s) ?? ISO8601DateFormatter().date(from: s)
        }
    }

    enum EpicStatusBadge: String {
        case onTrack = "on_track"
        case atRisk = "at_risk"
        case behind = "behind"

        var label: String {
            switch self {
            case .onTrack: "On Track"
            case .atRisk: "At Risk"
            case .behind: "Behind"
            }
        }

        var color: (foreground: String, background: String) {
            switch self {
            case .onTrack: ("green", "green")
            case .atRisk: ("yellow", "yellow")
            case .behind: ("red", "red")
            }
        }
    }

    // MARK: - Computed

    var filteredEpics: [EpicItem] {
        guard !searchText.isEmpty else { return epics }
        let query = searchText.lowercased()
        return epics.filter {
            $0.key.lowercased().contains(query)
            || $0.name.lowercased().contains(query)
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
                try Int.fetchOne(
                    db,
                    sql: "SELECT COUNT(*) FROM jira_issues WHERE is_deleted = 0"
                ) ?? 0
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
        do {
            let items = try dbManager.dbPool.read { db -> [EpicItem] in
                let epicIssues = try JiraQueries.fetchAllEpics(db)
                return try epicIssues.map { epic in
                    try Self.buildEpicItem(db: db, epic: epic)
                }
            }
            epics = items.sorted { lhs, rhs in
                // Behind first, then at risk, then on track
                let lhsOrder = Self.statusSortOrder(lhs.statusBadge)
                let rhsOrder = Self.statusSortOrder(rhs.statusBadge)
                if lhsOrder != rhsOrder { return lhsOrder < rhsOrder }
                return lhs.progressPct < rhs.progressPct
            }
            errorMessage = nil
        } catch {
            epics = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    // MARK: - Build

    private static func buildEpicItem(
        db: Database,
        epic: JiraIssue
    ) throws -> EpicItem {
        let childIssues = try JiraQueries.fetchIssuesByEpicKey(db, epicKey: epic.key)
        let total = childIssues.count
        let done = childIssues.filter { $0.statusCategory == "done" }.count
        let inProgress = childIssues.filter {
            $0.statusCategory == "indeterminate" || $0.statusCategory == "in_progress"
        }.count

        let stale = childIssues.filter { issue in
            guard issue.statusCategory != "done" else { return false }
            return daysSince(issue.statusCategoryChangedAt) > 7
        }.count

        let blocked = childIssues.filter { issue in
            issue.status.lowercased().contains("block")
        }.count

        let progressPct = total > 0 ? Double(done) / Double(total) : 0.0

        let participants = try JiraQueries.fetchParticipantsForEpic(db, epicKey: epic.key)

        // Build ping targets from epic assignee + reporter
        var pingTargets: [PingTargetItem] = []
        var seenSlack: Set<String> = []
        if !epic.assigneeSlackId.isEmpty, !seenSlack.contains(epic.assigneeSlackId) {
            seenSlack.insert(epic.assigneeSlackId)
            pingTargets.append(PingTargetItem(
                slackUserID: epic.assigneeSlackId,
                displayName: epic.assigneeDisplayName,
                reason: "assignee"
            ))
        }
        if !epic.reporterSlackId.isEmpty, !seenSlack.contains(epic.reporterSlackId) {
            seenSlack.insert(epic.reporterSlackId)
            pingTargets.append(PingTargetItem(
                slackUserID: epic.reporterSlackId,
                displayName: epic.reporterDisplayName,
                reason: "reporter"
            ))
        }
        // Add blocked-issue assignees
        for issue in childIssues where issue.status.lowercased().contains("block") {
            if !issue.assigneeSlackId.isEmpty, !seenSlack.contains(issue.assigneeSlackId) {
                seenSlack.insert(issue.assigneeSlackId)
                pingTargets.append(PingTargetItem(
                    slackUserID: issue.assigneeSlackId,
                    displayName: issue.assigneeDisplayName,
                    reason: "assignee_blocker"
                ))
            }
        }

        // Forecast: weeks remaining at current velocity
        let forecastWeeks: Double? = {
            guard done > 0, total > done else { return total > 0 ? nil : 0 }
            // Use last 28 days resolved count for velocity
            let recentDone = childIssues.filter { issue in
                guard issue.statusCategory == "done", !issue.resolvedAt.isEmpty else { return false }
                return daysSince(issue.resolvedAt) <= 28
            }.count
            let weeklyVelocity = Double(recentDone) / 4.0
            guard weeklyVelocity > 0 else { return nil }
            return Double(total - done) / weeklyVelocity
        }()

        // Compute velocity metrics matching Go epic_progress.go algorithm.
        let resolvedLastWeek = childIssues.filter { issue in
            guard issue.statusCategory == "done", !issue.resolvedAt.isEmpty else { return false }
            return daysSince(issue.resolvedAt) <= 7
        }.count
        let resolvedLast4W = childIssues.filter { issue in
            guard issue.statusCategory == "done", !issue.resolvedAt.isEmpty else { return false }
            return daysSince(issue.resolvedAt) <= 28
        }.count
        let velocityPerWeek = Double(resolvedLast4W) / 4.0

        let badge = computeStatusBadge(
            total: total,
            done: done,
            resolvedLastWeek: resolvedLastWeek,
            velocityPerWeek: velocityPerWeek
        )

        return EpicItem(
            key: epic.key,
            name: epic.summary,
            ownerName: epic.assigneeDisplayName.isEmpty ? nil : epic.assigneeDisplayName,
            ownerSlackID: epic.assigneeSlackId.isEmpty ? nil : epic.assigneeSlackId,
            progressPct: progressPct,
            statusBadge: badge,
            totalIssues: total,
            doneIssues: done,
            inProgressIssues: inProgress,
            staleCount: stale,
            blockedCount: blocked,
            forecastWeeks: forecastWeeks,
            issues: childIssues,
            participants: participants,
            pingTargets: pingTargets,
            createdAt: epic.createdAt
        )
    }

    /// Matches Go computeStatusBadge() from epic_progress.go:
    /// - remaining == 0 → on_track (epic complete)
    /// - velocity == 0 → behind
    /// - resolvedLastWeek == 0 → behind
    /// - resolvedLastWeek < velocity → at_risk
    /// - else → on_track
    private static func computeStatusBadge(
        total: Int,
        done: Int,
        resolvedLastWeek: Int,
        velocityPerWeek: Double
    ) -> EpicStatusBadge {
        let remaining = total - done
        if remaining == 0 {
            return .onTrack // epic is complete
        }

        if velocityPerWeek == 0 {
            return .behind
        }

        if resolvedLastWeek == 0 {
            return .behind
        }

        if Double(resolvedLastWeek) < velocityPerWeek {
            return .atRisk
        }

        return .onTrack
    }

    private static func statusSortOrder(_ badge: EpicStatusBadge) -> Int {
        switch badge {
        case .behind: 0
        case .atRisk: 1
        case .onTrack: 2
        }
    }

    // MARK: - Helpers

    private static let isoFormatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static func daysSince(_ dateStr: String) -> Int {
        guard !dateStr.isEmpty else { return 0 }
        guard let date = isoFormatter.date(from: dateStr)
                ?? ISO8601DateFormatter().date(from: dateStr) else {
            return 0
        }
        return max(
            0,
            Calendar.current.dateComponents([.day], from: date, to: Date()).day ?? 0
        )
    }
}
