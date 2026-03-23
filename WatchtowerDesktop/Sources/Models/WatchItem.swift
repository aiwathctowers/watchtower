import GRDB

struct WatchItem: FetchableRecord, Decodable, Identifiable, Equatable {
    let entityType: String
    let entityID: String
    let entityName: String?
    let priority: String
    let createdAt: String?

    var id: String { "\(entityType)_\(entityID)" }

    enum CodingKeys: String, CodingKey {
        case priority
        case entityType = "entity_type"
        case entityID = "entity_id"
        case entityName = "entity_name"
        case createdAt = "created_at"
    }
}
