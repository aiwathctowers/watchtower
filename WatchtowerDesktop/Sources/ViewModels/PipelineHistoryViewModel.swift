import Foundation
import GRDB

@MainActor
@Observable
final class PipelineHistoryViewModel {
    var runs: [PipelineRun] = []
    var steps: [Int64: [PipelineStepRecord]] = [:]
    var selectedDate: Date = Calendar.current.startOfDay(for: Date())
    var isLoading = false

    private var observationTask: Task<Void, Never>?
    private var dbPool: DatabasePool?

    func start(dbPool: DatabasePool) {
        self.dbPool = dbPool
        loadRuns()
        startObserving()
    }

    func stop() {
        observationTask?.cancel()
        observationTask = nil
    }

    private func startObserving() {
        guard observationTask == nil, let dbPool else { return }
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM pipeline_runs") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.loadRuns()
                }
            } catch {
                // observation cancelled or DB closed
            }
        }
    }

    func loadRuns() {
        guard let dbPool else { return }
        isLoading = true
        let date = selectedDate
        Task.detached {
            let result = try? await dbPool.read { db in
                try PipelineRunQueries.fetchByDate(db, on: date)
            }
            await MainActor.run {
                self.runs = result ?? []
                self.steps = [:]
                self.isLoading = false
            }
        }
    }

    func loadSteps(for runId: Int64) {
        guard steps[runId] == nil, let dbPool else { return }
        Task.detached {
            let result = try? await dbPool.read { db in
                try PipelineRunQueries.fetchSteps(db, runId: runId)
            }
            await MainActor.run {
                self.steps[runId] = result ?? []
            }
        }
    }

    func goToPreviousDay() {
        selectedDate = Calendar.current.date(byAdding: .day, value: -1, to: selectedDate) ?? selectedDate
        loadRuns()
    }

    func goToNextDay() {
        let next = Calendar.current.date(byAdding: .day, value: 1, to: selectedDate) ?? selectedDate
        let today = Calendar.current.startOfDay(for: Date())
        if next <= today {
            selectedDate = next
            loadRuns()
        }
    }

    func goToToday() {
        selectedDate = Calendar.current.startOfDay(for: Date())
        loadRuns()
    }

    var isToday: Bool {
        Calendar.current.isDateInToday(selectedDate)
    }

    // MARK: - Aggregations

    var totalInputTokens: Int { runs.reduce(0) { $0 + $1.inputTokens } }
    var totalOutputTokens: Int { runs.reduce(0) { $0 + $1.outputTokens } }
    var totalApiTokens: Int { runs.reduce(0) { $0 + $1.totalApiTokens } }
    var totalCalls: Int { runs.reduce(0) { $0 + $1.aiCallCount } }

}
