import SwiftUI
import Charts

struct ActivityStatisticsView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: UserStatsViewModel?

    var body: some View {
        Group {
            if let vm = viewModel {
                activityContent(vm)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = UserStatsViewModel(dbManager: db)
                viewModel?.startObserving()
            }
        }
    }

    private func activityContent(_ vm: UserStatsViewModel) -> some View {
        VStack(spacing: 0) {
            if vm.isLoading && vm.stats.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = vm.errorMessage {
                ErrorView(title: "Error", message: error, actionTitle: "Retry") { vm.load() }
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 16) {
                        summaryCards(vm)
                        chartsRow(vm)
                        botManagementSection(vm)
                    }
                    .padding()
                }
            }
        }
    }

    // MARK: - Summary Cards

    private func summaryCards(_ vm: UserStatsViewModel) -> some View {
        HStack(spacing: 12) {
            summaryCard(title: "Total Messages", value: formatNumber(vm.totalMessages),
                        subtitle: "\(vm.totalUsers) users", icon: "message", color: .blue)
            summaryCard(title: "Human", value: formatNumber(vm.humanMessages),
                        subtitle: "\(vm.totalHumans) users",
                        icon: "person", color: .green)
            summaryCard(title: "Bot", value: formatNumber(vm.botMessages),
                        subtitle: "\(vm.totalBots) bots",
                        icon: "cpu", color: .orange)
            summaryCard(title: "Muted", value: "\(vm.mutedCount)",
                        subtitle: "excluded from AI",
                        icon: "speaker.slash", color: .red)
        }
    }

    private func summaryCard(title: String, value: String, subtitle: String,
                             icon: String, color: Color) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(color)
                    .font(.caption)
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.title2.weight(.semibold).monospacedDigit())
            Text(subtitle)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    // MARK: - Charts Row

    private func chartsRow(_ vm: UserStatsViewModel) -> some View {
        HStack(alignment: .top, spacing: 12) {
            messageRatioChart(vm)
            topUsersChart(vm)
        }
    }

    // MARK: Human vs Bot Pie Chart

    private struct PieSlice: Identifiable {
        let id: String
        let label: String
        let count: Int
        let color: Color
    }

    private func messageRatioChart(_ vm: UserStatsViewModel) -> some View {
        let slices = [
            PieSlice(id: "human", label: "Human", count: vm.humanMessages, color: .blue),
            PieSlice(id: "bot", label: "Bot", count: vm.botMessages, color: .orange),
        ]
        let total = max(vm.totalMessages, 1)
        let humanPct = vm.totalMessages > 0 ? Int(Double(vm.humanMessages) * 100 / Double(total)) : 0
        let botPct = 100 - humanPct

        return VStack(alignment: .leading, spacing: 8) {
            Text("Messages by Type")
                .font(.subheadline.weight(.semibold))

            if vm.totalMessages == 0 {
                Text("No messages")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else {
                Chart(slices) { slice in
                    SectorMark(
                        angle: .value("Messages", slice.count),
                        innerRadius: .ratio(0.5),
                        angularInset: 2
                    )
                    .foregroundStyle(slice.color)
                    .cornerRadius(4)
                }
                .frame(height: 200)

                // Legend
                HStack(spacing: 16) {
                    HStack(spacing: 6) {
                        Circle().fill(.blue).frame(width: 8, height: 8)
                        Text("Human \(humanPct)%")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    HStack(spacing: 6) {
                        Circle().fill(.orange).frame(width: 8, height: 8)
                        Text("Bot \(botPct)%")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                // Absolute numbers
                HStack(spacing: 16) {
                    Text("\(formatNumber(vm.humanMessages)) human")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    Text("\(formatNumber(vm.botMessages)) bot")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(12)
        .frame(width: 280)
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    // MARK: Top Active Users

    private func topUsersChart(_ vm: UserStatsViewModel) -> some View {
        let top = Array(vm.stats
            .filter { $0.totalMessages > 0 && !$0.effectiveIsBot }
            .sorted { $0.totalMessages > $1.totalMessages }
            .prefix(12)
            .reversed())

        return VStack(alignment: .leading, spacing: 8) {
            Text("Top Active Users")
                .font(.subheadline.weight(.semibold))

            if top.isEmpty {
                Text("No data")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else {
                Chart(top) { user in
                    BarMark(
                        x: .value("Messages", user.totalMessages),
                        y: .value("User", user.bestName)
                    )
                    .foregroundStyle(.blue.gradient)
                }
                .chartXAxis {
                    AxisMarks { value in
                        AxisGridLine()
                        AxisValueLabel {
                            if let v = value.as(Int.self) {
                                Text(formatNumber(v))
                                    .font(.caption2)
                            }
                        }
                    }
                }
                .chartYAxis {
                    AxisMarks { value in
                        AxisValueLabel {
                            if let name = value.as(String.self) {
                                Text(name)
                                    .font(.caption)
                                    .lineLimit(1)
                                    .frame(width: 120, alignment: .trailing)
                            }
                        }
                    }
                }
                .frame(height: CGFloat(top.count * 28 + 20))
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity)
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    // MARK: - Bot Management

    private func botManagementSection(_ vm: UserStatsViewModel) -> some View {
        let bots = vm.stats
            .filter(\.effectiveIsBot)
            .sorted { $0.totalMessages > $1.totalMessages }

        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Bot Management")
                    .font(.headline)
                Spacer()
                HStack(spacing: 12) {
                    legendDot(color: .orange, label: "Active")
                    legendDot(color: .red, label: "Muted")
                }
            }

            if bots.isEmpty {
                Text("No bots found")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 20)
            } else {
                // Header
                HStack(spacing: 0) {
                    Text("Bot")
                        .frame(minWidth: 180, alignment: .leading)
                    Text("Messages")
                        .frame(width: 80, alignment: .trailing)
                    Text("Channels")
                        .frame(width: 70, alignment: .trailing)
                    Text("Activity")
                        .frame(width: 100, alignment: .center)
                    Text("Status")
                        .frame(width: 80, alignment: .center)
                    Spacer()
                        .frame(width: 80)
                }
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 8)
                .padding(.vertical, 6)

                Divider()

                ForEach(bots) { bot in
                    botRow(bot, vm: vm)
                    Divider()
                }
            }
        }
        .padding()
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    private func botRow(_ bot: UserStat, vm: UserStatsViewModel) -> some View {
        HStack(spacing: 0) {
            // Bot name
            HStack(spacing: 6) {
                Image(systemName: "cpu")
                    .foregroundStyle(bot.isMutedForLLM ? .red : .orange)
                    .font(.caption)
                VStack(alignment: .leading, spacing: 1) {
                    Text(bot.bestName)
                        .lineLimit(1)
                    if !bot.name.isEmpty && bot.name != bot.bestName {
                        Text("@\(bot.name)")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            }
            .frame(minWidth: 180, alignment: .leading)

            Text(formatNumber(bot.totalMessages))
                .frame(width: 80, alignment: .trailing)
                .monospacedDigit()

            Text("\(bot.channelCount)")
                .frame(width: 70, alignment: .trailing)
                .monospacedDigit()

            // Activity bar
            activityBar(bot, maxMessages: vm.stats.filter(\.effectiveIsBot).map(\.totalMessages).max() ?? 1)
                .frame(width: 100)

            // Muted status
            HStack(spacing: 4) {
                if bot.isMutedForLLM {
                    Text("Muted")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.red.opacity(0.15), in: Capsule())
                        .foregroundStyle(.red)
                }
            }
            .frame(width: 80, alignment: .center)

            // Action: toggle mute
            Button {
                vm.toggleMuteForLLM(userID: bot.id)
            } label: {
                Image(systemName: bot.isMutedForLLM ? "speaker.wave.2" : "speaker.slash")
                    .font(.caption)
                    .foregroundStyle(bot.isMutedForLLM ? .green : .red)
            }
            .buttonStyle(.borderless)
            .help(bot.isMutedForLLM ? "Unmute for AI" : "Mute for AI")
            .frame(width: 80, alignment: .center)
        }
        .font(.body)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
    }

    private func activityBar(_ bot: UserStat, maxMessages: Int) -> some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 3)
                    .fill(Color.secondary.opacity(0.15))
                RoundedRectangle(cornerRadius: 3)
                    .fill(bot.isMutedForLLM ? Color.red.opacity(0.5) : Color.orange)
                    .frame(width: geo.size.width * CGFloat(bot.totalMessages) / CGFloat(max(maxMessages, 1)))
            }
        }
        .frame(height: 14)
    }

    // MARK: - Helpers

    private func legendDot(color: Color, label: String) -> some View {
        HStack(spacing: 4) {
            Circle().fill(color).frame(width: 6, height: 6)
            Text(label).font(.caption2).foregroundStyle(.secondary)
        }
    }

    private func formatNumber(_ n: Int) -> String {
        if n >= 1000 {
            return String(format: "%.1fK", Double(n) / 1000.0)
        }
        return "\(n)"
    }
}
