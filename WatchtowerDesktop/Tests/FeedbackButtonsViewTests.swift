import XCTest
import SwiftUI
import GRDB
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class FeedbackButtonsViewTests: XCTestCase {

    // MARK: - Helpers

    private func makeView() throws -> (FeedbackButtons, DatabaseManager, String) {
        let (db, path) = try TestDatabase.createDatabaseManager()
        let view = FeedbackButtons(
            entityType: "digest",
            entityID: "42",
            dbManager: db
        )
        return (view, db, path)
    }

    /// Опрашиваем БД до timeout — Task внутри submitFeedback пишет асинхронно.
    private func waitForFeedback(
        _ db: DatabaseManager,
        entityID: String,
        timeout: TimeInterval = 1.0
    ) async throws -> Feedback? {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            // В async-контексте Swift выбирает async-overload `read`, которому нужен await.
            let fb = try await db.dbPool.read { db in
                try FeedbackQueries.getFeedback(db, entityType: "digest", entityID: entityID)
            }
            if fb != nil { return fb }
            try await Task.sleep(nanoseconds: 20_000_000)  // 20ms
        }
        return nil
    }

    // MARK: - Tests

    /// Рендерятся обе кнопки и у них правильные подсказки.
    func testBothButtonsPresent() throws {
        let (view, _, path) = try makeView()
        defer { TestDatabase.cleanup(path: path) }

        let buttons = try view.inspect().findAll(ViewType.Button.self)
        XCTAssertEqual(buttons.count, 2, "Expected thumbs up + thumbs down")

        let helps = try buttons.map { try $0.help().string() }
        XCTAssertTrue(helps.contains("Good result"))
        XCTAssertTrue(helps.contains("Bad result"))
    }

    /// Тап «Good result» (thumbs up) → запись в БД с rating=+1.
    func testTapThumbsUpWritesPositiveFeedback() async throws {
        let (view, db, path) = try makeView()
        defer { TestDatabase.cleanup(path: path) }

        let upButton = try view.inspect().find(button: "Good result")
        try upButton.tap()

        let fb = try await waitForFeedback(db, entityID: "42")
        XCTAssertNotNil(fb, "Feedback row must be persisted after tap")
        XCTAssertEqual(fb?.rating, 1)
    }

    /// Тап «Bad result» (thumbs down) → запись в БД с rating=-1.
    func testTapThumbsDownWritesNegativeFeedback() async throws {
        let (view, db, path) = try makeView()
        defer { TestDatabase.cleanup(path: path) }

        let downButton = try view.inspect().find(button: "Bad result")
        try downButton.tap()

        let fb = try await waitForFeedback(db, entityID: "42")
        XCTAssertNotNil(fb)
        XCTAssertEqual(fb?.rating, -1)
    }
}
