import XCTest
@testable import WatchtowerDesktop

final class MeetingRecapServiceTests: XCTestCase {

    // MARK: - Happy path

    func test_generateInvokesCLIWithRightArgs() async throws {
        let payload = """
        {"event_id":"evt-1","summary":"x","key_decisions":[],"action_items":[],"open_questions":[],"created_at":"","updated_at":""}
        """
        let fake = FakeCLIRunner(stdout: Data(payload.utf8))
        let svc = MeetingRecapService(runner: fake)

        try await svc.generate(eventID: "evt-1", text: "raw")

        guard let captured = fake.invocations.first else {
            return XCTFail("no invocation recorded")
        }
        XCTAssertEqual(captured.first, "meeting-prep")
        XCTAssertTrue(captured.contains("recap"))
        XCTAssertTrue(captured.contains("--event-id"))
        XCTAssertTrue(captured.contains("evt-1"))
        XCTAssertTrue(captured.contains("--text"))
        XCTAssertTrue(captured.contains("raw"))
        XCTAssertTrue(captured.contains("--json"))
    }

    func test_generateArgOrderMatchesSpec() async throws {
        let fake = FakeCLIRunner(stdout: Data())
        let svc = MeetingRecapService(runner: fake)

        try await svc.generate(eventID: "my-event", text: "notes here")

        guard let args = fake.invocations.first else {
            return XCTFail("no invocation recorded")
        }
        // Expected: ["meeting-prep", "recap", "--event-id", "my-event", "--text", "notes here", "--json"]
        XCTAssertEqual(args.count, 7)
        XCTAssertEqual(args[0], "meeting-prep")
        XCTAssertEqual(args[1], "recap")
        XCTAssertEqual(args[2], "--event-id")
        XCTAssertEqual(args[3], "my-event")
        XCTAssertEqual(args[4], "--text")
        XCTAssertEqual(args[5], "notes here")
        XCTAssertEqual(args[6], "--json")
    }

    // MARK: - Error propagation

    func test_generatePropagatesNonZeroExit() async {
        let fake = FakeCLIRunner(error: CLIRunnerError.nonZeroExit(code: 1, stderr: "boom"))
        let svc = MeetingRecapService(runner: fake)
        do {
            try await svc.generate(eventID: "evt-1", text: "raw")
            XCTFail("expected throw")
        } catch CLIRunnerError.nonZeroExit(let code, _) {
            XCTAssertEqual(code, 1)
        } catch {
            XCTFail("unexpected error type: \(error)")
        }
    }
}
