import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class ChannelPickerViewTests: XCTestCase {

    // MARK: - Helpers

    /// Channel — Decodable; используем JSON как самый стабильный путь к фикстуре.
    private func makeChannel(id: String, name: String, type: String = "public") -> Channel {
        let json = """
        {"id":"\(id)","name":"\(name)","type":"\(type)","topic":"","purpose":"",
         "is_archived":false,"is_member":true,"dm_user_id":null,"num_members":5,
         "updated_at":"2026-01-01T00:00:00Z"}
        """
        return try! JSONDecoder().decode(Channel.self, from: Data(json.utf8))
    }

    // MARK: - Tests

    /// Title рендерится в заголовке.
    func testTitleRendered() throws {
        var ids: [String] = []
        let view = ChannelPicker(
            title: "Watch channels",
            allChannels: [],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "Watch channels"))
    }

    /// Пустой selection — placeholder "None".
    func testEmptySelectionShowsNonePlaceholder() throws {
        var ids: [String] = []
        let view = ChannelPicker(
            title: "Watch",
            allChannels: [makeChannel(id: "C1", name: "general")],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "None"))
    }

    /// Selected channel рендерится с префиксом #.
    func testSelectedChannelRenderedWithHash() throws {
        var ids: [String] = ["C1"]
        let view = ChannelPicker(
            title: "Watch",
            allChannels: [
                makeChannel(id: "C1", name: "general"),
                makeChannel(id: "C2", name: "random"),
            ],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "#general"))
        // random не выбран — не должен фигурировать в основном дереве (popover закрыт).
        XCTAssertThrowsError(try view.inspect().find(text: "#random"))
    }

    /// Если выбранного ID нет в allChannels — fallback на сам ID.
    func testUnknownIDFallsBackToRawID() throws {
        var ids: [String] = ["C_UNKNOWN"]
        let view = ChannelPicker(
            title: "Watch",
            allChannels: [],
            selectedIDs: Binding(get: { ids }, set: { ids = $0 })
        )
        XCTAssertNoThrow(try view.inspect().find(text: "#C_UNKNOWN"))
    }
}
