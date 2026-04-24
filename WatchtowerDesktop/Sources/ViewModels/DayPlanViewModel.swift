import Foundation
import GRDB
import Observation

// MARK: - DayPlanViewModel

@MainActor
@Observable
final class DayPlanViewModel {

    // MARK: - Published State

    var plan: DayPlan?
    var items: [DayPlanItem] = []
    var calendarEventsByID: [String: CalendarEvent] = [:]
    var isGenerating: Bool = false
    var generationError: String?
    var feedbackDraft: String = ""

    // MARK: - Dependencies

    private let dbPool: any DatabaseWriter
    private let cli: any CLIRunnerProtocol
    private var currentDate: String = ""

    // MARK: - Init

    init(databasePool: any DatabaseWriter, cliRunner: any CLIRunnerProtocol) {
        self.dbPool = databasePool
        self.cli = cliRunner
    }

    // MARK: - Load

    func loadFor(date: String) async {
        currentDate = date
        await reload()
    }

    private func reload() async {
        do {
            let fetchedPlan = try await dbPool.read { [currentDate] db in
                try DayPlanQueries.fetchByDate(db, date: currentDate)
            }
            plan = fetchedPlan
            if let p = fetchedPlan {
                items = try await dbPool.read { db in
                    try DayPlanQueries.fetchItems(db, planId: p.id)
                }
                calendarEventsByID = try await loadCalendarEvents(for: items)
            } else {
                items = []
                calendarEventsByID = [:]
            }
        } catch {
            generationError = error.localizedDescription
        }
    }

    private func loadCalendarEvents(for items: [DayPlanItem]) async throws -> [String: CalendarEvent] {
        let ids = items.compactMap { item -> String? in
            guard item.sourceType == .calendar, let sid = item.sourceId, !sid.isEmpty else { return nil }
            return sid
        }
        guard !ids.isEmpty else { return [:] }
        return try await dbPool.read { db in
            var map: [String: CalendarEvent] = [:]
            for id in ids {
                if let ev = try CalendarQueries.fetchEvent(db, id: id) {
                    map[id] = ev
                }
            }
            return map
        }
    }

    // MARK: - Computed Views

    /// Timed timeblocks (excluding all-day calendar events) sorted by start time.
    var timeblocks: [DayPlanItem] {
        items.filter { $0.kind == .timeblock && !isAllDayCalendar($0) }
             .sorted { ($0.startTime ?? .distantFuture) < ($1.startTime ?? .distantFuture) }
    }

    /// All-day calendar timeblocks, shown in a collapsible chip above the timeline.
    var allDayItems: [DayPlanItem] {
        items.filter { $0.kind == .timeblock && isAllDayCalendar($0) }
             .sorted { ($0.startTime ?? .distantFuture) < ($1.startTime ?? .distantFuture) }
    }

    /// Backlog items sorted by order_index.
    var backlogItems: [DayPlanItem] {
        items.filter { $0.kind == .backlog }
             .sorted { $0.orderIndex < $1.orderIndex }
    }

    private func isAllDayCalendar(_ item: DayPlanItem) -> Bool {
        guard item.sourceType == .calendar, let sid = item.sourceId else { return false }
        return calendarEventsByID[sid]?.isAllDay ?? false
    }

    /// (done count, total count) across all items.
    var progress: (done: Int, total: Int) {
        (items.filter { $0.status == .done }.count, items.count)
    }

    var hasConflicts: Bool { plan?.hasConflicts ?? false }

    // MARK: - Item Actions

    func markDone(_ item: DayPlanItem) async {
        do {
            try await dbPool.write { db in
                try DayPlanQueries.markItemDone(
                    db,
                    itemId: item.id,
                    cascadeToTask: item.sourceType == .task
                )
            }
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    func markPending(_ item: DayPlanItem) async {
        do {
            try await dbPool.write { db in
                try DayPlanQueries.markItemPending(
                    db,
                    itemId: item.id,
                    cascadeToTask: item.sourceType == .task
                )
            }
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    func delete(_ item: DayPlanItem) async {
        guard !item.isReadOnly else { return }
        do {
            try await dbPool.write { db in
                try DayPlanQueries.deleteItem(db, itemId: item.id)
            }
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    func reorderBacklog(_ orderedIds: [Int64]) async {
        guard let planId = plan?.id else { return }
        do {
            try await dbPool.write { db in
                try DayPlanQueries.reorderBacklog(db, planId: planId, orderedIds: orderedIds)
            }
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    func addManual(kind: DayPlanItemKind, title: String, startTime: Date?, endTime: Date?) async {
        guard let planId = plan?.id else { return }
        do {
            _ = try await dbPool.write { db in
                try DayPlanQueries.addManualItem(
                    db,
                    planId: planId,
                    kind: kind,
                    title: title,
                    startTime: startTime,
                    endTime: endTime
                )
            }
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    // MARK: - CLI Actions

    /// Calls `watchtower day-plan generate [--feedback <text>] --date <date>`.
    /// Updates `isGenerating` and reloads from DB on success.
    func regenerate(feedback: String?) async {
        isGenerating = true
        generationError = nil
        defer { isGenerating = false }

        var args = ["day-plan", "generate"]
        if let fb = feedback, !fb.isEmpty {
            args += ["--feedback", fb]
        }
        args += ["--date", currentDate]

        do {
            _ = try await cli.run(args: args)
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    /// Calls `watchtower day-plan reset <date>` and reloads.
    func reset() async {
        isGenerating = true
        generationError = nil
        defer { isGenerating = false }

        do {
            _ = try await cli.run(args: ["day-plan", "reset", currentDate])
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }

    /// Calls `watchtower day-plan check-conflicts <date>` and reloads.
    func checkConflicts() async {
        isGenerating = true
        generationError = nil
        defer { isGenerating = false }

        do {
            _ = try await cli.run(args: ["day-plan", "check-conflicts", currentDate])
            await reload()
        } catch {
            generationError = error.localizedDescription
        }
    }
}
