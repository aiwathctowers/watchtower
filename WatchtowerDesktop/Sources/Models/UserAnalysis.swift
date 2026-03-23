import GRDB
import Foundation

struct UserAnalysis: FetchableRecord, Decodable, Identifiable, Equatable {
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
    let communicationStyle: String
    let decisionRole: String
    let redFlags: String
    let highlights: String
    let styleDetails: String
    let recommendations: String
    let concerns: String
    let accomplishments: String
    let model: String
    let inputTokens: Int?
    let outputTokens: Int?
    let costUSD: Double?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, summary, model, highlights
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
        case communicationStyle = "communication_style"
        case decisionRole = "decision_role"
        case redFlags = "red_flags"
        case styleDetails = "style_details"
        case recommendations
        case concerns, accomplishments
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case createdAt = "created_at"
    }

    private static let decoder = JSONDecoder()

    var parsedRedFlags: [String] {
        guard let data = redFlags.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedHighlights: [String] {
        guard let data = highlights.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedRecommendations: [String] {
        guard let data = recommendations.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedConcerns: [String] {
        guard let data = concerns.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedAccomplishments: [String] {
        guard let data = accomplishments.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var hasConcerns: Bool {
        !parsedConcerns.isEmpty
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
