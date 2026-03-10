import GRDB

struct Message: FetchableRecord, Decodable, Identifiable, Equatable {
    let channelID: String
    let ts: String
    let userID: String
    let text: String
    let threadTS: String?
    let replyCount: Int
    let isEdited: Bool
    let isDeleted: Bool
    let subtype: String
    let permalink: String
    let tsUnix: Double
    let rawJSON: String

    var id: String { "\(channelID)_\(ts)" }

    enum CodingKeys: String, CodingKey {
        case ts, text, subtype, permalink
        case channelID = "channel_id"
        case userID = "user_id"
        case threadTS = "thread_ts"
        case replyCount = "reply_count"
        case isEdited = "is_edited"
        case isDeleted = "is_deleted"
        case tsUnix = "ts_unix"
        case rawJSON = "raw_json"
    }
}
