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

enum JiraQueries {

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

        let sprintID: Int = row["id"]
        let sprintName: String = row["name"]
        let endDateStr: String = row["end_date"]

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
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [
                .withInternetDateTime,
                .withFractionalSeconds
            ]
            if let endDate = formatter.date(from: endDateStr)
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
        let formatter = ISO8601DateFormatter()
        let fromStr = formatter.string(from: from)
        let toStr = formatter.string(from: to)

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
        let nowStr = formatter.string(from: Date())
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
