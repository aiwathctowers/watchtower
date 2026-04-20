import GRDB

struct JiraSlackLink: Codable, FetchableRecord, TableRecord, Identifiable {
    static let databaseTableName = "jira_slack_links"

    let id: Int
    var issueKey: String
    var channelId: String
    var messageTs: String
    var trackId: Int?
    var digestId: Int?
    var linkType: String
    var detectedAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case issueKey = "issue_key"
        case channelId = "channel_id"
        case messageTs = "message_ts"
        case trackId = "track_id"
        case digestId = "digest_id"
        case linkType = "link_type"
        case detectedAt = "detected_at"
    }
}
