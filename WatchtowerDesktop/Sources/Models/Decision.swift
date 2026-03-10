import Foundation

struct Decision: Codable, Identifiable, Equatable {
    let text: String
    let by: String?
    let messageTS: String?
    let importance: String?  // "high", "medium", "low" — nil defaults to "medium"

    // M2: stable ID using hash to avoid collisions from underscore separator
    var id: Int { var hasher = Hasher(); hasher.combine(text); hasher.combine(by); hasher.combine(messageTS); return hasher.finalize() }

    /// Resolved importance level (defaults to "medium" for old digests without this field).
    var resolvedImportance: String { importance ?? "medium" }

    enum CodingKeys: String, CodingKey {
        case text, by, importance
        case messageTS = "message_ts"
    }
}
