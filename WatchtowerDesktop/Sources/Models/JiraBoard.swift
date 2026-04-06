import GRDB

struct JiraBoard: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_boards"
    let id: Int
    var name: String
    var projectKey: String
    var boardType: String        // "scrum" | "kanban" | "simple"
    var isSelected: Bool
    var issueCount: Int
    var syncedAt: String
}
