import Foundation
import GRDB

struct DayEvents: Identifiable {
    let id: Date
    let label: String
    let events: [CalendarEvent]
}

@MainActor
@Observable
final class CalendarViewModel {
    var dailyEvents: [DayEvents] = []
    var nextEvent: CalendarEvent?
    var isConnected: Bool = false

    /// Non-nil when the daemon has detected that the Google refresh token is revoked or failing.
    /// The Desktop shows a reconnect popup while this is present.
    var authState: CalendarQueries.AuthState?

    private let dbPool: DatabasePool
    private var observationTask: Task<Void, Never>?

    /// Number of days to display (including today).
    private let daysAhead = 7

    init(dbPool: DatabasePool) {
        self.dbPool = dbPool
        loadEvents()
        startObserving()
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }

    // MARK: - Convenience accessors (backward compat)

    var todayEvents: [CalendarEvent] {
        dailyEvents.first?.events ?? []
    }

    var tomorrowEvents: [CalendarEvent] {
        dailyEvents.dropFirst().first?.events ?? []
    }

    // MARK: - Data Loading

    func loadEvents() {
        let cal = Calendar.current
        let today = cal.startOfDay(for: Date())

        let result = try? dbPool.read { db -> ([DayEvents], CalendarEvent?, CalendarQueries.AuthState?) in
            var days: [DayEvents] = []
            for offset in 0..<self.daysAhead {
                let dayStart = today.addingTimeInterval(Double(offset) * 86400)
                let dayEnd = dayStart.addingTimeInterval(86400)
                let items = try CalendarQueries.fetchEvents(db, from: dayStart, to: dayEnd)
                if !items.isEmpty {
                    let label = Self.label(for: dayStart, calendar: cal)
                    days.append(DayEvents(id: dayStart, label: label, events: items))
                }
            }
            let next = try CalendarQueries.fetchNextEvent(db)
            let auth = (try? CalendarQueries.fetchAuthState(db)) ?? nil
            return (days, next, auth)
        }

        dailyEvents = result?.0 ?? []
        nextEvent = result?.1

        let auth = result?.2
        if let auth, auth.status == "revoked" || auth.status == "error" {
            authState = auth
        } else {
            authState = nil
        }

        let hasEvents = (try? dbPool.read { db in
            try CalendarQueries.eventCount(db) > 0
        }) ?? false
        isConnected = hasEvents
    }

    // MARK: - Helpers

    private static func label(for date: Date, calendar cal: Calendar) -> String {
        if cal.isDateInToday(date) { return "Today" }
        if cal.isDateInTomorrow(date) { return "Tomorrow" }
        let fmt = DateFormatter()
        fmt.locale = Locale.current
        fmt.dateFormat = "EEEE, d MMM"
        return fmt.string(from: date)
    }

    // MARK: - Observation

    private func startObserving() {
        observationTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(30))
                guard !Task.isCancelled, let self else { break }
                self.loadEvents()
            }
        }
    }
}
