import Foundation
import GRDB

@MainActor
@Observable
final class ProjectMapViewModel {
    var epics: [EpicItem] = []
    var isLoading = false
    var errorMessage: String?
    var searchText: String = ""
    var filterMode: FilterMode = .all
    var currentUserID: String?
    var reportIDs: [String] = []

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    // MARK: - Filter Mode

    enum FilterMode: String, CaseIterable {
        case all
        case mine
        case myReports
    }

    // MARK: - Types

    struct EpicItem: Identifiable {
        var id: String { key }
        let key: String
        let name: String
        let ownerName: String?
        let ownerSlackID: String?
        let progressPct: Double
        let statusBadge: EpicStatusBadge
        let statusReason: String
        let totalIssues: Int
        let doneIssues: Int
        let inProgressIssues: Int
        let staleCount: Int
        let blockedCount: Int
        let forecastWeeks: Double?
        let totalStoryPoints: Double
        let doneStoryPoints: Double
        let velocityPerWeek: Double
        let dueDate: String?
        let issues: [JiraIssue]
        let participants: [(slackID: String, name: String)]
        let pingTargets: [PingTargetItem]
        let createdAt: String?

        func withFilteredIssues(_ filtered: [JiraIssue]) -> EpicItem {
            EpicItem(
                key: key, name: name, ownerName: ownerName,
                ownerSlackID: ownerSlackID, progressPct: progressPct,
                statusBadge: statusBadge, statusReason: statusReason,
                totalIssues: totalIssues, doneIssues: doneIssues,
                inProgressIssues: inProgressIssues, staleCount: staleCount,
                blockedCount: blockedCount, forecastWeeks: forecastWeeks,
                totalStoryPoints: totalStoryPoints, doneStoryPoints: doneStoryPoints,
                velocityPerWeek: velocityPerWeek, dueDate: dueDate,
                issues: filtered, participants: participants,
                pingTargets: pingTargets, createdAt: createdAt
            )
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
        var result = epics

        // Apply filter mode
        switch filterMode {
        case .all:
            break
        case .mine:
            guard let uid = currentUserID, !uid.isEmpty else { break }
            result = result.compactMap { epic in
                let epicMatches = epic.ownerSlackID == uid
                let matchingIssues = epic.issues.filter { $0.assigneeSlackId == uid }
                guard epicMatches || !matchingIssues.isEmpty else { return nil }
                // Return epic with filtered issues but original stats
                return epic.withFilteredIssues(matchingIssues)
            }
        case .myReports:
            let reportSet = Set(reportIDs)
            guard !reportSet.isEmpty else { break }
            result = result.compactMap { epic in
                let epicMatches = epic.ownerSlackID.map { reportSet.contains($0) } ?? false
                let matchingIssues = epic.issues.filter { reportSet.contains($0.assigneeSlackId) }
                guard epicMatches || !matchingIssues.isEmpty else { return nil }
                return epic.withFilteredIssues(matchingIssues)
            }
        }

        // Apply search text filter
        if !searchText.isEmpty {
            let query = searchText.lowercased()
            result = result.filter {
                $0.key.lowercased().contains(query)
                || $0.name.lowercased().contains(query)
            }
        }

        return result
    }

    // MARK: - Init

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    // MARK: - Observation

