import SwiftUI

/// Reusable multi-select picker for Slack users.
/// Shows a searchable list of all synced users and lets the caller manage a set of selected user IDs.
struct SlackUserPicker: View {
    let title: String
    let allUsers: [User]
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
                .help("Add person")
                .popover(isPresented: $showingPopover, arrowEdge: .trailing) {
                    userSearchPopover
                }
            }

            if selectedIDs.isEmpty {
                Text("None")
                    .foregroundStyle(.secondary)
                    .font(.subheadline)
            } else {
                ForEach(selectedIDs, id: \.self) { uid in
                    HStack {
                        let user = allUsers.first { $0.id == uid }
                        Text(user?.bestName ?? uid)
                            .font(.subheadline)
                        if let user, !user.name.isEmpty {
                            Text("@\(user.name)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button {
                            selectedIDs.removeAll { $0 == uid }
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

    private var userSearchPopover: some View {
        VStack(spacing: 0) {
            TextField("Search users...", text: $searchText)
                .textFieldStyle(.roundedBorder)
                .padding(8)

            let filtered = allUsers.filter { user in
                !selectedIDs.contains(user.id)
                    && !user.isBot
                    && (searchText.isEmpty
                        || user.bestName.localizedCaseInsensitiveContains(searchText)
                        || user.name.localizedCaseInsensitiveContains(searchText))
            }

            List(filtered.prefix(20)) { user in
                Button {
                    selectedIDs.append(user.id)
                    searchText = ""
                    showingPopover = false
                } label: {
                    VStack(alignment: .leading) {
                        Text(user.bestName)
                        if !user.name.isEmpty {
                            Text("@\(user.name)")
                                .font(.caption)
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
