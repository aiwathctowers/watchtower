import XCTest
@testable import WatchtowerDesktop

final class TargetPromoteSubItemServiceTests: XCTestCase {
    // MARK: - Happy path

    func testPromoteParsesJSONIntoResult() async throws {
        let json = """
        {
          "id": 42,
          "text": "first sub-item",
          "level": "week",
          "priority": "high",
          "status": "todo",
          "due_date": "",
          "period_start": "2026-04-20",
          "period_end": "2026-04-26",
          "parent_id": 7,
          "source_type": "promoted_subitem",
          "source_id": "7:0"
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetPromoteSubItemService(runner: runner)

        let result = try await service.promote(parentID: 7, index: 0)

        XCTAssertEqual(result.id, 42)
        XCTAssertEqual(result.text, "first sub-item")
        XCTAssertEqual(result.level, "week")
        XCTAssertEqual(result.priority, "high")
        XCTAssertEqual(result.status, "todo")
        XCTAssertEqual(result.parentID, 7)
        XCTAssertEqual(result.sourceType, "promoted_subitem")
        XCTAssertEqual(result.sourceID, "7:0")
    }

    // MARK: - Argument plumbing

    func testPromotePassesPositionalArgsAndJSONFlag() async throws {
        let runner = FakeCLIRunner(stdout: Data(minimalJSON().utf8))
        let service = TargetPromoteSubItemService(runner: runner)

        _ = try await service.promote(parentID: 99, index: 3)

        let args = runner.invocations.last ?? []
        XCTAssertEqual(args.prefix(5).map { String($0) },
                       ["targets", "promote-subitem", "99", "3", "--json"])
    }

    func testPromoteOmitsAllOverrideFlagsByDefault() async throws {
        let runner = FakeCLIRunner(stdout: Data(minimalJSON().utf8))
        let service = TargetPromoteSubItemService(runner: runner)

        _ = try await service.promote(parentID: 1, index: 0)

        let args = runner.invocations.last ?? []
        for flag in ["--text", "--intent", "--level", "--priority", "--ownership",
                     "--due", "--period-start", "--period-end", "--tags"] {
            XCTAssertFalse(args.contains(flag), "flag \(flag) should not appear when nil")
        }
    }

    func testPromoteIncludesEveryProvidedOverride() async throws {
        let runner = FakeCLIRunner(stdout: Data(minimalJSON().utf8))
        let service = TargetPromoteSubItemService(runner: runner)

        let overrides = PromoteSubItemOverrides(
            text: "polished",
            intent: "ci",
            level: "day",
            priority: "low",
            ownership: "delegated",
            dueDate: "2026-05-01T10:00",
            periodStart: "2026-04-30",
            periodEnd: "2026-04-30",
            tags: ["x", "y"]
        )
        _ = try await service.promote(parentID: 1, index: 0, overrides: overrides)

        let args = runner.invocations.last ?? []
        let pairs: [(String, String)] = [
            ("--text", "polished"),
            ("--intent", "ci"),
            ("--level", "day"),
            ("--priority", "low"),
            ("--ownership", "delegated"),
            ("--due", "2026-05-01T10:00"),
            ("--period-start", "2026-04-30"),
            ("--period-end", "2026-04-30"),
            ("--tags", "x,y"),
        ]
        for (flag, value) in pairs {
            guard let i = args.firstIndex(of: flag) else {
                XCTFail("missing flag \(flag)"); continue
            }
            XCTAssertLessThan(i + 1, args.count, "value missing for \(flag)")
            XCTAssertEqual(args[i + 1], value, "value for \(flag)")
        }
    }

    func testPromotePropagatesEmptyTagsToClearParentTags() async throws {
        let runner = FakeCLIRunner(stdout: Data(minimalJSON().utf8))
        let service = TargetPromoteSubItemService(runner: runner)

        // Empty array (not nil) tells the CLI to clear parent tags.
        _ = try await service.promote(
            parentID: 1, index: 0,
            overrides: PromoteSubItemOverrides(tags: [])
        )

        let args = runner.invocations.last ?? []
        guard let i = args.firstIndex(of: "--tags") else {
            return XCTFail("expected --tags to be present even when empty (clears parent tags)")
        }
        XCTAssertEqual(args[i + 1], "")
    }

    // MARK: - Error paths

    func testPromotePropagatesCLIError() async {
        let runner = FakeCLIRunner(
            error: CLIRunnerError.nonZeroExit(code: 1, stderr: "out of range")
        )
        let service = TargetPromoteSubItemService(runner: runner)
        do {
            _ = try await service.promote(parentID: 1, index: 99)
            XCTFail("expected error")
        } catch {
            // OK
        }
    }

    func testPromoteThrowsOnMalformedJSON() async {
        let runner = FakeCLIRunner(stdout: Data("not json".utf8))
        let service = TargetPromoteSubItemService(runner: runner)
        do {
            _ = try await service.promote(parentID: 1, index: 0)
            XCTFail("expected decoding error")
        } catch {
            // OK
        }
    }

    // MARK: - Helpers

    private func minimalJSON() -> String {
        """
        {
          "id": 1, "text": "x", "level": "day", "priority": "medium", "status": "todo",
          "due_date": "", "period_start": "2026-04-20", "period_end": "2026-04-20",
          "parent_id": 1, "source_type": "promoted_subitem", "source_id": "1:0"
        }
        """
    }
}
