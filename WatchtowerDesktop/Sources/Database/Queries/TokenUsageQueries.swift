import Foundation
import GRDB

struct TokenUsageRow: Decodable, FetchableRecord, Identifiable {
    let pipeline: String
    let model: String
    let calls: Int
    let inputTokens: Int
    let outputTokens: Int
    let costUSD: Double
    let totalApiTokens: Int

    var id: String { "\(pipeline)-\(model)" }

    enum CodingKeys: String, CodingKey {
        case pipeline, model, calls
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
        case totalApiTokens = "total_api_tokens"
    }
}

struct ModelUsage {
    let model: String
    var input: Int
    var output: Int
    var cost: Double
    var calls: Int
    var totalApiTokens: Int
}

struct TokenUsageSummary {
    let rows: [TokenUsageRow]

    var totalInputTokens: Int { rows.reduce(0) { $0 + $1.inputTokens } }
    var totalOutputTokens: Int { rows.reduce(0) { $0 + $1.outputTokens } }
    var totalCost: Double { rows.reduce(0) { $0 + $1.costUSD } }
    var totalCalls: Int { rows.reduce(0) { $0 + $1.calls } }
    var totalApiTokens: Int { rows.reduce(0) { $0 + $1.totalApiTokens } }

    /// Grouped by model, with per-model totals.
    var byModel: [ModelUsage] {
        var map: [String: ModelUsage] = [:]
        for row in rows {
            let key = row.model.isEmpty ? "(unknown)" : row.model
            var existing = map[key, default: ModelUsage(model: key, input: 0, output: 0, cost: 0, calls: 0, totalApiTokens: 0)]
            existing.input += row.inputTokens
            existing.output += row.outputTokens
            existing.cost += row.costUSD
            existing.calls += row.calls
            existing.totalApiTokens += row.totalApiTokens
            map[key] = existing
        }
        return map.values.sorted { $0.cost > $1.cost }
    }
}

enum TokenUsageQueries {
    /// Fetch all-time usage (no date filter).
    static func fetchUsage(_ db: Database) throws -> TokenUsageSummary {
        try fetchUsage(db, on: nil)
    }

    /// Fetch usage for a specific day, or all-time if date is nil.
    static func fetchUsage(_ db: Database, on date: Date?) throws -> TokenUsageSummary {
        let dateFilter: String
        var args: [DatabaseValueConvertible] = []

        if let date {
            let cal = Calendar.current
            let dayStart = cal.startOfDay(for: date)
            let dayEnd = cal.date(byAdding: .day, value: 1, to: dayStart) ?? dayStart
            let fmt = ISO8601DateFormatter()
            fmt.formatOptions = [.withInternetDateTime]
            let startStr = fmt.string(from: dayStart)
            let endStr = fmt.string(from: dayEnd)
            dateFilter = " AND pr.started_at >= ? AND pr.started_at < ?"
            args = [startStr, endStr]
        } else {
            dateFilter = ""
        }

        let sql = """
            SELECT pr.pipeline, pr.model,
                   COALESCE(SUM(CASE WHEN sc.cnt > 0 THEN sc.cnt ELSE 1 END), 0) AS calls,
                   COALESCE(SUM(pr.input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(pr.output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(pr.cost_usd), 0) AS cost_usd,
                   COALESCE(SUM(pr.total_api_tokens), 0) AS total_api_tokens
            FROM pipeline_runs pr
            LEFT JOIN (SELECT run_id, COUNT(*) AS cnt FROM pipeline_steps GROUP BY run_id) sc ON sc.run_id = pr.id
            WHERE pr.status IN ('done', 'error')
                  AND (pr.input_tokens > 0 OR pr.output_tokens > 0 OR pr.total_api_tokens > 0)\(dateFilter)
            GROUP BY pr.pipeline, pr.model
            """
        let rows = try TokenUsageRow.fetchAll(
            db, sql: sql, arguments: StatementArguments(args)
        )
        return TokenUsageSummary(rows: rows)
    }
}
