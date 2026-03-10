import Foundation
import GRDB

struct ActionItemHistoryEntry: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let actionItemID: Int
    let event: String       // "created", "status_changed", "priority_changed", "reopened", etc.
    let field: String
    let oldValue: String
    let newValue: String
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, event, field
        case actionItemID = "action_item_id"
        case oldValue = "old_value"
        case newValue = "new_value"
        case createdAt = "created_at"
    }

    var createdDate: Date {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt.date(from: createdAt) ?? Date()
    }

    var displayText: String {
        switch event {
        case "created":
            return "Created"
        case "status_changed":
            return "Status: \(oldValue) → \(newValue)"
        case "reopened":
            return "Reopened"
        case "priority_changed":
            return "Priority: \(oldValue) → \(newValue)"
        case "context_updated":
            return "Context updated"
        case "due_date_changed":
            return "Due date changed"
        default:
            return event.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }
}
