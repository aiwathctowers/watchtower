import Foundation

/// Manages background pipeline tasks (digests, action items) after onboarding sync.
@MainActor
@Observable
final class BackgroundTaskManager {
    enum TaskKind: String, CaseIterable, Identifiable {
        case digests
        case actions

        var id: String { rawValue }

        var title: String {
            switch self {
            case .digests: "Generating Digests"
            case .actions: "Generating Action Items"
            }
        }

        var icon: String {
            switch self {
            case .digests: "doc.text.magnifyingglass"
            case .actions: "checklist"
            }
        }

        var cliArguments: [String] {
            switch self {
            case .digests: ["digest", "generate", "--progress-json"]
            case .actions: ["actions", "generate", "--progress-json"]
            }
        }
    }

    enum TaskStatus: Equatable {
        case pending
        case running
        case done
        case error(String)
    }

    struct TaskState {
        var status: TaskStatus = .pending
        var progress: InsightProgressData?
        var startedAt: Date?
        /// Estimated seconds remaining, nil if unknown.
        var etaSeconds: Double?
    }

    /// Current state of each background task.
    var tasks: [TaskKind: TaskState] = [:]

    /// Whether any task is still running or pending.
    var hasActiveTasks: Bool {
        tasks.values.contains { $0.status == .pending || $0.status == .running }
    }

    /// Whether all tasks are done (successfully or with error).
    var allFinished: Bool {
        guard !tasks.isEmpty else { return true }
        return tasks.values.allSatisfy {
            if case .done = $0.status { return true }
            if case .error = $0.status { return true }
            return false
        }
    }

    /// Whether there are any visible tasks (not all done successfully).
    var hasVisibleTasks: Bool {
        guard !tasks.isEmpty else { return false }
        // Show panel if any task is pending, running, or errored
        return tasks.values.contains {
            switch $0.status {
            case .pending, .running: return true
            case .error: return true
            case .done: return false
            }
        }
    }

    private var runningProcess: Process?

    /// Start all background pipelines sequentially (digests first, then actions).
    func startPipelines() {
        // Initialize task states
        for kind in TaskKind.allCases {
            tasks[kind] = TaskState()
        }

        Task {
            await runTask(.digests)
            await runTask(.actions)

            // Start daemon after all pipelines complete
            if let path = Constants.findCLIPath() {
                await Self.runCLIFireAndForget(path: path, arguments: ["sync", "--daemon", "--detach"])
            }
        }
    }

    /// Retry a failed task.
    func retry(_ kind: TaskKind) {
        tasks[kind] = TaskState()
        Task {
            await runTask(kind)

            // If this was the last task to complete, start daemon
            if allFinished, let path = Constants.findCLIPath() {
                await Self.runCLIFireAndForget(path: path, arguments: ["sync", "--daemon", "--detach"])
            }
        }
    }

    /// Dismiss a completed or errored task from the sidebar.
    func dismiss(_ kind: TaskKind) {
        tasks.removeValue(forKey: kind)
    }

    // MARK: - Private

    private func runTask(_ kind: TaskKind) async {
        guard let path = Constants.findCLIPath() else {
            tasks[kind]?.status = .error("watchtower CLI not found")
            return
        }

        tasks[kind]?.status = .running
        tasks[kind]?.startedAt = Date()

        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = kind.cliArguments
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            tasks[kind]?.status = .error(error.localizedDescription)
            return
        }

        runningProcess = process
        let decoder = JSONDecoder()

        // Stream JSON lines from stdout
        let readTask = Task<InsightProgressData?, Never> {
            var lastFinished: InsightProgressData?
            do {
                for try await line in stdoutPipe.fileHandleForReading.bytes.lines {
                    if let data = line.data(using: .utf8),
                       let json = try? decoder.decode(InsightProgressData.self, from: data) {
                        await MainActor.run {
                            self.tasks[kind]?.progress = json
                            self.updateETA(kind: kind, progress: json)
                        }
                        if json.finished == true {
                            lastFinished = json
                        }
                    }
                }
            } catch {
                // EOF or pipe closed
            }
            return lastFinished
        }

        // Wait for process exit
        let exitCode: Int32 = await withCheckedContinuation { cont in
            process.terminationHandler = { p in
                cont.resume(returning: p.terminationStatus)
            }
        }

        _ = await readTask.value

        // Read stderr off main actor to avoid blocking UI
        let stderrText: String = await Task.detached {
            let data = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            return String(data: data, encoding: .utf8) ?? ""
        }.value

        runningProcess = nil

        if exitCode == 0 {
            tasks[kind]?.status = .done
            tasks[kind]?.etaSeconds = nil
        } else {
            let errorMsg = stderrText.isEmpty
                ? "Failed (exit code \(exitCode))"
                : stderrText.prefix(200).trimmingCharacters(in: .whitespacesAndNewlines)
            tasks[kind]?.status = .error(String(errorMsg))
        }
    }

    private func updateETA(kind: TaskKind, progress: InsightProgressData) {
        guard let state = tasks[kind],
              let startedAt = state.startedAt,
              progress.total > 0, progress.done > 0 else {
            tasks[kind]?.etaSeconds = nil
            return
        }

        let elapsed = Date().timeIntervalSince(startedAt)
        let rate = Double(progress.done) / elapsed
        let remaining = Double(progress.total - progress.done) / rate
        tasks[kind]?.etaSeconds = remaining
    }

    private nonisolated static func runCLIFireAndForget(path: String, arguments: [String]) async {
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.currentDirectoryURL = URL(fileURLWithPath: NSHomeDirectory())
            process.arguments = arguments
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            process.terminationHandler = { _ in
                cont.resume()
            }
            do {
                try process.run()
            } catch {
                cont.resume()
            }
        }
    }
}
