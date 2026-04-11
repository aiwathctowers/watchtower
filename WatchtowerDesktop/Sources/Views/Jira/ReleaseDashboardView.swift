import SwiftUI

struct ReleaseDashboardView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: ReleaseDashboardViewModel?

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
                let vm = ReleaseDashboardViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
        .onDisappear {
            viewModel?.stopObserving()
        }
        .onChange(of: appState.isDBAvailable) {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = ReleaseDashboardViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
    }

    // MARK: - Main Content

    @ViewBuilder
    private func mainContent(_ vm: ReleaseDashboardViewModel) -> some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Releases")
                    .font(.title2)
                    .fontWeight(.bold)

                if !vm.releases.isEmpty {
                    Text("\(vm.releases.count)")
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(.white)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(.blue, in: Capsule())
                }

                Spacer()
            }
            .padding()

            Divider()

            // Search bar
            if !vm.releases.isEmpty {
                searchBar(vm)
            }

            // Content
            if vm.isLoading {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if vm.filteredReleases.isEmpty {
                emptyState(isFiltered: !vm.searchText.isEmpty)
            } else {
                releaseList(vm)
            }
        }
    }

    // MARK: - Search Bar

    private func searchBar(_ vm: ReleaseDashboardViewModel) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.caption)
                .foregroundStyle(.tertiary)

            @Bindable var vmBindable = vm
            TextField("Filter releases...", text: $vmBindable.searchText)
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

    // MARK: - Release List

    private func releaseList(_ vm: ReleaseDashboardViewModel) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 8) {
                // Summary bar
                summaryBar(vm)
                    .padding(.horizontal, 12)

                ForEach(vm.filteredReleases) { release in
                    ReleaseCardView(release: release)
                        .padding(.horizontal, 10)
                }
            }
            .padding(.vertical, 8)
        }
    }

    // MARK: - Summary Bar

    private func summaryBar(_ vm: ReleaseDashboardViewModel) -> some View {
        HStack(spacing: 16) {
            summaryChip(
                count: vm.releases.count,
                label: "Releases",
                color: .blue
            )

            if vm.atRiskCount > 0 {
                summaryChip(count: vm.atRiskCount, label: "At Risk", color: .red)
            }
            if vm.overdueCount > 0 {
                summaryChip(count: vm.overdueCount, label: "Overdue", color: .orange)
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
            Image(systemName: isFiltered ? "magnifyingglass" : "shippingbox")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text(isFiltered ? "No matching releases" : "No releases found")
                .font(.title3)
                .foregroundStyle(.secondary)

            Text(
                isFiltered
                    ? "Try adjusting your search"
                    : "Unreleased versions from Jira will appear here"
            )
            .font(.caption)
            .foregroundStyle(.tertiary)
            .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Release Card

struct ReleaseCardView: View {
    let release: ReleaseDashboardViewModel.ReleaseItem
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Card header (always visible)
            cardHeader
                .contentShape(Rectangle())
                .onTapGesture {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        isExpanded.toggle()
                    }
                }

            // Expanded detail
            if isExpanded {
                Divider()
                    .padding(.horizontal, 12)

                ReleaseDetailView(release: release)
                    .padding(12)
            }
        }
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(Color.secondary.opacity(0.15), lineWidth: 1)
        )
    }

    private var cardHeader: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                // Expand chevron
                Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .frame(width: 12)

                Text(release.name)
                    .font(.headline)
                    .lineLimit(1)

                Text(release.projectKey)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 4)
                    .padding(.vertical, 1)
                    .background(Color.secondary.opacity(0.1), in: Capsule())

                Spacer()

                // Status badges
                statusBadges
            }

            // Progress bar
            HStack(spacing: 8) {
                ProgressView(value: release.progressPct)
                    .tint(progressColor)

                Text("\(Int(release.progressPct * 100))%")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 36, alignment: .trailing)
            }

            // Stats row
            HStack(spacing: 12) {
                if !release.releaseDate.isEmpty {
                    Label(release.releaseDate, systemImage: "calendar")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Label(
                    "\(release.doneIssues)/\(release.totalIssues) issues",
                    systemImage: "checkmark.circle"
                )
                .font(.caption)
                .foregroundStyle(.secondary)

                if release.blockedCount > 0 {
                    Label(
                        "\(release.blockedCount) blocked",
                        systemImage: "exclamationmark.triangle"
                    )
                    .font(.caption)
                    .foregroundStyle(.orange)
                }

                Spacer()
            }
        }
        .padding(12)
    }

    @ViewBuilder
    private var statusBadges: some View {
        if release.isOverdue {
            statusChip(text: "Overdue", color: .orange)
        }
        if release.atRisk {
            statusChip(text: "At Risk", color: .red)
        }
        if !release.isOverdue && !release.atRisk {
            statusChip(text: "On Track", color: .green)
        }
    }

    private func statusChip(text: String, color: Color) -> some View {
        Text(text)
            .font(.caption2)
            .fontWeight(.semibold)
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.12), in: Capsule())
    }

    private var progressColor: Color {
        if release.isOverdue { return .orange }
        if release.atRisk { return .red }
        if release.progressPct >= 0.8 { return .green }
        return .blue
    }
}
