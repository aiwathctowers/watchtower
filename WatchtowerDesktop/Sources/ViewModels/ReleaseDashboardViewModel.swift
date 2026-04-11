import Foundation
import GRDB

@MainActor
@Observable
final class ReleaseDashboardViewModel {
    var releases: [ReleaseItem] = []
    var isLoading = false
    var errorMessage: String?
    var searchText: String = ""

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    // MARK: - Types

    struct ReleaseItem: Identifiable {
        let id: Int
        let name: String
        let projectKey: String
        let releaseDate: String
        let released: Bool
        let isOverdue: Bool
        let atRisk: Bool
        let atRiskReason: String
        let progressPct: Double
        let totalIssues: Int
        let doneIssues: Int
        let blockedCount: Int
        let epicProgress: [EpicProgressItem]
        let scopeChanges: (added: Int, removed: Int)
        let issues: [JiraIssue]
        let pingTargets: [PingTargetItem]
    }

    struct EpicProgressItem: Identifiable {
        let id: String
        let key: String
        let name: String
        let progressPct: Double
        let statusBadge: String
        let total: Int
        let done: Int
    }

    // MARK: - Computed

    var filteredReleases: [ReleaseItem] {
        guard !searchText.isEmpty else { return releases }
        let query = searchText.lowercased()
        return releases.filter {
            $0.name.lowercased().contains(query)
                || $0.projectKey.lowercased().contains(query)
        }
    }

    var atRiskCount: Int { releases.filter(\.atRisk).count }
    var overdueCount: Int { releases.filter(\.isOverdue).count }

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
            let observation = ValueObservation.tracking { db -> (Int, Int) in
                let releases = try Int.fetchOne(
                    db,
                    sql: "SELECT COUNT(*) FROM jira_releases"
                ) ?? 0
                let issues = try Int.fetchOne(
                    db,
                    sql: "SELECT COUNT(*) FROM jira_issues WHERE is_deleted = 0"
                ) ?? 0
                return (releases, issues)
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
            let items = try dbManager.dbPool.read { db -> [ReleaseItem] in
                let allReleases = try JiraQueries.fetchUnreleasedReleases(db)
                return try allReleases.map { release in
                    try Self.buildReleaseItem(db: db, release: release)
                }
            }
            releases = items.sorted { lhs, rhs in
                // Overdue first, then at-risk, then by date
                if lhs.isOverdue != rhs.isOverdue { return lhs.isOverdue }
                if lhs.atRisk != rhs.atRisk { return lhs.atRisk }
                return lhs.releaseDate < rhs.releaseDate
            }
            errorMessage = nil
        } catch {
            releases = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    // MARK: - Build

    private static func buildReleaseItem(
        db: Database,
        release: JiraRelease
    ) throws -> ReleaseItem {
        let issues = try JiraQueries.fetchIssuesByFixVersion(db, versionName: release.name)
        let total = issues.count
        let done = issues.filter { $0.statusCategory == "done" }.count
        let blocked = issues.filter { $0.status.lowercased().contains("block") }.count
        let progressPct = total > 0 ? Double(done) / Double(total) : 0.0

        // Group by epic
        var epicMap: [String: (epic: JiraIssue?, issues: [JiraIssue])] = [:]
        for issue in issues {
            let epicKey = issue.epicKey.isEmpty ? "_none_" : issue.epicKey
            var entry = epicMap[epicKey] ?? (epic: nil, issues: [])
            entry.issues.append(issue)
            epicMap[epicKey] = entry
        }

        // Load epic details for each group
        var epicProgress: [EpicProgressItem] = []
        for (epicKey, data) in epicMap where epicKey != "_none_" {
            let epicIssue = try? JiraIssue.fetchOne(
                db,
                sql: "SELECT * FROM jira_issues WHERE key = ? AND is_deleted = 0",
                arguments: [epicKey]
            )
            let epicTotal = data.issues.count
            let epicDone = data.issues.filter { $0.statusCategory == "done" }.count
            let epicPct = epicTotal > 0 ? Double(epicDone) / Double(epicTotal) : 0.0
            let badge: String
            if epicPct >= 1.0 {
                badge = "on_track"
            } else if epicPct >= 0.5 {
                badge = "at_risk"
            } else {
                badge = "behind"
            }
            epicProgress.append(EpicProgressItem(
                id: epicKey,
                key: epicKey,
                name: epicIssue?.summary ?? epicKey,
                progressPct: epicPct,
                statusBadge: badge,
                total: epicTotal,
                done: epicDone
            ))
        }
        epicProgress.sort { $0.progressPct < $1.progressPct }

        // Overdue check
        let isOverdue = Self.isDateInPast(release.releaseDate)

        // At-risk logic
        let blockedRatio = total > 0 ? Double(blocked) / Double(total) : 0.0
        let daysUntilRelease = Self.daysUntil(release.releaseDate)
        var atRisk = false
        var atRiskReason = ""
        if blockedRatio > 0.3 {
            atRisk = true
            atRiskReason = "\(Int(blockedRatio * 100))% blocked"
        } else if let days = daysUntilRelease, days < 7, progressPct < 0.8 {
            atRisk = true
            atRiskReason = "\(days)d left, \(Int(progressPct * 100))% done"
        }

        // Scope changes (last 7 days)
        let weekAgo = Calendar.current.date(byAdding: .day, value: -7, to: Date()) ?? Date()
        let scopeChanges = try JiraQueries.fetchScopeChanges(
            db,
            versionName: release.name,
            since: weekAgo
        )

        // Ping targets from blocked issues
        var pingTargets: [PingTargetItem] = []
        var seenSlack: Set<String> = []
        for issue in issues where issue.status.lowercased().contains("block") {
            if !issue.assigneeSlackId.isEmpty, !seenSlack.contains(issue.assigneeSlackId) {
                seenSlack.insert(issue.assigneeSlackId)
                pingTargets.append(PingTargetItem(
                    slackUserID: issue.assigneeSlackId,
                    displayName: issue.assigneeDisplayName,
                    reason: "assignee_blocker"
                ))
            }
        }

        return ReleaseItem(
            id: release.id,
            name: release.name,
            projectKey: release.projectKey,
            releaseDate: release.releaseDate,
            released: release.released,
            isOverdue: isOverdue,
            atRisk: atRisk,
            atRiskReason: atRiskReason,
            progressPct: progressPct,
            totalIssues: total,
            doneIssues: done,
            blockedCount: blocked,
            epicProgress: epicProgress,
            scopeChanges: scopeChanges,
            issues: issues,
            pingTargets: pingTargets
        )
    }

    // MARK: - Date Helpers

    private static let isoFormatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static let dateOnlyFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    private static func parseDate(_ dateStr: String) -> Date? {
        guard !dateStr.isEmpty else { return nil }
        return dateOnlyFormatter.date(from: dateStr)
            ?? isoFormatter.date(from: dateStr)
            ?? ISO8601DateFormatter().date(from: dateStr)
    }

    private static func isDateInPast(_ dateStr: String) -> Bool {
        guard let date = parseDate(dateStr) else { return false }
        return date < Date()
    }

    private static func daysUntil(_ dateStr: String) -> Int? {
        guard let date = parseDate(dateStr) else { return nil }
        return Calendar.current.dateComponents([.day], from: Date(), to: date).day
    }
}
