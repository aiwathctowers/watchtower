import GRDB
import Foundation

struct CommunicationGuide: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let userID: String
    let periodFrom: Double
    let periodTo: Double
    let messageCount: Int
    let channelsActive: Int
    let threadsInitiated: Int
    let threadsReplied: Int
    let avgMessageLength: Double
    let activeHoursJSON: String
    let volumeChangePct: Double
    let summary: String
    let communicationPreferences: String
    let availabilityPatterns: String
    let decisionProcess: String
    let situationalTactics: String
    let effectiveApproaches: String
    let recommendations: String
    let relationshipContext: String
    let model: String
    let inputTokens: Int?
    let outputTokens: Int?
    let costUSD: Double?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, summary, model, recommendations
        case userID = "user_id"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case messageCount = "message_count"
        case channelsActive = "channels_active"
        case threadsInitiated = "threads_initiated"
        case threadsReplied = "threads_replied"
        case avgMessageLength = "avg_message_length"
        case activeHoursJSON = "active_hours_json"
        case volumeChangePct = "volume_change_pct"
        case communicationPreferences = "communication_preferences"
        case availabilityPatterns = "availability_patterns"
        case decisionProcess = "decision_process"
        case situationalTactics = "situational_tactics"
        case effectiveApproaches = "effective_approaches"
        case relationshipContext = "relationship_context"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case createdAt = "created_at"
    }

    private static let decoder = JSONDecoder()

    var parsedSituationalTactics: [String] {
        guard let data = situationalTactics.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedEffectiveApproaches: [String] {
        guard let data = effectiveApproaches.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedRecommendations: [String] {
        guard let data = recommendations.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedActiveHours: [String: Int] {
        guard let data = activeHoursJSON.data(using: .utf8) else { return [:] }
        return (try? Self.decoder.decode([String: Int].self, from: data)) ?? [:]
    }

    var periodFromDate: Date {
        Date(timeIntervalSince1970: periodFrom)
    }

    var periodToDate: Date {
        Date(timeIntervalSince1970: periodTo)
    }
}

struct GuideSummary: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let periodFrom: Double
    let periodTo: Double
    let summary: String
    let tips: String
    let model: String
    let inputTokens: Int?
    let outputTokens: Int?
    let costUSD: Double?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, summary, model, tips
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case createdAt = "created_at"
    }

    private static let decoder = JSONDecoder()

    var parsedTips: [String] {
        guard let data = tips.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var periodFromDate: Date {
        Date(timeIntervalSince1970: periodFrom)
    }

    var periodToDate: Date {
        Date(timeIntervalSince1970: periodTo)
    }
}
