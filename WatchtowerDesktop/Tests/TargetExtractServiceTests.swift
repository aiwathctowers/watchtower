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
        let runner = StubCLIRunner(stdout: Data(json.utf8))
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
        let runner = StubCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        _ = try await service.extract(text: "hello world", sourceRef: "inbox:42")

        XCTAssertTrue(runner.capturedArgs.contains("targets"))
        XCTAssertTrue(runner.capturedArgs.contains("extract"))
        XCTAssertTrue(runner.capturedArgs.contains("--json"))
        XCTAssertTrue(runner.capturedArgs.contains("--text"))
        XCTAssertTrue(runner.capturedArgs.contains("hello world"))
        XCTAssertTrue(runner.capturedArgs.contains("--source-ref"))
        XCTAssertTrue(runner.capturedArgs.contains("inbox:42"))
    }

    func testExtractOmitsSourceRefWhenEmpty() async throws {
        let json = "{\"extracted\": [], \"omitted_count\": 0, \"notes\": \"\"}"
        let runner = StubCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        _ = try await service.extract(text: "hello")

        XCTAssertFalse(runner.capturedArgs.contains("--source-ref"))
    }

    // MARK: - CLI failure

    func testExtractPropagatesCLIError() async {
        let runner = StubCLIRunner(
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
        let runner = StubCLIRunner(stdout: Data("not json".utf8))
        let service = TargetExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "sample")
            XCTFail("expected decoding error")
        } catch {
            // OK
        }
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
        let runner = StubCLIRunner(stdout: Data(json.utf8))
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

// MARK: - Stub runner

final class StubCLIRunner: CLIRunnerProtocol {
    let stdout: Data
    let error: Error?
    var capturedArgs: [String] = []

    init(stdout: Data = Data(), error: Error? = nil) {
        self.stdout = stdout
        self.error = error
    }

    func run(args: [String]) async throws -> Data {
        capturedArgs = args
        if let error { throw error }
        return stdout
    }
}
