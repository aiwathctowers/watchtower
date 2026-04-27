import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - Target Model Tests

final class TargetModelTests: XCTestCase {

    // MARK: - Decode from row

    func testDecodeFromRow() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.text, "Ship the feature")
        XCTAssertEqual(target.level, "week")
        XCTAssertEqual(target.status, "todo")
        XCTAssertEqual(target.priority, "medium")
        XCTAssertEqual(target.ownership, "mine")
        XCTAssertEqual(target.periodStart, "2026-04-20")
        XCTAssertEqual(target.periodEnd, "2026-04-26")
        XCTAssertEqual(target.progress, 0.0)
        XCTAssertEqual(target.sourceType, "manual")
        XCTAssertTrue(target.dueDate.isEmpty)
        XCTAssertTrue(target.ballOn.isEmpty)
        XCTAssertNil(target.parentId)
        XCTAssertNil(target.aiLevelConfidence)
    }

    func testDecodeWithOptionalFields() throws {
        let db = try TestDatabase.create()
        try db.write {
            try TestDatabase.insertTarget(
                $0,
                text: "Q2 Goal",
                level: "quarter",
                customLabel: "",
                parentId: nil,
                aiLevelConfidence: 0.92
            )
        }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.level, "quarter")
        XCTAssertEqual(target.aiLevelConfidence ?? 0, 0.92, accuracy: 0.001)
    }

    // MARK: - isActive

    func testIsActiveTodo() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "todo") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.isActive)
    }

    func testIsActiveInProgress() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "in_progress") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.isActive)
    }

    func testIsActiveBlocked() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "blocked") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.isActive)
    }

    func testIsActiveDone() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "done") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isActive)
    }

    func testIsActiveDismissed() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "dismissed") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isActive)
    }

    func testIsActiveSnoozed() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, status: "snoozed") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isActive)
    }

    // MARK: - isOverdue

    func testIsOverdueYesterday() throws {
        let db = try TestDatabase.create()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTarget($0, dueDate: fmt.string(from: yesterday)) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.isOverdue)
    }

    func testIsNotOverdueTomorrow() throws {
        let db = try TestDatabase.create()
        let tomorrow = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: 1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTarget($0, dueDate: fmt.string(from: tomorrow)) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isOverdue)
    }

    func testIsNotOverdueWhenDone() throws {
        let db = try TestDatabase.create()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: Date()))
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTarget($0, status: "done", dueDate: fmt.string(from: yesterday)) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isOverdue)
    }

    func testIsNotOverdueNoDueDate() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertFalse(target.isOverdue)
    }

    // MARK: - isDueToday

    func testIsDueToday() throws {
        let db = try TestDatabase.create()
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        try db.write { try TestDatabase.insertTarget($0, dueDate: fmt.string(from: Date())) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.isDueToday)
    }

    // MARK: - Level order

    func testLevelOrder() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, level: "quarter") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.levelOrder, 0)
    }

    func testLevelOrderDay() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, level: "day") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.levelOrder, 3)
    }

    // MARK: - Priority order

    func testPriorityOrderHigh() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, priority: "high") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.priorityOrder, 0)
    }

    func testPriorityOrderLow() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, priority: "low") }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.priorityOrder, 2)
    }

    // MARK: - JSON decoders

    func testDecodedTags() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, tags: #"["urgent","backend"]"#) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.decodedTags, ["urgent", "backend"])
    }

    func testDecodedTagsEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertTrue(target.decodedTags.isEmpty)
    }

    func testDecodedSubItems() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Step 1","done":false},{"text":"Step 2","done":true}]"#
        try db.write { try TestDatabase.insertTarget($0, subItems: json) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        let items = target.decodedSubItems
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].text, "Step 1")
        XCTAssertFalse(items[0].done)
        XCTAssertEqual(items[1].text, "Step 2")
        XCTAssertTrue(items[1].done)
    }

    func testSubItemsProgress() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"A","done":true},{"text":"B","done":false},{"text":"C","done":true}]"#
        try db.write { try TestDatabase.insertTarget($0, subItems: json) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertEqual(target.subItemsProgress, "2/3")
    }

    func testSubItemsProgressNilWhenEmpty() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        XCTAssertNil(target.subItemsProgress)
    }

    func testDecodedNotes() throws {
        let db = try TestDatabase.create()
        let json = #"[{"text":"Note 1","created_at":"2026-04-23T10:00:00Z"}]"#
        try db.write { try TestDatabase.insertTarget($0, notes: json) }
        let target = try XCTUnwrap(db.read { try Target.fetchOne($0, sql: "SELECT * FROM targets LIMIT 1") })
        let notes = target.decodedNotes
        XCTAssertEqual(notes.count, 1)
        XCTAssertEqual(notes[0].text, "Note 1")
    }
}

