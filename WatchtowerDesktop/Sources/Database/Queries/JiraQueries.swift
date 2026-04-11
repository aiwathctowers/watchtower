import Foundation
import GRDB

// MARK: - Query Result Models

struct SprintStats {
    let sprintName: String
    let total: Int
    let done: Int
    let inProgress: Int
    let todo: Int
    let daysLeft: Int
}

struct JiraDeliveryStats {
    let issuesClosed: Int
    let avgCycleTimeDays: Double
    let storyPointsCompleted: Double
    let openIssues: Int
    let overdueIssues: Int
    let components: [String]
    let labels: [String]
}

struct EpicProgressRow: Codable, FetchableRecord, Identifiable {
    var id: String { epicKey }
    var epicKey: String
    var epicName: String
    var totalIssues: Int
    var doneIssues: Int
    var inProgressIssues: Int
    var progressPct: Double
    var weeklyResolvedCount: Int
    var monthlyResolvedCount: Int

    enum CodingKeys: String, CodingKey {
        case epicKey = "epic_key"
        case epicName = "epic_name"
        case totalIssues = "total_issues"
        case doneIssues = "done_issues"
        case inProgressIssues = "in_progress_issues"
        case progressPct = "progress_pct"
        case weeklyResolvedCount = "weekly_resolved_count"
        case monthlyResolvedCount = "monthly_resolved_count"
    }
}

struct WithoutJiraRow: Codable, FetchableRecord, Identifiable {
    var id: String { channelID }
    var channelID: String
    var channelName: String
    var digestCount: Int
    var distinctDays: Int
    var messageCount: Int

    enum CodingKeys: String, CodingKey {
        case channelID = "channel_id"
        case channelName = "channel_name"
        case digestCount = "digest_count"
        case distinctDays = "distinct_days"
        case messageCount = "message_count"
    }
}

struct TeamWorkloadRow: Codable, FetchableRecord {
    var slackUserID: String
    var displayName: String
    var openIssues: Int
    var storyPoints: Double
    var overdueCount: Int
    var blockedCount: Int
    var avgCycleTimeDays: Double

    enum CodingKeys: String, CodingKey {
        case slackUserID = "slack_user_id"
        case displayName = "display_name"
        case openIssues = "open_issues"
        case storyPoints = "story_points"
        case overdueCount = "overdue_count"
        case blockedCount = "blocked_count"
        case avgCycleTimeDays = "avg_cycle_time_days"
    }
}

enum JiraQueries {

    // MARK: - Shared Formatters

    private static let isoFormatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static let dayFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    // MARK: - Connection Status

    /// Check if jira_token.json exists in any workspace directory.
    static func isConnected() -> Bool {
        let basePath = Constants.databasePath
        let fileManager = FileManager.default
        guard let contents = try? fileManager.contentsOfDirectory(
            atPath: basePath
        ) else {
            return false
        }
        for dir in contents {
            let tokenPath = "\(basePath)/\(dir)/jira_token.json"
            if fileManager.fileExists(atPath: tokenPath) {
                return true
            }
        }
        return false
    }

    // MARK: - Boards

    static func fetchAllBoards(_ db: Database) throws -> [JiraBoard] {
        try JiraBoard.order(Column("name")).fetchAll(db)
    }

    static func fetchSelectedBoards(
        _ db: Database
    ) throws -> [JiraBoard] {
        try JiraBoard
            .filter(Column("is_selected") == true)
            .order(Column("name"))
            .fetchAll(db)
    }

    static func fetchBoard(
        _ db: Database,
        id: Int
    ) throws -> JiraBoard? {
        try JiraBoard.fetchOne(db, key: id)
    }

    // MARK: - Sync State

    static func fetchLastSyncTime(
        _ db: Database
    ) throws -> String? {
        try String.fetchOne(
            db,
            sql: """
                SELECT MAX(last_synced_at)
                FROM jira_sync_state
                WHERE last_synced_at != ''
                """
        )
    }

    static func fetchSyncStates(
        _ db: Database
    ) throws -> [JiraSyncState] {
        try JiraSyncState.fetchAll(db)
    }

