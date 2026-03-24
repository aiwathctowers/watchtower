import Foundation
import GRDB

struct TokenUsageRow: Decodable, FetchableRecord, Identifiable {
    let source: String
    let model: String
    let calls: Int
    let inputTokens: Int
    let outputTokens: Int
    let costUSD: Double

    var id: String { "\(source)-\(model)" }
    var totalTokens: Int { inputTokens + outputTokens }

    enum CodingKeys: String, CodingKey {
        case source, model, calls
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
    }
}

struct ModelUsage {
    let model: String
    var input: Int
    var output: Int
    var cost: Double
    var calls: Int
}

struct TokenUsageSummary {
    let rows: [TokenUsageRow]

    var totalInputTokens: Int { rows.reduce(0) { $0 + $1.inputTokens } }
    var totalOutputTokens: Int { rows.reduce(0) { $0 + $1.outputTokens } }
    var totalCost: Double { rows.reduce(0) { $0 + $1.costUSD } }
    var totalCalls: Int { rows.reduce(0) { $0 + $1.calls } }

    /// Grouped by model, with per-model totals.
    var byModel: [ModelUsage] {
        var map: [String: ModelUsage] = [:]
        for row in rows {
            let key = row.model.isEmpty ? "(unknown)" : row.model
            var existing = map[key, default: ModelUsage(model: key, input: 0, output: 0, cost: 0, calls: 0)]
            existing.input += row.inputTokens
            existing.output += row.outputTokens
            existing.cost += row.costUSD
            existing.calls += row.calls
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
            dateFilter = " AND created_at >= ? AND created_at < ?"
            args = [startStr, endStr]
        } else {
            dateFilter = ""
        }

        let sql = """
            SELECT 'digests' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM digests
            WHERE (input_tokens > 0 OR output_tokens > 0)\(dateFilter)
            GROUP BY model

            UNION ALL

            SELECT 'people' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM user_analyses
            WHERE (input_tokens > 0 OR output_tokens > 0)\(dateFilter)
            GROUP BY model

            UNION ALL

            SELECT 'summaries' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM period_summaries
            WHERE (input_tokens > 0 OR output_tokens > 0)\(dateFilter)
            GROUP BY model

            UNION ALL

            SELECT 'tracks' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM tracks
            WHERE (input_tokens > 0 OR output_tokens > 0)\(dateFilter)
            GROUP BY model

            UNION ALL

            SELECT 'people' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM people_cards
            WHERE (input_tokens > 0 OR output_tokens > 0)\(dateFilter)
            GROUP BY model
            """
        // Repeat args for each UNION ALL segment (5 tables)
        let allArgs = args + args + args + args + args
        let rows = try TokenUsageRow.fetchAll(
            db, sql: sql, arguments: StatementArguments(allArgs)
        )
        return TokenUsageSummary(rows: rows)
    }
}
