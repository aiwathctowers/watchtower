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

    enum CodingKeys: String, CodingKey {
        case key, id, summary, status, priority, labels, components
        case projectKey = "project_key"
        case boardId = "board_id"
        case descriptionText = "description_text"
        case issueType = "issue_type"
        case issueTypeCategory = "issue_type_category"
        case isBug = "is_bug"
        case statusCategory = "status_category"
        case statusCategoryChangedAt = "status_category_changed_at"
        case assigneeAccountId = "assignee_account_id"
        case assigneeEmail = "assignee_email"
        case assigneeDisplayName = "assignee_display_name"
        case assigneeSlackId = "assignee_slack_id"
        case reporterAccountId = "reporter_account_id"
        case reporterEmail = "reporter_email"
        case reporterDisplayName = "reporter_display_name"
        case reporterSlackId = "reporter_slack_id"
        case storyPoints = "story_points"
        case dueDate = "due_date"
        case sprintId = "sprint_id"
        case sprintName = "sprint_name"
        case epicKey = "epic_key"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case resolvedAt = "resolved_at"
        case fixVersions = "fix_versions"
        case rawJson = "raw_json"
        case syncedAt = "synced_at"
        case isDeleted = "is_deleted"
    }

    var decodedFixVersions: [String] {
        guard let data = fixVersions.data(using: .utf8),
              let versions = try? JSONDecoder().decode([String].self, from: data) else { return [] }
        return versions
    }
}
