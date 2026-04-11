import Foundation
import GRDB

struct JiraIssue: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_issues"
    let key: String              // PK "PROJ-123"
    var id: String
    var projectKey: String
    var boardId: Int?
    var summary: String
    var descriptionText: String
    var issueType: String
    var issueTypeCategory: String // "epic" | "standard" | "subtask"
    var isBug: Bool
    var status: String
    var statusCategory: String    // "todo" | "in_progress" | "done"
    var statusCategoryChangedAt: String
    var assigneeAccountId: String
    var assigneeEmail: String
    var assigneeDisplayName: String
    var assigneeSlackId: String
    var reporterAccountId: String
    var reporterEmail: String
    var reporterDisplayName: String
    var reporterSlackId: String
    var priority: String
    var storyPoints: Double?
    var dueDate: String
    var sprintId: Int?
    var sprintName: String
    var epicKey: String
    var labels: String            // JSON array
    var components: String        // JSON array
    var createdAt: String
    var updatedAt: String
    var resolvedAt: String
    var fixVersions: String
    var rawJson: String
    var syncedAt: String
    var isDeleted: Bool

    var decodedFixVersions: [String] {
        guard let data = fixVersions.data(using: .utf8),
              let versions = try? JSONDecoder().decode([String].self, from: data) else { return [] }
        return versions
    }
}
