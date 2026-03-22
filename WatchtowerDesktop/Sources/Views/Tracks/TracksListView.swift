import SwiftUI

struct TracksListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: TracksViewModel?
    @State private var selectedItemID: Int?

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let id = selectedItemID, let item = vm.itemByID(id) {
                    Divider()
                    TrackDetailView(item: item, viewModel: vm) { selectedItemID = nil }
                        .id(id)
                        .frame(minWidth: 400, idealWidth: 500)
                        .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            } else {
                ProgressView("Loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedItemID)
        .onAppear {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = TracksViewModel(dbManager: db)
                viewModel = vm
                vm.statusFilter = appState.trackStatusFilter
                vm.ownershipFilter = appState.trackOwnershipFilter
                vm.startObserving()
            } else {
                syncFilterAndLoad()
            }
        }
        .onChange(of: appState.isDBAvailable) {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = TracksViewModel(dbManager: db)
                viewModel = vm
                vm.statusFilter = appState.trackStatusFilter
                vm.ownershipFilter = appState.trackOwnershipFilter
                vm.startObserving()
            }
        }
        .onChange(of: appState.trackStatusFilter) {
            syncFilterAndLoad()
        }
        .onChange(of: appState.trackOwnershipFilter) {
            syncFilterAndLoad()
        }
    }

    private func syncFilterAndLoad() {
        viewModel?.statusFilter = appState.trackStatusFilter
        viewModel?.ownershipFilter = appState.trackOwnershipFilter
        viewModel?.load()
    }

    // MARK: - List Panel

    private func listPanel(_ vm: TracksViewModel) -> some View {
        VStack(spacing: 0) {
            // Toolbar
            HStack {
                Text(statusTitle)
                    .font(.title2)
                    .fontWeight(.bold)

                if vm.updatedCount > 0 {
                    Image(systemName: "bell.badge.fill")
                        .foregroundStyle(.orange)
                        .font(.caption)
                }

                Spacer()

                // Ownership filter
                Picker("Ownership", selection: Bindable(vm).ownershipFilter) {
                    Text("All").tag(String?.none)
                    Label("Mine", systemImage: "person.fill").tag(String?.some("mine"))
                    Label("Delegated", systemImage: "arrow.right.circle.fill").tag(String?.some("delegated"))
                    Label("Watching", systemImage: "eye.fill").tag(String?.some("watching"))
                }
                .frame(maxWidth: 140)

                // Priority filter
                Picker("Priority", selection: Bindable(vm).priorityFilter) {
                    Text("All").tag(String?.none)
                    Label("High", systemImage: "exclamationmark.triangle.fill").tag(String?.some("high"))
                    Label("Medium", systemImage: "minus.circle").tag(String?.some("medium"))
                    Label("Low", systemImage: "arrow.down.circle").tag(String?.some("low"))
                }
                .frame(maxWidth: 140)

                // Starred channels filter
                Toggle(isOn: Bindable(vm).starredOnly) {
                    Image(systemName: vm.starredOnly ? "star.fill" : "star")
                        .foregroundStyle(vm.starredOnly ? .yellow : .secondary)
                }
                .toggleStyle(.button)
                .buttonStyle(.borderless)
                .help(vm.starredOnly ? "Show all channels" : "Show starred channels only")
            }
            .padding()
            .onChange(of: vm.ownershipFilter) {
                appState.trackOwnershipFilter = vm.ownershipFilter
                vm.load()
            }
            .onChange(of: vm.priorityFilter) { vm.load() }
            .onChange(of: vm.starredOnly) { vm.load() }

            Divider()

            if vm.isLoading {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if vm.items.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "checkmark.circle")
                        .font(.system(size: 48))
                        .foregroundStyle(.secondary)
                    Text("No tracks")
                        .font(.title3)
                        .foregroundStyle(.secondary)
                    Text("Tracks are extracted from your Slack messages by AI")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(spacing: 8) {
                        ForEach(vm.items) { item in
                            itemRow(item, vm: vm)
                        }
                    }
                    .padding(.vertical, 8)
                    .padding(.horizontal, 8)
                }
            }
        }
        .frame(minWidth: 350, idealWidth: 420)
    }

    private var statusTitle: String {
        switch appState.trackStatusFilter {
        case nil: "Inbox"
        case "inbox": "Inbox"
        case "active": "Active"
        case "done": "Done"
        case "dismissed": "Dismissed"
        case "snoozed": "Snoozed"
        case "all": "All Tracks"
        default: "Tracks"
        }
    }

    private func itemRow(_ item: Track, vm: TracksViewModel) -> some View {
        TrackRow(item: item, viewModel: vm)
            .contentShape(Rectangle())
            .onTapGesture { selectedItemID = item.id }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(
                selectedItemID == item.id
                    ? Color.accentColor.opacity(0.12)
                    : item.hasUpdates
                        ? Color.orange.opacity(0.06)
                        : Color(nsColor: .controlBackgroundColor),
                in: RoundedRectangle(cornerRadius: 8)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(
                        selectedItemID == item.id
                            ? Color.accentColor.opacity(0.3)
                            : item.hasUpdates
                                ? Color.orange.opacity(0.25)
                                : Color.primary.opacity(0.06),
                        lineWidth: 1
                    )
            )
    }
}

