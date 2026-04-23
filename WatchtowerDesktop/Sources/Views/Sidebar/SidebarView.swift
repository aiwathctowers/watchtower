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
    @State private var activeTaskCount: Int = 0
    @State private var overdueTaskCount: Int = 0
    @State private var inboxPendingCount: Int = 0
    @State private var inboxHighPriorityCount: Int = 0
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

            // Next calendar event
            if let calVM = appState.calendarViewModel, let nextEvt = calVM.nextEvent {
                HStack(spacing: 4) {
                    Image(systemName: "calendar")
                        .foregroundStyle(.secondary)
                        .frame(width: 16)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(nextEvt.title)
                            .font(.caption)
                            .lineLimit(1)
                        Text(nextEvt.startDate, style: .relative)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
            }

            // Jira connection indicator
            if JiraQueries.isConnected() {
                HStack(spacing: 4) {
                    Image(systemName: "bolt.horizontal.circle.fill")
                        .foregroundStyle(.blue)
                        .frame(width: 16)
                    Text("Jira connected")
                        .font(.caption)
                        .lineLimit(1)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 4)
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
        if item == .dayPlan {
            if appState.dayPlanViewModel?.hasConflicts == true {
                Circle()
                    .fill(Color.red)
                    .frame(width: 6, height: 6)
            }
        } else {
            let count: Int = {
                switch item {
                case .briefings: return unreadBriefingCount
                case .inbox: return inboxPendingCount
                case .tasks: return overdueTaskCount > 0 ? overdueTaskCount : activeTaskCount
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
                        item == .tracks ? .orange
                            : item == .inbox && inboxHighPriorityCount > 0 ? .red
                            : item == .inbox ? .blue
                            : item == .tasks && overdueTaskCount > 0 ? .red
                            : item == .tasks ? .blue
                            : .red,
                        in: Capsule()
                    )
            }
        }
    }

    // MARK: - Data Loading

    private func startObservingCounts() {
        guard countsObservationTask == nil, let db = appState.databaseManager else { return }
        loadCounts(db: db)
        let dbPool = db.dbPool
        countsObservationTask = Task {
            let observation = ValueObservation.tracking { db -> (Int, Int, Int, Int) in
                let tracks = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tracks") ?? 0
                let briefings = try Int.fetchOne(
                    db, sql: "SELECT COUNT(*) FROM briefings"
                ) ?? 0
                let tasks = try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tasks") ?? 0
                let inbox = (try? Int.fetchOne(db, sql: "SELECT COUNT(*) FROM inbox_items")) ?? 0
                return (tracks, briefings, tasks, inbox)
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
        let activeTaskCount: Int
        let overdueTaskCount: Int
        let inboxPendingCount: Int
        let inboxHighPriorityCount: Int
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
                        recommendationCount: 0,
                        activeTaskCount: 0,
                        overdueTaskCount: 0,
                        inboxPendingCount: 0,
                        inboxHighPriorityCount: 0
                    )
                }

                let trackCounts = try TrackQueries.fetchCounts(db)
                let taskCounts = try TaskQueries.fetchCounts(db)
                let inboxCounts = (try? InboxQueries.fetchCounts(db)) ?? (pending: 0, unread: 0, highPriority: 0)

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
                    recommendationCount: recCount,
                    activeTaskCount: taskCounts.active,
                    overdueTaskCount: taskCounts.overdue,
                    inboxPendingCount: inboxCounts.pending,
                    inboxHighPriorityCount: inboxCounts.highPriority
                )
            }
            if let r = result {
                self.updatedTrackCount = r.updatedTrackCount
                self.totalTrackCount = r.totalTrackCount
                self.unreadDigestCount = r.unreadDigestCount
                self.unreadBriefingCount = r.unreadBriefingCount
                self.recommendationCount = r.recommendationCount
                self.activeTaskCount = r.activeTaskCount
                self.overdueTaskCount = r.overdueTaskCount
                self.inboxPendingCount = r.inboxPendingCount
                self.inboxHighPriorityCount = r.inboxHighPriorityCount
            }
        }
    }
}
