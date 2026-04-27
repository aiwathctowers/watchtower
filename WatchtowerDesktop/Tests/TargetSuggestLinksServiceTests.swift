import XCTest
@testable import WatchtowerDesktop

final class TargetSuggestLinksServiceTests: XCTestCase {
    func testParsesParentAndSecondaryLinks() async throws {
        let json = """
        {
          "parent_id": 12,
          "secondary_links": [
            {"target_id": null, "external_ref": "jira:PROJ-7", "relation": "blocks", "confidence": 0.65},
            {"target_id": 33, "external_ref": "", "relation": "related", "confidence": null}
          ]
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetSuggestLinksService(runner: runner)

        let result = try await service.suggest(targetID: 99)

        XCTAssertEqual(result.parentID, 12)
        XCTAssertEqual(result.secondaryLinks.count, 2)
        XCTAssertEqual(result.secondaryLinks[0].relation, "blocks")
        XCTAssertEqual(result.secondaryLinks[0].externalRef, "jira:PROJ-7")
        XCTAssertNil(result.secondaryLinks[0].targetId)
        XCTAssertEqual(result.secondaryLinks[1].targetId, 33)
        XCTAssertEqual(result.secondaryLinks[1].relation, "related")
    }

    func testHandlesEmptyResult() async throws {
        let json = "{\"parent_id\": null, \"secondary_links\": []}"
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetSuggestLinksService(runner: runner)

        let result = try await service.suggest(targetID: 1)

        XCTAssertNil(result.parentID)
        XCTAssertTrue(result.secondaryLinks.isEmpty)
    }

    func testPassesCorrectArgsToRunner() async throws {
        let json = "{\"parent_id\": null, \"secondary_links\": []}"
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = TargetSuggestLinksService(runner: runner)

        _ = try await service.suggest(targetID: 42)

        let args = runner.invocations.last ?? []
        XCTAssertTrue(args.contains("targets"))
        XCTAssertTrue(args.contains("suggest-links"))
        XCTAssertTrue(args.contains("42"))
        XCTAssertTrue(args.contains("--json"))
    }

    func testPropagatesCLIError() async {
        let runner = FakeCLIRunner(error: CLIRunnerError.nonZeroExit(code: 1, stderr: "boom"))
        let service = TargetSuggestLinksService(runner: runner)
        do {
            _ = try await service.suggest(targetID: 1)
            XCTFail("expected error")
        } catch {
            // OK
        }
    }

    func testThrowsOnMalformedJSON() async {
        let runner = FakeCLIRunner(stdout: Data("not json".utf8))
        let service = TargetSuggestLinksService(runner: runner)
        do {
            _ = try await service.suggest(targetID: 1)
            XCTFail("expected decoding error")
        } catch {
            // OK
        }
    }
}
