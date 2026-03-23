import GRDB

struct User: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: String
    let name: String
    let displayName: String
    let realName: String
    let email: String
    let isBot: Bool
    let isDeleted: Bool
    let profileJSON: String
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id, name, email
        case displayName = "display_name"
        case realName = "real_name"
        case isBot = "is_bot"
        case isDeleted = "is_deleted"
        case profileJSON = "profile_json"
        case updatedAt = "updated_at"
    }

    var bestName: String {
        if !displayName.isEmpty { return displayName }
        if !realName.isEmpty { return realName }
        if !name.isEmpty { return name }
        return id
    }
}
