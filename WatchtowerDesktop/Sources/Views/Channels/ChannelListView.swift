import SwiftUI

struct ChannelListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: ChannelViewModel?
    @State private var selectedChannelID: String?
    @State private var searchText = ""

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                channelList(vm)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            if let channelID = selectedChannelID {
                Divider()
                ChannelDetailView(channelID: channelID)
                    .id(channelID)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedChannelID)
        .navigationTitle("Channels")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = ChannelViewModel(dbManager: db)
                viewModel?.load()
            }
            consumePendingChannel()
        }
        .onChange(of: appState.pendingChannelID) {
            consumePendingChannel()
        }
    }

    private func consumePendingChannel() {
        if let channelID = appState.pendingChannelID {
            appState.pendingChannelID = nil
            selectedChannelID = channelID
        }
    }

    private var filteredChannels: [Channel] {
        guard let vm = viewModel else { return [] }
        if searchText.isEmpty { return vm.channels }
        return vm.channels.filter {
            let name = vm.displayName(for: $0)
            return name.localizedCaseInsensitiveContains(searchText)
                || $0.name.localizedCaseInsensitiveContains(searchText)
        }
    }

    private func channelList(_ vm: ChannelViewModel) -> some View {
        VStack(spacing: 0) {
            // Search field (same style as SearchView)
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(text: $searchText, placeholder: "Filter channels...")
                    .frame(height: 22)
            }
            .padding(12)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(filteredChannels) { channel in
                        channelRow(channel, vm: vm)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                selectedChannelID = channel.id
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(
                                selectedChannelID == channel.id
                                    ? Color.accentColor.opacity(0.15)
                                    : Color.clear,
                                in: RoundedRectangle(cornerRadius: 6)
                            )
                            .padding(.horizontal, 4)
                    }
                }
                .padding(.vertical, 4)
            }
        }
        .frame(minWidth: 280, idealWidth: 320)
    }

    private func channelRow(_ channel: Channel, vm: ChannelViewModel) -> some View {
        HStack {
            Image(systemName: channelIcon(channel.type))
                .foregroundStyle(channel.isArchived ? Color.secondary : Color.accentColor)

            VStack(alignment: .leading) {
                HStack {
                    Text(vm.displayName(for: channel))
                        .fontWeight(.medium)
                    if channel.isArchived {
                        Text("archived")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                if !channel.topic.isEmpty {
                    Text(channel.topic)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            if vm.watchedIDs.contains(channel.id) {
                Image(systemName: "star.fill")
                    .foregroundStyle(.yellow)
                    .font(.caption)
            }

            Text("\(channel.numMembers)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .opacity(channel.isArchived ? 0.6 : 1.0)
    }

    private func channelIcon(_ type: String) -> String {
        switch type {
        case "private": "lock"
        case "dm": "person"
        case "group_dm": "person.2"
        default: "number"
        }
    }
}
