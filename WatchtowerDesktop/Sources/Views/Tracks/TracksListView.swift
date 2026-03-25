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
    }

    // MARK: - List Panel

    private func listPanel(_ vm: TracksViewModel) -> some View {
        VStack(spacing: 0) {
            // Toolbar
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
            .padding()
            .onChange(of: vm.priorityFilter) { vm.load() }

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
                    LazyVStack(alignment: .leading, spacing: 0) {
                        // Updates section
                        if !vm.updatedTracks.isEmpty {
                            sectionHeader("Updates", count: vm.updatedTracks.count, color: .orange)
                            ForEach(vm.updatedTracks) { track in
                                trackRow(track, vm: vm, isUpdate: true)
                            }
                        }

                        // All tracks section
                        if !vm.allTracks.isEmpty {
                            sectionHeader("All Tracks", count: vm.allTracks.count, color: .secondary)
                            ForEach(vm.allTracks) { track in
                                trackRow(track, vm: vm, isUpdate: false)
                            }
                        }
                    }
                    .padding(.vertical, 8)
                    .padding(.horizontal, 8)
                }
            }
        }
        .frame(minWidth: 350, idealWidth: 420)
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
        TrackRow(track: track, viewModel: vm)
            .contentShape(Rectangle())
            .onTapGesture { selectedItemID = track.id }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(
                selectedItemID == track.id
                    ? Color.accentColor.opacity(0.12)
                    : isUpdate
                        ? Color.orange.opacity(0.06)
                        : Color(nsColor: .controlBackgroundColor),
                in: RoundedRectangle(cornerRadius: 8)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(
                        selectedItemID == track.id
                            ? Color.accentColor.opacity(0.3)
                            : isUpdate
                                ? Color.orange.opacity(0.25)
                                : Color.primary.opacity(0.06),
                        lineWidth: 1
                    )
            )
            .padding(.vertical, 2)
    }
}

// MARK: - Track Row

struct TrackRow: View {
    let track: Track
    let viewModel: TracksViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Top: priority + update badge + channels + time
            HStack(spacing: 6) {
                priorityIcon
                if track.hasUpdates {
                    Image(systemName: "bell.badge.fill")
                        .foregroundStyle(.orange)
                        .font(.caption)
                }
                channelBadges
                Spacer()
                Text(track.updatedAgo)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            // Title
            Text(track.title)
                .font(.subheadline)
                .fontWeight(.medium)
                .lineLimit(2)

            // Current status snippet
            if !track.currentStatus.isEmpty {
                Text(viewModel.resolveUserIDs(track.currentStatus))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            // Tags
            let trackTags = track.decodedTags
            if !trackTags.isEmpty {
                HStack(spacing: 4) {
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
                }
            }

            // Participants
            let people = track.decodedParticipants
            if !people.isEmpty {
                HStack(spacing: 4) {
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
        .padding(.vertical, 2)
    }

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
