import Foundation

/// Manages Jira board sync processes. Lives beyond view lifecycle so progress survives navigation.
@MainActor
@Observable
final class JiraBoardSyncManager {
    static let shared = JiraBoardSyncManager()

    private(set) var syncingBoardID: Int?
    private(set) var progress: InsightProgressData?
    private(set) var startedAt: Date?
    private(set) var error: String?

    /// Called on MainActor when sync finishes (success or failure).
    var onComplete: (() -> Void)?

    var isSyncing: Bool { syncingBoardID != nil }

    var elapsedFormatted: String? {
        guard let startedAt else { return nil }
        return Self.formatDuration(Date().timeIntervalSince(startedAt))
    }

    func startSync(boardID: Int) {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }
        guard syncingBoardID == nil else { return }

        syncingBoardID = boardID
        progress = nil
        startedAt = Date()
        error = nil

        Task {
            let result = await Self.runSyncProcess(cliPath: cliPath, boardID: boardID) { json in
                await MainActor.run { [weak self] in
                    self?.progress = json
                }
            }

            syncingBoardID = nil
            progress = nil
            startedAt = nil
            if let errMsg = result {
                error = errMsg
            }
            onComplete?()
        }
    }

    /// Runs the sync CLI process off the main actor. Calls `onProgress` for each JSON line.
    private nonisolated static func runSyncProcess(
        cliPath: String,
        boardID: Int,
        onProgress: @Sendable (InsightProgressData) async -> Void
    ) async -> String? {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: cliPath)
        proc.arguments = [
            "jira", "sync", "--board", String(boardID), "--progress-json",
        ]
        proc.environment = Constants.resolvedEnvironment()
        proc.currentDirectoryURL = Constants.processWorkingDirectory()

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        proc.standardOutput = stdoutPipe
        proc.standardError = stderrPipe

        do {
            try proc.run()
        } catch {
            return "Failed to launch sync"
        }

        let decoder = JSONDecoder()
        do {
            for try await line in stdoutPipe.fileHandleForReading.bytes.lines {
                if let data = line.data(using: .utf8),
                   let json = try? decoder.decode(InsightProgressData.self, from: data) {
                    await onProgress(json)
                }
            }
        } catch {
            // EOF or pipe closed.
        }

        proc.waitUntilExit()

        if proc.terminationStatus != 0 {
            let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            let stderr = String(data: stderrData, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return stderr.isEmpty ? "Sync failed" : String(stderr.prefix(200))
        }
        return nil
    }

    private static func formatDuration(_ seconds: TimeInterval) -> String {
        let s = Int(seconds)
        if s < 60 { return "\(s)s" }
        return "\(s / 60)m \(s % 60)s"
    }

    private init() {}
}
