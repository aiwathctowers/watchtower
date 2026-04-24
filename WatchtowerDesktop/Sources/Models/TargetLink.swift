import Foundation
import GRDB

// MARK: - TargetLink

struct TargetLink: FetchableRecord, TableRecord, Codable, Identifiable, Equatable, Hashable {
    static var databaseTableName = "target_links"

    let id: Int
    let sourceTargetId: Int
    let targetTargetId: Int?    // nullable — link can be to an external ref instead
    let externalRef: String     // e.g. 'jira:PROJ-123', 'slack:C123:1714567890.123456'
    let relation: String        // "contributes_to", "blocks", "related", "duplicates"
    let confidence: Double?     // AI-assigned, nil if user-created
    let createdBy: String       // "ai", "user"
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case sourceTargetId  = "source_target_id"
        case targetTargetId  = "target_target_id"
        case externalRef     = "external_ref"
        case relation
        case confidence
        case createdBy       = "created_by"
        case createdAt       = "created_at"
    }

    init(row: Row) {
        id             = row["id"]
        sourceTargetId = row["source_target_id"]
        targetTargetId = row["target_target_id"]
        externalRef    = row["external_ref"] ?? ""
        relation       = row["relation"] ?? ""
        confidence     = row["confidence"]
        createdBy      = row["created_by"] ?? "ai"
        createdAt      = row["created_at"] ?? ""
    }

    // MARK: - Hashable

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.id == rhs.id
    }

    // MARK: - Helpers

    var isAICreated: Bool { createdBy == "ai" }

    var isExternalLink: Bool { !externalRef.isEmpty }
}
