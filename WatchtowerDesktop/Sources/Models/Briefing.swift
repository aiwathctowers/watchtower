import Foundation
import GRDB

// MARK: - Section Item Types

struct AttentionItem: Decodable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let sourceType: String?
    let sourceID: String?
    let priority: String?
    let reason: String?
    let suggestTrack: Bool? // swiftlint:disable:this discouraged_optional_boolean
    let suggestTask: Bool? // swiftlint:disable:this discouraged_optional_boolean

    enum CodingKeys: String, CodingKey {
        case text
        case sourceType = "source_type"
        case sourceID = "source_id"
        case priority, reason
        case suggestTrack = "suggest_track"
        case suggestTask = "suggest_task"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        text = try container.decode(String.self, forKey: .text)
        sourceType = try container.decodeIfPresent(String.self, forKey: .sourceType)
        priority = try container.decodeIfPresent(String.self, forKey: .priority)
        reason = try container.decodeIfPresent(String.self, forKey: .reason)
        suggestTrack = try container.decodeIfPresent(Bool.self, forKey: .suggestTrack)
        suggestTask = try container.decodeIfPresent(Bool.self, forKey: .suggestTask)
        // Accept both string and int for source_id
        if let str = try? container.decodeIfPresent(String.self, forKey: .sourceID) {
            sourceID = str
        } else if let num = try? container.decodeIfPresent(Int.self, forKey: .sourceID) {
            sourceID = String(num)
        } else {
            sourceID = nil
        }
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.sourceType == rhs.sourceType
            && lhs.sourceID == rhs.sourceID && lhs.priority == rhs.priority
            && lhs.reason == rhs.reason && lhs.suggestTrack == rhs.suggestTrack
            && lhs.suggestTask == rhs.suggestTask
    }
}

struct YourDayItem: Decodable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let trackID: Int?
    let taskID: Int?
    let dueDate: String?
    let priority: String?
    let status: String?
    let ownership: String?

    enum CodingKeys: String, CodingKey {
        case text
        case trackID = "track_id"
        case taskID = "task_id"
        case dueDate = "due_date"
        case priority, status, ownership
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.trackID == rhs.trackID
            && lhs.taskID == rhs.taskID
            && lhs.dueDate == rhs.dueDate && lhs.priority == rhs.priority
            && lhs.status == rhs.status && lhs.ownership == rhs.ownership
    }
}

struct WhatHappenedItem: Decodable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let digestID: Int?
    let channelName: String?
    let itemType: String?
    let importance: String?

    enum CodingKeys: String, CodingKey {
        case text
        case digestID = "digest_id"
        case channelName = "channel_name"
        case itemType = "item_type"
        case importance
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.digestID == rhs.digestID
            && lhs.channelName == rhs.channelName && lhs.itemType == rhs.itemType
            && lhs.importance == rhs.importance
    }
}

struct TeamPulseItem: Decodable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let userID: String?
    let signalType: String?
    let detail: String?

    enum CodingKeys: String, CodingKey {
        case text
        case userID = "user_id"
        case signalType = "signal_type"
        case detail
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.userID == rhs.userID
            && lhs.signalType == rhs.signalType && lhs.detail == rhs.detail
    }
}

struct CoachingItem: Decodable, Identifiable, Equatable {
    let id = UUID()
    let text: String
    let relatedUserID: String?
    let category: String?

    enum CodingKeys: String, CodingKey {
        case text
        case relatedUserID = "related_user_id"
        case category
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.relatedUserID == rhs.relatedUserID
            && lhs.category == rhs.category
    }
}

// MARK: - Briefing

struct Briefing: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let userID: String
    let date: String
    let role: String
    let attention: String
    let yourDay: String
    let whatHappened: String
    let teamPulse: String
    let coaching: String
    let model: String
    let inputTokens: Int
    let outputTokens: Int
    let costUSD: Double
    let promptVersion: Int
    let readAt: String?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, date, role, attention, coaching, model
        case userID = "user_id"
        case yourDay = "your_day"
        case whatHappened = "what_happened"
        case teamPulse = "team_pulse"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case promptVersion = "prompt_version"
        case readAt = "read_at"
        case createdAt = "created_at"
    }

    var isRead: Bool { readAt != nil }

    private static let decoder = JSONDecoder()

    var parsedAttention: [AttentionItem] {
        guard let data = attention.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([AttentionItem].self, from: data)) ?? []
    }

    var parsedYourDay: [YourDayItem] {
        guard let data = yourDay.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([YourDayItem].self, from: data)) ?? []
    }

    var parsedWhatHappened: [WhatHappenedItem] {
        guard let data = whatHappened.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([WhatHappenedItem].self, from: data)) ?? []
    }

    var parsedTeamPulse: [TeamPulseItem] {
        guard let data = teamPulse.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([TeamPulseItem].self, from: data)) ?? []
    }

    var parsedCoaching: [CoachingItem] {
        guard let data = coaching.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([CoachingItem].self, from: data)) ?? []
    }

    var dateLabel: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        // Parse YYYY-MM-DD
        let isoFmt = DateFormatter()
        isoFmt.dateFormat = "yyyy-MM-dd"
        if let parsed = isoFmt.date(from: date) {
            return formatter.string(from: parsed)
        }
        return date
    }
}
