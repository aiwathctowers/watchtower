import Foundation
import GRDB

struct InboxFeedback: Codable, FetchableRecord, PersistableRecord, Identifiable, Equatable {
    static let databaseTableName = "inbox_feedback"

    var id: Int64?
    var inboxItemId: Int64
    var rating: Int
    var reason: String
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case inboxItemId = "inbox_item_id"
        case rating
        case reason
        case createdAt = "created_at"
    }
}
