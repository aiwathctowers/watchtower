import Foundation
import GRDB

enum PipelineRunQueries {
    static func fetchRecent(_ db: Database, limit: Int = 50) throws -> [PipelineRun] {
        try PipelineRun.fetchAll(
            db,
            sql: """
                SELECT id, pipeline, source, status, error_msg, items_found,
                    input_tokens, output_tokens, cost_usd, total_api_tokens,
                    period_from, period_to, started_at, finished_at, duration_seconds
                FROM pipeline_runs
                ORDER BY started_at DESC
                LIMIT ?
                """,
            arguments: [limit]
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
