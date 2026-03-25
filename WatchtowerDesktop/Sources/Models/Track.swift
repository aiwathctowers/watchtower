import Foundation
import GRDB

// MARK: - Track v3 Supporting Types

struct TrackParticipant: Decodable, Identifiable, Equatable {
    let id = UUID()
    let userID: String?
    let name: String
    let role: String?

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case name, role
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.userID == rhs.userID && lhs.name == rhs.name && lhs.role == rhs.role
    }
}

struct TrackTimelineEvent: Decodable, Identifiable, Equatable {
    let id = UUID()
    let date: String
    let event: String
    let channel: String?

    enum CodingKeys: String, CodingKey {
        case date, event, channel
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.date == rhs.date && lhs.event == rhs.event && lhs.channel == rhs.channel
    }
}

struct TrackKeyMessage: Decodable, Identifiable, Equatable {
    let id = UUID()
    let ts: String
    let author: String
    let text: String
    let channel: String?

    enum CodingKeys: String, CodingKey {
        case ts, author, text, channel
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.ts == rhs.ts && lhs.author == rhs.author && lhs.text == rhs.text
    }

    var date: Date? {
        let parts = ts.split(separator: ".", maxSplits: 1)
        guard let epoch = Double(parts.first ?? "") else { return nil }
        return Date(timeIntervalSince1970: epoch)
    }
}

struct TrackSourceRef: Decodable, Identifiable, Equatable {
    let digestID: Int
    let topicID: Int?
    let channelID: String?
    let timestamp: Double?

    var id: String { "\(digestID)-\(topicID ?? 0)" }

    enum CodingKeys: String, CodingKey {
        case digestID = "digest_id"
        case topicID = "topic_id"
        case channelID = "channel_id"
        case timestamp
    }
}

// MARK: - Track

struct Track: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let title: String
    let narrative: String
    let currentStatus: String
    let participants: String
    let timeline: String
    let keyMessages: String
    let priority: String
    let tags: String
    let channelIDs: String
    let sourceRefs: String
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
        title = row["title"] ?? ""
        narrative = row["narrative"] ?? ""
        currentStatus = row["current_status"] ?? ""
        participants = row["participants"] ?? "[]"
        timeline = row["timeline"] ?? "[]"
        keyMessages = row["key_messages"] ?? "[]"
        priority = row["priority"] ?? "medium"
        tags = row["tags"] ?? "[]"
        channelIDs = row["channel_ids"] ?? "[]"
        sourceRefs = row["source_refs"] ?? "[]"
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

    // MARK: - Predicates

    var isRead: Bool { readAt != nil && !hasUpdates }
    var isUnread: Bool { !isRead }

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

    // MARK: - JSON decoders

    var decodedParticipants: [TrackParticipant] {
        guard !participants.isEmpty, participants != "[]",
              let data = participants.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackParticipant].self, from: data)) ?? []
    }

    var decodedTimeline: [TrackTimelineEvent] {
        guard !timeline.isEmpty, timeline != "[]",
              let data = timeline.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackTimelineEvent].self, from: data)) ?? []
    }

    var decodedKeyMessages: [TrackKeyMessage] {
        guard !keyMessages.isEmpty, keyMessages != "[]",
              let data = keyMessages.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackKeyMessage].self, from: data)) ?? []
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

    var decodedSourceRefs: [TrackSourceRef] {
        guard !sourceRefs.isEmpty, sourceRefs != "[]",
              let data = sourceRefs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSourceRef].self, from: data)) ?? []
    }

    /// Unique digest IDs from source_refs for cascade read.
    var linkedDigestIDs: [Int] {
        Array(Set(decodedSourceRefs.map(\.digestID).filter { $0 > 0 }))
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
