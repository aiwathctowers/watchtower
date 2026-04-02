import Foundation
import GRDB

@MainActor
@Observable
final class CalendarViewModel {
    var todayEvents: [CalendarEvent] = []
    var tomorrowEvents: [CalendarEvent] = []
    var nextEvent: CalendarEvent?
    var isConnected: Bool = false

    private let dbPool: DatabasePool
    private var observationTask: Task<Void, Never>?

    init(dbPool: DatabasePool) {
        self.dbPool = dbPool
        loadEvents()
        startObserving()
    }

    func stopObserving() {
        observationTask?.cancel()
        observationTask = nil
    }

    // MARK: - Data Loading

    func loadEvents() {
        let cal = Calendar.current
        let today = cal.startOfDay(for: Date())
        let tomorrow = today.addingTimeInterval(86400)
        let dayAfter = tomorrow.addingTimeInterval(86400)

        let result = try? dbPool.read { db -> ([CalendarEvent], [CalendarEvent], CalendarEvent?) in
            let todayItems = try CalendarQueries.fetchEvents(db, from: today, to: tomorrow)
            let tomorrowItems = try CalendarQueries.fetchEvents(db, from: tomorrow, to: dayAfter)
            let next = try CalendarQueries.fetchNextEvent(db)
            return (todayItems, tomorrowItems, next)
        }

        todayEvents = result?.0 ?? []
        tomorrowEvents = result?.1 ?? []
        nextEvent = result?.2

        let hasEvents = (try? dbPool.read { db in
            try CalendarQueries.eventCount(db) > 0
        }) ?? false
        isConnected = hasEvents
    }

    // MARK: - Observation

    private func startObserving() {
        observationTask = Task { [weak self] in
            guard let self else { return }
            let observation = ValueObservation.tracking { db -> Int in
                try CalendarQueries.eventCount(db)
            }
            do {
                for try await _ in observation.values(in: self.dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self.loadEvents()
                }
            } catch {}
        }
    }
}
