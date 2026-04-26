import XCTest
import SwiftUI
@testable import WatchtowerDesktop

/// Smoke-coverage for `PromoteSubItemSheet` — verifies the view can be
/// constructed with realistic inputs and that its `body` does not crash
/// when SwiftUI renders the description tree. Behavioral logic
/// (override plumbing, CLI args, descending order) is exercised by
/// `TargetPromoteSubItemServiceTests` and `TargetsViewModelTests`.
@MainActor
final class PromoteSubItemSheetTests: XCTestCase {
    /// Inserts a parent target into a temp DB and returns it via the same
    /// fetch path the production UI uses.
    private func makeParent(
        mgr: DatabaseManager,
        dueDate: String = "",
        subItems: String = #"[{"text":"first","done":false}]"#
    ) async throws -> Target {
        _ = try await mgr.dbPool.write { db in
            try TestDatabase.insertTarget(
                db,
                text: "Parent target",
                intent: "the why",
                level: "week",
                periodStart: "2026-04-20",
                periodEnd: "2026-04-26",
                status: "in_progress",
                priority: "high",
                ownership: "mine",
                dueDate: dueDate,
                subItems: subItems
            )
        }
        let fetched = try await mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) }
        return try XCTUnwrap(fetched)
    }

    func testSheetInitializesWithSubItemDefaults() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let vm = TargetsViewModel(dbManager: mgr)
        let parent = try await makeParent(mgr: mgr)
        let subItem = TargetSubItem(text: "first", done: false, dueDate: nil)

        let sheet = PromoteSubItemSheet(
            parent: parent,
            subItem: subItem,
            subItemIndex: 0,
            viewModel: vm
        )

        // Touching `body` ensures the view's struct/state initializers do
        // not trap on the inheritance defaults.
        _ = sheet.body
    }

    func testSheetInitializesWithSubItemDueDateOverride() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let vm = TargetsViewModel(dbManager: mgr)
        let parent = try await makeParent(mgr: mgr)
        // Sub-item carries its own due date — sheet should pre-toggle the
        // due-date control instead of falling back to the parent's empty due_date.
        let subItem = TargetSubItem(text: "with date", done: false, dueDate: "2026-05-01T10:00")

        let sheet = PromoteSubItemSheet(
            parent: parent,
            subItem: subItem,
            subItemIndex: 0,
            viewModel: vm
        )
        _ = sheet.body
    }

    func testSheetInitializesWhenParentDueDatePresent() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let vm = TargetsViewModel(dbManager: mgr)
        let parent = try await makeParent(mgr: mgr, dueDate: "2026-04-30T17:00")
        let subItem = TargetSubItem(text: "x", done: false, dueDate: nil)

        let sheet = PromoteSubItemSheet(
            parent: parent,
            subItem: subItem,
            subItemIndex: 2,
            viewModel: vm
        )
        _ = sheet.body
    }
}
