import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TokenUsageQueryTests: XCTestCase {

    func testFetchUsageEmpty() throws {
        let db = try TestDatabase.create()
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertTrue(summary.rows.isEmpty)
        XCTAssertEqual(summary.totalInputTokens, 0)
        XCTAssertEqual(summary.totalOutputTokens, 0)
        XCTAssertEqual(summary.totalCost, 0)
        XCTAssertEqual(summary.totalCalls, 0)
        XCTAssertEqual(summary.totalApiTokens, 0)
    }

    func testFetchUsageFromDigests() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'cli', 'haiku', 'done', 1000, 500, 0.01, 3000)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalInputTokens, 1000)
        XCTAssertEqual(summary.totalOutputTokens, 500)
        XCTAssertEqual(summary.totalCost, 0.01, accuracy: 0.001)
        XCTAssertEqual(summary.totalCalls, 1)
        XCTAssertEqual(summary.totalApiTokens, 3000)
    }

    func testFetchUsageAggregatesMultipleSources() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'cli', 'haiku', 'done', 1000, 500, 0.01, 2000)
                """)
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('people', 'cli', 'sonnet', 'done', 2000, 1000, 0.05, 5000)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalInputTokens, 3000)
        XCTAssertEqual(summary.totalOutputTokens, 1500)
        XCTAssertEqual(summary.totalCost, 0.06, accuracy: 0.001)
        XCTAssertEqual(summary.totalCalls, 2)
        XCTAssertEqual(summary.totalApiTokens, 7000)
    }

    func testByModelGrouping() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'daemon', 'haiku', 'done', 1000, 500, 0.01, 2000)
                """)
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'daemon', 'haiku', 'done', 500, 250, 0.005, 1000)
                """)
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('people', 'cli', 'sonnet', 'done', 2000, 1000, 0.05, 5000)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        let byModel = summary.byModel

        // Sorted by cost DESC: sonnet (0.05) then haiku (0.015)
        XCTAssertEqual(byModel.count, 2)
        XCTAssertEqual(byModel[0].model, "sonnet")
        XCTAssertEqual(byModel[0].input, 2000)
        XCTAssertEqual(byModel[0].totalApiTokens, 5000)
        XCTAssertEqual(byModel[1].model, "haiku")
        XCTAssertEqual(byModel[1].input, 1500) // 1000 + 500
        XCTAssertEqual(byModel[1].calls, 2)
        XCTAssertEqual(byModel[1].totalApiTokens, 3000) // 2000 + 1000
    }

    func testByModelEmptyModel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'cli', '', 'done', 100, 50, 0.01, 200)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        let byModel = summary.byModel
        XCTAssertEqual(byModel.count, 1)
        XCTAssertEqual(byModel[0].model, "(unknown)")
    }

    func testTokenUsageSummaryByModelSortedByCost() throws {
        let summary = TokenUsageSummary(rows: [
            makeRow(pipeline: "digests", model: "haiku", cost: 0.01),
            makeRow(pipeline: "people", model: "opus", cost: 0.10),
            makeRow(pipeline: "tracks", model: "sonnet", cost: 0.05)
        ])
        let byModel = summary.byModel
        XCTAssertEqual(byModel[0].model, "opus")
        XCTAssertEqual(byModel[1].model, "sonnet")
        XCTAssertEqual(byModel[2].model, "haiku")
    }

    func testExcludesRunningButIncludesErrorRuns() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            // 'done' run — should be counted
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'cli', 'haiku', 'done', 1000, 500, 0.01, 2000)
                """)
            // 'running' run — should be excluded
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('digests', 'cli', 'haiku', 'running', 500, 250, 0.005, 1000)
                """)
            // 'error' run — should be included (tokens were spent)
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('tracks', 'cli', 'haiku', 'error', 300, 150, 0.003, 600)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalCalls, 2)
        XCTAssertEqual(summary.totalInputTokens, 1300)
        XCTAssertEqual(summary.totalOutputTokens, 650)
        XCTAssertEqual(summary.totalCost, 0.013, accuracy: 0.001)
        XCTAssertEqual(summary.totalApiTokens, 2600)
    }

    func testBriefingIncluded() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO pipeline_runs (pipeline, source, model, status,
                    input_tokens, output_tokens, cost_usd, total_api_tokens)
                VALUES ('briefing', 'daemon', 'haiku', 'done', 5000, 2000, 0.10, 15000)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalCalls, 1)
        XCTAssertEqual(summary.totalInputTokens, 5000)
        XCTAssertEqual(summary.totalApiTokens, 15000)
    }

    // Helper to create TokenUsageRow in-memory (no DB roundtrip needed)
    private func makeRow(pipeline: String, model: String, cost: Double) -> TokenUsageRow {
        TokenUsageRow(
            pipeline: pipeline, model: model, calls: 1,
            inputTokens: 100, outputTokens: 50, costUSD: cost, totalApiTokens: 200
        )
    }
}
