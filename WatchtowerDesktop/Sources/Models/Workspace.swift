import GRDB

struct Workspace: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: String
    let name: String
    let domain: String
    let syncedAt: String?
    let searchLastDate: String
    let currentUserID: String

    enum CodingKeys: String, CodingKey {
        case id, name, domain
        case syncedAt = "synced_at"
        case searchLastDate = "search_last_date"
        case currentUserID = "current_user_id"
    }
}
