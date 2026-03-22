import SwiftUI
import GRDB

struct SidebarView: View {
    @Binding var selection: SidebarDestination
    @Environment(AppState.self) private var appState
    @State private var statusCounts: [String: Int] = [:]
    @State private var totalCount: Int = 0
    @State private var ownershipCounts: [String: Int] = [:]
    @State private var unreadChainCount: Int = 0
    @State private var unreadDigestCount: Int = 0
    @State private var countsObservationTask: Task<Void, Never>?

    private var isTracksExpanded: Bool { selection == .tracks }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(SidebarDestination.mainItems) { item in
                sidebarButton(item)

                // Track status sub-items
                if item == .tracks && isTracksExpanded {
                    trackSubItems
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
        .onAppear { startObservingCounts() }
        .onChange(of: selection) { loadCounts(db: appState.databaseManager!) }
        .onDisappear { countsObservationTask?.cancel() }
    }

    // MARK: - Track Sub-Items

    private var trackSubItems: some View {
        VStack(alignment: .leading, spacing: 1) {
            // Ownership filters
            Text("OWNERSHIP")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.quaternary)
                .padding(.horizontal, 8)
                .padding(.top, 2)
            trackOwnershipRow(label: "Mine", ownership: "mine", icon: "person.fill", count: ownershipCounts["mine"] ?? 0)
            trackOwnershipRow(label: "Delegated", ownership: "delegated", icon: "arrow.right.circle.fill", count: ownershipCounts["delegated"] ?? 0)
            trackOwnershipRow(label: "Watching", ownership: "watching", icon: "eye.fill", count: ownershipCounts["watching"] ?? 0)

            Divider()
                .padding(.vertical, 2)

            // Status filters
            trackFilterRow(label: "Inbox", filter: nil, icon: "tray", count: statusCounts["inbox"] ?? 0)
            trackFilterRow(label: "Active", filter: "active", icon: "bolt.circle", count: statusCounts["active"] ?? 0)
            trackFilterRow(label: "Done", filter: "done", icon: "checkmark.circle", count: statusCounts["done"] ?? 0)
            trackFilterRow(label: "Dismissed", filter: "dismissed", icon: "xmark.circle", count: statusCounts["dismissed"] ?? 0)
            trackFilterRow(label: "Snoozed", filter: "snoozed", icon: "moon.zzz", count: statusCounts["snoozed"] ?? 0)
            trackFilterRow(label: "All", filter: "all", icon: "list.bullet", count: totalCount)
        }
        .padding(.leading, 20)
        .padding(.trailing, 2)
        .padding(.vertical, 2)
    }

    private func trackOwnershipRow(label: String, ownership: String, icon: String, count: Int) -> some View {
        Button {
            // Toggle: if already selected, clear; otherwise set
            if appState.trackOwnershipFilter == ownership {
                appState.trackOwnershipFilter = nil
            } else {
                appState.trackOwnershipFilter = ownership
            }
        } label: {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 10))
                    .foregroundStyle(appState.trackOwnershipFilter == ownership ? .white : .secondary)
                    .frame(width: 16)
                Text(label)
                    .font(.subheadline)
                    .foregroundStyle(appState.trackOwnershipFilter == ownership ? .white : .primary)
                Spacer()
                if count > 0 {
                    Text("\(count)")
                        .font(.caption2)
                        .foregroundStyle(appState.trackOwnershipFilter == ownership ? Color.white.opacity(0.8) : Color.secondary)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                appState.trackOwnershipFilter == ownership ? Color.accentColor : Color.clear,
                in: RoundedRectangle(cornerRadius: 5)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func trackFilterRow(label: String, filter: String?, icon: String, count: Int) -> some View {
        let isSelected = appState.trackStatusFilter == filter
        return Button {
            appState.trackStatusFilter = filter
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
                badgeCount(for: item)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(
                isSelected
                    ? Color.accentColor
                    : Color.clear,
                in: RoundedRectangle(cornerRadius: 6)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder
    private func badgeCount(for item: SidebarDestination) -> some View {
        let count: Int = {
            switch item {
            case .tracks: return statusCounts["inbox"] ?? 0
            case .chains: return unreadChainCount
            default: return 0
            }
        }()
        if count > 0 {
            Text("\(count)")
                .font(.caption2)
                .fontWeight(.semibold)
                .foregroundStyle(.white)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(.red, in: Capsule())
        }
    }

    // MARK: - Data Loading

    private func startObservingCounts() {
        guard countsObservationTask == nil, let db = appState.databaseManager else { return }
        loadCounts(db: db)
        let dbPool = db.dbPool
        countsObservationTask = Task {
            // Observe both tracks and chains tables for badge updates
            let observation = ValueObservation.tracking { db -> (Int, Int) in
                let tracks = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tracks") ?? 0
                let chains = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM chains") ?? 0
                return (tracks, chains)
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    loadCounts(db: appState.databaseManager!)
                }
            } catch {}
        }
    }

    private func loadCounts(db: DatabaseManager) {
        Task {
            let result = try? await db.dbPool.read { db -> ([String: Int], Int, [String: Int], Int, Int) in
                let uid = try TrackQueries.fetchCurrentUserID(db)
                guard let uid else { return ([:], 0, [:], 0, 0) }
                let counts = try TrackQueries.fetchStatusCounts(db, assigneeUserID: uid)
                let total = try TrackQueries.fetchTotalCount(db, assigneeUserID: uid)
                let oCounts = try TrackQueries.fetchOwnershipCounts(db, assigneeUserID: uid)
                let unreadChains = try ChainQueries.fetchUnreadCount(db)
                let unreadDigests = try DigestQueries.unreadDigestCount(db)
                return (counts, total, oCounts, unreadChains, unreadDigests)
            }
            if let r = result {
                self.statusCounts = r.0
                self.totalCount = r.1
                self.ownershipCounts = r.2
                self.unreadChainCount = r.3
                self.unreadDigestCount = r.4
            }
        }
    }
}
