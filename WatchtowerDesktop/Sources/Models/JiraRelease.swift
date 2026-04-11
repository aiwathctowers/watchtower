import GRDB

struct JiraRelease: Codable, FetchableRecord, TableRecord, Identifiable {
    static let databaseTableName = "jira_releases"

    let id: Int
    let projectKey: String
    let name: String
    let description: String
    let releaseDate: String
    let released: Bool
    let archived: Bool
    let syncedAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case projectKey = "project_key"
        case name
        case description
        case releaseDate = "release_date"
        case released
        case archived
        case syncedAt = "synced_at"
    }
}
