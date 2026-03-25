import SwiftUI
import GRDB

struct SidebarView: View {
    @Binding var selection: SidebarDestination
    @Environment(AppState.self) private var appState
    @State private var updatedTrackCount: Int = 0
    @State private var totalTrackCount: Int = 0
    @State private var unreadDigestCount: Int = 0
    @State private var unreadBriefingCount: Int = 0
    @State private var recommendationCount: Int = 0
    @State private var countsObservationTask: Task<Void, Never>?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(SidebarDestination.mainItems) { item in
                sidebarButton(item)
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
        .onDisappear { countsObservationTask?.cancel() }
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
            case .briefings: return unreadBriefingCount
            case .tracks: return updatedTrackCount
            case .digests: return unreadDigestCount
            case .statistics: return recommendationCount
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
                .background(
                    item == .tracks ? .orange : .red,
                    in: Capsule()
                )
        }
    }

    // MARK: - Data Loading

    private func startObservingCounts() {
        guard countsObservationTask == nil, let db = appState.databaseManager else { return }
        loadCounts(db: db)
        let dbPool = db.dbPool
        countsObservationTask = Task {
            let observation = ValueObservation.tracking { db -> (Int, Int) in
                let tracks = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tracks") ?? 0
                let briefings = try Int.fetchOne(
                    db, sql: "SELECT COUNT(*) FROM briefings"
                ) ?? 0
                return (tracks, briefings)
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled, let dbMgr = appState.databaseManager else { break }
                    loadCounts(db: dbMgr)
                }
            } catch {}
        }
    }

    private struct SidebarCounts {
        let updatedTrackCount: Int
        let totalTrackCount: Int
        let unreadDigestCount: Int
        let unreadBriefingCount: Int
        let recommendationCount: Int
    }

    private func loadCounts(db: DatabaseManager) {
        Task {
            let result = try? await db.dbPool.read { db -> SidebarCounts in
                let uid = try TrackQueries.fetchCurrentUserID(db)

                guard let uid else {
                    return SidebarCounts(
                        updatedTrackCount: 0,
                        totalTrackCount: 0,
                        unreadDigestCount: 0,
                        unreadBriefingCount: 0,
                        recommendationCount: 0
                    )
                }

                let trackCounts = try TrackQueries.fetchCounts(db)

                let recCount: Int
                if let allStats = try? ChannelStatsQueries.fetchAll(db, currentUserID: uid) {
                    recCount = ChannelStatsQueries.computeRecommendations(from: allStats).count
                } else {
                    recCount = 0
                }
                return SidebarCounts(
                    updatedTrackCount: trackCounts.updated,
                    totalTrackCount: trackCounts.total,
                    unreadDigestCount: try DigestQueries.unreadDigestCount(db),
                    unreadBriefingCount: try BriefingQueries.unreadCount(db),
                    recommendationCount: recCount
                )
            }
            if let r = result {
                self.updatedTrackCount = r.updatedTrackCount
                self.totalTrackCount = r.totalTrackCount
                self.unreadDigestCount = r.unreadDigestCount
                self.unreadBriefingCount = r.unreadBriefingCount
                self.recommendationCount = r.recommendationCount
            }
        }
    }
}
