import XCTest
import SwiftUI
import GRDB
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class WhoToPingViewTests: XCTestCase {

    // MARK: - Helpers

    private func makePool() throws -> (DatabasePool, String) {
        let (db, path) = try TestDatabase.createDatabaseManager()
        return (db.dbPool, path)
    }

    private func makeTarget(
        id: String = "U1",
        name: String = "Alice",
        reason: String = "expert"
    ) -> PingTargetItem {
        PingTargetItem(slackUserID: id, displayName: name, reason: reason)
    }

    // MARK: - Tests

    /// Пустой список targets → EmptyView; ни заголовка "Who to Ping",
    /// ни кнопки Slack в дереве нет.
    func testEmptyTargetsRendersEmptyView() throws {
        let (pool, path) = try makePool()
        defer { TestDatabase.cleanup(path: path) }

        let view = WhoToPingView(targets: [], dbPool: pool)
        XCTAssertThrowsError(try view.inspect().find(text: "Who to Ping"))
    }

    /// Заголовок секции виден, когда targets непуст.
    func testHeaderShownForNonEmpty() throws {
        let (pool, path) = try makePool()
        defer { TestDatabase.cleanup(path: path) }

        let view = WhoToPingView(targets: [makeTarget()], dbPool: pool)
        XCTAssertNoThrow(try view.inspect().find(text: "Who to Ping"))
    }

    /// Имя и reasonLabel ("Expert") рендерятся в строке.
    func testTargetNameAndReasonShown() throws {
        let (pool, path) = try makePool()
        defer { TestDatabase.cleanup(path: path) }

        let view = WhoToPingView(
            targets: [makeTarget(name: "Alice", reason: "expert")],
            dbPool: pool
        )
        XCTAssertNoThrow(try view.inspect().find(text: "Alice"))
        XCTAssertNoThrow(try view.inspect().find(text: "Expert"))
    }

    /// Не более 3 строк (prefix(3)) — ровно 3 кнопки Slack даже при 5 targets.
    func testRendersAtMostThreeTargets() throws {
        let (pool, path) = try makePool()
        defer { TestDatabase.cleanup(path: path) }

        let targets = (1...5).map { makeTarget(id: "U\($0)", name: "User \($0)") }
        let view = WhoToPingView(targets: targets, dbPool: pool)

        let buttons = try view.inspect().findAll(ViewType.Button.self)
        XCTAssertEqual(buttons.count, 3, "WhoToPingView should cap visible rows at 3")
    }

    /// Reason mapping в reasonLabel: assignee → "Assignee", unknown → capitalized.
    func testUnknownReasonCapitalized() throws {
        let (pool, path) = try makePool()
        defer { TestDatabase.cleanup(path: path) }

        let view = WhoToPingView(
            targets: [makeTarget(reason: "weirdo")],
            dbPool: pool
        )
        XCTAssertNoThrow(try view.inspect().find(text: "Weirdo"))
    }
}
