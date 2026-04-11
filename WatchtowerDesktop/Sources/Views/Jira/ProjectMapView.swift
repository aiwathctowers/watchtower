import SwiftUI

struct ProjectMapView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: ProjectMapViewModel?
    @State private var selectedTab: Tab = .list

    enum Tab: String, CaseIterable {
        case list = "List"
        case gantt = "Gantt"
    }

    var body: some View {
        Group {
            if let vm = viewModel {
                mainContent(vm)
            } else {
                ProgressView("Loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .onAppear {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = ProjectMapViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
        .onDisappear {
            viewModel?.stopObserving()
        }
        .onChange(of: appState.isDBAvailable) {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = ProjectMapViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
    }

    // MARK: - Main Content

    @ViewBuilder
    private func mainContent(_ vm: ProjectMapViewModel) -> some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Project Map")
                    .font(.title2)
                    .fontWeight(.bold)

                if !vm.epics.isEmpty {
                    Text("\(vm.epics.count)")
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(.white)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(.blue, in: Capsule())
                }

                Spacer()

                // Tab picker
                Picker("View", selection: $selectedTab) {
                    ForEach(Tab.allCases, id: \.self) { tab in
                        Text(tab.rawValue).tag(tab)
                    }
                }
                .pickerStyle(.segmented)
                .frame(width: 140)
            }
            .padding()

            Divider()

            // Search bar (list tab only)
            if selectedTab == .list, !vm.epics.isEmpty {
                searchBar(vm)
            }

            // Content
            switch selectedTab {
            case .list:
                listContent(vm)
            case .gantt:
                ganttContent(vm)
            }
        }
    }

    // MARK: - Search Bar

    private func searchBar(_ vm: ProjectMapViewModel) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.caption)
                .foregroundStyle(.tertiary)

            @Bindable var vmBindable = vm
            TextField("Filter epics...", text: $vmBindable.searchText)
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
        .background(Color.secondary.opacity(0.06))
    }

    // MARK: - List Content

    @ViewBuilder
    private func listContent(_ vm: ProjectMapViewModel) -> some View {
        if vm.isLoading {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if vm.filteredEpics.isEmpty {
            emptyState(isFiltered: !vm.searchText.isEmpty)
        } else {
            listView(vm)
        }
    }

    private func listView(_ vm: ProjectMapViewModel) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 8) {
                // Summary stats
                summaryBar(vm)
                    .padding(.horizontal, 12)

                ForEach(vm.filteredEpics) { epic in
                    if let dbManager = appState.databaseManager {
                        EpicCardView(epic: epic, dbPool: dbManager.dbPool)
                            .padding(.horizontal, 10)
                    }
                }
            }
            .padding(.vertical, 8)
        }
    }

    // MARK: - Summary Bar

    private func summaryBar(_ vm: ProjectMapViewModel) -> some View {
        HStack(spacing: 16) {
            let behind = vm.epics.filter { $0.statusBadge == .behind }.count
            let atRisk = vm.epics.filter { $0.statusBadge == .atRisk }.count
            let onTrack = vm.epics.filter { $0.statusBadge == .onTrack }.count

            if behind > 0 {
                summaryChip(count: behind, label: "Behind", color: .red)
            }
            if atRisk > 0 {
                summaryChip(count: atRisk, label: "At Risk", color: .orange)
            }
            if onTrack > 0 {
                summaryChip(count: onTrack, label: "On Track", color: .green)
            }

            Spacer()
        }
        .padding(.vertical, 4)
    }

    private func summaryChip(count: Int, label: String, color: Color) -> some View {
        HStack(spacing: 4) {
            Circle()
                .fill(color)
                .frame(width: 8, height: 8)
            Text("\(count)")
                .font(.caption)
                .fontWeight(.bold)
            Text(label)
                .font(.caption)
        }
        .foregroundStyle(color)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(color.opacity(0.1), in: Capsule())
    }

    // MARK: - Empty State

    private func emptyState(isFiltered: Bool) -> some View {
        VStack(spacing: 12) {
            Image(systemName: isFiltered ? "magnifyingglass" : "map")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text(isFiltered ? "No matching epics" : "No epics found")
                .font(.title3)
                .foregroundStyle(.secondary)

            Text(
                isFiltered
                    ? "Try adjusting your search"
                    : "Epic issues from Jira will appear here"
            )
            .font(.caption)
            .foregroundStyle(.tertiary)
            .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Gantt Content

    @ViewBuilder
    private func ganttContent(_ vm: ProjectMapViewModel) -> some View {
        if vm.isLoading {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if vm.epics.isEmpty {
            emptyState(isFiltered: false)
        } else {
            GanttChartView(epics: vm.epics)
        }
    }
}
