import Foundation

/// Lightweight track parsed from a digest's inline JSON (not the full DB model).
struct DigestTrack: Codable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let assignee: String?
    let status: String?  // "open", "done", etc.

    enum CodingKeys: String, CodingKey {
        case text, assignee, status
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.assignee == rhs.assignee && lhs.status == rhs.status
    }
}
