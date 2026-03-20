import Foundation
import GRDB

struct PeopleCard: FetchableRecord, Decodable, Identifiable, Equatable {
    var id: Int64
    var userID: String
    var periodFrom: Double
    var periodTo: Double
    var messageCount: Int
    var channelsActive: Int
    var threadsInitiated: Int
    var threadsReplied: Int
    var avgMessageLength: Double
    var activeHoursJSON: String
    var volumeChangePct: Double
    var summary: String
    var communicationStyle: String
    var decisionRole: String
    var redFlags: String      // JSON array
    var highlights: String    // JSON array
    var accomplishments: String // JSON array
    var howToCommunicate: String
    var decisionStyle: String
    var tactics: String       // JSON array
    var relationshipContext: String
    var model: String
    var inputTokens: Int
    var outputTokens: Int
    var costUSD: Double
    var promptVersion: Int
    var createdAt: String

    // Column mapping
    enum CodingKeys: String, CodingKey {
        case id, userID = "user_id", periodFrom = "period_from", periodTo = "period_to"
        case messageCount = "message_count", channelsActive = "channels_active"
        case threadsInitiated = "threads_initiated", threadsReplied = "threads_replied"
        case avgMessageLength = "avg_message_length", activeHoursJSON = "active_hours_json"
        case volumeChangePct = "volume_change_pct"
        case summary, communicationStyle = "communication_style", decisionRole = "decision_role"
        case redFlags = "red_flags", highlights, accomplishments
        case howToCommunicate = "how_to_communicate", decisionStyle = "decision_style"
        case tactics, relationshipContext = "relationship_context"
        case model, inputTokens = "input_tokens", outputTokens = "output_tokens"
        case costUSD = "cost_usd", promptVersion = "prompt_version", createdAt = "created_at"
    }

    // Parsed helpers
    var parsedRedFlags: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(redFlags.utf8))) ?? []
    }
    var parsedHighlights: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(highlights.utf8))) ?? []
    }
    var parsedAccomplishments: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(accomplishments.utf8))) ?? []
    }
    var parsedTactics: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(tactics.utf8))) ?? []
    }
    var parsedActiveHours: [String: Int] {
        (try? JSONDecoder().decode([String: Int].self, from: Data(activeHoursJSON.utf8))) ?? [:]
    }
    var periodFromDate: Date { Date(timeIntervalSince1970: periodFrom) }
    var periodToDate: Date { Date(timeIntervalSince1970: periodTo) }

    var hasRedFlags: Bool {
        !parsedRedFlags.isEmpty
    }

    var styleEmoji: String {
        switch communicationStyle.lowercased() {
        case "driver": return "🚀"
        case "collaborator": return "🤝"
        case "executor": return "⚡"
        case "observer": return "👀"
        case "facilitator": return "🎯"
        default: return "💬"
        }
    }
}

struct PeopleCardSummary: FetchableRecord, Decodable, Identifiable, Equatable {
    var id: Int64
    var periodFrom: Double
    var periodTo: Double
    var summary: String
    var attention: String  // JSON array
    var tips: String       // JSON array
    var model: String
    var inputTokens: Int
    var outputTokens: Int
    var costUSD: Double
    var promptVersion: Int
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, periodFrom = "period_from", periodTo = "period_to"
        case summary, attention, tips
        case model, inputTokens = "input_tokens", outputTokens = "output_tokens"
        case costUSD = "cost_usd", promptVersion = "prompt_version", createdAt = "created_at"
    }

    var parsedAttention: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(attention.utf8))) ?? []
    }
    var parsedTips: [String] {
        (try? JSONDecoder().decode([String].self, from: Data(tips.utf8))) ?? []
    }
}
