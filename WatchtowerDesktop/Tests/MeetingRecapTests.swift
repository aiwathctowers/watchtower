import XCTest
@testable import WatchtowerDesktop

final class MeetingRecapTests: XCTestCase {
    func test_parsedReturnsContentForValidJSON() throws {
        let recap = MeetingRecap(
            eventID: "evt-1",
            sourceText: "raw",
            recapJSON: """
            {
              "summary": "Talked about Q3.",
              "key_decisions": ["ship friday"],
              "action_items": ["draft post"],
              "open_questions": ["pricing?"]
            }
            """,
            createdAt: "2026-04-27T10:00:00Z",
            updatedAt: "2026-04-27T10:00:00Z"
        )
        let parsed = try XCTUnwrap(recap.parsed)
        XCTAssertEqual(parsed.summary, "Talked about Q3.")
        XCTAssertEqual(parsed.keyDecisions, ["ship friday"])
        XCTAssertEqual(parsed.actionItems, ["draft post"])
        XCTAssertEqual(parsed.openQuestions, ["pricing?"])
    }

    func test_parsedReturnsNilForMalformedJSON() {
        let recap = MeetingRecap(
            eventID: "x", sourceText: "", recapJSON: "not json",
            createdAt: "", updatedAt: ""
        )
        XCTAssertNil(recap.parsed)
    }

    func test_parsedDecodesSnakeCaseKeys() throws {
        let json = """
        {"summary":"s","key_decisions":[],"action_items":[],"open_questions":[]}
        """
        let recap = MeetingRecap(eventID: "x", sourceText: "", recapJSON: json,
                                 createdAt: "", updatedAt: "")
        let parsed = try XCTUnwrap(recap.parsed)
        XCTAssertEqual(parsed.summary, "s")
        XCTAssertTrue(parsed.keyDecisions.isEmpty)
    }
}
