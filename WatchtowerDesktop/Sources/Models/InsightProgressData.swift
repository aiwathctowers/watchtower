import Foundation

struct InsightProgressData: Codable {
    let pipeline: String
    let done: Int
    let total: Int
    let status: String?
    let inputTokens: Int
    let outputTokens: Int
    let costUsd: Double
    let error: String?
    let finished: Bool?
    let itemsFound: Int?

    enum CodingKeys: String, CodingKey {
        case pipeline, done, total, status
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUsd = "cost_usd"
        case error, finished
        case itemsFound = "items_found"
    }
}