// MARK: - TargetLink Model Tests

final class TargetLinkModelTests: XCTestCase {

    func testDecodeFromRow() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Parent")
            try TestDatabase.insertTarget(db, text: "Child")
            try TestDatabase.insertTargetLink(db, sourceTargetId: 1, targetTargetId: 2, relation: "contributes_to", confidence: 0.85, createdBy: "ai")
        }
        let link = try XCTUnwrap(db.read { try TargetLink.fetchOne($0, sql: "SELECT * FROM target_links LIMIT 1") })
        XCTAssertEqual(link.sourceTargetId, 1)
        XCTAssertEqual(link.targetTargetId, 2)
        XCTAssertEqual(link.relation, "contributes_to")
        XCTAssertEqual(link.confidence ?? 0, 0.85, accuracy: 0.001)
        XCTAssertEqual(link.createdBy, "ai")
        XCTAssertTrue(link.isAICreated)
        XCTAssertFalse(link.isExternalLink)
    }

    func testDecodeExternalLink() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Target A")
            try TestDatabase.insertTargetLink(
                db,
                sourceTargetId: 1,
                targetTargetId: nil,
                externalRef: "jira:PROJ-123",
                relation: "related",
                createdBy: "user"
            )
        }
        let link = try XCTUnwrap(db.read { try TargetLink.fetchOne($0, sql: "SELECT * FROM target_links LIMIT 1") })
        XCTAssertNil(link.targetTargetId)
        XCTAssertEqual(link.externalRef, "jira:PROJ-123")
        XCTAssertEqual(link.createdBy, "user")
        XCTAssertFalse(link.isAICreated)
        XCTAssertTrue(link.isExternalLink)
    }
}

// MARK: - TargetQueries Tests

final class TargetQueryTests: XCTestCase {

    // MARK: - fetchAll

