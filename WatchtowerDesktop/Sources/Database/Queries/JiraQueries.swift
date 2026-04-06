import Foundation
import GRDB

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
}
