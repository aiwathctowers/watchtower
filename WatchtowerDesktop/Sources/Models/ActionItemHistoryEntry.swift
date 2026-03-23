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
            if newValue == "from_digest" {
                return "Created from digest"
            }
            return "Created"
        case "status_changed":
            return "Status: \(oldValue) → \(newValue)"
        case "accepted":
            return "Accepted — moved to active"
        case "reopened":
            return "Reopened — moved back to inbox"
        case "snoozed":
            if !newValue.isEmpty, newValue != "snoozed" {
                return "Snoozed \(newValue)"
            }
            return "Snoozed"
        case "reactivated":
            return "Reactivated — snooze expired"
        case "priority_changed":
            return "Priority: \(oldValue) → \(newValue)"
        case "context_updated":
            return "Context updated"
        case "due_date_changed":
            if oldValue.isEmpty {
                return "Due date set"
            }
            return "Due date changed"
        case "re_extracted":
            return "Re-extracted — new data from Slack"
        case "decision_evolved":
            return "Decision updated"
        case "digest_linked":
            if !newValue.isEmpty {
                return "Digest \(newValue)"
            }
            return "Linked to digest"
        case "sub_items_updated":
            if !newValue.isEmpty {
                return "Checklist: \(newValue)"
            }
            return "Checklist updated"
        case "update_detected":
            return "New activity in thread"
        case "update_read":
            return "Update marked as read"
        default:
            return event.replacingOccurrences(of: "_", with: " ").capitalized
        }
    }

    /// Additional detail showing what specifically changed.
    var detailText: String? {
        switch event {
        case "context_updated":
            return nonEmpty(newValue)
        case "re_extracted":
            return nonEmpty(newValue)
        case "decision_evolved":
            return nonEmpty(newValue)
        case "due_date_changed":
            // old format: Unix timestamps as strings; new format: human-readable
            if !oldValue.isEmpty, !newValue.isEmpty {
                return "\(oldValue) → \(newValue)"
            }
            return nonEmpty(newValue)
        default:
            return nil
        }
    }

    private func nonEmpty(_ s: String) -> String? {
        s.isEmpty ? nil : s
    }
}