struct TrackRow: View {
    let item: Track
    let viewModel: TracksViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            topRow
            trackText
            metadataRow
            subItemsRow
            contextRow
            blockingRow
            participantsRow
            snoozeRow
        }
        .padding(.vertical, 4)
    }

    private var topRow: some View {
        HStack(spacing: 6) {
            priorityIcon
            if item.hasUpdates {
                Image(systemName: "bell.badge.fill")
                    .foregroundStyle(.orange)
                    .font(.caption)
            }
            ownershipBadge
            channelLink
            if item.isOverdue {
                Text("OVERDUE")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(.white)
                    .padding(.horizontal, 4)
                    .padding(.vertical, 1)
                    .background(.red, in: Capsule())
            }
            Spacer()
            Text(item.sourceDate, style: .relative)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
    }

    @ViewBuilder
    private var ownershipBadge: some View {
        if item.isDelegated {
            Text("Delegated")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.white)
                .padding(.horizontal, 4)
                .padding(.vertical, 1)
                .background(.indigo, in: Capsule())
        } else if item.isWatching {
            Text("Watching")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.white)
                .padding(.horizontal, 4)
                .padding(.vertical, 1)
                .background(.teal, in: Capsule())
        }
    }

    @ViewBuilder
    private var channelLink: some View {
        if !item.sourceChannelName.isEmpty {
            if let url = viewModel.slackChannelURL(channelID: item.channelID) {
                Link(destination: url) {
                    Text("#\(item.sourceChannelName)")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            } else {
                Text("#\(item.sourceChannelName)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var trackText: some View {
        Text(item.text)
            .font(.subheadline)
            .fontWeight((item.isInbox || item.isActive) ? .medium : .regular)
            .strikethrough(item.isDone)
            .foregroundStyle(item.isDone || item.isDismissed ? .secondary : .primary)
            .lineLimit(2)
    }

    private var metadataRow: some View {
        HStack(spacing: 6) {
            if !item.categoryLabel.isEmpty {
                Text(item.categoryLabel)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(.quaternary, in: Capsule())
            }
            if !item.requesterName.isEmpty {
                Text("from \(item.requesterName)")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    @ViewBuilder
    private var subItemsRow: some View {
        let subProgress = item.subItemsProgress
        if subProgress.total > 0 {
            HStack(spacing: 4) {
                Image(systemName: "checklist")
                    .font(.system(size: 9))
                    .foregroundStyle(subProgress.done == subProgress.total ? .green : .secondary)
                Text("\(subProgress.done)/\(subProgress.total)")
                    .font(.caption2)
                    .foregroundStyle(subProgress.done == subProgress.total ? .green : .secondary)
                ProgressView(value: Double(subProgress.done), total: Double(subProgress.total))
                    .frame(width: 50)
            }
        }
    }

    @ViewBuilder
    private var contextRow: some View {
        if !item.context.isEmpty {
            Text(item.context)
                .font(.caption)
                .foregroundStyle(.tertiary)
                .lineLimit(2)
        }
    }

    @ViewBuilder
    private var blockingRow: some View {
        if !item.blocking.isEmpty {
            HStack(spacing: 4) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 9))
                    .foregroundStyle(.orange)
                Text(item.blocking)
                    .font(.caption2)
                    .foregroundStyle(.orange)
                    .lineLimit(1)
            }
        }
    }

    @ViewBuilder
    private var participantsRow: some View {
        let people = item.decodedParticipants
        if !people.isEmpty {
            HStack(spacing: 4) {
                Image(systemName: "person.2.fill")
                    .font(.system(size: 9))
                    .foregroundStyle(.tertiary)
                Text(people.map(\.name).joined(separator: ", "))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
        }
    }

    @ViewBuilder
    private var snoozeRow: some View {
        if item.status == "snoozed", let snoozeText = item.snoozeUntilFormatted {
            Label("Snoozed until \(snoozeText)", systemImage: "moon.zzz.fill")
                .font(.caption2)
                .foregroundStyle(.purple)
        }
    }

    @ViewBuilder
    private var priorityIcon: some View {
        switch item.priority {
        case "high":
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
                .font(.caption)
        case "medium":
            Image(systemName: "minus.circle.fill")
                .foregroundStyle(.orange)
                .font(.caption)
        default:
            Image(systemName: "arrow.down.circle.fill")
                .foregroundStyle(.blue)
                .font(.caption)
        }
    }
}