    func testFetchAllDefaultExcludesDoneAndDismissed() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Active", status: "todo")
            try TestDatabase.insertTarget(db, text: "Done", status: "done")
            try TestDatabase.insertTarget(db, text: "Dismissed", status: "dismissed")
            try TestDatabase.insertTarget(db, text: "Snoozed", status: "snoozed")
        }
        let targets = try db.read { try TargetQueries.fetchAll($0) }
        // done and dismissed hidden by default; todo and snoozed visible
        XCTAssertEqual(targets.count, 2)
        let texts = Set(targets.map(\.text))
        XCTAssertTrue(texts.contains("Active"))
        XCTAssertTrue(texts.contains("Snoozed"))
    }

    func testFetchAllIncludeDone() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Active", status: "todo")
            try TestDatabase.insertTarget(db, text: "Done", status: "done")
        }
        var filter = TargetFilter()
        filter.includeDone = true
        let targets = try db.read { try TargetQueries.fetchAll($0, filter: filter) }
        XCTAssertEqual(targets.count, 2)
    }

    func testFetchAllFilterByLevel() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Weekly", level: "week")
            try TestDatabase.insertTarget(db, text: "Daily", level: "day")
        }
        var filter = TargetFilter()
        filter.level = "week"
        let targets = try db.read { try TargetQueries.fetchAll($0, filter: filter) }
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets[0].text, "Weekly")
    }

    func testFetchAllFilterByStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Todo", status: "todo")
            try TestDatabase.insertTarget(db, text: "Blocked", status: "blocked")
        }
        var filter = TargetFilter()
        filter.status = "blocked"
        let targets = try db.read { try TargetQueries.fetchAll($0, filter: filter) }
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets[0].text, "Blocked")
    }

    func testFetchAllFilterByPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "High", priority: "high")
            try TestDatabase.insertTarget(db, text: "Low", priority: "low")
        }
        var filter = TargetFilter()
        filter.priority = "high"
        let targets = try db.read { try TargetQueries.fetchAll($0, filter: filter) }
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets[0].text, "High")
    }

    func testFetchAllFilterBySearch() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Deploy backend")
            try TestDatabase.insertTarget(db, text: "Write docs")
        }
        var filter = TargetFilter()
        filter.search = "backend"
        let targets = try db.read { try TargetQueries.fetchAll($0, filter: filter) }
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets[0].text, "Deploy backend")
    }

    func testFetchAllSortedByLevelThenPriority() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Day Low", level: "day",
                                          periodStart: "2026-04-23", periodEnd: "2026-04-23", priority: "low")
            try TestDatabase.insertTarget(db, text: "Quarter High", level: "quarter",
                                          periodStart: "2026-01-01", periodEnd: "2026-03-31", priority: "high")
            try TestDatabase.insertTarget(db, text: "Week Medium", level: "week",
                                          periodStart: "2026-04-20", periodEnd: "2026-04-26", priority: "medium")
        }
        let targets = try db.read { try TargetQueries.fetchAll($0) }
        XCTAssertEqual(targets[0].text, "Quarter High")
        XCTAssertEqual(targets[1].text, "Week Medium")
        XCTAssertEqual(targets[2].text, "Day Low")
    }

    // MARK: - fetchByID

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, text: "Find me") }
        let target = try db.read { try TargetQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(target)
        XCTAssertEqual(target?.text, "Find me")
    }

    func testFetchByIDNotFound() throws {
        let db = try TestDatabase.create()
        let target = try db.read { try TargetQueries.fetchByID($0, id: 999) }
        XCTAssertNil(target)
    }

    // MARK: - fetchBySourceRef

    func testFetchBySourceRef() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "From briefing", sourceType: "briefing", sourceID: "5")
            try TestDatabase.insertTarget(db, text: "Manual")
        }
        let targets = try db.read { try TargetQueries.fetchBySourceRef($0, sourceType: "briefing", sourceID: "5") }
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets[0].text, "From briefing")
    }

    // MARK: - fetchCounts

    func testFetchCountsReturnsCorrectStructure() throws {
        let db = try TestDatabase.create()
        let now = Date()
        let yesterday = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: -1, to: now))
        let dateFmt = DateFormatter()
        dateFmt.dateFormat = "yyyy-MM-dd"
        dateFmt.locale = Locale(identifier: "en_US_POSIX")
        let yesterdayStr = dateFmt.string(from: yesterday)

        // DueToday must be strictly after `now` and strictly before midnight.
        // Pick 30 minutes from now, but if that crosses midnight, fall back to 23:59 today.
        let futureToday = now.addingTimeInterval(30 * 60)
        let sameDay = Calendar.current.isDate(futureToday, inSameDayAs: now)
        let dueTodayDate: Date = sameDay
            ? futureToday
            : try XCTUnwrap(Calendar.current.date(bySettingHour: 23, minute: 59, second: 0, of: now))
        let dtFmt = DateFormatter()
        dtFmt.dateFormat = "yyyy-MM-dd'T'HH:mm"
        dtFmt.locale = Locale(identifier: "en_US_POSIX")
        let dueTodayStr = dtFmt.string(from: dueTodayDate)
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Active1", status: "todo", priority: "medium")
            try TestDatabase.insertTarget(db, text: "Active2", status: "in_progress", priority: "high")
            try TestDatabase.insertTarget(db, text: "Overdue", status: "todo", priority: "medium", dueDate: yesterdayStr)
            try TestDatabase.insertTarget(db, text: "DueToday", status: "todo", priority: "low", dueDate: dueTodayStr)
            try TestDatabase.insertTarget(db, text: "Done", status: "done", priority: "high")
        }
        let counts = try db.read { try TargetQueries.fetchCounts($0) }
        XCTAssertEqual(counts.active, 4)        // Active1 + Active2 + Overdue + DueToday
        XCTAssertEqual(counts.overdue, 1)       // Overdue only (yesterday)
        XCTAssertEqual(counts.dueToday, 1)      // DueToday
        XCTAssertEqual(counts.highPriority, 1)  // Active2 (high priority, active)
    }

    // MARK: - create + fetchByID roundtrip

    func testCreateAndFetchByID() throws {
        let db = try TestDatabase.create()
        let newID = try db.write { db in
            try TargetQueries.create(
                db,
                text: "New target",
                intent: "Ship by Friday",
                level: "week",
                periodStart: "2026-04-20",
                periodEnd: "2026-04-26",
                priority: "high",
                sourceType: "extract",
                sourceID: "inbox:7"
            )
        }
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: newID) })
        XCTAssertEqual(target.text, "New target")
        XCTAssertEqual(target.intent, "Ship by Friday")
        XCTAssertEqual(target.level, "week")
        XCTAssertEqual(target.priority, "high")
        XCTAssertEqual(target.sourceType, "extract")
        XCTAssertEqual(target.sourceID, "inbox:7")
        XCTAssertEqual(target.status, "todo")
    }

    func testCreateWithParent() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Parent", level: "quarter",
                                          periodStart: "2026-01-01", periodEnd: "2026-03-31")
        }
        let childID = try db.write { db in
            try TargetQueries.create(
                db,
                text: "Child",
                level: "week",
                periodStart: "2026-01-06",
                periodEnd: "2026-01-12",
                parentId: 1
            )
        }
        let child = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: childID) })
        XCTAssertEqual(child.parentId, 1)
    }

    // MARK: - updateStatus

    func testUpdateStatus() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        try db.write { try TargetQueries.updateStatus($0, id: 1, status: "done") }
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(target.status, "done")
    }

    // MARK: - updatePriority

    func testUpdatePriority() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0, priority: "medium") }
        try db.write { try TargetQueries.updatePriority($0, id: 1, priority: "high") }
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(target.priority, "high")
    }

    // MARK: - updateSubItems

    func testUpdateSubItems() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let items = [TargetSubItem(text: "Step 1", done: false), TargetSubItem(text: "Step 2", done: true)]
        try db.write { try TargetQueries.updateSubItems($0, id: 1, subItems: items) }
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 1) })
        let decoded = target.decodedSubItems
        XCTAssertEqual(decoded.count, 2)
        XCTAssertFalse(decoded[0].done)
        XCTAssertTrue(decoded[1].done)
    }

    // MARK: - snooze

    func testSnooze() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let snoozeDate = try XCTUnwrap(Calendar.current.date(byAdding: .day, value: 3, to: Date()))
        try db.write { try TargetQueries.snooze($0, id: 1, until: snoozeDate) }
        let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 1) })
        XCTAssertEqual(target.status, "snoozed")
        XCTAssertFalse(target.snoozeUntil.isEmpty)
    }

    // MARK: - delete

    func testDelete() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        try db.write { try TargetQueries.delete($0, id: 1) }
        let target = try db.read { try TargetQueries.fetchByID($0, id: 1) }
        XCTAssertNil(target)
    }

    // MARK: - fetchLinks

    func testFetchLinksOutbound() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Source")
            try TestDatabase.insertTarget(db, text: "Destination")
            try TestDatabase.insertTargetLink(db, sourceTargetId: 1, targetTargetId: 2, relation: "contributes_to")
        }
        let links = try db.read { try TargetQueries.fetchLinks($0, targetID: 1, direction: .outbound) }
        XCTAssertEqual(links.count, 1)
        XCTAssertEqual(links[0].sourceTargetId, 1)
        XCTAssertEqual(links[0].targetTargetId, 2)
    }

    func testFetchLinksInbound() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Source")
            try TestDatabase.insertTarget(db, text: "Destination")
            try TestDatabase.insertTargetLink(db, sourceTargetId: 1, targetTargetId: 2, relation: "blocks")
        }
        let links = try db.read { try TargetQueries.fetchLinks($0, targetID: 2, direction: .inbound) }
        XCTAssertEqual(links.count, 1)
        XCTAssertEqual(links[0].relation, "blocks")
    }

    func testFetchLinksBoth() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "A")
            try TestDatabase.insertTarget(db, text: "B")
            try TestDatabase.insertTarget(db, text: "C")
            // A → B (outbound from A's perspective)
            try TestDatabase.insertTargetLink(db, sourceTargetId: 1, targetTargetId: 2, relation: "contributes_to")
            // C → A (inbound to A)
            try TestDatabase.insertTargetLink(db, sourceTargetId: 3, targetTargetId: 1, relation: "related")
        }
        let links = try db.read { try TargetQueries.fetchLinks($0, targetID: 1, direction: .both) }
        XCTAssertEqual(links.count, 2)
    }

    func testFetchLinksEmptyWhenNone() throws {
        let db = try TestDatabase.create()
        try db.write { try TestDatabase.insertTarget($0) }
        let links = try db.read { try TargetQueries.fetchLinks($0, targetID: 1, direction: .both) }
        XCTAssertTrue(links.isEmpty)
    }

    func testFetchLinksExternal() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "My target")
            try TestDatabase.insertTargetLink(
                db,
                sourceTargetId: 1,
                targetTargetId: nil,
                externalRef: "jira:PROJ-42",
                relation: "contributes_to",
                createdBy: "user"
            )
        }
        let links = try db.read { try TargetQueries.fetchLinks($0, targetID: 1, direction: .outbound) }
        XCTAssertEqual(links.count, 1)
        XCTAssertTrue(links[0].isExternalLink)
        XCTAssertEqual(links[0].externalRef, "jira:PROJ-42")
    }

    // MARK: - Parent-child cascade

    func testDeleteParentSetsChildParentToNull() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertTarget(db, text: "Parent", level: "quarter",
                                          periodStart: "2026-01-01", periodEnd: "2026-03-31")
            try TestDatabase.insertTarget(db, text: "Child", level: "week",
                                          periodStart: "2026-01-06", periodEnd: "2026-01-12", parentId: 1)
        }
        try db.write { try TargetQueries.delete($0, id: 1) }
        let child = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 2) })
        XCTAssertNil(child.parentId)
    }
}

