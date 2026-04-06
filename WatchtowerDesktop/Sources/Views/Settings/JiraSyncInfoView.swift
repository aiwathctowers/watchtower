import SwiftUI
import GRDB

struct JiraSyncInfoView: View {
    @Environment(AppState.self) private var appState
    @State private var lastSyncTime: String?
    @State private var issueCount: Int = 0
    @State private var isSyncing: Bool = false
    @State private var syncError: String?
    @State private var observationTask: Task<Void, Never>?

    var body: some View {
        Section("Sync") {
            LabeledContent("Last sync") {
                if let syncTime = lastSyncTime,
                   !syncTime.isEmpty {
                    Text(relativeSyncTime(syncTime))
                        .foregroundStyle(.secondary)
                } else {
                    Text("Never")
                        .foregroundStyle(.secondary)
                }
            }

            LabeledContent("Issues synced") {
                Text("\(issueCount)")
                    .foregroundStyle(.secondary)
            }

            HStack {
                Button {
                    runSync()
                } label: {
                    HStack(spacing: 4) {
                        if isSyncing {
                            ProgressView()
                                .controlSize(.small)
                        }
                        Text(
                            isSyncing
                                ? "Syncing..."
                                : "Sync Now"
                        )
                    }
                }
                .disabled(isSyncing)

                if let err = syncError {
                    Text(err)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .lineLimit(2)
                }
            }
        }
        .onAppear { startObserving() }
        .onDisappear { observationTask?.cancel() }
    }

    private func relativeSyncTime(_ isoString: String) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [
            .withInternetDateTime,
            .withFractionalSeconds
        ]
        guard let date = formatter.date(from: isoString)
                ?? ISO8601DateFormatter().date(
                    from: isoString
                ) else {
            return isoString
        }
        let relative = RelativeDateTimeFormatter()
        relative.unitsStyle = .full
        return relative.localizedString(for: date, relativeTo: Date())
    }

    private func runSync() {
        guard let cliPath = Constants.findCLIPath() else {
            syncError = "Watchtower CLI not found"
            return
        }

        isSyncing = true
        syncError = nil

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["jira", "sync"]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()

            let stderrPipe = Pipe()
            process.standardOutput = Pipe()
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                await MainActor.run {
                    isSyncing = false
                    syncError = "Failed to launch CLI"
                }
                return
            }

            let stderrData = stderrPipe.fileHandleForReading
                .readDataToEndOfFile()
            process.waitUntilExit()

            await MainActor.run {
                isSyncing = false
                if process.terminationStatus != 0 {
                    let stderr = String(
                        data: stderrData, encoding: .utf8
                    )?.trimmingCharacters(
                        in: .whitespacesAndNewlines
                    ) ?? ""
                    syncError = stderr.isEmpty
                        ? "Sync failed (exit \(process.terminationStatus))"
                        : String(stderr.prefix(200))
                }
            }
        }
    }

    private func startObserving() {
        guard let db = appState.databaseManager else { return }
        loadSyncInfo(db: db)
        let dbPool = db.dbPool
        observationTask = Task {
            let observation = ValueObservation.tracking { db in
                (
                    lastSync: try JiraQueries.fetchLastSyncTime(db),
                    count: try JiraQueries.fetchIssueCount(db)
                )
            }
            do {
                for try await info in observation.values(
                    in: dbPool
                ).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self.lastSyncTime = info.lastSync
                    self.issueCount = info.count
                }
            } catch {}
        }
    }

    private func loadSyncInfo(db: DatabaseManager) {
        Task {
            let result = try? await db.dbPool.read { db in
                (
                    lastSync: try JiraQueries.fetchLastSyncTime(db),
                    count: try JiraQueries.fetchIssueCount(db)
                )
            }
            if let result {
                lastSyncTime = result.lastSync
                issueCount = result.count
            }
        }
    }
}
