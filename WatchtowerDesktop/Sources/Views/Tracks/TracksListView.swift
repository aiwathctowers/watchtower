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
                    TrackDetailView(track: item, viewModel: vm) { selectedItemID = nil }
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
                vm.startObserving()
            }
        }
        .onChange(of: appState.isDBAvailable) {
            if viewModel == nil, let db = appState.databaseManager {
                let vm = TracksViewModel(dbManager: db)
                viewModel = vm
                vm.startObserving()
            }
        }
        .onChange(of: selectedItemID) { _, newID in
            if let id = newID, let track = viewModel?.itemByID(id), track.isUnread {
                viewModel?.markRead(track)
            }
        }
    }

    // MARK: - List Panel

    private func listPanel(_ vm: TracksViewModel) -> some View {
        VStack(spacing: 0) {
            // Toolbar
            VStack(spacing: 8) {
                HStack {
                    Text("Tracks")
                        .font(.title2)
                        .fontWeight(.bold)

                    if vm.updatedCount > 0 {
                        Text("\(vm.updatedCount)")
                            .font(.caption2)
                            .fontWeight(.semibold)
                            .foregroundStyle(.white)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(.orange, in: Capsule())
                    }

                    Spacer()

                    // Jira filter (hidden if not connected)
                    if vm.isJiraConnected {
                        Picker("Jira", selection: Bindable(vm).jiraFilter) {
                            ForEach(
                                TracksViewModel.JiraFilter.allCases,
                                id: \.self
                            ) { filter in
                                Text(filter.rawValue).tag(filter)
                            }
                        }
                        .frame(maxWidth: 120)
                    }

                    // Priority filter
                    Picker("Priority", selection: Bindable(vm).priorityFilter) {
                        Text("All").tag(String?.none)
                        Label("High", systemImage: "exclamationmark.triangle.fill")
                            .tag(String?.some("high"))
                        Label("Medium", systemImage: "minus.circle")
                            .tag(String?.some("medium"))
                        Label("Low", systemImage: "arrow.down.circle")
                            .tag(String?.some("low"))
                    }
                    .frame(maxWidth: 140)
                }

                // Ownership filter + view toggles
                HStack(spacing: 4) {
                    ownershipButton(vm, label: "All", value: nil)
                    ownershipButton(vm, label: "Mine", value: "mine")
                    ownershipButton(vm, label: "Delegated", value: "delegated")
                    ownershipButton(vm, label: "Watching", value: "watching")
                    Spacer()

                    Button {
                        vm.showRead.toggle()
                        vm.load()
                    } label: {
                        Image(systemName: vm.showRead ? "eye.fill" : "eye.slash")
                            .font(.caption)
                            .foregroundStyle(vm.showRead ? .primary : .secondary)
                    }
                    .buttonStyle(.plain)
                    .help(vm.showRead ? "Hide read tracks" : "Show read tracks")

                    Button {
                        vm.showDismissed.toggle()
                        vm.load()
                    } label: {
                        Image(systemName: "archivebox")
                            .font(.caption)
                            .foregroundStyle(vm.showDismissed ? .primary : .secondary)
                    }
                    .buttonStyle(.plain)
                    .help(vm.showDismissed ? "Hide dismissed" : "Show dismissed")
                }
            }
            .padding()
            .onChange(of: vm.priorityFilter) { vm.load() }
            .onChange(of: vm.ownershipFilter) { vm.load() }
            .onChange(of: vm.jiraFilter) { vm.load() }

            Divider()

            if vm.isLoading {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if vm.updatedTracks.isEmpty && vm.allTracks.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "binoculars")
                        .font(.system(size: 48))
                        .foregroundStyle(.secondary)
                    Text("No tracks yet")
                        .font(.title3)
                        .foregroundStyle(.secondary)
                    Text("Tracks are created automatically from your workspace activity")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 1) {
                        // Updates section
                        if !vm.updatedTracks.isEmpty {
                            sectionHeader("Updates", count: vm.updatedTracks.count, color: .orange)
                            ForEach(vm.updatedTracks) { track in
                                trackRow(track, vm: vm, isUpdate: true)
                            }
                        }

                        // All tracks section
                        if !vm.allTracks.isEmpty {
                            sectionHeader(
                                "All Tracks", count: vm.allTracks.count, color: .secondary
                            )
                            ForEach(vm.allTracks) { track in
                                trackRow(track, vm: vm, isUpdate: false)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
        }
        .frame(minWidth: 350, idealWidth: 420)
    }

    private func ownershipButton(
        _ vm: TracksViewModel, label: String, value: String?
    ) -> some View {
        Button {
            vm.ownershipFilter = value
        } label: {
            Text(label)
                .font(.caption)
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(
                    vm.ownershipFilter == value
                        ? Color.accentColor.opacity(0.15)
                        : Color.secondary.opacity(0.08),
                    in: Capsule()
                )
                .foregroundStyle(
                    vm.ownershipFilter == value ? .primary : .secondary
                )
        }
        .buttonStyle(.plain)
    }

    private func sectionHeader(
        _ title: String, count: Int, color: Color
    ) -> some View {
        HStack(spacing: 6) {
            Text(title.uppercased())
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(color)
            Text("\(count)")
                .font(.system(size: 9, weight: .bold))
                .foregroundStyle(.white)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(color, in: Capsule())
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.top, 12)
        .padding(.bottom, 4)
    }

    private func trackRow(_ track: Track, vm: TracksViewModel, isUpdate: Bool) -> some View {
        let isSelected = selectedItemID == track.id
        let bgColor: Color = isSelected
            ? Color.accentColor.opacity(0.15)
            : track.isDismissed
                ? Color.secondary.opacity(0.04)
                : track.isUnread
                    ? Color.blue.opacity(0.06)
                    : Color.clear

        return TrackRow(track: track, viewModel: vm)
            .contentShape(Rectangle())
            .onTapGesture { selectedItemID = track.id }
            .contextMenu {
                if track.isDismissed {
                    Button {
                        vm.restoreTrack(track)
                    } label: {
                        Label("Restore", systemImage: "arrow.uturn.backward")
                    }
                } else {
                    Button {
                        if selectedItemID == track.id { selectedItemID = nil }
                        vm.dismissTrack(track)
                    } label: {
                        Label("Dismiss", systemImage: "archivebox")
                    }
                }
                if track.isUnread {
                    Button {
                        vm.markRead(track)
                    } label: {
                        Label("Mark as Read", systemImage: "eye")
                    }
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(bgColor, in: RoundedRectangle(cornerRadius: 6))
            .padding(.horizontal, 4)
    }
}

// MARK: - Track Row

struct TrackRow: View {
    let track: Track
    let viewModel: TracksViewModel

    private var isRead: Bool { track.isRead }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Line 1: unread dot + priority + category + channels + time
            HStack(alignment: .center, spacing: 6) {
                priorityIcon

                Text(track.categoryLabel)
                    .font(.caption2)
                    .fontWeight(.semibold)
                    .foregroundStyle(categoryColor)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(categoryColor.opacity(0.12), in: Capsule())

                channelBadges

                Spacer()

                Text(track.updatedAgo)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            // Line 2: main text
            Text(viewModel.resolveUserIDs(track.text))
                .font(.subheadline)
                .fontWeight(isRead ? .regular : .medium)
                .foregroundStyle(isRead ? .secondary : .primary)
                .lineLimit(2)

            // Line 3: metadata row
            HStack(spacing: 10) {
                // Requester
                if !track.requesterName.isEmpty {
                    Label(
                        viewModel.resolveUserIDs(track.requesterName),
                        systemImage: "person.fill"
                    )
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                }

                // Due date
                if let dueFormatted = track.dueDateFormatted {
                    Label(dueFormatted, systemImage: "calendar")
                        .font(.caption2)
                        .foregroundStyle(track.isOverdue ? Color.red : Color.secondary)
                }

                // Task count
                let tasks = viewModel.taskCount(for: track.id)
                if tasks > 0 {
                    Label("\(tasks)", systemImage: "checkmark.circle")
                        .font(.caption2)
                        .foregroundStyle(.green)
                }

                Spacer()

                // Ownership badge
                ownershipBadge
            }

            // Jira badges
            let jiraIssues = viewModel.jiraIssues(for: track.id)
            if !jiraIssues.isEmpty {
                HStack(spacing: 4) {
                    ForEach(jiraIssues.prefix(3), id: \.key) { issue in
                        JiraBadgeView(
                            issue: issue,
                            siteURL: viewModel.jiraSiteURL
                        )
                    }
                    if jiraIssues.count > 3 {
                        Text("+\(jiraIssues.count - 3)")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            // Tags + participants on one line
            let trackTags = track.decodedTags
            let people = track.decodedParticipants
            if !trackTags.isEmpty || !people.isEmpty {
                HStack(spacing: 6) {
                    ForEach(trackTags.prefix(3), id: \.self) { tag in
                        Text(tag)
                            .font(.system(size: 9))
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(.quaternary, in: Capsule())
                    }
                    if trackTags.count > 3 {
                        Text("+\(trackTags.count - 3)")
                            .font(.system(size: 9))
                            .foregroundStyle(.tertiary)
                    }

                    if !people.isEmpty {
                        Spacer()
                        Image(systemName: "person.2.fill")
                            .font(.system(size: 9))
                            .foregroundStyle(.tertiary)
                        Text(people.prefix(3).map(\.name).joined(separator: ", "))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                        if people.count > 3 {
                            Text("+\(people.count - 3)")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Components

    @ViewBuilder
    private var priorityIcon: some View {
        switch track.priority {
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

    @ViewBuilder
    private var ownershipBadge: some View {
        switch track.ownership {
        case "mine":
            Label("Mine", systemImage: "person.fill")
                .font(.system(size: 9))
                .foregroundStyle(.green)
        case "delegated":
            Label("Delegated", systemImage: "arrow.right.circle.fill")
                .font(.system(size: 9))
                .foregroundStyle(.purple)
        case "watching":
            Label("Watching", systemImage: "eye.fill")
                .font(.system(size: 9))
                .foregroundStyle(.secondary)
        default:
            EmptyView()
        }
    }

    private var categoryColor: Color {
        switch track.category {
        case "decision": .orange
        case "risk", "blocker": .red
        case "fyi": .blue
        case "question": .purple
        case "project": .indigo
        default: .secondary
        }
    }

    @ViewBuilder
    private var channelBadges: some View {
        let channels = track.decodedChannelIDs
        if !channels.isEmpty {
            HStack(spacing: 4) {
                ForEach(channels.prefix(2), id: \.self) { chID in
                    let name = viewModel.channelName(for: chID) ?? chID
                    Text("#\(name)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                if channels.count > 2 {
                    Text("+\(channels.count - 2)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }
}