// MARK: - TargetsViewModel Tests

@MainActor
final class TargetsViewModelTests: XCTestCase {

    func testLoadPopulatesTodayAndAll() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        let today = fmt.string(from: Date())
        try mgr.dbPool.write { db in
            // High priority → goes to today section
            try TestDatabase.insertTarget(db, text: "High prio", periodStart: today, periodEnd: today, priority: "high")
            // Low priority, not due today → goes to all section
            try TestDatabase.insertTarget(db, text: "Low prio",
                                          periodStart: "2026-01-01", periodEnd: "2026-01-31", priority: "low")
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.load()
        XCTAssertFalse(vm.todayTargets.isEmpty, "high-priority target should be in todayTargets")
        XCTAssertFalse(vm.allTargets.isEmpty, "low-priority non-today target should be in allTargets")
        XCTAssertEqual(vm.todayTargets.first?.text, "High prio")
        XCTAssertEqual(vm.allTargets.first?.text, "Low prio")
    }

    func testLevelFilterNarrowsResults() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let today = "2026-04-23"
        try mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Quarter target", level: "quarter",
                                          periodStart: "2026-01-01", periodEnd: "2026-03-31")
            try TestDatabase.insertTarget(db, text: "Day target", level: "day",
                                          periodStart: today, periodEnd: today)
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.levelFilter = "quarter"
        vm.load()
        let all = vm.todayTargets + vm.allTargets
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all.first?.level, "quarter")
    }

    func testShowDoneIncludesDoneTargets() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        try mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Done target",
                                          periodStart: "2026-04-01", periodEnd: "2026-04-30", status: "done")
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.load()
        XCTAssertTrue((vm.todayTargets + vm.allTargets).isEmpty, "done hidden by default")
        vm.showDone = true
        vm.load()
        XCTAssertEqual((vm.todayTargets + vm.allTargets).count, 1)
    }

    func testSortOrderLevelThenPeriod() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        try mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Week A", level: "week",
                                          periodStart: "2026-04-20", periodEnd: "2026-04-26")
            try TestDatabase.insertTarget(db, text: "Quarter A", level: "quarter",
                                          periodStart: "2026-01-01", periodEnd: "2026-03-31")
            try TestDatabase.insertTarget(db, text: "Month A", level: "month",
                                          periodStart: "2026-04-01", periodEnd: "2026-04-30")
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.load()
        let all = vm.todayTargets + vm.allTargets
        XCTAssertEqual(all.count, 3)
        XCTAssertEqual(all[0].level, "quarter")
        XCTAssertEqual(all[1].level, "month")
        XCTAssertEqual(all[2].level, "week")
    }

    func testMarkDoneMovesTargetOutOfActive() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let today = "2026-04-23"
        try mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Active",
                                          periodStart: today, periodEnd: today, priority: "high")
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.load()
        let target = try XCTUnwrap(vm.todayTargets.first)
        vm.markDone(target)
        XCTAssertTrue(vm.todayTargets.isEmpty)
    }

    func testSearchTextFilters() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        let today = "2026-04-23"
        try mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Deploy backend", periodStart: today, periodEnd: today)
            try TestDatabase.insertTarget(db, text: "Write docs", periodStart: today, periodEnd: today)
        }
        let vm = TargetsViewModel(dbManager: mgr)
        vm.searchText = "backend"
        vm.load()
        let all = vm.todayTargets + vm.allTargets
        XCTAssertEqual(all.count, 1)
        XCTAssertEqual(all.first?.text, "Deploy backend")
    }

    func testDeleteTargetRemovesRow() throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        try mgr.dbPool.write { try TestDatabase.insertTarget($0) }

        let vm = TargetsViewModel(dbManager: mgr)
        let target = try XCTUnwrap(mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) })

        vm.deleteTarget(target)

        let gone = try mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) }
        XCTAssertNil(gone)
        XCTAssertNil(vm.errorMessage)
    }

    // MARK: - promoteSubItem

    private func samplePromoteResponseJSON(id: Int = 2, parentID: Int = 1) -> String {
        """
        {
          "id": \(id), "text": "x", "level": "day", "priority": "medium", "status": "todo",
          "due_date": "", "period_start": "2026-04-20", "period_end": "2026-04-26",
          "parent_id": \(parentID), "source_type": "promoted_subitem", "source_id": "\(parentID):0"
        }
        """
    }

    func testPromoteSubItemCallsCLIAndReturnsChildID() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        _ = try await mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "Parent",
                                          subItems: #"[{"text":"first","done":false}]"#)
        }
        let runner = FakeCLIRunner(stdout: Data(samplePromoteResponseJSON(id: 99).utf8))
        let vm = TargetsViewModel(dbManager: mgr, cliRunner: runner)
        let fetched = try await mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) }
        let parent = try XCTUnwrap(fetched)

        let newID = try await vm.promoteSubItem(parent, index: 0)

        XCTAssertEqual(newID, 99)
        let args = runner.invocations.last ?? []
        XCTAssertEqual(Array(args.prefix(5)),
                       ["targets", "promote-subitem", "1", "0", "--json"])
        XCTAssertNil(vm.errorMessage)
    }

    func testPromoteSubItemPassesOverridesAsFlags() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        _ = try await mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "P",
                                          subItems: #"[{"text":"x"}]"#)
        }
        let runner = FakeCLIRunner(stdout: Data(samplePromoteResponseJSON().utf8))
        let vm = TargetsViewModel(dbManager: mgr, cliRunner: runner)
        let fetched = try await mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) }
        let parent = try XCTUnwrap(fetched)

        var overrides = PromoteSubItemOverrides()
        overrides.level = "day"
        overrides.priority = "low"
        _ = try await vm.promoteSubItem(parent, index: 0, overrides: overrides)

        let args = runner.invocations.last ?? []
        XCTAssertTrue(args.contains("--level"))
        XCTAssertTrue(args.contains("day"))
        XCTAssertTrue(args.contains("--priority"))
        XCTAssertTrue(args.contains("low"))
    }

    func testPromoteSubItemThrowsOnCLIFailure() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        _ = try await mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "P",
                                          subItems: #"[{"text":"x"}]"#)
        }
        let runner = FakeCLIRunner(
            error: CLIRunnerError.nonZeroExit(code: 1, stderr: "out of range")
        )
        let vm = TargetsViewModel(dbManager: mgr, cliRunner: runner)
        let fetched = try await mgr.dbPool.read { try TargetQueries.fetchByID($0, id: 1) }
        let parent = try XCTUnwrap(fetched)

        do {
            _ = try await vm.promoteSubItem(parent, index: 99)
            XCTFail("expected error")
        } catch {
            // OK — caller (sheet) is responsible for surfacing the error.
        }
        // `errorMessage` is intentionally NOT set on failure to avoid the
        // double-channel signaling that would surface the same error twice
        // (banner from VM + alert from the sheet's own catch).
        XCTAssertNil(vm.errorMessage)
    }

    func testPromoteSubItemsAfterCreateInvokesInDescendingIndexOrder() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        _ = try await mgr.dbPool.write { db in
            try TestDatabase.insertTarget(db, text: "P",
                                          subItems: #"[{"text":"a"},{"text":"b"},{"text":"c"},{"text":"d"}]"#)
        }
        let runner = FakeCLIRunner(stdout: Data(samplePromoteResponseJSON().utf8))
        let vm = TargetsViewModel(dbManager: mgr, cliRunner: runner)

        // Inputs are intentionally in non-monotonic order so a buggy "reverse"
        // implementation would fail to produce the strict descending sequence.
        try await vm.promoteSubItemsAfterCreate(parentID: 1, items: [
            (index: 1, overrides: PromoteSubItemOverrides()),
            (index: 3, overrides: PromoteSubItemOverrides()),
            (index: 0, overrides: PromoteSubItemOverrides()),
        ])

        XCTAssertEqual(runner.invocations.count, 3)
        XCTAssertEqual(runner.invocations[0][3], "3", "first batch call must use the highest index")
        XCTAssertEqual(runner.invocations[1][3], "1", "second batch call must use the next-lower index")
        XCTAssertEqual(runner.invocations[2][3], "0", "third batch call must use the lowest index")
    }

    func testPromoteSubItemsAfterCreateNoOpOnEmpty() async throws {
        let (mgr, path) = try TestDatabase.createDatabaseManager()
        defer { TestDatabase.cleanup(path: path) }
        _ = try await mgr.dbPool.write { try TestDatabase.insertTarget($0) }
        let runner = FakeCLIRunner(stdout: Data(samplePromoteResponseJSON().utf8))
        let vm = TargetsViewModel(dbManager: mgr, cliRunner: runner)

        try await vm.promoteSubItemsAfterCreate(parentID: 1, items: [])

        XCTAssertTrue(runner.invocations.isEmpty, "no CLI calls should fire when items list is empty")
    }
}

