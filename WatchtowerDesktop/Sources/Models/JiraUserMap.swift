import GRDB

struct JiraUserMap: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_user_map"
    let jiraAccountId: String
    var email: String
    var slackUserId: String
    var displayName: String
    var matchMethod: String      // "email" | "display_name" | "manual" | "unresolved"
    var matchConfidence: Double
    var resolvedAt: String
}
