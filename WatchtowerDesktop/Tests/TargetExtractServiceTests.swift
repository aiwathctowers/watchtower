import XCTest
@testable import WatchtowerDesktop

final class TargetExtractServiceTests: XCTestCase {
    // MARK: - Happy path

    func testExtractParsesJSONIntoProposedTargets() async throws {
        let json = """
        {
          "extracted": [
            {
              "text": "Write onboarding docs",
              "intent": "",
              "level": "week",
              "custom_label": "",
              "period_start": "2026-04-24",
              "period_end": "2026-04-30",
              "priority": "high",
              "due_date": "",
              "parent_id": null,
              "ai_level_confidence": 0.82,
              "secondary_links": []
            }
          ],
          "omitted_count": 2,
          "notes": "skipped duplicates"
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        let result = try await service.extract(text: "sample")

        XCTAssertEqual(result.extracted.count, 1)
        XCTAssertEqual(result.extracted[0].text, "Write onboarding docs")
        XCTAssertEqual(result.extracted[0].level, "week")
        XCTAssertEqual(result.extracted[0].levelConfidence ?? 0, 0.82, accuracy: 0.001)
        XCTAssertNil(result.extracted[0].parentId)
        XCTAssertEqual(result.omittedCount, 2)
        XCTAssertEqual(result.notes, "skipped duplicates")
    }

    func testExtractPassesTextAndSourceRefToRunner() async throws {
        let json = "{\"extracted\": [], \"omitted_count\": 0, \"notes\": \"\"}"
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        _ = try await service.extract(text: "hello world", sourceRef: "inbox:42")

        let lastArgs = runner.invocations.last ?? []
        XCTAssertTrue(lastArgs.contains("targets"))
        XCTAssertTrue(lastArgs.contains("extract"))
        XCTAssertTrue(lastArgs.contains("--json"))
        XCTAssertTrue(lastArgs.contains("--text"))
        XCTAssertTrue(lastArgs.contains("hello world"))
        XCTAssertTrue(lastArgs.contains("--source-ref"))
        XCTAssertTrue(lastArgs.contains("inbox:42"))
    }

    func testExtractOmitsSourceRefWhenEmpty() async throws {
        let json = "{\"extracted\": [], \"omitted_count\": 0, \"notes\": \"\"}"
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        _ = try await service.extract(text: "hello")

        XCTAssertFalse((runner.invocations.last ?? []).contains("--source-ref"))
    }

    // MARK: - CLI failure

    func testExtractPropagatesCLIError() async {
        let runner = FakeCLIRunner(
            error: CLIRunnerError.nonZeroExit(code: 1, stderr: "boom")
        )
        let service = TargetExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "sample")
            XCTFail("expected error")
        } catch {
            // OK
        }
    }

    // MARK: - Malformed JSON

    func testExtractThrowsOnMalformedJSON() async {
        let runner = FakeCLIRunner(stdout: Data("not json".utf8))
        let service = TargetExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "sample")
            XCTFail("expected decoding error")
        } catch {
            // OK
        }
    }

    // MARK: - Sub-items (single-goal-with-steps grouping)

    func testExtractDecodesSubItems() async throws {
        let json = """
        {
          "extracted": [
            {
              "text": "Актуализировать и согласовать план найма",
              "intent": "",
              "level": "week",
              "custom_label": "",
              "period_start": "2026-04-21",
              "period_end": "2026-04-27",
              "priority": "high",
              "due_date": "",
              "parent_id": null,
              "ai_level_confidence": 0.8,
              "secondary_links": [],
              "sub_items": [
                {"text": "Пообщаться со всеми хедами"},
                {"text": "Подготовить план от разработки"},
                {"text": "Учесть LLM Dev Pipelines"}
              ]
            }
          ],
          "omitted_count": 0,
          "notes": ""
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        let result = try await service.extract(text: "sample")

        XCTAssertEqual(result.extracted.count, 1)
        XCTAssertEqual(result.extracted[0].subItems.count, 3)
        XCTAssertEqual(result.extracted[0].subItems[0].text, "Пообщаться со всеми хедами")
        XCTAssertFalse(result.extracted[0].subItems[0].done)
    }

    func testExtractHandlesMissingSubItemsField() async throws {
        // Existing JSON without sub_items must still decode (backward-compat).
        let json = """
        {
          "extracted": [
            {
              "text": "legacy target",
              "intent": "",
              "level": "day",
              "custom_label": "",
              "period_start": "2026-04-24",
              "period_end": "2026-04-24",
              "priority": "medium",
              "due_date": "",
              "parent_id": null,
              "ai_level_confidence": null,
              "secondary_links": []
            }
          ],
          "omitted_count": 0,
          "notes": ""
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        let result = try await service.extract(text: "sample")

        XCTAssertEqual(result.extracted[0].subItems.count, 0)
    }

    // MARK: - Secondary links

    func testExtractDecodesSecondaryLinks() async throws {
        let json = """
        {
          "extracted": [
            {
              "text": "Ship feature",
              "intent": "",
              "level": "day",
              "custom_label": "",
              "period_start": "2026-04-24",
              "period_end": "2026-04-24",
              "priority": "medium",
              "due_date": "",
              "parent_id": 7,
              "ai_level_confidence": null,
              "secondary_links": [
                {"target_id": 12, "external_ref": "", "relation": "contributes_to", "confidence": 0.8},
                {"target_id": null, "external_ref": "jira:PROJ-1", "relation": "blocks", "confidence": null}
              ]
            }
          ],
          "omitted_count": 0,
          "notes": ""
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        let result = try await service.extract(text: "sample")

        XCTAssertEqual(result.extracted[0].parentId, 7)
        XCTAssertNil(result.extracted[0].levelConfidence)
        XCTAssertEqual(result.extracted[0].secondaryLinks.count, 2)
        XCTAssertEqual(result.extracted[0].secondaryLinks[0].targetId, 12)
        XCTAssertEqual(result.extracted[0].secondaryLinks[0].relation, "contributes_to")
        XCTAssertEqual(result.extracted[0].secondaryLinks[1].externalRef, "jira:PROJ-1")
        XCTAssertNil(result.extracted[0].secondaryLinks[1].targetId)
    }
}

