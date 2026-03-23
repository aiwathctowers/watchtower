import GRDB
import Foundation

struct PeriodSummary: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let periodFrom: Double
    let periodTo: Double
    let summary: String
    let attention: String
    let model: String
    let inputTokens: Int?
    let outputTokens: Int?
    let costUSD: Double?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, summary, model, attention
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case createdAt = "created_at"
    }

    private static let decoder = JSONDecoder()

    var parsedAttention: [String] {
        guard let data = attention.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }

    var periodFromDate: Date {
        Date(timeIntervalSince1970: periodFrom)
    }

    var periodToDate: Date {
        Date(timeIntervalSince1970: periodTo)
    }
}
