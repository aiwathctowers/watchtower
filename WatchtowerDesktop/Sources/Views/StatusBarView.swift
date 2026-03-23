import SwiftUI
import GRDB

struct StatusBarView: View {
    @Environment(AppState.self) private var appState
    @State private var daemonManager = DaemonManager()
    @State private var lastSync: String?
    @State private var stats: WorkspaceStats?
    @State private var refreshTask: Task<Void, Never>?

    var body: some View {
        HStack(spacing: 16) {
            // Daemon status
            HStack(spacing: 4) {
                Circle()
                    .fill(daemonManager.isRunning ? .green : .gray)
                    .frame(width: 6, height: 6)
                Text(daemonManager.isRunning ? "Daemon running" : "Daemon stopped")
            }

            Divider().frame(height: 12)

            // Last sync
            if let lastSync {
                Text("Synced \(TimeFormatting.relativeTime(from: lastSync))")
            } else {
                Text("Never synced")
            }

            Spacer()

            // Stats
            if let stats {
                HStack(spacing: 12) {
                    Label("\(stats.channelCount)", systemImage: "number")
                    Label("\(stats.userCount)", systemImage: "person.2")
                    Label(formatNumber(stats.messageCount), systemImage: "message")
                }
            }

            Divider().frame(height: 12)

            Text("v\(Constants.appVersion)")
                .foregroundStyle(.tertiary)
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(Color(.windowBackgroundColor))
        .onAppear {
            daemonManager.resolvePathIfNeeded()
            daemonManager.startPolling()
            loadStats()
            refreshTask = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(30))
                    loadStats()
                }
            }
        }
        .onDisappear {
            daemonManager.stopPolling()
            refreshTask?.cancel()
            refreshTask = nil
        }
    }

    private func loadStats() {
        guard let db = appState.databaseManager else { return }
        Task.detached {
            let (ws, st) = try await db.dbPool.read { db in
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let st = try WorkspaceStats.fetch(db)
                return (ws, st)
            }
            await MainActor.run {
                lastSync = ws?.syncedAt
                stats = st
            }
        }
    }

    private func formatNumber(_ n: Int) -> String {
        if n >= 1_000_000 { return String(format: "%.1fM", Double(n) / 1_000_000) }
        if n >= 1_000 { return String(format: "%.1fK", Double(n) / 1_000) }
        return "\(n)"
    }
}
