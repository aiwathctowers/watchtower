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
    let finished: Bool
    let itemsFound: Int?
    let messageCount: Int?
    let periodFrom: Double?
    let periodTo: Double?
    let stepDurationSeconds: Double?
    let stepInputTokens: Int?
    let stepOutputTokens: Int?
    let stepCostUsd: Double?
    let totalApiTokens: Int?

    enum CodingKeys: String, CodingKey {
        case pipeline, done, total, status
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUsd = "cost_usd"
        case error, finished
        case itemsFound = "items_found"
        case messageCount = "message_count"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case stepDurationSeconds = "step_duration_seconds"
        case stepInputTokens = "step_input_tokens"
        case stepOutputTokens = "step_output_tokens"
        case stepCostUsd = "step_cost_usd"
        case totalApiTokens = "total_api_tokens"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        pipeline = try container.decode(String.self, forKey: .pipeline)
        done = try container.decode(Int.self, forKey: .done)
        total = try container.decode(Int.self, forKey: .total)
        status = try container.decodeIfPresent(String.self, forKey: .status)
        inputTokens = try container.decode(Int.self, forKey: .inputTokens)
        outputTokens = try container.decode(Int.self, forKey: .outputTokens)
        costUsd = try container.decode(Double.self, forKey: .costUsd)
        error = try container.decodeIfPresent(String.self, forKey: .error)
        finished = try container.decodeIfPresent(Bool.self, forKey: .finished) ?? false
        itemsFound = try container.decodeIfPresent(Int.self, forKey: .itemsFound)
        messageCount = try container.decodeIfPresent(Int.self, forKey: .messageCount)
        periodFrom = Self.decodePeriod(container: container, key: .periodFrom)
        periodTo = Self.decodePeriod(container: container, key: .periodTo)
        stepDurationSeconds = try container.decodeIfPresent(Double.self, forKey: .stepDurationSeconds)
        stepInputTokens = try container.decodeIfPresent(Int.self, forKey: .stepInputTokens)
        stepOutputTokens = try container.decodeIfPresent(Int.self, forKey: .stepOutputTokens)
        stepCostUsd = try container.decodeIfPresent(Double.self, forKey: .stepCostUsd)
        totalApiTokens = try container.decodeIfPresent(Int.self, forKey: .totalApiTokens)
    }

    /// Decode period field that may be a Double (unix timestamp) or String (RFC3339).
    private static func decodePeriod(container: KeyedDecodingContainer<CodingKeys>, key: CodingKeys) -> Double? {
        if let value = try? container.decodeIfPresent(Double.self, forKey: key) {
            return value
        }
        if let str = try? container.decodeIfPresent(String.self, forKey: key) {
            let fmt = ISO8601DateFormatter()
            fmt.formatOptions = [.withInternetDateTime]
            if let date = fmt.date(from: str) {
                return date.timeIntervalSince1970
            }
        }
        return nil
    }
}
