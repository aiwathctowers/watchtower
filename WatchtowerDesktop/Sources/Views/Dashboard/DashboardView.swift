import SwiftUI

struct DashboardView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: DashboardViewModel?
    @State private var daemonManager = DaemonManager()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let vm = viewModel {
                    SyncStatusBanner(
                        syncedAt: vm.workspace?.syncedAt,
                        isRunning: daemonManager.isRunning
                    )

                    LazyVGrid(columns: [
                        GridItem(.flexible()),
                        GridItem(.flexible())
                    ], spacing: 16) {
                        StatsCard(title: "Channels", value: "\(vm.stats.channelCount)", icon: "number")
                        StatsCard(title: "Users", value: "\(vm.stats.userCount)", icon: "person.2")
                        StatsCard(title: "Messages", value: formatNumber(vm.stats.messageCount), icon: "message")
                        StatsCard(title: "Digests", value: "\(vm.stats.digestCount)", icon: "doc.text")
                    }

                    ActivityFeed(
                        messages: vm.recentActivity
                    ) { vm.slackChannelURL(channelID: $0) }

                    if let error = vm.errorMessage {
                        Text(error)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                } else {
                    ProgressView()
                }
            }
            .padding()
        }
        .navigationTitle("Dashboard")
        .onAppear {
            // M8: guard against re-creation
            if let db = appState.databaseManager, viewModel == nil {
                let vm = DashboardViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
            daemonManager.startPolling()
        }
        .onDisappear {
            daemonManager.stopPolling()
        }
    }

    private func formatNumber(_ n: Int) -> String {
        if n >= 1_000_000 { return String(format: "%.1fM", Double(n) / 1_000_000) }
        if n >= 1_000 { return String(format: "%.1fK", Double(n) / 1_000) }
        return "\(n)"
    }
}
