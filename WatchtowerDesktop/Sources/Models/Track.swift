import Foundation
import GRDB

// MARK: - Participant & SourceRef

struct TrackParticipant: Decodable, Identifiable, Equatable {
    let id = UUID()
    let name: String
    let userID: String?
    let stance: String?

    enum CodingKeys: String, CodingKey {
        case name
        case userID = "user_id"
        case stance
    }

    static func == (lhs: TrackParticipant, rhs: TrackParticipant) -> Bool {
        lhs.name == rhs.name && lhs.userID == rhs.userID && lhs.stance == rhs.stance
    }
}

struct TrackSourceRef: Decodable, Identifiable, Equatable {
    var id: String { ts }
    let ts: String
    let author: String
    let text: String
}

struct TrackDecisionOption: Decodable, Identifiable, Equatable {
    let id = UUID()
    let option: String
    let supporters: [String]?
    let pros: String?
    let cons: String?

    enum CodingKeys: String, CodingKey {
        case option, supporters, pros, cons
    }

    static func == (lhs: TrackDecisionOption, rhs: TrackDecisionOption) -> Bool {
        lhs.option == rhs.option && lhs.supporters == rhs.supporters && lhs.pros == rhs.pros && lhs.cons == rhs.cons
    }
}

struct TrackSubItem: Codable, Identifiable, Equatable {
    let id: UUID

    let text: String
    var status: String // "open" or "done"

    enum CodingKeys: String, CodingKey {
        case text, status
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = UUID()
        self.text = try container.decode(String.self, forKey: .text)
        self.status = try container.decode(String.self, forKey: .status)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(text, forKey: .text)
        try container.encode(status, forKey: .status)
    }

    var isDone: Bool { status == "done" }

    static func == (lhs: TrackSubItem, rhs: TrackSubItem) -> Bool {
        lhs.text == rhs.text && lhs.status == rhs.status
    }
}

// MARK: - Track

struct Track: FetchableRecord, Identifiable, Equatable {
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
    let promptVersion: Int
    let ownership: String       // "mine", "delegated", "watching"
    let ballOn: String          // user_id of person who needs to act next
    let ownerUserID: String     // owner of the track

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
        promptVersion = row["prompt_version"] ?? 0
        ownership = row["ownership"] ?? "mine"
        ballOn = row["ball_on"] ?? ""
        ownerUserID = row["owner_user_id"] ?? ""
    }

    var isInbox: Bool { status == "inbox" }
    var isActive: Bool { status == "active" }
    var isDone: Bool { status == "done" }
    var isDismissed: Bool { status == "dismissed" }
    var isSnoozed: Bool { status == "snoozed" }
    var isMine: Bool { ownership == "mine" }
    var isDelegated: Bool { ownership == "delegated" }
    var isWatching: Bool { ownership == "watching" }

    var ownershipLabel: String {
        switch ownership {
        case "delegated": return "Delegated"
        case "watching": return "Watching"
        default: return "Mine"
        }
    }

    var snoozeUntilDate: Date? {
        guard let ts = snoozeUntil, ts > 0 else { return nil }
        return Date(timeIntervalSince1970: ts)
    }

    private static let mediumDateTimeFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .short
        return fmt
    }()

    private static let mediumDateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateStyle = .medium
        fmt.timeStyle = .none
        return fmt
    }()

    private static let iso8601WithFractional: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static let iso8601Standard: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    var snoozeUntilFormatted: String? {
        guard let date = snoozeUntilDate else { return nil }
        return Self.mediumDateTimeFormatter.string(from: date)
    }

    var dueDateFormatted: String? {
        guard let ts = dueDate, ts > 0 else { return nil }
        let date = Date(timeIntervalSince1970: ts)
        return Self.mediumDateFormatter.string(from: date)
    }

    var createdDate: Date {
        if let d = Self.iso8601WithFractional.date(from: createdAt) { return d }
        return Self.iso8601Standard.date(from: createdAt) ?? Date()
    }

    var isOverdue: Bool {
        guard let ts = dueDate, ts > 0, isInbox || isActive else { return false }
        return Date(timeIntervalSince1970: ts) < Date()
    }

    var decodedParticipants: [TrackParticipant] {
        guard !participants.isEmpty,
              let data = participants.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackParticipant].self, from: data)) ?? []
    }

    var decodedSourceRefs: [TrackSourceRef] {
        guard !sourceRefs.isEmpty,
              let data = sourceRefs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSourceRef].self, from: data)) ?? []
    }

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedDecisionOptions: [TrackDecisionOption] {
        guard !decisionOptions.isEmpty, decisionOptions != "[]",
              let data = decisionOptions.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackDecisionOption].self, from: data)) ?? []
    }

    var decodedRelatedDigestIDs: [Int] {
        guard !relatedDigestIDs.isEmpty, relatedDigestIDs != "[]",
              let data = relatedDigestIDs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([Int].self, from: data)) ?? []
    }

    var decodedSubItems: [TrackSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSubItem].self, from: data)) ?? []
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
