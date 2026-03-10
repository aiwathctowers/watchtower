import Foundation
import GRDB

// MARK: - Participant & SourceRef

struct ActionItemParticipant: Decodable, Identifiable, Equatable {
    var id: String { name + (userID ?? "") }
    let name: String
    let userID: String?
    let stance: String?

    enum CodingKeys: String, CodingKey {
        case name
        case userID = "user_id"
        case stance
    }
}

struct ActionItemSourceRef: Decodable, Identifiable, Equatable {
    var id: String { ts }
    let ts: String
    let author: String
    let text: String
}

struct ActionItemDecisionOption: Decodable, Identifiable, Equatable {
    var id: String { option }
    let option: String
    let supporters: [String]?
    let pros: String?
    let cons: String?
}

struct ActionItemSubItem: Codable, Identifiable, Equatable {
    var id: String { text }
    let text: String
    var status: String // "open" or "done"

    var isDone: Bool { status == "done" }
}

// MARK: - ActionItem

struct ActionItem: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let channelID: String
    let assigneeUserID: String
    let assigneeRaw: String
    let text: String
    let context: String
    let sourceMessageTS: String
    let sourceChannelName: String
    let status: String       // "inbox", "active", "done", "dismissed", "snoozed"
    let priority: String     // "high", "medium", "low"
    let dueDate: Double?
    let hasUpdates: Bool
    let lastCheckedTS: String
    let snoozeUntil: Double?
    let preSnoozeStatus: String
    let periodFrom: Double
    let periodTo: Double
    let model: String
    let inputTokens: Int
    let outputTokens: Int
    let costUSD: Double
    let createdAt: String
    let completedAt: String?
    let participants: String
    let sourceRefs: String
    let requesterName: String
    let requesterUserID: String
    let category: String
    let blocking: String
    let tags: String
    let decisionSummary: String
    let decisionOptions: String
    let relatedDigestIDs: String
    let subItems: String

    init(row: Row) {
        id = row["id"]
        channelID = row["channel_id"]
        assigneeUserID = row["assignee_user_id"]
        assigneeRaw = row["assignee_raw"] ?? ""
        text = row["text"]
        context = row["context"] ?? ""
        sourceMessageTS = row["source_message_ts"] ?? ""
        sourceChannelName = row["source_channel_name"] ?? ""
        status = row["status"] ?? "inbox"
        priority = row["priority"] ?? "medium"
        dueDate = row["due_date"]
        hasUpdates = row["has_updates"] ?? false
        lastCheckedTS = row["last_checked_ts"] ?? ""
        snoozeUntil = row["snooze_until"]
        preSnoozeStatus = row["pre_snooze_status"] ?? ""
        periodFrom = row["period_from"]
        periodTo = row["period_to"]
        model = row["model"] ?? ""
        inputTokens = row["input_tokens"] ?? 0
        outputTokens = row["output_tokens"] ?? 0
        costUSD = row["cost_usd"] ?? 0
        createdAt = row["created_at"] ?? ""
        completedAt = row["completed_at"]
        participants = row["participants"] ?? ""
        sourceRefs = row["source_refs"] ?? ""
        requesterName = row["requester_name"] ?? ""
        requesterUserID = row["requester_user_id"] ?? ""
        category = row["category"] ?? ""
        blocking = row["blocking"] ?? ""
        tags = row["tags"] ?? ""
        decisionSummary = row["decision_summary"] ?? ""
        decisionOptions = row["decision_options"] ?? ""
        relatedDigestIDs = row["related_digest_ids"] ?? ""
        subItems = row["sub_items"] ?? ""
    }

    var isInbox: Bool { status == "inbox" }
    var isActive: Bool { status == "active" }
    var isDone: Bool { status == "done" }
    var isDismissed: Bool { status == "dismissed" }
    var isSnoozed: Bool { status == "snoozed" }

    var snoozeUntilDate: Date? {
        guard let ts = snoozeUntil, ts > 0 else { return nil }
        return Date(timeIntervalSince1970: ts)
    }

    var snoozeUntilFormatted: String? {
        guard let date = snoozeUntilDate else { return nil }
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .short
        return fmt.string(from: date)
    }

    var dueDateFormatted: String? {
        guard let ts = dueDate, ts > 0 else { return nil }
        let date = Date(timeIntervalSince1970: ts)
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .none
        return fmt.string(from: date)
    }

    var createdDate: Date {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = fmt.date(from: createdAt) { return d }
        fmt.formatOptions = [.withInternetDateTime]
        return fmt.date(from: createdAt) ?? Date()
    }

    var isOverdue: Bool {
        guard let ts = dueDate, ts > 0, isInbox || isActive else { return false }
        return Date(timeIntervalSince1970: ts) < Date()
    }

    var decodedParticipants: [ActionItemParticipant] {
        guard !participants.isEmpty,
              let data = participants.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([ActionItemParticipant].self, from: data)) ?? []
    }

    var decodedSourceRefs: [ActionItemSourceRef] {
        guard !sourceRefs.isEmpty,
              let data = sourceRefs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([ActionItemSourceRef].self, from: data)) ?? []
    }

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedDecisionOptions: [ActionItemDecisionOption] {
        guard !decisionOptions.isEmpty, decisionOptions != "[]",
              let data = decisionOptions.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([ActionItemDecisionOption].self, from: data)) ?? []
    }

    var decodedRelatedDigestIDs: [Int] {
        guard !relatedDigestIDs.isEmpty, relatedDigestIDs != "[]",
              let data = relatedDigestIDs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([Int].self, from: data)) ?? []
    }

    var decodedSubItems: [ActionItemSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([ActionItemSubItem].self, from: data)) ?? []
    }

    var subItemsProgress: (done: Int, total: Int) {
        let items = decodedSubItems
        let done = items.filter { $0.isDone }.count
        return (done, items.count)
    }

    var categoryLabel: String {
        switch category {
        case "code_review": return "Review"
        case "decision_needed": return "Decision"
        case "info_request": return "Info"
        case "task": return "Task"
        case "approval": return "Approval"
        case "follow_up": return "Follow-up"
        case "bug_fix": return "Bug"
        case "discussion": return "Discussion"
        default: return ""
        }
    }
}
