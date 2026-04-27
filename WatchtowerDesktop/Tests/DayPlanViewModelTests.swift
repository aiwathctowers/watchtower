import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - DayPlanViewModelTests

@MainActor
final class DayPlanViewModelTests: XCTestCase {

    var pool: DatabaseQueue!
    var cli: FakeCLIRunner!
    var vm: DayPlanViewModel!

    override func setUp() async throws {
        pool = try TestDatabase.create()
        cli = FakeCLIRunner()
        vm = DayPlanViewModel(databasePool: pool, cliRunner: cli)
    }

    override func tearDown() async throws {
        vm = nil
        cli = nil
        pool = nil
    }

    // MARK: - loadFor: no plan

    func testLoadForDateNoPlan() async throws {
        await vm.loadFor(date: "2026-04-23")
        XCTAssertNil(vm.plan)
        XCTAssertTrue(vm.items.isEmpty)
    }

    // MARK: - loadFor: existing plan

    func testLoadForDateWithPlan() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Write tests")
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "timeblock",
                                               sourceType: "calendar", title: "Standup",
                                               startTime: "2026-04-23T09:00:00Z",
                                               endTime: "2026-04-23T09:30:00Z")
        }

        await vm.loadFor(date: "2026-04-23")

        XCTAssertNotNil(vm.plan)
        XCTAssertEqual(vm.plan?.planDate, "2026-04-23")
        XCTAssertEqual(vm.items.count, 2)
    }

    // MARK: - timeblocks / backlogItems computed properties

    func testTimeblocksAndBacklogItemsFiltered() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Backlog A", orderIndex: 1)
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Backlog B", orderIndex: 0)
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "timeblock",
                                               sourceType: "manual", title: "Block X",
                                               startTime: "2026-04-23T10:00:00Z",
                                               endTime: "2026-04-23T11:00:00Z")
        }

        await vm.loadFor(date: "2026-04-23")

        XCTAssertEqual(vm.timeblocks.count, 1)
        XCTAssertEqual(vm.timeblocks.first?.title, "Block X")
        XCTAssertEqual(vm.backlogItems.count, 2)
        // Sorted by orderIndex ascending
        XCTAssertEqual(vm.backlogItems.map(\.title), ["Backlog B", "Backlog A"])
    }

    // MARK: - progress

    func testProgress() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Done", status: "done")
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Pending")
        }

        await vm.loadFor(date: "2026-04-23")

        XCTAssertEqual(vm.progress.done, 1)
        XCTAssertEqual(vm.progress.total, 2)
    }

    // MARK: - markDone cascades to task

    func testCascadeMarkDone() async throws {
        try await pool.write { db in
            try db.execute(sql: """
                INSERT INTO tasks (id, text, intent, status, priority, ownership, tags, sub_items, created_at, updated_at)
                VALUES (42, 'T', '', 'todo', 'medium', 'mine', '[]', '[]', datetime('now'), datetime('now'))
                """)
        }
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "task", sourceID: "42", title: "T")
        }

        await vm.loadFor(date: "2026-04-23")
        XCTAssertEqual(vm.items.count, 1)

        await vm.markDone(vm.items.first!)

        let taskStatus: String = try await pool.read { db in
            try String.fetchOne(db, sql: "SELECT status FROM tasks WHERE id=42") ?? ""
        }
        XCTAssertEqual(taskStatus, "done")
        XCTAssertEqual(vm.items.first?.status, .done)
        XCTAssertNil(vm.generationError)
    }

    // MARK: - markPending resets task to 'todo'

    func testCascadeMarkPending() async throws {
        try await pool.write { db in
            try db.execute(sql: """
                INSERT INTO tasks (id, text, intent, status, priority, ownership, tags, sub_items, created_at, updated_at)
                VALUES (10, 'Task', '', 'done', 'medium', 'mine', '[]', '[]', datetime('now'), datetime('now'))
                """)
        }
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "task", sourceID: "10",
                                               title: "Task", status: "done")
        }

        await vm.loadFor(date: "2026-04-23")
        await vm.markPending(vm.items.first!)

        let taskStatus: String = try await pool.read { db in
            try String.fetchOne(db, sql: "SELECT status FROM tasks WHERE id=10") ?? ""
        }
        XCTAssertEqual(taskStatus, "todo")
        XCTAssertEqual(vm.items.first?.status, .pending)
    }

    // MARK: - delete skips calendar items

    func testDeleteCalendarItemIsNoop() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "timeblock",
                                               sourceType: "calendar", title: "Meeting")
        }

        await vm.loadFor(date: "2026-04-23")
        XCTAssertEqual(vm.items.count, 1)
        let calItem = vm.items.first!

        await vm.delete(calItem)

        // isReadOnly guard fires before DB call — item survives
        XCTAssertEqual(vm.items.count, 1)
    }

    func testDeleteManualItem() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "Removable")
        }

        await vm.loadFor(date: "2026-04-23")
        XCTAssertEqual(vm.items.count, 1)

        await vm.delete(vm.items.first!)

        XCTAssertTrue(vm.items.isEmpty)
    }

    // MARK: - reorderBacklog

    func testReorderBacklog() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        let id1 = try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "A", orderIndex: 0)
        }
        let id2 = try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "B", orderIndex: 1)
        }
        let id3 = try await pool.write { db in
            try TestDatabase.insertDayPlanItem(db, dayPlanID: planId, kind: "backlog",
                                               sourceType: "manual", title: "C", orderIndex: 2)
        }

        await vm.loadFor(date: "2026-04-23")
        await vm.reorderBacklog([id3, id1, id2])

        XCTAssertEqual(vm.backlogItems.map(\.title), ["C", "A", "B"])
    }

    // MARK: - addManual

    func testAddManualItem() async throws {
        let planId = try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }
        _ = planId

        await vm.loadFor(date: "2026-04-23")
        XCTAssertTrue(vm.items.isEmpty)

        await vm.addManual(kind: .backlog, title: "New Task", startTime: nil, endTime: nil)

        XCTAssertEqual(vm.items.count, 1)
        XCTAssertEqual(vm.items.first?.title, "New Task")
        XCTAssertEqual(vm.items.first?.kind, .backlog)
    }

    // MARK: - regenerate shells to CLI

    func testRegenerateShellsToCLI() async throws {
        try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }

        await vm.loadFor(date: "2026-04-23")
        await vm.regenerate(feedback: "разгрузи вечер")

        XCTAssertEqual(cli.invocations, [["day-plan", "generate", "--feedback", "разгрузи вечер", "--date", "2026-04-23"]])
        XCTAssertFalse(vm.isGenerating)
        XCTAssertNil(vm.generationError)
    }

    func testRegenerateWithoutFeedback() async throws {
        try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }

        await vm.loadFor(date: "2026-04-23")
        await vm.regenerate(feedback: nil)

        XCTAssertEqual(cli.invocations, [["day-plan", "generate", "--date", "2026-04-23"]])
    }

    func testRegenerateWithEmptyFeedbackOmitsFeedbackFlag() async throws {
        try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23")
        }

        await vm.loadFor(date: "2026-04-23")
        await vm.regenerate(feedback: "")

        XCTAssertEqual(cli.invocations, [["day-plan", "generate", "--date", "2026-04-23"]])
    }

    // MARK: - regenerate CLI error sets generationError

    func testRegenerateCLIErrorSetsGenerationError() async throws {
        cli.shouldThrow = CLIRunnerError.nonZeroExit(code: 1, stderr: "something went wrong")

        await vm.loadFor(date: "2026-04-23")
        await vm.regenerate(feedback: nil)

        XCTAssertFalse(vm.isGenerating)
        XCTAssertNotNil(vm.generationError)
    }

    // MARK: - reset shells to CLI

    func testResetShellsToCLI() async throws {
        await vm.loadFor(date: "2026-04-23")
        await vm.reset()

        XCTAssertEqual(cli.invocations, [["day-plan", "reset", "2026-04-23"]])
        XCTAssertFalse(vm.isGenerating)
    }

    // MARK: - checkConflicts shells to CLI

    func testCheckConflictsShellsToCLI() async throws {
        await vm.loadFor(date: "2026-04-23")
        await vm.checkConflicts()

        XCTAssertEqual(cli.invocations, [["day-plan", "check-conflicts", "2026-04-23"]])
        XCTAssertFalse(vm.isGenerating)
    }

    // MARK: - hasConflicts reflects plan state

    func testHasConflictsReflectsPlan() async throws {
        try await pool.write { db in
            try TestDatabase.insertDayPlan(db, userID: "U1", planDate: "2026-04-23",
                                           hasConflicts: true,
                                           conflictSummary: "Overlapping meetings at 10am")
        }

        await vm.loadFor(date: "2026-04-23")

        XCTAssertTrue(vm.hasConflicts)
    }
}
