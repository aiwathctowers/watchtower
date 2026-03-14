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

struct TokenUsageSummary {
    let rows: [TokenUsageRow]

    var totalInputTokens: Int { rows.reduce(0) { $0 + $1.inputTokens } }
    var totalOutputTokens: Int { rows.reduce(0) { $0 + $1.outputTokens } }
    var totalCost: Double { rows.reduce(0) { $0 + $1.costUSD } }
    var totalCalls: Int { rows.reduce(0) { $0 + $1.calls } }

    /// Grouped by model, with per-model totals.
    var byModel: [(model: String, input: Int, output: Int, cost: Double, calls: Int)] {
        var map: [String: (input: Int, output: Int, cost: Double, calls: Int)] = [:]
        for row in rows {
            let key = row.model.isEmpty ? "(unknown)" : row.model
            let existing = map[key, default: (0, 0, 0, 0)]
            map[key] = (
                existing.input + row.inputTokens,
                existing.output + row.outputTokens,
                existing.cost + row.costUSD,
                existing.calls + row.calls
            )
        }
        return map
            .map { (model: $0.key, input: $0.value.input, output: $0.value.output, cost: $0.value.cost, calls: $0.value.calls) }
            .sorted { $0.cost > $1.cost }
    }
}

enum TokenUsageQueries {
    static func fetchUsage(_ db: Database) throws -> TokenUsageSummary {
        let sql = """
            SELECT 'digests' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM digests
            WHERE input_tokens > 0 OR output_tokens > 0
            GROUP BY model

            UNION ALL

            SELECT 'people' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM user_analyses
            WHERE input_tokens > 0 OR output_tokens > 0
            GROUP BY model

            UNION ALL

            SELECT 'summaries' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM period_summaries
            WHERE input_tokens > 0 OR output_tokens > 0
            GROUP BY model

            UNION ALL

            SELECT 'tracks' AS source, model,
                   COUNT(*) AS calls,
                   COALESCE(SUM(input_tokens), 0) AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(cost_usd), 0) AS cost_usd
            FROM tracks
            WHERE input_tokens > 0 OR output_tokens > 0
            GROUP BY model
            """
        let rows = try TokenUsageRow.fetchAll(db, sql: sql)
        return TokenUsageSummary(rows: rows)
    }
}