    // MARK: - Issues

    static func fetchIssueCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE is_deleted = 0
                """
        ) ?? 0
    }

    static func fetchIssueCountByBoard(
        _ db: Database,
        boardId: Int
    ) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE board_id = ? AND is_deleted = 0
                """,
            arguments: [boardId]
        ) ?? 0
    }

    // MARK: - User Mapping

    static func fetchUserMappings(
        _ db: Database
    ) throws -> [JiraUserMap] {
        try JiraUserMap.order(Column("display_name")).fetchAll(db)
    }

    static func fetchMappedCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_user_map
                WHERE slack_user_id != ''
                """
        ) ?? 0
    }

    static func fetchUnmappedCount(_ db: Database) throws -> Int {
        try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_user_map
                WHERE slack_user_id = ''
                """
        ) ?? 0
    }

    // MARK: - Issue Lookups (Phase 1)

    /// Fetch a single issue by its key (e.g. "PROJ-123").
    static func fetchIssueByKey(
        _ db: Database,
        key: String
    ) throws -> JiraIssue? {
        try JiraIssue.filter(Column("key") == key).fetchOne(db)
    }

    /// Fetch all issues linked to a track via jira_slack_links.
    static func fetchIssuesForTrack(
        _ db: Database,
        trackID: Int
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT DISTINCT ji.*
                FROM jira_issues ji
                JOIN jira_slack_links jsl ON jsl.issue_key = ji.key
                WHERE jsl.track_id = ? AND ji.is_deleted = 0
                ORDER BY ji.updated_at DESC
                """,
            arguments: [trackID]
        )
    }

    /// Batch-fetch issues for multiple tracks in a single query.
    static func fetchIssuesForTracks(
        _ db: Database,
        trackIDs: [Int]
    ) throws -> [Int: [JiraIssue]] {
        guard !trackIDs.isEmpty else { return [:] }

        let placeholders = trackIDs.map { _ in "?" }.joined(separator: ",")
        let sql = """
            SELECT DISTINCT ji.*, jsl.track_id AS _track_id
            FROM jira_issues ji
            JOIN jira_slack_links jsl ON jsl.issue_key = ji.key
            WHERE jsl.track_id IN (\(placeholders)) AND ji.is_deleted = 0
            ORDER BY ji.updated_at DESC
            """
        let rows = try Row.fetchAll(
            db,
            sql: sql,
            arguments: StatementArguments(trackIDs)
        )

        var result: [Int: [JiraIssue]] = [:]
        for row in rows {
            guard let trackID: Int = row["_track_id"] else { continue }
            let issue = try JiraIssue(row: row)
            result[trackID, default: []].append(issue)
        }
        return result
    }

    /// Fetch all issues linked to a digest via jira_slack_links.
    static func fetchIssuesForDigest(
        _ db: Database,
        digestID: Int
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT DISTINCT ji.*
                FROM jira_issues ji
                JOIN jira_slack_links jsl ON jsl.issue_key = ji.key
                WHERE jsl.digest_id = ? AND ji.is_deleted = 0
                ORDER BY ji.updated_at DESC
                """,
            arguments: [digestID]
        )
    }

    /// Fetch active (non-done) issues assigned to a user by Slack ID.
    static func fetchIssuesByAssigneeSlackID(
        _ db: Database,
        slackID: String
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category != 'done'
                  AND is_deleted = 0
                ORDER BY priority, updated_at DESC
                """,
            arguments: [slackID]
        )
    }

    /// Return just the issue keys linked to a track.
    static func fetchLinkedIssueKeys(
        _ db: Database,
        trackID: Int
    ) throws -> [String] {
        try String.fetchAll(
            db,
            sql: """
                SELECT DISTINCT jsl.issue_key
                FROM jira_slack_links jsl
                JOIN jira_issues ji ON ji.key = jsl.issue_key
                WHERE jsl.track_id = ? AND ji.is_deleted = 0
                ORDER BY jsl.issue_key
                """,
            arguments: [trackID]
        )
    }

    // MARK: - Issue Links

    /// Fetch all issue links where source_key or target_key matches the given key.
    static func fetchIssueLinks(
        _ db: Database,
        issueKey: String
    ) throws -> [JiraIssueLink] {
        try JiraIssueLink.fetchAll(
            db,
            sql: """
                SELECT id, source_key, target_key, link_type, synced_at
                FROM jira_issue_links
                WHERE source_key = ? OR target_key = ?
                ORDER BY id
                """,
            arguments: [issueKey, issueKey]
        )
    }

    // MARK: - Sprint Stats

    /// Compute stats for the active sprint on a board.
    static func fetchActiveSprintStats(
        _ db: Database,
        boardID: Int
    ) throws -> SprintStats? {
        // Find the active sprint for this board.
        guard let row = try Row.fetchOne(
            db,
            sql: """
                SELECT id, name, end_date
                FROM jira_sprints
                WHERE board_id = ? AND state = 'active'
                ORDER BY start_date
                LIMIT 1
                """,
            arguments: [boardID]
        ) else {
            return nil
        }

        guard let sprintID: Int = row["id"],
              let sprintName: String = row["name"] else {
            return nil
        }
        let endDateStr: String = row["end_date"] ?? ""

        // Count issues by status category.
        let total = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE sprint_id = ? AND is_deleted = 0
                """,
            arguments: [sprintID]
        ) ?? 0

        let done = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE sprint_id = ? AND status_category = 'done'
                  AND is_deleted = 0
                """,
            arguments: [sprintID]
        ) ?? 0

        let inProgress = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE sprint_id = ? AND status_category = 'in_progress'
                  AND is_deleted = 0
                """,
            arguments: [sprintID]
        ) ?? 0

        let todo = total - done - inProgress

        // Calculate days left.
        var daysLeft = 0
        if !endDateStr.isEmpty {
            if let endDate = isoFormatter.date(from: endDateStr)
                ?? ISO8601DateFormatter().date(from: endDateStr) {
                let remaining = Calendar.current.dateComponents(
                    [.day],
                    from: Date(),
                    to: endDate
                ).day ?? 0
                daysLeft = max(0, remaining)
            }
        }

        return SprintStats(
            sprintName: sprintName,
            total: total,
            done: done,
            inProgress: inProgress,
            todo: todo,
            daysLeft: daysLeft
        )
    }

    // MARK: - Delivery Stats

    /// Compute delivery metrics for a user (by Slack ID) within a date range.
    static func fetchDeliveryStats(
        _ db: Database,
        slackID: String,
        from: Date,
        to: Date
    ) throws -> JiraDeliveryStats {
        let fromStr = isoFormatter.string(from: from)
        let toStr = isoFormatter.string(from: to)

        // Issues closed in the period.
        let issuesClosed = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category = 'done'
                  AND resolved_at >= ? AND resolved_at <= ?
                  AND is_deleted = 0
                """,
            arguments: [slackID, fromStr, toStr]
        ) ?? 0

        // Average cycle time (created → resolved) in days.
        let avgCycleTimeDays = try Double.fetchOne(
            db,
            sql: """
                SELECT AVG(
                    julianday(resolved_at) - julianday(created_at)
                )
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category = 'done'
                  AND resolved_at >= ? AND resolved_at <= ?
                  AND resolved_at != ''
                  AND is_deleted = 0
                """,
            arguments: [slackID, fromStr, toStr]
        ) ?? 0.0

        // Total story points completed.
        let storyPointsCompleted = try Double.fetchOne(
            db,
            sql: """
                SELECT COALESCE(SUM(story_points), 0)
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category = 'done'
                  AND resolved_at >= ? AND resolved_at <= ?
                  AND is_deleted = 0
                """,
            arguments: [slackID, fromStr, toStr]
        ) ?? 0.0

        // Currently open issues.
        let openIssues = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category != 'done'
                  AND is_deleted = 0
                """,
            arguments: [slackID]
        ) ?? 0

        // Overdue issues.
        let nowStr = isoFormatter.string(from: Date())
        let overdueIssues = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category != 'done'
                  AND due_date != '' AND due_date < ?
                  AND is_deleted = 0
                """,
            arguments: [slackID, nowStr]
        ) ?? 0

        // Distinct components from closed issues.
        let componentRows = try String.fetchAll(
            db,
            sql: """
                SELECT DISTINCT components
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category = 'done'
                  AND resolved_at >= ? AND resolved_at <= ?
                  AND components != '[]'
                  AND is_deleted = 0
                """,
            arguments: [slackID, fromStr, toStr]
        )
        let components = Self.extractJSONArrayValues(componentRows)

        // Distinct labels from closed issues.
        let labelRows = try String.fetchAll(
            db,
            sql: """
                SELECT DISTINCT labels
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND status_category = 'done'
                  AND resolved_at >= ? AND resolved_at <= ?
                  AND labels != '[]'
                  AND is_deleted = 0
                """,
            arguments: [slackID, fromStr, toStr]
        )
        let labels = Self.extractJSONArrayValues(labelRows)

        return JiraDeliveryStats(
            issuesClosed: issuesClosed,
            avgCycleTimeDays: avgCycleTimeDays,
            storyPointsCompleted: storyPointsCompleted,
            openIssues: openIssues,
            overdueIssues: overdueIssues,
            components: components,
            labels: labels
        )
    }

    // MARK: - Team Workload

    /// Aggregate workload metrics per assignee (mirrors Go GetJiraTeamWorkload).
    static func fetchTeamWorkload(_ db: Database) throws -> [TeamWorkloadRow] {
        let today = Self.dayFormatter.string(from: Date())
        let thirtyDaysAgo = Self.dayFormatter.string(
            from: Calendar.current.date(byAdding: .day, value: -30, to: Date()) ?? Date()
        )

        let sql = """
            SELECT
                ji.assignee_slack_id AS slack_user_id,
                ji.assignee_display_name AS display_name,
                COUNT(*) FILTER (WHERE ji.status_category != 'done') AS open_issues,
                COALESCE(SUM(ji.story_points) FILTER (WHERE ji.status_category != 'done'), 0) AS story_points,
                COUNT(*) FILTER (WHERE ji.status_category != 'done' AND ji.due_date != '' AND ji.due_date < ?) AS overdue_count,
                COUNT(*) FILTER (WHERE ji.status_category != 'done' AND LOWER(ji.status) LIKE '%block%') AS blocked_count,
                COALESCE(AVG(julianday(ji.resolved_at) - julianday(ji.created_at))
                    FILTER (WHERE ji.status_category = 'done' AND ji.resolved_at != '' AND ji.resolved_at >= ?), 0) AS avg_cycle_time_days
            FROM jira_issues ji
            WHERE ji.assignee_slack_id != '' AND ji.is_deleted = 0
            GROUP BY ji.assignee_slack_id
            ORDER BY open_issues DESC
            """

        return try TeamWorkloadRow.fetchAll(db, sql: sql, arguments: [today, thirtyDaysAgo])
    }

    /// Count Slack messages by a user in a time range.
    static func fetchSlackMessageCount(_ db: Database, userID: String, from: Date, to: Date) throws -> Int {
        let fromUnix = from.timeIntervalSince1970
        let toUnix = to.timeIntervalSince1970
        return try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM messages
                WHERE user_id = ? AND ts_unix >= ? AND ts_unix < ?
                """,
            arguments: [userID, fromUnix, toUnix]
        ) ?? 0
    }

    /// Sum meeting hours from calendar_events for a user in a time range.
    /// Returns 0 if the table does not exist or is empty.
    static func fetchMeetingHours(_ db: Database, userID: String, from: Date, to: Date) throws -> Double {
        // Check if calendar_events table exists
        let tableExists = try Bool.fetchOne(
            db,
            sql: """
                SELECT COUNT(*) > 0
                FROM sqlite_master
                WHERE type = 'table' AND name = 'calendar_events'
                """
        ) ?? false
        guard tableExists else { return 0 }

        let fromStr = isoFormatter.string(from: from)
        let toStr = isoFormatter.string(from: to)

        // Sum duration in hours. Attendees is JSON array — check if userID appears.
        return try Double.fetchOne(
            db,
            sql: """
                SELECT COALESCE(SUM(
                    (julianday(end_time) - julianday(start_time)) * 24.0
                ), 0)
                FROM calendar_events
                WHERE start_time >= ? AND end_time <= ?
                  AND (attendees LIKE ? OR attendees LIKE '%' || ? || '%')
                """,
            arguments: [fromStr, toStr, "%\(userID)%", userID]
        ) ?? 0
    }

    /// Fetch all issues (not done, not deleted) assigned to a given Slack user ID.
    static func fetchIssuesByAssignee(_ db: Database, slackID: String) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE assignee_slack_id = ?
                  AND is_deleted = 0
                ORDER BY
                  CASE status_category WHEN 'in_progress' THEN 0 WHEN 'todo' THEN 1 ELSE 2 END,
                  priority,
                  updated_at DESC
                """,
            arguments: [slackID]
        )
    }

    // MARK: - Blocker Map Queries

    /// Fetch all blocked issues (status contains "block", not done).
    static func fetchBlockedIssues(_ db: Database) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE LOWER(status) LIKE '%block%'
                  AND status_category != 'done'
                  AND is_deleted = 0
                ORDER BY updated_at DESC
                """
        )
    }

    /// Fetch stale issues (in_progress, status_category_changed_at > staleDays ago).
    static func fetchStaleIssues(
        _ db: Database,
        staleDays: Int = 7
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE status_category = 'in_progress'
                  AND status_category_changed_at != ''
                  AND julianday('now') - julianday(status_category_changed_at) > ?
                  AND is_deleted = 0
                ORDER BY status_category_changed_at ASC
                """,
            arguments: [staleDays]
        )
    }

    /// Fetch issue links for a set of issue keys (for chain building).
    static func fetchIssueLinksForKeys(
        _ db: Database,
        keys: [String]
    ) throws -> [JiraIssueLink] {
        guard !keys.isEmpty else { return [] }
        let placeholders = keys.map { _ in "?" }.joined(separator: ",")
        return try JiraIssueLink.fetchAll(
            db,
            sql: """
                SELECT id, source_key, target_key, link_type, synced_at
                FROM jira_issue_links
                WHERE source_key IN (\(placeholders))
                   OR target_key IN (\(placeholders))
                ORDER BY id
                """,
            arguments: StatementArguments(keys + keys)
        )
    }

    /// Fetch the latest Slack message text referencing an issue (via jira_slack_links).
    static func fetchSlackContextForIssue(
        _ db: Database,
        issueKey: String
    ) throws -> String? {
        try String.fetchOne(
            db,
            sql: """
                SELECT m.text
                FROM jira_slack_links jsl
                JOIN messages m ON m.channel_id = jsl.channel_id
                                AND m.ts = jsl.message_ts
                WHERE jsl.issue_key = ?
                  AND jsl.message_ts != ''
                ORDER BY m.ts DESC
                LIMIT 1
                """,
            arguments: [issueKey]
        )
    }

    // MARK: - Epic Progress

    /// Aggregate epic progress: total/done/in-progress counts plus resolved in 7 and 28 days.
    static func fetchEpicProgress(_ db: Database) throws -> [EpicProgressRow] {
        let sevenDaysAgo = dayFormatter.string(
            from: Calendar.current.date(byAdding: .day, value: -7, to: Date()) ?? Date()
        )
        let twentyEightDaysAgo = dayFormatter.string(
            from: Calendar.current.date(byAdding: .day, value: -28, to: Date()) ?? Date()
        )

        let sql = """
            SELECT
                ji.epic_key,
                COALESCE(epic.summary, ji.epic_key) AS epic_name,
                COUNT(*) AS total_issues,
                COUNT(*) FILTER (WHERE ji.status_category = 'done') AS done_issues,
                COUNT(*) FILTER (WHERE ji.status_category = 'in_progress') AS in_progress_issues,
                CASE WHEN COUNT(*) > 0
                     THEN CAST(COUNT(*) FILTER (WHERE ji.status_category = 'done') AS REAL) / COUNT(*)
                     ELSE 0 END AS progress_pct,
                COUNT(*) FILTER (WHERE ji.status_category = 'done' AND ji.resolved_at >= ?) AS weekly_resolved_count,
                COUNT(*) FILTER (WHERE ji.status_category = 'done' AND ji.resolved_at >= ?) AS monthly_resolved_count
            FROM jira_issues ji
            LEFT JOIN jira_issues epic ON epic.key = ji.epic_key AND epic.is_deleted = 0
            WHERE ji.epic_key != '' AND ji.is_deleted = 0
            GROUP BY ji.epic_key
            HAVING COUNT(*) >= 3
            ORDER BY progress_pct DESC, total_issues DESC
            """

        return try EpicProgressRow.fetchAll(
            db,
            sql: sql,
            arguments: [sevenDaysAgo, twentyEightDaysAgo]
        )
    }

    // MARK: - Without Jira Detection

    /// Channels that have digests in the period but no linked Jira issues.
    static func fetchChannelsWithoutJira(
        _ db: Database,
        since: Date
    ) throws -> [WithoutJiraRow] {
        let sinceUnix = since.timeIntervalSince1970

        let sql = """
            SELECT
                d.channel_id,
                COALESCE(c.name, d.channel_id) AS channel_name,
                COUNT(DISTINCT d.id) AS digest_count,
                COUNT(DISTINCT DATE(d.created_at)) AS distinct_days,
                SUM(d.message_count) AS message_count
            FROM digests d
            LEFT JOIN channels c ON c.id = d.channel_id
            WHERE d.channel_id != ''
              AND d.type = 'channel'
              AND d.period_from >= ?
              AND d.channel_id NOT IN (
                  SELECT DISTINCT jsl.channel_id
                  FROM jira_slack_links jsl
                  WHERE jsl.channel_id != ''
              )
            GROUP BY d.channel_id
            ORDER BY message_count DESC
            """

        return try WithoutJiraRow.fetchAll(db, sql: sql, arguments: [sinceUnix])
    }

    // MARK: - Linked Issues Grouped

    /// Fetch linked issues grouped by relationship type for a given issue key.
    static func fetchLinkedIssuesGrouped(
        _ db: Database,
        issueKey: String
    ) throws -> (blocks: [JiraIssue], blockedBy: [JiraIssue], relatesTo: [JiraIssue]) {
        let links = try fetchIssueLinks(db, issueKey: issueKey)

        var blocks: [JiraIssue] = []
        var blockedBy: [JiraIssue] = []
        var relatesTo: [JiraIssue] = []

        for link in links {
            let isSource = link.sourceKey == issueKey
            let otherKey = isSource ? link.targetKey : link.sourceKey
            guard let issue = try JiraIssue
                .filter(Column("key") == otherKey)
                .filter(Column("is_deleted") == false)
                .fetchOne(db) else { continue }

            let linkType = link.linkType.lowercased()
            if linkType.contains("block") {
                if isSource {
                    blocks.append(issue)
                } else {
                    blockedBy.append(issue)
                }
            } else {
                relatesTo.append(issue)
            }
        }

        return (blocks: blocks, blockedBy: blockedBy, relatesTo: relatesTo)
    }

    // MARK: - Releases

    /// Fetch releases for a project, sorted by release_date.
    static func fetchReleases(
        _ db: Database,
        projectKey: String
    ) throws -> [JiraRelease] {
        try JiraRelease
            .filter(Column("project_key") == projectKey)
            .order(Column("release_date"))
            .fetchAll(db)
    }

    /// Fetch unreleased, non-archived releases across all projects.
    static func fetchUnreleasedReleases(
        _ db: Database
    ) throws -> [JiraRelease] {
        try JiraRelease
            .filter(Column("released") == false)
            .filter(Column("archived") == false)
            .order(Column("release_date"))
            .fetchAll(db)
    }

    /// Fetch issues where fix_versions JSON array contains versionName.
    static func fetchIssuesByFixVersion(
        _ db: Database,
        versionName: String
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE fix_versions LIKE ?
                  AND is_deleted = 0
                ORDER BY
                  CASE status_category WHEN 'in_progress' THEN 0 WHEN 'todo' THEN 1 ELSE 2 END,
                  priority,
                  updated_at DESC
                """,
            arguments: ["%\"\(versionName)\"%"]
        )
    }

    /// Fetch all epic-type issues (non-deleted).
    static func fetchAllEpics(_ db: Database) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE issue_type_category = 'epic'
                  AND is_deleted = 0
                ORDER BY updated_at DESC
                """
        )
    }

    /// Fetch non-deleted issues belonging to an epic.
    static func fetchIssuesByEpicKey(
        _ db: Database,
        epicKey: String
    ) throws -> [JiraIssue] {
        try JiraIssue.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_issues
                WHERE epic_key = ?
                  AND is_deleted = 0
                ORDER BY
                  CASE status_category WHEN 'in_progress' THEN 0 WHEN 'todo' THEN 1 ELSE 2 END,
                  priority,
                  updated_at DESC
                """,
            arguments: [epicKey]
        )
    }

    /// Fetch unique assignees (Slack ID + display name) for issues in an epic.
    static func fetchParticipantsForEpic(
        _ db: Database,
        epicKey: String
    ) throws -> [(slackID: String, name: String)] {
        let rows = try Row.fetchAll(
            db,
            sql: """
                SELECT DISTINCT assignee_slack_id, assignee_display_name
                FROM jira_issues
                WHERE epic_key = ?
                  AND assignee_slack_id != ''
                  AND is_deleted = 0
                ORDER BY assignee_display_name
                """,
            arguments: [epicKey]
        )
        return rows.compactMap { row in
            guard let slackID: String = row["assignee_slack_id"],
                  let name: String = row["assignee_display_name"] else { return nil }
            return (slackID: slackID, name: name)
        }
    }

    /// Batch-fetch Slack links for a set of issue keys.
    static func fetchSlackLinksForIssueKeys(
        _ db: Database,
        keys: [String]
    ) throws -> [JiraSlackLink] {
        guard !keys.isEmpty else { return [] }
        let placeholders = keys.map { _ in "?" }.joined(separator: ",")
        return try JiraSlackLink.fetchAll(
            db,
            sql: """
                SELECT *
                FROM jira_slack_links
                WHERE issue_key IN (\(placeholders))
                ORDER BY detected_at DESC
                """,
            arguments: StatementArguments(keys)
        )
    }

    /// Count issues added to / removed from a fix version since a given date.
    /// "Added" = issues currently in the version with synced_at >= since.
    /// "Removed" = approximated as 0 (fix_versions is current state, no history).
    static func fetchScopeChanges(
        _ db: Database,
        versionName: String,
        since: Date
    ) throws -> (added: Int, removed: Int) {
        let sinceStr = isoFormatter.string(from: since)
        let added = try Int.fetchOne(
            db,
            sql: """
                SELECT COUNT(*)
                FROM jira_issues
                WHERE fix_versions LIKE ?
                  AND synced_at >= ?
                  AND is_deleted = 0
                """,
            arguments: ["%\"\(versionName)\"%", sinceStr]
        ) ?? 0

        // Removed count requires historical data not available in current schema.
        return (added: added, removed: 0)
    }

    // MARK: - Helpers

    /// Parse an array of JSON-encoded string arrays and return
    /// the unique flattened values.
    private static func extractJSONArrayValues(
        _ jsonStrings: [String]
    ) -> [String] {
        var result = Set<String>()
        for jsonStr in jsonStrings {
            guard let data = jsonStr.data(using: .utf8),
                  let arr = try? JSONDecoder().decode(
                      [String].self,
                      from: data
                  ) else {
                continue
            }
            for value in arr {
                result.insert(value)
            }
        }
        return Array(result).sorted()
    }
}
