import Foundation

/// Lightweight action item parsed from a digest's inline JSON (not the full DB model).
struct DigestActionItem: Codable, Identifiable, Equatable {
    let text: String
    let assignee: String?
    let status: String?  // "open", "done", etc.

    var id: Int { var hasher = Hasher(); hasher.combine(text); hasher.combine(assignee); return hasher.finalize() }
}
