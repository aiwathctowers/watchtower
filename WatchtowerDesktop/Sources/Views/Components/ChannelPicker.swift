import SwiftUI

/// Reusable multi-select picker for Slack channels.
/// Shows a searchable list of all synced channels and lets the caller manage a set of selected channel IDs.
struct ChannelPicker: View {
    let title: String
    let allChannels: [Channel]
    @Binding var selectedIDs: [String]

    @State private var searchText = ""
    @State private var showingPopover = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(title)
                    .font(.headline)
                Spacer()
                Button {
                    showingPopover = true
                } label: {
                    Image(systemName: "plus")
                }
                .buttonStyle(.plain)
                .help("Add channel")
                .popover(isPresented: $showingPopover, arrowEdge: .trailing) {
                    channelSearchPopover
                }
            }

            if selectedIDs.isEmpty {
                Text("None")
                    .foregroundStyle(.secondary)
                    .font(.subheadline)
            } else {
                ForEach(selectedIDs, id: \.self) { cid in
                    HStack {
                        let channel = allChannels.first { $0.id == cid }
                        Text("#\(channel?.name ?? cid)")
                            .font(.subheadline)
                        Spacer()
                        Button {
                            selectedIDs.removeAll { $0 == cid }
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private var channelSearchPopover: some View {
        VStack(spacing: 0) {
            TextField("Search channels...", text: $searchText)
                .textFieldStyle(.roundedBorder)
                .padding(8)

            let filtered = allChannels.filter { channel in
                !selectedIDs.contains(channel.id)
                    && !channel.isArchived
                    && channel.type != "dm"
                    && channel.type != "group_dm"
                    && (searchText.isEmpty
                        || channel.name.localizedCaseInsensitiveContains(searchText))
            }

            List(filtered.prefix(20)) { channel in
                Button {
                    selectedIDs.append(channel.id)
                    searchText = ""
                    showingPopover = false
                } label: {
                    HStack {
                        Text("#\(channel.name)")
                        if channel.type == "private" {
                            Image(systemName: "lock.fill")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .buttonStyle(.plain)
            }
            .listStyle(.plain)
        }
        .frame(width: 250, height: 300)
    }
}
