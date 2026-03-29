import Foundation
import GRDB

// MARK: - Track v2 Supporting Types

struct TrackParticipant: Codable, Identifiable, Equatable {
    let id = UUID()
    let name: String
    let userID: String?
    let stance: String?

    enum CodingKeys: String, CodingKey {
        case name
        case userID = "user_id"
        case stance
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.name == rhs.name && lhs.userID == rhs.userID && lhs.stance == rhs.stance
    }
}

struct TrackSourceRef: Codable, Identifiable, Equatable {
    let ts: String
    let author: String
    let text: String

    var id: String { "\(ts)-\(author)" }
}

struct TrackDecisionOption: Codable, Identifiable, Equatable {
    let option: String
    let supporters: [String]
    let pros: String
    let cons: String

    var id: String { option }

    enum CodingKeys: String, CodingKey {
        case option, supporters, pros, cons
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        option = try container.decodeIfPresent(String.self, forKey: .option) ?? ""
        supporters = try container.decodeIfPresent([String].self, forKey: .supporters) ?? []
        pros = try container.decodeIfPresent(String.self, forKey: .pros) ?? ""
        cons = try container.decodeIfPresent(String.self, forKey: .cons) ?? ""
    }
}

struct TrackSubItem: Codable, Identifiable, Equatable {
    let text: String
    var status: String // "open" or "done"

    var id: String { text }
    var isDone: Bool { status == "done" }
}

// MARK: - Track

struct Track: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let assigneeUserID: String
    let text: String
    let context: String
    let category: String
    let ownership: String
    let ballOn: String
    let ownerUserID: String
    let requesterName: String
    let requesterUserID: String
    let blocking: String
    let decisionSummary: String
    let decisionOptions: String
    let subItems: String
    let participants: String
    let sourceRefs: String
    let tags: String
    let channelIDs: String
    let relatedDigestIDs: String
    let priority: String
    let dueDate: Double?
    let fingerprint: String
    let readAt: String?
    let hasUpdates: Bool
    let model: String
    let inputTokens: Int
    let outputTokens: Int
    let costUSD: Double
    let promptVersion: Int
    let createdAt: String
    let updatedAt: String

    init(row: Row) {
        id = row["id"]
        assigneeUserID = row["assignee_user_id"] ?? ""
        text = row["text"] ?? ""
        context = row["context"] ?? ""
        category = row["category"] ?? "task"
        ownership = row["ownership"] ?? "mine"
        ballOn = row["ball_on"] ?? ""
        ownerUserID = row["owner_user_id"] ?? ""
        requesterName = row["requester_name"] ?? ""
        requesterUserID = row["requester_user_id"] ?? ""
        blocking = row["blocking"] ?? ""
        decisionSummary = row["decision_summary"] ?? ""
        decisionOptions = row["decision_options"] ?? "[]"
        subItems = row["sub_items"] ?? "[]"
        participants = row["participants"] ?? "[]"
        sourceRefs = row["source_refs"] ?? "[]"
        tags = row["tags"] ?? "[]"
        channelIDs = row["channel_ids"] ?? "[]"
        relatedDigestIDs = row["related_digest_ids"] ?? "[]"
        priority = row["priority"] ?? "medium"
        dueDate = row["due_date"]
        fingerprint = row["fingerprint"] ?? "[]"
        readAt = row["read_at"]
        hasUpdates = row["has_updates"] ?? false
        model = row["model"] ?? ""
        inputTokens = row["input_tokens"] ?? 0
        outputTokens = row["output_tokens"] ?? 0
        costUSD = row["cost_usd"] ?? 0
        promptVersion = row["prompt_version"] ?? 0
        createdAt = row["created_at"] ?? ""
        updatedAt = row["updated_at"] ?? ""
    }

    // MARK: - Ownership predicates

    var isMine: Bool { ownership == "mine" }
    var isDelegated: Bool { ownership == "delegated" }
    var isWatching: Bool { ownership == "watching" }

    // MARK: - Read predicates

    var isRead: Bool { readAt != nil && !hasUpdates }
    var isUnread: Bool { !isRead }

    // MARK: - Labels

    var categoryLabel: String {
        switch category {
        case "task": return "Task"
        case "decision": return "Decision"
        case "risk": return "Risk"
        case "blocker": return "Blocker"
        case "fyi": return "FYI"
        case "question": return "Question"
        case "project": return "Project"
        default: return category.capitalized
        }
    }

    var ownershipLabel: String {
        switch ownership {
        case "mine": return "Mine"
        case "delegated": return "Delegated"
        case "watching": return "Watching"
        default: return ownership.capitalized
        }
    }

    // MARK: - Date helpers

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

    var createdDate: Date {
        if let date = Self.iso8601WithFractional.date(from: createdAt) { return date }
        return Self.iso8601Standard.date(from: createdAt) ?? Date()
    }

    var updatedDate: Date {
        if let date = Self.iso8601WithFractional.date(from: updatedAt) { return date }
        return Self.iso8601Standard.date(from: updatedAt) ?? Date()
    }

    var updatedAgo: String {
        let interval = Date().timeIntervalSince(updatedDate)
        if interval < 60 { return "just now" }
        if interval < 3600 { return "\(Int(interval / 60))m ago" }
        if interval < 86400 { return "\(Int(interval / 3600))h ago" }
        let days = Int(interval / 86400)
        return days == 1 ? "1d ago" : "\(days)d ago"
    }

    var dueDateFormatted: String? {
        guard let dueDate else { return nil }
        let date = Date(timeIntervalSince1970: dueDate)
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: date)
    }

    var isOverdue: Bool {
        guard let dueDate else { return false }
        return Date(timeIntervalSince1970: dueDate) < Date()
    }

    // MARK: - JSON decoders

    var decodedParticipants: [TrackParticipant] {
        guard !participants.isEmpty, participants != "[]",
              let data = participants.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackParticipant].self, from: data)) ?? []
    }

    var decodedSourceRefs: [TrackSourceRef] {
        guard !sourceRefs.isEmpty, sourceRefs != "[]",
              let data = sourceRefs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSourceRef].self, from: data)) ?? []
    }

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedChannelIDs: [String] {
        guard !channelIDs.isEmpty, channelIDs != "[]",
              let data = channelIDs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedRelatedDigestIDs: [Int] {
        guard !relatedDigestIDs.isEmpty, relatedDigestIDs != "[]",
              let data = relatedDigestIDs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([Int].self, from: data)) ?? []
    }

    var decodedDecisionOptions: [TrackDecisionOption] {
        guard !decisionOptions.isEmpty, decisionOptions != "[]",
              let data = decisionOptions.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackDecisionOption].self, from: data)) ?? []
    }

    var decodedSubItems: [TrackSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSubItem].self, from: data)) ?? []
    }

    var subItemsProgress: (done: Int, total: Int) {
        let items = decodedSubItems
        let done = items.filter(\.isDone).count
        return (done, items.count)
    }

    // MARK: - Priority helpers

    var priorityOrder: Int {
        switch priority {
        case "high": return 0
        case "medium": return 1
        case "low": return 2
        default: return 1
        }
    }
}
