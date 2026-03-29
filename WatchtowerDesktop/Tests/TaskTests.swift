import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TaskModelTests: XCTestCase {

    // MARK: - Init / Defaults

    func testDefaultValues() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.text, "Review PR")
        XCTAssertEqual(task.status, "todo")
        XCTAssertEqual(task.priority, "medium")
        XCTAssertEqual(task.ownership, "mine")
        XCTAssertEqual(task.sourceType, "manual")
        XCTAssertTrue(task.dueDate.isEmpty)
        XCTAssertTrue(task.ballOn.isEmpty)
    }

    // MARK: - isActive

    func testIsActiveTodo() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "todo") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.isActive)
    }

    func testIsActiveInProgress() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "in_progress") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.isActive)
    }

    func testIsActiveBlocked() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "blocked") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.isActive)
    }

    func testIsActiveDone() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "done") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertFalse(task.isActive)
    }

    func testIsActiveDismissed() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "dismissed") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertFalse(task.isActive)
    }

    // MARK: - isOverdue

    func testIsOverdueYesterday() throws {
        let db = try TestDatabase.create()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTask($0, dueDate: fmt.string(from: yesterday)) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.isOverdue)
    }

    func testIsNotOverdueTomorrow() throws {
        let db = try TestDatabase.create()
        let tomorrow = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: 1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTask($0, dueDate: fmt.string(from: tomorrow)) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertFalse(task.isOverdue)
    }

    func testIsNotOverdueWhenDone() throws {
        let db = try TestDatabase.create()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTask($0, status: "done", dueDate: fmt.string(from: yesterday)) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertFalse(task.isOverdue)
    }

    func testIsNotOverdueNoDueDate() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertFalse(task.isOverdue)
    }

    // MARK: - isDueToday

    func testIsDueToday() throws {
        let db = try TestDatabase.create()
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTask($0, dueDate: fmt.string(from: Date())) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.isDueToday)
    }

    // MARK: - Priority

    func testPriorityOrder() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, priority: "high") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.priorityOrder, 0)
    }

    func testPriorityOrderLow() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, priority: "low") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.priorityOrder, 2)
    }

    // MARK: - Status Display

    func testStatusIcon() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "done") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.statusIcon, "checkmark.circle.fill")
    }

    func testStatusColor() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, status: "blocked") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.statusColor, "red")
    }

    // MARK: - JSON Decoders

    func testDecodedTags() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, tags: #"["urgent","backend"]"#) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.decodedTags, ["urgent", "backend"])
    }

    func testDecodedTagsEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertTrue(task.decodedTags.isEmpty)
    }

    func testDecodedSubItems() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Step 1","done":false},{"text":"Step 2","done":true}]"#
        try db.write { try TestDatabase.insertTask($0, subItems: json) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        let items = task.decodedSubItems
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].text, "Step 1")
        XCTAssertFalse(items[0].done)
        XCTAssertEqual(items[1].text, "Step 2")
        XCTAssertTrue(items[1].done)
    }

    func testSubItemsProgress() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"A","done":true},{"text":"B","done":false},{"text":"C","done":true}]"#
        try db.write { try TestDatabase.insertTask($0, subItems: json) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.subItemsProgress, "2/3")
    }

    func testSubItemsProgressNil() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertNil(task.subItemsProgress)
    }

    // MARK: - Source Helpers

    func testSourceTrackID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, sourceType: "track", sourceID: "42") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.sourceTrackID, 42)
        XCTAssertNil(task.sourceDigestID)
        XCTAssertNil(task.sourceBriefingID)
    }

    func testSourceDigestID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, sourceType: "digest", sourceID: "7") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.sourceDigestID, 7)
        XCTAssertNil(task.sourceTrackID)
    }

    func testSourceBriefingID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, sourceType: "briefing", sourceID: "3") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertEqual(task.sourceBriefingID, 3)
    }

    func testSourceManualReturnsNil() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertNil(task.sourceTrackID)
        XCTAssertNil(task.sourceDigestID)
        XCTAssertNil(task.sourceBriefingID)
    }

    // MARK: - Date Helpers

    func testDueDateFormatted() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, dueDate: "2026-03-25") }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertNotNil(task.dueDateFormatted)
    }

    func testDueDateFormattedEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        let task = try XCTUnwrap(db.read { try TaskItem.fetchOne($0, sql: "SELECT * FROM tasks LIMIT 1") })
        XCTAssertNil(task.dueDateFormatted)
    }
}

// MARK: - TaskQueries Tests

final class TaskQueryTests: XCTestCase {

    // MARK: - fetchAll + ordering

