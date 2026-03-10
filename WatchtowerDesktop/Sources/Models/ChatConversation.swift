import Foundation
import GRDB

struct ChatConversation: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int64
    let title: String
    let sessionID: String?
    let contextType: String?
    let contextID: String?
    let createdAt: Double
    let updatedAt: Double

    enum CodingKeys: String, CodingKey {
        case id
        case title
        case sessionID = "session_id"
        case contextType = "context_type"
        case contextID = "context_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var createdDate: Date {
        Date(timeIntervalSince1970: createdAt)
    }

    var updatedDate: Date {
        Date(timeIntervalSince1970: updatedAt)
    }

    var displayTitle: String {
        title.isEmpty ? "New Chat" : title
    }
}
