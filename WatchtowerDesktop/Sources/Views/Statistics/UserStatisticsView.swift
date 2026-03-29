import SwiftUI
import Charts

struct UserStatisticsView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: UserStatsViewModel?

    var body: some View {
        Group {
            if let vm = viewModel {
                userContent(vm)
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

    private func userContent(_ vm: UserStatsViewModel) -> some View {
        VStack(spacing: 0) {
            toolbar(vm)
            Divider()

            if vm.isLoading && vm.stats.isEmpty {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let error = vm.errorMessage {
                ErrorView(title: "Error", message: error, actionTitle: "Retry") { vm.load() }
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 16) {
                        summaryCards(vm)
                        chartsSection(vm)
                        userTable(vm)

                        if vm.inactiveCount > 0 {
                            inactiveBar(vm)
                        }
                    }
                    .padding()
                }
            }
        }
    }

    // MARK: - Summary Cards

    private func summaryCards(_ vm: UserStatsViewModel) -> some View {
        HStack(spacing: 12) {
            summaryCard(title: "Users", value: "\(vm.totalUsers)",
                        subtitle: "\(vm.activeUsers) active this week", icon: "person.2", color: .blue)
            summaryCard(title: "Humans", value: "\(vm.totalHumans)",
                        subtitle: "\(vm.totalDeleted) deleted", icon: "person", color: .green)
            summaryCard(title: "Bots", value: "\(vm.totalBots)",
                        subtitle: "\(vm.overriddenCount) manually set", icon: "cpu", color: .orange)
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

    // MARK: - Charts

    private func chartsSection(_ vm: UserStatsViewModel) -> some View {
        HStack(alignment: .top, spacing: 12) {
            topUsersChart(vm)
            botBreakdownChart(vm)
        }
    }

    private func topUsersChart(_ vm: UserStatsViewModel) -> some View {
        let top = Array(vm.stats
            .filter { $0.totalMessages > 0 && !$0.effectiveIsBot }
            .sorted { $0.totalMessages > $1.totalMessages }
            .prefix(10)
            .reversed())

        return VStack(alignment: .leading, spacing: 8) {
            Text("Top Users by Messages")
                .font(.subheadline.weight(.semibold))

            if top.isEmpty {
                Text("No data")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                Chart(top) { user in
                    BarMark(
                        x: .value("Messages", user.totalMessages),
                        y: .value("User", user.bestName)
                    )
                    .foregroundStyle(.blue)
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
                                    .frame(width: 130, alignment: .trailing)
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

    private func botBreakdownChart(_ vm: UserStatsViewModel) -> some View {
        let bots = vm.stats
            .filter { $0.effectiveIsBot && $0.totalMessages > 0 }
            .sorted { $0.totalMessages > $1.totalMessages }
            .prefix(12)

        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Bot Activity")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                HStack(spacing: 8) {
                    legendDot(color: .orange, label: "Slack bot")
                    legendDot(color: .purple, label: "Manual")
                }
            }

            if bots.isEmpty {
                Text("No bot messages")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                let maxMsgs = bots.map(\.totalMessages).max() ?? 1
                VStack(spacing: 4) {
                    ForEach(Array(bots), id: \.id) { bot in
                        HStack(spacing: 8) {
                            Text(bot.bestName)
                                .font(.caption)
                                .lineLimit(1)
                                .frame(width: 100, alignment: .leading)

                            GeometryReader { geo in
                                ZStack(alignment: .leading) {
                                    RoundedRectangle(cornerRadius: 3)
                                        .fill(Color.secondary.opacity(0.15))
                                    RoundedRectangle(cornerRadius: 3)
                                        .fill(bot.isBotOverride != nil ? Color.purple : Color.orange)
                                        .frame(width: geo.size.width * CGFloat(bot.totalMessages) / CGFloat(maxMsgs))
                                }
                            }
                            .frame(height: 14)

                            Text(formatNumber(bot.totalMessages))
                                .font(.caption.monospacedDigit())
                                .frame(width: 40, alignment: .trailing)
                        }
                    }
                }
            }
        }
        .padding(12)
        .frame(width: 320)
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    private func legendDot(color: Color, label: String) -> some View {
        HStack(spacing: 4) {
            Circle().fill(color).frame(width: 6, height: 6)
            Text(label).font(.caption2).foregroundStyle(.secondary)
        }
    }

    // MARK: - Toolbar

    private func toolbar(_ vm: UserStatsViewModel) -> some View {
        HStack(spacing: 12) {
            Picker("Filter", selection: Binding(
                get: { vm.filter },
                set: { vm.filter = $0 }
            )) {
                ForEach(UserStatsViewModel.Filter.allCases, id: \.self) { filter in
                    Text(filter.rawValue).tag(filter)
                }
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 350)

            Spacer()

            Picker("Sort", selection: Binding(
                get: { vm.sortOrder },
                set: { vm.sortOrder = $0 }
            )) {
                ForEach(UserStatsViewModel.SortOrder.allCases, id: \.self) { sort in
                    Text(sort.rawValue).tag(sort)
                }
            }
            .frame(width: 130)

            TextField("Search users...", text: Binding(
                get: { vm.searchText },
                set: { vm.searchText = $0 }
            ))
            .textFieldStyle(.roundedBorder)
            .frame(width: 180)
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    // MARK: - User Table

    private func userTable(_ vm: UserStatsViewModel) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack(spacing: 0) {
                Text("User")
                    .frame(minWidth: 180, alignment: .leading)
                Text("Messages")
                    .frame(width: 70, alignment: .trailing)
                Text("Channels")
                    .frame(width: 70, alignment: .trailing)
                Text("Threads")
                    .frame(width: 60, alignment: .trailing)
                Text("Activity")
                    .frame(width: 80, alignment: .trailing)
                Text("Type")
                    .frame(width: 80, alignment: .center)
                Spacer()
                    .frame(width: 60)
            }
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 8)
            .padding(.vertical, 6)

            Divider()

            let filtered = vm.filteredStats
            if filtered.isEmpty {
                Text("No users match the current filter")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 20)
            } else {
                ForEach(filtered) { stat in
                    userRow(stat, vm: vm)
                    Divider()
                }
            }
        }
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    private func userRow(_ stat: UserStat, vm: UserStatsViewModel) -> some View {
        HStack(spacing: 0) {
            // User name
            HStack(spacing: 6) {
                Image(systemName: stat.effectiveIsBot ? "cpu" : "person.circle")
                    .foregroundStyle(stat.effectiveIsBot ? .orange : .blue)
                    .font(.caption)
                VStack(alignment: .leading, spacing: 1) {
                    Text(stat.bestName)
                        .lineLimit(1)
                    if !stat.name.isEmpty && stat.name != stat.bestName {
                        Text("@\(stat.name)")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            }
            .frame(minWidth: 180, alignment: .leading)

            Text("\(stat.totalMessages)")
                .frame(width: 70, alignment: .trailing)
                .monospacedDigit()

            Text("\(stat.channelCount)")
                .frame(width: 70, alignment: .trailing)
                .monospacedDigit()

            Text("\(stat.threadReplies)")
                .frame(width: 60, alignment: .trailing)
                .monospacedDigit()

            Text(lastActivityText(stat))
                .frame(width: 80, alignment: .trailing)
                .foregroundStyle(lastActivityColor(stat))
                .font(.caption)

            // Type badges
            HStack(spacing: 4) {
                if stat.effectiveIsBot {
                    Text("Bot")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.orange.opacity(0.15), in: Capsule())
                        .foregroundStyle(.orange)
                }
                if stat.isDeleted {
                    Text("Deleted")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.red.opacity(0.15), in: Capsule())
                        .foregroundStyle(.red)
                }
                if stat.isMutedForLLM {
                    Image(systemName: "speaker.slash.fill")
                        .font(.caption2)
                        .foregroundStyle(.red)
                        .help("Muted for AI")
                }
                if stat.isBotOverride != nil {
                    Image(systemName: "hand.raised.fill")
                        .font(.caption2)
                        .foregroundStyle(.purple)
                        .help("Manually set")
                }
            }
            .frame(width: 80, alignment: .center)

            // Actions
            HStack(spacing: 4) {
                Button {
                    vm.toggleBotOverride(userID: stat.id)
                } label: {
                    Image(systemName: stat.effectiveIsBot ? "person" : "cpu")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
                .help(stat.effectiveIsBot ? "Mark as human" : "Mark as bot")

                if stat.effectiveIsBot {
                    Button {
                        vm.toggleMuteForLLM(userID: stat.id)
                    } label: {
                        Image(systemName: stat.isMutedForLLM ? "speaker.wave.2" : "speaker.slash")
                            .font(.caption)
                            .foregroundStyle(stat.isMutedForLLM ? .green : .orange)
                    }
                    .buttonStyle(.borderless)
                    .help(stat.isMutedForLLM ? "Unmute for AI" : "Mute for AI")
                }
            }
            .frame(width: 60, alignment: .center)
        }
        .font(.body)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
    }

    // MARK: - Inactive Bar

    private func inactiveBar(_ vm: UserStatsViewModel) -> some View {
        HStack {
            Image(systemName: "eye.slash")
                .foregroundStyle(.secondary)
            Text("\(vm.inactiveCount) users hidden (no messages)")
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
            Button(vm.showInactive ? "Hide inactive" : "Show all users") {
                vm.showInactive.toggle()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .padding(10)
        .background(.background, in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(.quaternary, lineWidth: 1)
        )
    }

    // MARK: - Helpers

    private func lastActivityText(_ stat: UserStat) -> String {
        guard let days = stat.lastActivityDaysAgo else { return "Never" }
        if days == 0 { return "Today" }
        if days == 1 { return "Yesterday" }
        if days < 7 { return "\(days)d ago" }
        if days < 30 { return "\(days / 7)w ago" }
        return "\(days / 30)mo ago"
    }

    private func lastActivityColor(_ stat: UserStat) -> Color {
        guard let days = stat.lastActivityDaysAgo else { return .secondary }
        if days <= 1 { return .green }
        if days <= 7 { return .primary }
        if days <= 30 { return .orange }
        return .red
    }

    private func formatNumber(_ n: Int) -> String {
        if n >= 1000 {
            return String(format: "%.1fK", Double(n) / 1000.0)
        }
        return "\(n)"
    }
}