    func testFetchAllDefaultExcludesDone() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Active", status: "todo")
            try TestDatabase.insertTask(db, text: "Done", status: "done")
            try TestDatabase.insertTask(db, text: "Dismissed", status: "dismissed")
            try TestDatabase.insertTask(db, text: "Snoozed", status: "snoozed")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0) }
        // Snoozed tasks are visible (not excluded), only done/dismissed hidden
        XCTAssertEqual(tasks.count, 2)
        let texts = tasks.map(\.text)
        XCTAssertTrue(texts.contains("Active"))
        XCTAssertTrue(texts.contains("Snoozed"))
    }

    func testFetchAllIncludeDone() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Active", status: "todo")
            try TestDatabase.insertTask(db, text: "Done", status: "done")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0, includeDone: true) }
        XCTAssertEqual(tasks.count, 2)
    }

    func testFetchAllFilterByStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Todo", status: "todo")
            try TestDatabase.insertTask(db, text: "Blocked", status: "blocked")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0, status: "blocked") }
        XCTAssertEqual(tasks.count, 1)
        XCTAssertEqual(tasks[0].text, "Blocked")
    }

    func testFetchAllFilterByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "High", priority: "high")
            try TestDatabase.insertTask(db, text: "Low", priority: "low")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0, priority: "high") }
        XCTAssertEqual(tasks.count, 1)
        XCTAssertEqual(tasks[0].text, "High")
    }

    func testFetchAllFilterByOwnership() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Mine", ownership: "mine")
            try TestDatabase.insertTask(db, text: "Watching", ownership: "watching")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0, ownership: "mine") }
        XCTAssertEqual(tasks.count, 1)
        XCTAssertEqual(tasks[0].text, "Mine")
    }

    func testFetchAllOrderByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Low", priority: "low")
            try TestDatabase.insertTask(db, text: "High", priority: "high")
            try TestDatabase.insertTask(db, text: "Medium", priority: "medium")
        }
        let tasks = try db.read { try TaskQueries.fetchAll($0) }
        XCTAssertEqual(tasks[0].text, "High")
        XCTAssertEqual(tasks[1].text, "Medium")
        XCTAssertEqual(tasks[2].text, "Low")
    }

    // MARK: - fetchCounts

    func testFetchCounts() throws {
        let db = try TestDatabase.create()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { db in
            try TestDatabase.insertTask(db, text: "Active1", status: "todo")
            try TestDatabase.insertTask(db, text: "Active2", status: "in_progress")
            try TestDatabase.insertTask(db, text: "Overdue", status: "todo", dueDate: fmt.string(from: yesterday))
            try TestDatabase.insertTask(db, text: "Done", status: "done")
        }
        let counts = try db.read { try TaskQueries.fetchCounts($0) }
        XCTAssertEqual(counts.active, 3)
        XCTAssertEqual(counts.overdue, 1)
    }

    // MARK: - create + fetchByID

    func testCreateAndFetchByID() throws {
        let db = try TestDatabase.create()
        let taskID = try db.write { db in
            try TaskQueries.create(
                db,
                text: "New task",
                intent: "Get it done",
                priority: "high",
                sourceType: "track",
                sourceID: "5"
            )
        }
        let task = try XCTUnwrap(db.read { try TaskQueries.fetchByID($0, id: Int(taskID)) })
        XCTAssertEqual(task.text, "New task")
        XCTAssertEqual(task.intent, "Get it done")
        XCTAssertEqual(task.priority, "high")
        XCTAssertEqual(task.sourceType, "track")
        XCTAssertEqual(task.sourceID, "5")
        XCTAssertEqual(task.status, "todo")
    }

    // MARK: - updateStatus

    func testUpdateStatus() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        try db.write { try TaskQueries.updateStatus($0, id: 1, status: "done") }
        let task = try XCTUnwrap(db.read { try TaskQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(task.status, "done")
    }

    // MARK: - updatePriority

    func testUpdatePriority() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0, priority: "medium") }
        try db.write { try TaskQueries.updatePriority($0, id: 1, priority: "high") }
        let task = try XCTUnwrap(db.read { try TaskQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(task.priority, "high")
    }

    // MARK: - snooze

    func testSnooze() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        try db.write { try TaskQueries.snooze($0, id: 1, until: "2026-04-01") }
        let task = try XCTUnwrap(db.read { try TaskQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(task.status, "snoozed")
        XCTAssertEqual(task.snoozeUntil, "2026-04-01")
    }

    // MARK: - fetchBySourceRef

    func testFetchBySourceRef() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTask(db, text: "From track", sourceType: "track", sourceID: "10")
            try TestDatabase.insertTask(db, text: "Manual", sourceType: "manual")
        }
        let tasks = try db.read { try TaskQueries.fetchBySourceRef($0, sourceType: "track", sourceID: "10") }
        XCTAssertEqual(tasks.count, 1)
        XCTAssertEqual(tasks[0].text, "From track")
    }

    // MARK: - delete

    func testDelete() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTask($0) }
        try db.write { try TaskQueries.delete($0, id: 1) }
        let task = try db.read { try TaskQueries.fetchByID($0, id: 1) }
        XCTAssertNil(task)
    }

    // MARK: - updateSubItems

    func testUpdateSubItems() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Step 1","done":false}]"#
        try db.write { try TestDatabase.insertTask($0, subItems: json) }
        let newJSON = #"[{"text":"Step 1","done":true}]"#
        try db.write { try TaskQueries.updateSubItems($0, id: 1, subItems: newJSON) }
        let task = try XCTUnwrap(db.read { try TaskQueries.fetchByID($0, id: 1) })
        let items = task.decodedSubItems
        XCTAssertEqual(items.count, 1)
        XCTAssertTrue(items[0].done)
    }
}

