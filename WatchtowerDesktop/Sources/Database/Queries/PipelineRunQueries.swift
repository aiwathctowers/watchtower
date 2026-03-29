import Foundation
import GRDB

enum PipelineRunQueries {
    static func fetchRecent(_ db: Database, limit: Int = 50) throws -> [PipelineRun] {
        try PipelineRun.fetchAll(
            db,
            sql: """
                SELECT pr.id, pr.pipeline, pr.source, pr.model, pr.status, pr.error_msg, pr.items_found,
                    pr.input_tokens, pr.output_tokens, pr.cost_usd, pr.total_api_tokens,
                    pr.period_from, pr.period_to, pr.started_at, pr.finished_at, pr.duration_seconds,
                    COALESCE(sc.cnt, 0) AS step_count
                FROM pipeline_runs pr
                LEFT JOIN (SELECT run_id, COUNT(*) AS cnt FROM pipeline_steps GROUP BY run_id) sc ON sc.run_id = pr.id
                ORDER BY pr.started_at DESC
                LIMIT ?
                """,
            arguments: [limit]
        )
    }

    static func fetchByDate(_ db: Database, on date: Date) throws -> [PipelineRun] {
        let cal = Calendar.current
        let dayStart = cal.startOfDay(for: date)
        let dayEnd = cal.date(byAdding: .day, value: 1, to: dayStart) ?? dayStart
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return try PipelineRun.fetchAll(
            db,
            sql: """
                SELECT pr.id, pr.pipeline, pr.source, pr.model, pr.status, pr.error_msg, pr.items_found,
                    pr.input_tokens, pr.output_tokens, pr.cost_usd, pr.total_api_tokens,
                    pr.period_from, pr.period_to, pr.started_at, pr.finished_at, pr.duration_seconds,
                    COALESCE(sc.cnt, 0) AS step_count
                FROM pipeline_runs pr
                LEFT JOIN (SELECT run_id, COUNT(*) AS cnt FROM pipeline_steps GROUP BY run_id) sc ON sc.run_id = pr.id
                WHERE pr.started_at >= ? AND pr.started_at < ?
                    AND pr.status IN ('done', 'error')
                    AND (pr.input_tokens > 0 OR pr.output_tokens > 0 OR pr.total_api_tokens > 0)
                ORDER BY pr.started_at DESC
                """,
            arguments: [fmt.string(from: dayStart), fmt.string(from: dayEnd)]
        )
    }

    static func fetchSteps(_ db: Database, runId: Int64) throws -> [PipelineStepRecord] {
        try PipelineStepRecord.fetchAll(
            db,
            sql: """
                SELECT id, run_id, step, total, status, channel_id, channel_name,
                    input_tokens, output_tokens, cost_usd, total_api_tokens,
                    message_count, period_from, period_to, duration_seconds, created_at
                FROM pipeline_steps
                WHERE run_id = ?
                ORDER BY step
                """,
            arguments: [runId]
        )
    }
}
