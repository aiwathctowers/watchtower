import Foundation
import GRDB

struct MeetingRecap: Codable, FetchableRecord, PersistableRecord {
    static let databaseTableName = "meeting_recaps"

    let eventID: String
    let sourceText: String
    let recapJSON: String
    let createdAt: String
    let updatedAt: String

    struct Content: Decodable, Equatable {
        let summary: String
        let keyDecisions: [String]
        let actionItems: [String]
        let openQuestions: [String]

        enum CodingKeys: String, CodingKey {
            case summary
            case keyDecisions = "key_decisions"
            case actionItems = "action_items"
            case openQuestions = "open_questions"
        }
    }

    var parsed: Content? {
        guard let data = recapJSON.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(Content.self, from: data)
    }

    enum CodingKeys: String, CodingKey {
        case eventID = "event_id"
        case sourceText = "source_text"
        case recapJSON = "recap_json"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}