// MARK: - Briefing Model Extension Tests

final class BriefingTaskIntegrationTests: XCTestCase {

    func testYourDayItemWithTaskID() throws {
        let json = #"[{"text":"Review PR","task_id":5,"priority":"high"}]"#
        guard let data = json.data(using: .utf8) else {
            XCTFail("Failed to encode JSON")
            return
        }
        let items = try JSONDecoder().decode([YourDayItem].self, from: data)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].taskID, 5)
        XCTAssertEqual(items[0].text, "Review PR")
    }

    func testYourDayItemWithoutTaskID() throws {
        let json = #"[{"text":"Review PR","track_id":3}]"#
        guard let data = json.data(using: .utf8) else {
            XCTFail("Failed to encode JSON")
            return
        }
        let items = try JSONDecoder().decode([YourDayItem].self, from: data)
        XCTAssertEqual(items.count, 1)
        XCTAssertNil(items[0].taskID)
        XCTAssertEqual(items[0].trackID, 3)
    }

    func testAttentionItemWithSuggestTask() throws {
        let json = #"[{"text":"Deadline approaching","suggest_task":true,"suggest_track":false}]"#
        guard let data = json.data(using: .utf8) else {
            XCTFail("Failed to encode JSON")
            return
        }
        let items = try JSONDecoder().decode([AttentionItem].self, from: data)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].suggestTask, true)
        XCTAssertEqual(items[0].suggestTrack, false)
    }

    func testAttentionItemWithoutSuggestTask() throws {
        let json = #"[{"text":"Check this"}]"#
        guard let data = json.data(using: .utf8) else {
            XCTFail("Failed to encode JSON")
            return
        }
        let items = try JSONDecoder().decode([AttentionItem].self, from: data)
        XCTAssertEqual(items.count, 1)
        XCTAssertNil(items[0].suggestTask)
    }
}

// MARK: - TasksViewModel Tests

final class TasksViewModelTests: XCTestCase {

    @MainActor
    func testLoadSplitsTodayAndAll() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        let todayStr = fmt.string(from: Date())
        let yesterdayStr = fmt.string(from: yesterday)

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "Overdue", dueDate: yesterdayStr)
            try TestDatabase.insertTask(db, text: "Due today", dueDate: todayStr)
            try TestDatabase.insertTask(db, text: "High priority", priority: "high")
            try TestDatabase.insertTask(db, text: "Normal task", priority: "low")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()

        // Today: overdue + due today + high priority
        XCTAssertEqual(vm.todayTasks.count, 3)
        // All: the rest
        XCTAssertEqual(vm.allTasks.count, 1)
        XCTAssertEqual(vm.allTasks[0].text, "Normal task")
    }

    @MainActor
    func testMarkDone() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "Task 1")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()
        XCTAssertEqual(vm.activeCount, 1)

        let task = try XCTUnwrap(vm.todayTasks.first ?? vm.allTasks.first)
        vm.markDone(task)
        XCTAssertEqual(vm.activeCount, 0)
    }

    @MainActor
    func testDismiss() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "Task 1")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()

        let task = try XCTUnwrap(vm.todayTasks.first ?? vm.allTasks.first)
        vm.dismiss(task)
        XCTAssertEqual(vm.activeCount, 0)
    }

    @MainActor
    func testSnooze() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "Task 1")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()

        let task = try XCTUnwrap(vm.todayTasks.first ?? vm.allTasks.first)
        vm.snooze(task, until: "2026-04-01")
        XCTAssertEqual(vm.activeCount, 0)
    }

    @MainActor
    func testToggleSubItem() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        let json = #"[{"text":"Step 1","done":false},{"text":"Step 2","done":false}]"#
        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "With subs", subItems: json)
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()

        let task = try XCTUnwrap(vm.todayTasks.first ?? vm.allTasks.first)
        vm.toggleSubItem(task, index: 0)

        let updated = try XCTUnwrap(vm.itemByID(task.id))
        XCTAssertTrue(updated.decodedSubItems[0].done)
        XCTAssertFalse(updated.decodedSubItems[1].done)
    }

    @MainActor
    func testDeleteTask() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "To delete")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.load()

        let task = try XCTUnwrap(vm.todayTasks.first ?? vm.allTasks.first)
        vm.deleteTask(task)
        XCTAssertEqual(vm.activeCount, 0)
        XCTAssertNil(vm.itemByID(task.id))
    }

    @MainActor
    func testShowDoneFilter() throws {
        let (dbManager, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }

        try dbManager.dbPool.write { db in
            try TestDatabase.insertTask(db, text: "Active", status: "todo")
            try TestDatabase.insertTask(db, text: "Done", status: "done")
        }

        let vm = TasksViewModel(dbManager: dbManager)
        vm.showDone = true
        vm.load()

        let allTexts = (vm.todayTasks + vm.allTasks).map(\.text)
        XCTAssertTrue(allTexts.contains("Done"))
        XCTAssertTrue(allTexts.contains("Active"))
    }
}
