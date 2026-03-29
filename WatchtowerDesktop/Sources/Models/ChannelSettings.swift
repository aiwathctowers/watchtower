import GRDB

struct ChannelSettings: Codable, FetchableRecord, PersistableRecord {
    static let databaseTableName = "channel_settings"

    let channelID: String
    var isMutedForLLM: Bool
    var isFavorite: Bool

    enum CodingKeys: String, CodingKey {
        case channelID = "channel_id"
        case isMutedForLLM = "is_muted_for_llm"
        case isFavorite = "is_favorite"
    }
}
