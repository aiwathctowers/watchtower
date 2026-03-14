import GRDB
import Foundation

struct Feedback: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let entityType: String  // "digest", "track", "decision"
    let entityID: String
    let rating: Int         // +1 = good, -1 = bad
    let comment: String
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, rating, comment
        case entityType = "entity_type"
        case entityID = "entity_id"
        case createdAt = "created_at"
    }

    var isPositive: Bool { rating > 0 }
}

struct FeedbackStats: Equatable {
    let entityType: String
    let positive: Int
    let negative: Int
    let total: Int

    var positivePercent: Int {
        total > 0 ? positive * 100 / total : 0
    }
}
