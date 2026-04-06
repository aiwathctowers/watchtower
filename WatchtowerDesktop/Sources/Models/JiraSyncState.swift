import GRDB

struct JiraSyncState: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_sync_state"
    let projectKey: String
    var lastSyncedAt: String
    var issuesSynced: Int
    var lastError: String
    var lastErrorAt: String
}
