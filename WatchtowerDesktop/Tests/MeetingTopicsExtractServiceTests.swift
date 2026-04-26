import XCTest
@testable import WatchtowerDesktop

final class MeetingTopicsExtractServiceTests: XCTestCase {

    // MARK: - Happy path

    func testExtractParsesTopics() async throws {
        let json = """
        {
          "topics": [
            {"text": "Discuss SDET priorities", "priority": "high"},
            {"text": "Confirm QA staffing plan", "priority": ""}
          ],
          "notes": "skipped recap section"
        }
        """
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = MeetingTopicsExtractService(runner: runner)

        let result = try await service.extract(text: "long raw blob")

        XCTAssertEqual(result.topics.count, 2)
        XCTAssertEqual(result.topics[0].text, "Discuss SDET priorities")
        XCTAssertEqual(result.topics[0].priority, "high")
        XCTAssertEqual(result.topics[1].priority, "")
        XCTAssertEqual(result.notes, "skipped recap section")
        XCTAssertEqual(runner.invocations.first?.prefix(2).joined(separator: " "), "meeting-prep extract-topics")
    }

    // MARK: - Event id passthrough

    func testExtractPassesEventID() async throws {
        let json = #"{"topics": [], "notes": ""}"#
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = MeetingTopicsExtractService(runner: runner)

        _ = try await service.extract(text: "x", eventID: "evt_123")

        guard let args = runner.invocations.first else {
            return XCTFail("no invocation recorded")
        }
        XCTAssertTrue(args.contains("--event-id"))
        XCTAssertTrue(args.contains("evt_123"))
    }

    // MARK: - Missing priority tolerated

    func testExtractToleratesMissingPriorityField() async throws {
        let json = #"{"topics": [{"text": "Only text"}], "notes": ""}"#
        let runner = FakeCLIRunner(stdout: Data(json.utf8))
        let service = MeetingTopicsExtractService(runner: runner)

        let result = try await service.extract(text: "x")

        XCTAssertEqual(result.topics.count, 1)
        XCTAssertEqual(result.topics[0].priority, "")
    }

    // MARK: - CLI error

    func testExtractPropagatesCLIError() async {
        let runner = FakeCLIRunner(error: CLIRunnerError.nonZeroExit(code: 1, stderr: "boom"))
        let service = MeetingTopicsExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "x")
            XCTFail("expected error")
        } catch {
            // ok
        }
    }

    // MARK: - Malformed JSON

    func testExtractThrowsOnMalformedJSON() async {
        let runner = FakeCLIRunner(stdout: Data("not json".utf8))
        let service = MeetingTopicsExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "x")
            XCTFail("expected decoding error")
        } catch {
            // ok
        }
    }
}
