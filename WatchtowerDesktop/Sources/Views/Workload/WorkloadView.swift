import SwiftUI

struct WorkloadView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: WorkloadViewModel?
    @State private var selectedUserID: String?

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let userID = selectedUserID,
                   let entry = vm.entries.first(where: { $0.slackUserID == userID }),
                   let dbManager = appState.databaseManager {
                    Divider()
                    WorkloadPersonDetailView(
                        entry: entry,
                        dbManager: dbManager,
                        onClose: { selectedUserID = nil }
                    )
                        .id(userID)
                        .frame(minWidth: 400, idealWidth: 500)
                        .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            } else {
                ProgressView("Loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedUserID)
        .onAppear {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = WorkloadViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
        .onChange(of: appState.isDBAvailable) {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = WorkloadViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
    }

    // MARK: - List Panel

    private func listPanel(_ vm: WorkloadViewModel) -> some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Team Workload")
                    .font(.title2)
                    .fontWeight(.bold)

                Spacer()

                if !vm.entries.isEmpty {
                    let overloadCount = vm.entries.filter { $0.signal == .overload }.count
                    let watchCount = vm.entries.filter { $0.signal == .watch }.count
                    if overloadCount > 0 {
                        signalCountBadge(count: overloadCount, signal: .overload)
                    }
                    if watchCount > 0 {
                        signalCountBadge(count: watchCount, signal: .watch)
                    }
                }
            }
            .padding()

            Divider()

            // Search bar
            searchBar(vm)

            // Signal filter chips
            signalFilterChips(vm)

            Divider()

            if vm.isLoading {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if vm.filteredEntries.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "gauge.with.dots.needle.33percent")
                        .font(.system(size: 48))
                        .foregroundStyle(.secondary)
                    Text("No workload data")
                        .font(.title3)
                        .foregroundStyle(.secondary)
                    Text("Jira issues with assigned Slack users will appear here")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                workloadTable(vm)
            }
        }
        .frame(minWidth: 500, idealWidth: 650)
    }

    // MARK: - Table

    private func workloadTable(_ vm: WorkloadViewModel) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 1) {
                // Header row
                tableHeaderRow()
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)

                Divider()

                ForEach(vm.filteredEntries) { entry in
                    tableRow(entry)
                        .contentShape(Rectangle())
                        .onTapGesture { selectedUserID = entry.slackUserID }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(rowBackground(entry), in: RoundedRectangle(cornerRadius: 6))
                        .padding(.horizontal, 4)
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func tableHeaderRow() -> some View {
        HStack(spacing: 0) {
            Text("Name")
                .frame(minWidth: 120, alignment: .leading)
            Spacer()
            Group {
                Text("Open")
                    .frame(width: 40)
                Text("In Prog")
                    .frame(width: 50)
                Text("Testing")
                    .frame(width: 50)
                Text("Late")
                    .frame(width: 40)
                Text("Block")
                    .frame(width: 40)
                Text("Cycle")
                    .frame(width: 50)
                Text("Signal")
                    .frame(width: 80)
            }
        }
        .font(.caption)
        .fontWeight(.semibold)
        .foregroundStyle(.secondary)
    }

    private func tableRow(_ entry: WorkloadViewModel.WorkloadEntry) -> some View {
        HStack(spacing: 0) {
            Text(entry.displayName)
                .font(.subheadline)
                .fontWeight(.medium)
                .lineLimit(1)
                .frame(minWidth: 120, alignment: .leading)

            Spacer()

            Group {
                Text("\(entry.openIssues)")
                    .frame(width: 40)
                Text("\(entry.inProgressCount)")
                    .foregroundStyle(Color.blue)
                    .frame(width: 50)
                Text("\(entry.testingCount)")
                    .foregroundStyle(Color.purple)
                    .frame(width: 50)
                Text("\(entry.overdueCount)")
                    .foregroundStyle(entry.overdueCount > 0 ? Color.red : Color.primary)
                    .frame(width: 40)
                Text("\(entry.blockedCount)")
                    .foregroundStyle(entry.blockedCount > 0 ? Color.orange : Color.primary)
                    .frame(width: 40)
                Text(String(format: "%.1f", entry.avgCycleTimeDays))
                    .frame(width: 50)
                signalBadge(entry.signal)
                    .frame(width: 80)
            }
            .font(.caption)
        }
    }

    // MARK: - Search & Filter

    private func searchBar(_ vm: WorkloadViewModel) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.caption)
                .foregroundStyle(.tertiary)

            @Bindable var vmBindable = vm
            TextField("Filter people...", text: $vmBindable.searchText)
                .textFieldStyle(.plain)
                .font(.subheadline)

            if !vm.searchText.isEmpty {
                Button {
                    vm.searchText = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8))
        .padding(.horizontal, 12)
        .padding(.top, 8)
    }

    private func signalFilterChips(_ vm: WorkloadViewModel) -> some View {
        HStack(spacing: 6) {
            filterChip("All", isActive: vm.signalFilter == nil) {
                vm.signalFilter = nil
            }
            ForEach(
                [WorkloadViewModel.WorkloadSignal.overload, .watch, .normal, .low],
                id: \.rawValue
            ) { signal in
                filterChip(signal.label, color: signalColor(signal), isActive: vm.signalFilter == signal) {
                    vm.signalFilter = vm.signalFilter == signal ? nil : signal
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }

    private func filterChip(
        _ label: String,
        color: Color = .secondary,
        isActive: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Text(label)
                .font(.caption2)
                .fontWeight(.medium)
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(
                    isActive ? color.opacity(0.2) : Color.secondary.opacity(0.08),
                    in: Capsule()
                )
                .foregroundStyle(isActive ? color : .secondary)
        }
        .buttonStyle(.plain)
    }

    // MARK: - Signal Badge

    private func signalBadge(_ signal: WorkloadViewModel.WorkloadSignal) -> some View {
        HStack(spacing: 3) {
            Text(signal.emoji)
                .font(.caption2)
            Text(signal.label)
                .font(.caption2)
                .fontWeight(.medium)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(signalColor(signal).opacity(0.15), in: Capsule())
        .foregroundStyle(signalColor(signal))
    }

    private func signalCountBadge(
        count: Int,
        signal: WorkloadViewModel.WorkloadSignal
    ) -> some View {
        HStack(spacing: 3) {
            Text(signal.emoji)
                .font(.caption2)
            Text("\(count)")
                .font(.caption2)
                .fontWeight(.bold)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(signalColor(signal).opacity(0.15), in: Capsule())
    }

    private func signalColor(_ signal: WorkloadViewModel.WorkloadSignal) -> Color {
        switch signal {
        case .overload: .red
        case .watch: .orange
        case .low: .secondary
        case .normal: .green
        }
    }

    private func rowBackground(_ entry: WorkloadViewModel.WorkloadEntry) -> Color {
        let isSelected = selectedUserID == entry.slackUserID
        if isSelected {
            return Color.accentColor.opacity(0.15)
        }
        switch entry.signal {
        case .overload: return Color.red.opacity(0.06)
        case .watch: return Color.orange.opacity(0.04)
        case .low: return Color.secondary.opacity(0.04)
        case .normal: return Color.clear
        }
    }
}
