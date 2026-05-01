import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class SlackUserPickerViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeUser(
        id: String,
        name: String,
        displayName: String = "",
        realName: String = ""
    ) -> User {
        let json = """
        {"id":"\(id)","name":"\(name)","display_name":"\(displayName)",
         "real_name":"\(realName)","email":"","is_bot":false,"is_deleted":false,
         "profile_json":"{}","updated_at":"2026-01-01T00:00:00Z"}
        """
        return try! JSONDecoder().decode(User.self, from: Data(json.utf8))
    }

    // MARK: - Tests

    /// Title виден.
    func testTitleRendered() throws {
        var ids: [String] = []
        let view = SlackUserPicker(
            title: "Stakeholders",
            allUsers: [],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "Stakeholders"))
    }

    /// Пустой selection — "None".
    func testEmptyShowsNone() throws {
        var ids: [String] = []
        let view = SlackUserPicker(
            title: "Pick",
            allUsers: [makeUser(id: "U1", name: "alice")],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "None"))
    }

    /// Выбранный юзер с display_name → bestName рендерится, а @name тоже.
    func testSelectedUserShowsDisplayNameAndHandle() throws {
        var ids: [String] = ["U1"]
        let view = SlackUserPicker(
            title: "Pick",
            allUsers: [
                makeUser(id: "U1", name: "alice", displayName: "Alice Wonder"),
            ],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "Alice Wonder"))
        XCTAssertNoThrow(try view.inspect().find(text: "@alice"))
    }

    /// Юзер без displayName/realName → bestName == name (без @-handle, потому что блок
    /// показывается, только если name заполнен).
    func testSelectedUserBestNameFallback() throws {
        var ids: [String] = ["U2"]
        let view = SlackUserPicker(
            title: "Pick",
            allUsers: [makeUser(id: "U2", name: "bob")],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        // bestName = name = "bob"
        XCTAssertNoThrow(try view.inspect().find(text: "bob"))
        XCTAssertNoThrow(try view.inspect().find(text: "@bob"))
    }
}
