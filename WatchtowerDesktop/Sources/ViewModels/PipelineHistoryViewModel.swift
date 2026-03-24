import Foundation
import GRDB
import Combine

@MainActor
@Observable
final class PipelineHistoryViewModel {
    var runs: [PipelineRun] = []
    var steps: [Int64: [PipelineStepRecord]] = [:]

    private var cancellable: AnyDatabaseCancellable?
    private var dbPool: DatabasePool?

    func start(dbPool: DatabasePool) {
        self.dbPool = dbPool

        let observation = ValueObservation.tracking { db in
            try PipelineRunQueries.fetchRecent(db, limit: 100)
        }

        cancellable = observation.start(
            in: dbPool,
            onError: { _ in },
            onChange: { [weak self] newRuns in
                Task { @MainActor in
                    self?.runs = newRuns
                }
            }
        )
    }

    func stop() {
        cancellable?.cancel()
        cancellable = nil
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

    var totalCost: Double {
        runs.reduce(0) { $0 + $1.costUsd }
    }

    var totalInputTokens: Int {
        runs.reduce(0) { $0 + $1.inputTokens }
    }

    var totalOutputTokens: Int {
        runs.reduce(0) { $0 + $1.outputTokens }
    }
}