// MARK: - ExtractPreviewSheet Tests

final class ExtractPreviewSheetTests: XCTestCase {

    func testRendersCorrectCardCount() {
        let proposed = (1...5).map { i in
            ProposedTarget(
                text: "Target \(i)",
                intent: "",
                level: "day",
                customLabel: "",
                levelConfidence: 0.85,
                periodStart: "2026-04-23",
                periodEnd: "2026-04-23",
                priority: "medium",
                parentId: nil,
                secondaryLinks: []
            )
        }
        // Sheet is initialized with 5 proposed targets, all selected by default
        var sheet = proposed
        XCTAssertEqual(sheet.count, 5)
        let selected = sheet.filter(\.isSelected)
        XCTAssertEqual(selected.count, 5, "all items selected by default")
    }

    func testToggleCheckboxReducesSelected() {
        var proposed = [
            ProposedTarget(
                text: "A", intent: "", level: "day", customLabel: "",
                levelConfidence: nil, periodStart: "2026-04-23", periodEnd: "2026-04-23",
                priority: "medium", parentId: nil, secondaryLinks: [], isSelected: true
            ),
            ProposedTarget(
                text: "B", intent: "", level: "week", customLabel: "",
                levelConfidence: 0.7, periodStart: "2026-04-20", periodEnd: "2026-04-26",
                priority: "high", parentId: nil, secondaryLinks: [], isSelected: true
            ),
        ]
        // Toggle off item B
        proposed[1].isSelected = false
        let selected = proposed.filter(\.isSelected)
        XCTAssertEqual(selected.count, 1)
        XCTAssertEqual(selected.first?.text, "A")
    }

    func testAllDeselectedMeansNoneSelected() {
        var proposed = [
            ProposedTarget(
                text: "X", intent: "", level: "day", customLabel: "",
                levelConfidence: nil, periodStart: "2026-04-23", periodEnd: "2026-04-23",
                priority: "low", parentId: nil, secondaryLinks: [], isSelected: true
            ),
        ]
        proposed[0].isSelected = false
        XCTAssertTrue(proposed.filter(\.isSelected).isEmpty)
    }
}
