import SwiftUI
import AppKit

// MARK: - InboxFeedView

struct InboxFeedView: View {
    @Environment(AppState.self) private var appState
    @State private var vm: InboxViewModel?
    @State private var feedbackItem: InboxItem?
    @State private var tab: Tab = .feed

    enum Tab { case feed, learned }

    var body: some View {
        VStack(spacing: 0) {
            Picker("", selection: $tab) {
                Text("Feed").tag(Tab.feed)
                Text("Learned").tag(Tab.learned)
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.vertical, 8)

            Divider()

            if tab == .feed {
                if let vm {
                    feedContent(vm)
                } else {
                    ProgressView("Loading...")
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            } else {
                learnedContent
            }
        }
        .onAppear { initViewModel() }
        .onChange(of: appState.isDBAvailable) { initViewModel() }
        .sheet(item: $feedbackItem) { item in
            if let vm {
                InboxFeedbackSheet(item: item) { rating, reason in
                    vm.submitFeedback(item, rating: rating, reason: reason)
                    feedbackItem = nil
                }
            }
        }
    }

    // MARK: - Init

    private func initViewModel() {
        guard vm == nil, let db = appState.databaseManager else { return }
        let newVM = InboxViewModel(dbManager: db)
        vm = newVM
        newVM.startObserving()
    }

    // MARK: - Learned Tab

    @ViewBuilder
    private var learnedContent: some View {
        if let dbPool = appState.databaseManager?.dbPool {
            InboxLearnedRulesView(db: dbPool)
        } else {
            Text("Database unavailable")
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: - Feed Content

    @ViewBuilder
    private func feedContent(_ vm: InboxViewModel) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 6) {
                // Pinned section
                if !vm.pinnedItems.isEmpty {
                    Text("Pinned")
                        .font(.headline)
                        .padding(.horizontal)

                    ForEach(vm.pinnedItems) { item in
                        InboxCardView(
                            item: item,
                            size: .pinned,
                            onOpen: { openItem(item, vm: vm) },
                            onSnooze: { option in snoozeItem(item, option: option, vm: vm) },
                            onDismiss: { vm.dismiss(item) },
                            onCreateTask: { vm.createTask(from: item) },
                            onFeedback: { rating, _ in
                                if rating == -1 {
                                    feedbackItem = item
                                } else {
                                    vm.submitFeedback(item, rating: rating, reason: "")
                                }
                            }
                        )
                        .padding(.horizontal)
                    }
                }

                // Feed section grouped by day
                Text("Feed")
                    .font(.headline)
                    .padding(.horizontal)
                    .padding(.top, vm.pinnedItems.isEmpty ? 0 : 8)

                ForEach(groupedByDay(vm.feedItems), id: \.day) { group in
                    Text(group.day)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal)
                        .padding(.top, 4)

                    ForEach(group.items) { item in
                        InboxCardView(
                            item: item,
                            size: cardSize(for: item),
                            onOpen: { openItem(item, vm: vm) },
                            onSnooze: { option in snoozeItem(item, option: option, vm: vm) },
                            onDismiss: { vm.dismiss(item) },
                            onCreateTask: { vm.createTask(from: item) },
                            onFeedback: { rating, _ in
                                if rating == -1 {
                                    feedbackItem = item
                                } else {
                                    vm.submitFeedback(item, rating: rating, reason: "")
                                }
                            }
                        )
                        .padding(.horizontal)
                        .onAppear { vm.markSeen(item) }
                    }
                }

                // Load more
                if !vm.feedItems.isEmpty {
                    Button("Load more") { vm.loadMore() }
                        .padding()
                }
            }
            .padding(.vertical)
        }
    }

    // MARK: - Helpers

    private func cardSize(for item: InboxItem) -> CardSize {
        item.itemClass == .ambient ? .compact : .medium
    }

    private func openItem(_ item: InboxItem, vm: InboxViewModel) {
        if let url = vm.slackMessageURL(for: item) {
            NSWorkspace.shared.open(url)
        }
    }

    private func snoozeItem(_ item: InboxItem, option: InboxCardView.SnoozeOption, vm: InboxViewModel) {
        let until: String
        let cal = Calendar.current
        let now = Date()
        switch option {
        case .oneHour:
            until = iso8601String(cal.date(byAdding: .hour, value: 1, to: now) ?? now)
        case .tillTomorrow:
            let tomorrow = cal.startOfDay(for: cal.date(byAdding: .day, value: 1, to: now) ?? now)
            until = iso8601String(tomorrow)
        case .tillMonday:
            var comps = DateComponents()
            comps.weekday = 2 // Monday
            let monday = cal.nextDate(after: now, matching: comps, matchingPolicy: .nextTime) ?? now
            until = iso8601String(cal.startOfDay(for: monday))
        }
        vm.snooze(item, until: until)
    }

    private func iso8601String(_ date: Date) -> String {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt.string(from: date)
    }

    // MARK: - Day Grouping

    private struct DayGroup {
        let day: String
        let items: [InboxItem]
    }

    private func groupedByDay(_ items: [InboxItem]) -> [DayGroup] {
        guard !items.isEmpty else { return [] }

        let cal = Calendar.current
        let now = Date()
        let todayStart = cal.startOfDay(for: now)
        let yesterdayStart = cal.date(byAdding: .day, value: -1, to: todayStart) ?? todayStart
        let weekStart = cal.date(byAdding: .day, value: -7, to: todayStart) ?? todayStart

        var buckets: [(key: String, order: Int, items: [InboxItem])] = []
        var bucketIndex: [String: Int] = [:]

        let dateFormatter = DateFormatter()
        dateFormatter.dateStyle = .medium
        dateFormatter.timeStyle = .none

        for item in items {
            let date = item.messageDate
            let label: String
            if date >= todayStart {
                label = "Today"
            } else if date >= yesterdayStart {
                label = "Yesterday"
            } else if date >= weekStart {
                label = "Earlier this week"
            } else {
                label = dateFormatter.string(from: cal.startOfDay(for: date))
            }

            if let idx = bucketIndex[label] {
                buckets[idx].items.append(item)
            } else {
                let order: Int
                switch label {
                case "Today":              order = 0
                case "Yesterday":          order = 1
                case "Earlier this week":  order = 2
                default:                   order = 3
                }
                bucketIndex[label] = buckets.count
                buckets.append((key: label, order: order, items: [item]))
            }
        }

        return buckets
            .sorted { $0.order < $1.order }
            .map { DayGroup(day: $0.key, items: $0.items) }
    }
}
