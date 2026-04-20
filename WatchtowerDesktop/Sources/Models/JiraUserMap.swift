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

    enum CodingKeys: String, CodingKey {
        case jiraAccountId = "jira_account_id"
        case email
        case slackUserId = "slack_user_id"
        case displayName = "display_name"
        case matchMethod = "match_method"
        case matchConfidence = "match_confidence"
        case resolvedAt = "resolved_at"
    }
}
