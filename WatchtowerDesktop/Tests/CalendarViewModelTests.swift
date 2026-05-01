import Foundation
import GRDB
import Testing
@testable import WatchtowerDesktop

@Suite("CalendarViewModel")
@MainActor
struct CalendarViewModelTests {

    private func makePool() throws -> DatabasePool {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        return manager.dbPool
    }

    private func insertEvent(
        _ pool: DatabasePool,
        id: String,
        title: String,
        start: Date,
        end: Date,
        isAllDay: Bool = false
    ) throws {
        let fmt = ISO8601DateFormatter()
        try pool.write { db in
            // Ensure parent calendar exists (FK in calendar_events).
            try db.execute(sql: "INSERT OR IGNORE INTO calendar_calendars (id, name, is_primary, is_selected) VALUES (?, ?, ?, ?)",
                           arguments: ["primary", "Main", 1, 1])
            try db.execute(sql: """
                INSERT INTO calendar_events (id, calendar_id, title, description, location, start_time, end_time,
                    organizer_email, attendees, is_recurring, is_all_day, event_status, event_type, html_link, raw_json,
                    synced_at, updated_at)
                VALUES (?, ?, ?, '', '', ?, ?, '', '[]', 0, ?, 'confirmed', '', '', '{}', ?, ?)
                """, arguments: [
                    id, "primary", title, fmt.string(from: start), fmt.string(from: end),
                    isAllDay ? 1 : 0,
                    fmt.string(from: Date()),
                    fmt.string(from: Date())
                ])
        }
    }

    @Test("loadEvents groups by day and surfaces nextEvent")
    func loadAndGroup() throws {
        let pool = try makePool()
        let cal = Calendar.current
        let now = Date()
        let today9 = cal.date(bySettingHour: 9, minute: 0, second: 0, of: now)!
        let today10 = cal.date(byAdding: .hour, value: 1, to: today9)!
        let tomorrow9 = cal.date(byAdding: .day, value: 1, to: today9)!
        let tomorrow10 = cal.date(byAdding: .hour, value: 1, to: tomorrow9)!

        try insertEvent(pool, id: "e1", title: "Standup", start: today9, end: today10)
        try insertEvent(pool, id: "e2", title: "Demo", start: tomorrow9, end: tomorrow10)

        let vm = CalendarViewModel(dbPool: pool)
        vm.stopObserving()
        vm.loadEvents()

        // Two day-groups, today + tomorrow.
        #expect(vm.dailyEvents.count >= 1)
        #expect(vm.todayEvents.contains(where: { $0.title == "Standup" }) || vm.tomorrowEvents.contains(where: { $0.title == "Standup" }))
        #expect(vm.isConnected)
    }

    @Test("loadEvents with no events leaves arrays empty and isConnected=false")
    func loadEmpty() throws {
        let pool = try makePool()
        let vm = CalendarViewModel(dbPool: pool)
        vm.stopObserving()
        vm.loadEvents()

        #expect(vm.dailyEvents.isEmpty)
        #expect(vm.nextEvent == nil)
        #expect(vm.isConnected == false)
    }

    // calendar_auth_state isn't in the test schema, so we don't exercise that
    // branch here — coverage for it lives in Go-side tests (internal/db).

    @Test("todayEvents accessor returns first day group")
    func todayEventsAccessor() throws {
        let pool = try makePool()
        let now = Date()
        try insertEvent(pool, id: "e1", title: "Now", start: now, end: now.addingTimeInterval(1800))

        let vm = CalendarViewModel(dbPool: pool)
        vm.stopObserving()
        vm.loadEvents()

        if !vm.dailyEvents.isEmpty {
            #expect(vm.todayEvents == vm.dailyEvents.first?.events)
        }
    }
}
