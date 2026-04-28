import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TargetPrefillBuilderTests: XCTestCase {

    // MARK: - fromSubItem

    func testFromSubItem_BasicShape() {
        let parent = Self.makeTarget(
            id: 42,
            text: "Ship the rewrite",
            intent: "Unblock Q2 launch",
            subItems: [
                TargetSubItem(text: "Draft RFC", done: false),
                TargetSubItem(text: "Review with team", done: false),
                TargetSubItem(text: "Implement", done: false)
            ]
        )
        let subItem = parent.decodedSubItems[0]
        let prefill = TargetPrefillBuilder.fromSubItem(parent: parent, subItem: subItem, index: 0)

        XCTAssertEqual(prefill.text, "Draft RFC")
        XCTAssertEqual(prefill.sourceType, "promoted_subitem")
        XCTAssertEqual(prefill.sourceID, "42:0")
        XCTAssertEqual(prefill.parentID, 42)
        XCTAssertTrue(prefill.intent.contains("Sub-target of #42"))
        XCTAssertTrue(prefill.intent.contains("«Ship the rewrite»"))
        XCTAssertTrue(prefill.intent.contains("Unblock Q2 launch"))
        XCTAssertTrue(prefill.intent.contains("Review with team"))
        XCTAssertTrue(prefill.intent.contains("Implement"))
        XCTAssertTrue(prefill.secondaryLinks.isEmpty)
    }

    func testFromSubItem_NoSiblings_NoIntent() {
        let parent = Self.makeTarget(
            id: 7,
            text: "Lone target",
            intent: "",
            subItems: [TargetSubItem(text: "Only one", done: false)]
        )
        let subItem = parent.decodedSubItems[0]
        let prefill = TargetPrefillBuilder.fromSubItem(parent: parent, subItem: subItem, index: 0)

        XCTAssertTrue(prefill.intent.contains("Sub-target of #7 «Lone target»."))
        XCTAssertFalse(prefill.intent.contains("Parent context:"))
        XCTAssertFalse(prefill.intent.contains("Sibling sub-items:"))
    }

    // MARK: - Helpers

    static func makeTarget(
        id: Int,
        text: String,
        intent: String = "",
        level: String = "week",
        priority: String = "medium",
        ownership: String = "mine",
        dueDate: String = "",
        subItems: [TargetSubItem] = []
    ) -> Target {
        let subItemsJSON: String = {
            guard !subItems.isEmpty,
                  let data = try? JSONEncoder().encode(subItems),
                  let json = String(data: data, encoding: .utf8) else { return "[]" }
            return json
        }()
        let row: Row = [
            "id": id,
            "text": text,
            "intent": intent,
            "level": level,
            "priority": priority,
            "ownership": ownership,
            "due_date": dueDate,
            "sub_items": subItemsJSON,
            "period_start": "2026-04-20",
            "period_end": "2026-04-26",
            "status": "todo",
            "ball_on": "",
            "snooze_until": "",
            "blocking": "",
            "tags": "[]",
            "notes": "[]",
            "progress": 0.0,
            "source_type": "manual",
            "source_id": "",
            "custom_label": "",
            "created_at": "2026-04-20T00:00:00Z",
            "updated_at": "2026-04-20T00:00:00Z"
        ]
        return Target(row: row)
    }
}
