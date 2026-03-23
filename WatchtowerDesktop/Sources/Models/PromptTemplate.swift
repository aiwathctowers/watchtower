import GRDB
import Foundation

struct PromptTemplate: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: String    // "digest.channel", "actionitems.extract", etc.
    let template: String
    let version: Int
    let language: String
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id, template, version, language
        case updatedAt = "updated_at"
    }
}

struct PromptHistoryEntry: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let promptID: String
    let version: Int
    let template: String
    let reason: String
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, version, template, reason
        case promptID = "prompt_id"
        case createdAt = "created_at"
    }
}
