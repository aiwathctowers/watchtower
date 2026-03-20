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
    }

    func testFetchUsageFromDigests() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C001', 100, 200, 'channel', 'Test', 10, 'haiku', 1000, 500, 0.01)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalInputTokens, 1000)
        XCTAssertEqual(summary.totalOutputTokens, 500)
        XCTAssertEqual(summary.totalCost, 0.01, accuracy: 0.001)
        XCTAssertEqual(summary.totalCalls, 1)
    }

    func testFetchUsageAggregatesMultipleSources() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C001', 100, 200, 'channel', 'Test', 10, 'haiku', 1000, 500, 0.01)
                """)
            try db.execute(sql: """
                INSERT INTO user_analyses (user_id, period_from, period_to, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('U001', 100, 200, 'sonnet', 2000, 1000, 0.05)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        XCTAssertEqual(summary.totalInputTokens, 3000)
        XCTAssertEqual(summary.totalOutputTokens, 1500)
        XCTAssertEqual(summary.totalCost, 0.06, accuracy: 0.001)
        XCTAssertEqual(summary.totalCalls, 2)
    }

    func testByModelGrouping() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C001', 100, 200, 'channel', 'T1', 10, 'haiku', 1000, 500, 0.01)
                """)
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C002', 100, 200, 'channel', 'T2', 10, 'haiku', 500, 250, 0.005)
                """)
            try db.execute(sql: """
                INSERT INTO user_analyses (user_id, period_from, period_to, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('U001', 100, 200, 'sonnet', 2000, 1000, 0.05)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        let byModel = summary.byModel

        // Sorted by cost DESC: sonnet (0.05) then haiku (0.015)
        XCTAssertEqual(byModel.count, 2)
        XCTAssertEqual(byModel[0].model, "sonnet")
        XCTAssertEqual(byModel[0].input, 2000)
        XCTAssertEqual(byModel[1].model, "haiku")
        XCTAssertEqual(byModel[1].input, 1500) // 1000 + 500
        XCTAssertEqual(byModel[1].calls, 2)
    }

    func testByModelEmptyModel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C001', 100, 200, 'channel', 'T', 10, '', 100, 50, 0.01)
                """)
        }
        let summary = try db.read { try TokenUsageQueries.fetchUsage($0) }
        let byModel = summary.byModel
        XCTAssertEqual(byModel.count, 1)
        XCTAssertEqual(byModel[0].model, "(unknown)")
    }

    func testTokenUsageSummaryByModelSortedByCost() {
        let summary = TokenUsageSummary(rows: [
            makeRow(source: "digests", model: "haiku", cost: 0.01),
            makeRow(source: "people", model: "opus", cost: 0.10),
            makeRow(source: "tracks", model: "sonnet", cost: 0.05),
        ])
        let byModel = summary.byModel
        XCTAssertEqual(byModel[0].model, "opus")
        XCTAssertEqual(byModel[1].model, "sonnet")
        XCTAssertEqual(byModel[2].model, "haiku")
    }

    // Helper to create TokenUsageRow via DB roundtrip
    private func makeRow(source: String, model: String, cost: Double) -> TokenUsageRow {
        let db = try! TestDatabase.create()
        try! db.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count, model,
                    input_tokens, output_tokens, cost_usd)
                VALUES ('C001', 100, 200, 'channel', 'T', 10, ?, 100, 50, ?)
                """, arguments: [model, cost])
        }
        let rows = try! db.read { try TokenUsageQueries.fetchUsage($0) }.rows
        return rows[0]
    }
}
