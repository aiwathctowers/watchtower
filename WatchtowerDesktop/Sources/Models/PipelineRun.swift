import Foundation
import GRDB

struct PipelineRun: Decodable, FetchableRecord, Identifiable {
    let id: Int64
    let pipeline: String
    let source: String
    let model: String
    let status: String
    let errorMsg: String
    let itemsFound: Int
    let inputTokens: Int
    let outputTokens: Int
    let costUsd: Double
    let totalApiTokens: Int
    let periodFrom: Double?
    let periodTo: Double?
    let startedAt: String
    let finishedAt: String?
    let durationSeconds: Double
    let stepCount: Int

    /// Number of actual AI API calls (steps if available, otherwise 1 per run).
    var aiCallCount: Int { max(1, stepCount) }

    enum CodingKeys: String, CodingKey {
        case id, pipeline, source, model, status
        case errorMsg = "error_msg"
        case itemsFound = "items_found"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUsd = "cost_usd"
        case totalApiTokens = "total_api_tokens"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case startedAt = "started_at"
        case finishedAt = "finished_at"
        case durationSeconds = "duration_seconds"
        case stepCount = "step_count"
    }

    var pipelineTitle: String {
        switch pipeline {
        case "digests": return "Digests"
        case "tracks": return "Tracks"
        case "people": return "People Cards"
        default: return pipeline.capitalized
        }
    }

    var pipelineIcon: String {
        switch pipeline {
        case "digests": return "doc.text.magnifyingglass"
        case "tracks": return "checklist"
        case "people": return "person.2.circle"
        default: return "gearshape"
        }
    }

    var startedDate: Date? {
        ISO8601DateFormatter().date(from: startedAt)
    }
}

struct PipelineStepRecord: Decodable, FetchableRecord, Identifiable {
    let id: Int64
    let runId: Int64
    let step: Int
    let total: Int
    let status: String
    let channelId: String
    let channelName: String
    let inputTokens: Int
    let outputTokens: Int
    let costUsd: Double
    let totalApiTokens: Int
    let messageCount: Int
    let periodFrom: Double?
    let periodTo: Double?
    let durationSeconds: Double
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case runId = "run_id"
        case step, total, status
        case channelId = "channel_id"
        case channelName = "channel_name"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUsd = "cost_usd"
        case totalApiTokens = "total_api_tokens"
        case messageCount = "message_count"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case durationSeconds = "duration_seconds"
        case createdAt = "created_at"
    }
}
