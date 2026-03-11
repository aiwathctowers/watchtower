import SwiftUI

struct SidebarView: View {
    @Binding var selection: SidebarDestination
    @Environment(AppState.self) private var appState
    @State private var statusCounts: [String: Int] = [:]
    @State private var totalCount: Int = 0

    private var isActionsExpanded: Bool { selection == .actions }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(SidebarDestination.mainItems) { item in
                sidebarButton(item)

                // Action status sub-items
                if item == .actions && isActionsExpanded {
                    actionSubItems
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }

            Spacer()

            // Background tasks progress
            SidebarProgressView()

            // Tools section
            VStack(alignment: .leading, spacing: 2) {
                Text("TOOLS")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 2)

                ForEach(SidebarDestination.toolItems) { item in
                    sidebarButton(item)
                }
            }

            // Update available indicator
            if appState.updateService.isUpdateAvailable {
                Button {
                    NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "arrow.down.circle.fill")
                            .foregroundStyle(.blue)
                        Text("Update Available")
                            .font(.caption)
                            .foregroundStyle(.primary)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.blue.opacity(0.1), in: RoundedRectangle(cornerRadius: 6))
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 8)
        .padding(.horizontal, 8)
        .frame(maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear { loadCounts() }
        .onChange(of: selection) { loadCounts() }
    }

    // MARK: - Action Sub-Items

    private var actionSubItems: some View {
        VStack(alignment: .leading, spacing: 1) {
            actionFilterRow(label: "Inbox", filter: nil, icon: "tray", count: statusCounts["inbox"] ?? 0)
            actionFilterRow(label: "Active", filter: "active", icon: "bolt.circle", count: statusCounts["active"] ?? 0)
            actionFilterRow(label: "Done", filter: "done", icon: "checkmark.circle", count: statusCounts["done"] ?? 0)
            actionFilterRow(label: "Dismissed", filter: "dismissed", icon: "xmark.circle", count: statusCounts["dismissed"] ?? 0)
            actionFilterRow(label: "Snoozed", filter: "snoozed", icon: "moon.zzz", count: statusCounts["snoozed"] ?? 0)
            actionFilterRow(label: "All", filter: "all", icon: "list.bullet", count: totalCount)
        }
        .padding(.leading, 20)
        .padding(.trailing, 2)
        .padding(.vertical, 2)
    }

    private func actionFilterRow(label: String, filter: String?, icon: String, count: Int) -> some View {
        let isSelected = appState.actionStatusFilter == filter
        return Button {
            appState.actionStatusFilter = filter
        } label: {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 10))
                    .foregroundStyle(isSelected ? .white : .secondary)
                    .frame(width: 16)
                Text(label)
                    .font(.subheadline)
                    .foregroundStyle(isSelected ? .white : .primary)
                Spacer()
                if count > 0 {
                    Text("\(count)")
                        .font(.caption2)
                        .foregroundStyle(isSelected ? Color.white.opacity(0.8) : Color.secondary)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                isSelected ? Color.accentColor : Color.clear,
                in: RoundedRectangle(cornerRadius: 5)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: - Main Sidebar Button

    private func sidebarButton(_ item: SidebarDestination) -> some View {
        let isSelected = selection == item
        return Button {
            selection = item
        } label: {
            HStack(spacing: 8) {
                Image(systemName: item.icon)
                    .frame(width: 20)
                    .foregroundStyle(isSelected ? .white : .secondary)
                Text(item.title)
                    .foregroundStyle(isSelected ? .white : .primary)
                Spacer()
                if item == .actions {
                    let inboxCount = statusCounts["inbox"] ?? 0
                    if inboxCount > 0 {
                        Text("\(inboxCount)")
                            .font(.caption2)
                            .fontWeight(.semibold)
                            .foregroundStyle(.white)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(.red, in: Capsule())
                    }
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(
                isSelected && item != .actions
                    ? Color.accentColor
                    : Color.clear,
                in: RoundedRectangle(cornerRadius: 6)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: - Data Loading

    private func loadCounts() {
        guard let db = appState.databaseManager else { return }
        Task.detached {
            let result: ([String: Int], Int) = (try? await db.dbPool.read { db in
                let uid = try ActionItemQueries.fetchCurrentUserID(db)
                guard let uid else { return ([:], 0) }
                let counts = try ActionItemQueries.fetchStatusCounts(db, assigneeUserID: uid)
                let total = try ActionItemQueries.fetchTotalCount(db, assigneeUserID: uid)
                return (counts, total)
            }) ?? ([:], 0)
            await MainActor.run {
                self.statusCounts = result.0
                self.totalCount = result.1
            }
        }
    }
}
