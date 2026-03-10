import GRDB

struct Channel: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: String
    let name: String
    let type: String
    let topic: String
    let purpose: String
    let isArchived: Bool
    let isMember: Bool
    let dmUserID: String?
    let numMembers: Int
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id, name, type, topic, purpose
        case isArchived = "is_archived"
        case isMember = "is_member"
        case dmUserID = "dm_user_id"
        case numMembers = "num_members"
        case updatedAt = "updated_at"
    }
}
