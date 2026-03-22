import GRDB
import Foundation

struct Digest: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let channelID: String
    let periodFrom: Double
    let periodTo: Double
    let type: String
    let summary: String
    let topics: String
    let decisions: String
    let tracksJSON: String
    let messageCount: Int
    let model: String
    let inputTokens: Int?
    let outputTokens: Int?
    let costUSD: Double?
    let createdAt: String
    let readAt: String?
    let runningSummary: String?

    enum CodingKeys: String, CodingKey {
        case id, type, summary, topics, decisions, model
        case channelID = "channel_id"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case tracksJSON = "action_items"
        case messageCount = "message_count"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case createdAt = "created_at"
        case readAt = "read_at"
        case runningSummary = "running_summary"
    }

    var isRead: Bool { readAt != nil }

    // M3: cache parsed JSON via lazy static decoder
    private static let decoder = JSONDecoder()

    var parsedDecisions: [Decision] {
        guard let data = decisions.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([Decision].self, from: data)) ?? []
    }

    var parsedTracks: [DigestTrack] {
        guard let data = tracksJSON.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([DigestTrack].self, from: data)) ?? []
    }

    var parsedTopics: [String] {
        guard let data = topics.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var parsedRunningSummary: RunningSummary? {
        guard let raw = runningSummary, !raw.isEmpty,
              let data = raw.data(using: .utf8) else { return nil }
        return try? Self.decoder.decode(RunningSummary.self, from: data)
    }
}
