import Foundation
import GRDB

struct InboxLearnedRule: Codable, FetchableRecord, PersistableRecord, Identifiable, Equatable {
    static let databaseTableName = "inbox_learned_rules"

    var id: Int64?
    var ruleType: String
    var scopeKey: String
    var weight: Double
    var source: String
    var evidenceCount: Int
    var lastUpdated: String

    enum CodingKeys: String, CodingKey {
        case id
        case ruleType = "rule_type"
        case scopeKey = "scope_key"
        case weight
        case source
        case evidenceCount = "evidence_count"
        case lastUpdated = "last_updated"
    }
}