    func startObserving() {
        guard observationTask == nil else { return }
        Task { await load() }
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
                    await self?.load()
                }
            } catch {
                print("ProjectMap observation error: \(error)")
            }
        }
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }

    // MARK: - Load

    func load() async {
        isLoading = true
        do {
            // Load current user and reports for filtering
            let (uid, reports) = try await Task.detached { [dbManager] in
                try dbManager.dbPool.read { db -> (String?, [String]) in
                    let userID = try String.fetchOne(
                        db, sql: "SELECT current_user_id FROM workspace LIMIT 1"
                    )
                    var reps: [String] = []
                    if let uid = userID, !uid.isEmpty,
                       let json = try String.fetchOne(
                           db,
                           sql: "SELECT reports FROM user_profile WHERE slack_user_id = ? LIMIT 1",
                           arguments: [uid]
                       ),
                       let data = json.data(using: .utf8),
                       let parsed = try? JSONDecoder().decode([String].self, from: data)
                    {
                        reps = parsed
                    }
                    return (userID, reps)
                }
            }.value
            currentUserID = uid
            reportIDs = reports

            let items = try await Task.detached { [dbManager] in
                try dbManager.dbPool.read { db -> [EpicItem] in
                    // Build status→phase mapping from board profiles
                    let excludedPhases: Set<String> = ["backlog", "done", "other"]
                    let statusPhaseMap = Self.buildStatusPhaseMap(db)

                    let isActivePhase: (JiraIssue) -> Bool = { issue in
                        if let phase = statusPhaseMap[issue.status] {
                            return !excludedPhases.contains(phase)
                        }
                        return issue.statusCategory != "done" && issue.statusCategory != "todo"
                    }

                    // Batch-load all non-deleted issues
                    let allIssues = try JiraIssue.fetchAll(
                        db,
                        sql: "SELECT * FROM jira_issues WHERE is_deleted = 0"
                    )

                    // Group ALL child issues by epic (for stats/velocity)
                    var allIssuesByEpic: [String: [JiraIssue]] = [:]
                    for issue in allIssues where !issue.epicKey.isEmpty {
                        allIssuesByEpic[issue.epicKey, default: []].append(issue)
                    }

                    // Only show epics that have at least one active child
                    let epicKeys = Set(
                        allIssuesByEpic.filter { _, children in
                            children.contains(where: isActivePhase)
                        }.keys
                    )
                    let epicIssues = allIssues.filter {
                        $0.issueTypeCategory == "epic" && epicKeys.contains($0.key)
                    }

                    let now = Date()
                    return epicIssues.map { epic in
                        let allChildren = allIssuesByEpic[epic.key] ?? []
                        // Display only active-phase issues, but compute stats from all
                        let displayIssues = allChildren.filter(isActivePhase)
                        return Self.buildEpicItem(
                            epic: epic,
                            childIssues: allChildren,
                            displayIssues: displayIssues,
                            now: now
                        )
                    }
                }
            }.value
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

    private nonisolated static func buildEpicItem(
        epic: JiraIssue,
        childIssues: [JiraIssue],
        displayIssues: [JiraIssue]? = nil,
        now: Date
    ) -> EpicItem {
        let total = childIssues.count
        let done = childIssues.filter { $0.statusCategory == "done" }.count
        let inProgress = childIssues.filter {
            $0.statusCategory == "indeterminate" || $0.statusCategory == "in_progress"
        }.count

        let stale = childIssues.filter { issue in
            guard issue.statusCategory != "done" else { return false }
            return JiraHelpers.daysSince(issue.statusCategoryChangedAt) > JiraHelpers.staleThresholdDays
        }.count

        let blocked = childIssues.filter { issue in
            issue.status.lowercased().contains("block")
        }.count

        let progressPct = total > 0 ? Double(done) / Double(total) : 0.0

        // Extract unique participants from displayed issues
        let visibleIssues = displayIssues ?? childIssues
        var participantMap: [(slackID: String, name: String)] = []
        var seenParticipants: Set<String> = []
        for issue in visibleIssues {
            if !issue.assigneeSlackId.isEmpty, !seenParticipants.contains(issue.assigneeSlackId) {
                seenParticipants.insert(issue.assigneeSlackId)
                participantMap.append((slackID: issue.assigneeSlackId, name: issue.assigneeDisplayName))
            }
        }
        participantMap.sort { $0.name < $1.name }

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
                return JiraHelpers.daysSince(issue.resolvedAt) <= JiraHelpers.velocityWindowDays
            }.count
            let weeklyVelocity = Double(recentDone) / 4.0
            guard weeklyVelocity > 0 else { return nil }
            return Double(total - done) / weeklyVelocity
        }()

        // Compute velocity metrics matching Go epic_progress.go algorithm.
        let resolvedLastWeek = childIssues.filter { issue in
            guard issue.statusCategory == "done", !issue.resolvedAt.isEmpty else { return false }
            return JiraHelpers.daysSince(issue.resolvedAt) <= JiraHelpers.staleThresholdDays
        }.count
        let resolvedLast4W = childIssues.filter { issue in
            guard issue.statusCategory == "done", !issue.resolvedAt.isEmpty else { return false }
            return JiraHelpers.daysSince(issue.resolvedAt) <= JiraHelpers.velocityWindowDays
        }.count
        let velocityPerWeek = Double(resolvedLast4W) / 4.0

        // Due date from epic itself (needed for deadline-aware badge).
        let dueDate = epic.dueDate.isEmpty ? nil : epic.dueDate

        let (badge, statusReason) = computeStatusBadge(
            total: total,
            done: done,
            resolvedLastWeek: resolvedLastWeek,
            velocityPerWeek: velocityPerWeek,
            blocked: blocked,
            stale: stale,
            forecastWeeks: forecastWeeks,
            dueDate: dueDate
        )

        // Story points
        let totalSP = childIssues.compactMap(\.storyPoints).reduce(0, +)
        let doneSP = childIssues.filter { $0.statusCategory == "done" }
            .compactMap(\.storyPoints).reduce(0, +)

        return EpicItem(
            key: epic.key,
            name: epic.summary,
            ownerName: epic.assigneeDisplayName.isEmpty ? nil : epic.assigneeDisplayName,
            ownerSlackID: epic.assigneeSlackId.isEmpty ? nil : epic.assigneeSlackId,
            progressPct: progressPct,
            statusBadge: badge,
            statusReason: statusReason,
            totalIssues: total,
            doneIssues: done,
            inProgressIssues: inProgress,
            staleCount: stale,
            blockedCount: blocked,
            forecastWeeks: forecastWeeks,
            totalStoryPoints: totalSP,
            doneStoryPoints: doneSP,
            velocityPerWeek: velocityPerWeek,
            dueDate: dueDate,
            issues: visibleIssues,
            participants: participantMap,
            pingTargets: pingTargets,
            createdAt: epic.createdAt
        )
    }

    private nonisolated static func computeStatusBadge(
        total: Int,
        done: Int,
        resolvedLastWeek: Int,
        velocityPerWeek: Double,
        blocked: Int,
        stale: Int,
        forecastWeeks: Double?,
        dueDate: String?
    ) -> (EpicStatusBadge, String) {
        let remaining = total - done
        if remaining == 0 {
            return (.onTrack, "All issues done")
        }

        // Compute forecast date and deadline comparison.
        let now = Date()
        let cal = Calendar.current
        var forecastDateStr: String?
        var daysLate = 0

        if let fw = forecastWeeks, fw < 999 {
            let forecastDate = cal.date(byAdding: .day, value: Int(fw * 7), to: now) ?? now
            let fmt = DateFormatter()
            fmt.dateFormat = "yyyy-MM-dd"
            forecastDateStr = fmt.string(from: forecastDate)

            if let due = dueDate, !due.isEmpty, let dueD = fmt.date(from: due) {
                let diff = cal.dateComponents([.day], from: dueD, to: forecastDate).day ?? 0
                if diff > 0 { daysLate = diff }
            }
        } else if let due = dueDate, !due.isEmpty {
            let fmt = DateFormatter()
            fmt.dateFormat = "yyyy-MM-dd"
            if let dueD = fmt.date(from: due) {
                let diff = cal.dateComponents([.day], from: dueD, to: now).day ?? 0
                if diff > 0 { daysLate = diff }
            }
        }

        let shortDate: (String) -> String = { dateStr in
            JiraHelpers.shortDate(dateStr)
        }

        // Deadline-aware checks first.
        if daysLate > 0 && velocityPerWeek > 0, let fd = forecastDateStr, let due = dueDate {
            let weeks = Double(daysLate) / 7.0
            if weeks < 1 {
                return (.atRisk, "Tight — forecast \(shortDate(fd)), due \(shortDate(due))")
            }
            return (.behind, "Forecast \(shortDate(fd)), due \(shortDate(due)) — ~\(Int(weeks.rounded())) wk late")
        }
        if daysLate > 0 && velocityPerWeek == 0, let due = dueDate {
            return (.behind, "No velocity, \(daysLate)d past due \(shortDate(due))")
        }

        if velocityPerWeek == 0 {
            return (.behind, "No issues resolved in the last 4 weeks")
        }

        if resolvedLastWeek == 0 {
            var reason = "No issues resolved this week"
            if blocked > 0 { reason += ", \(blocked) blocked" }
            if stale > 0 { reason += ", \(stale) stale" }
            return (.behind, reason)
        }

        if Double(resolvedLastWeek) < velocityPerWeek {
            let pct = Int((1.0 - Double(resolvedLastWeek) / velocityPerWeek) * 100)
            return (.atRisk, "Velocity dropped \(pct)% vs avg (\(resolvedLastWeek) vs \(String(format: "%.1f", velocityPerWeek))/wk)")
        }

        // On track — include deadline info if available.
        if let fd = forecastDateStr, let due = dueDate, !due.isEmpty {
            return (.onTrack, "On pace — forecast \(shortDate(fd)), due \(shortDate(due))")
        }
        return (.onTrack, "\(String(format: "%.1f", velocityPerWeek)) issues/wk, \(remaining) remaining")
    }

    /// Build a mapping from Jira status name → phase using all board profiles + user overrides.
    private nonisolated static func buildStatusPhaseMap(_ db: Database) -> [String: String] {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase

        guard let boards = try? JiraBoard
            .filter(Column("is_selected") == true)
            .fetchAll(db) else { return [:] }

        var map: [String: String] = [:]

        for board in boards {
            guard !board.llmProfileJSON.isEmpty,
                  let data = board.llmProfileJSON.data(using: .utf8),
                  let profile = try? decoder.decode(BoardProfileDisplay.self, from: data)
            else { continue }

            // Parse user overrides for phase remapping
            var phaseOverrides: [String: String] = [:]
            if !board.userOverridesJSON.isEmpty,
               let overData = board.userOverridesJSON.data(using: .utf8),
               let wrapper = try? JSONDecoder().decode(UserOverridesWrapper.self, from: overData) {
                phaseOverrides = wrapper.phaseOverrides ?? [:]
            }

            for stage in profile.workflowStages {
                for status in stage.originalStatuses {
                    map[status] = phaseOverrides[status] ?? stage.phase
                }
            }
        }

        return map
    }

    private nonisolated static func statusSortOrder(_ badge: EpicStatusBadge) -> Int {
        switch badge {
        case .behind: 0
        case .atRisk: 1
        case .onTrack: 2
        }
    }
}
