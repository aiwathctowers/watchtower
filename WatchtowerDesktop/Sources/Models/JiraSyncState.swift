import GRDB

struct JiraSyncState: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_sync_state"
    let projectKey: String
    var lastSyncedAt: String
    var issuesSynced: Int
    var lastError: String
    var lastErrorAt: String

    enum CodingKeys: String, CodingKey {
        case projectKey = "project_key"
        case lastSyncedAt = "last_synced_at"
        case issuesSynced = "issues_synced"
        case lastError = "last_error"
        case lastErrorAt = "last_error_at"
    }
}
