import SwiftUI

struct PeopleListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: PeopleViewModel?
    @State private var selectedUserID: String?
    @State private var searchText = ""

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let userID = selectedUserID,
                   let card = vm.cards.first(where: { $0.userID == userID }) {
                    Divider()
                    PersonDetailView(
                        card: card,
                        userName: vm.userName(for: userID),
                        history: vm.cardHistory(userID: userID),
                        userNameResolver: { vm.userName(for: $0) },
                        onClose: { selectedUserID = nil },
                        isCurrentUser: userID == vm.currentUserID,
                        profile: userID == vm.currentUserID ? vm.currentProfile : nil,
                        interactions: userID == vm.currentUserID ? vm.interactions : [],
                        allCards: vm.cards,
                        onUpdateConnections: { reports, peers, manager in
                            vm.updateConnections(reports: reports, peers: peers, manager: manager)
                        }
                    )
                    .id(userID)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedUserID)
        .navigationTitle("People")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = PeopleViewModel(dbManager: db)
                viewModel?.load()
            }
        }
    }

    /// Current user's card (for "My Card" pinned row).
    private var myCard: PeopleCard? {
        guard let vm = viewModel, let uid = vm.currentUserID else { return nil }
        return vm.cards.first { $0.userID == uid }
    }

    private var filteredCards: [PeopleCard] {
        guard let vm = viewModel else { return [] }
        let excluding = vm.cards.filter { $0.userID != vm.currentUserID }
        if searchText.isEmpty { return excluding }
        let q = searchText.lowercased()
        return excluding.filter { card in
            let name = vm.userName(for: card.userID).lowercased()
            if name.contains(q) { return true }
            if card.summary.lowercased().contains(q) { return true }
            if card.communicationStyle.lowercased().contains(q) { return true }
            if card.decisionRole.lowercased().contains(q) { return true }
            return false
        }
    }

    private func listPanel(_ vm: PeopleViewModel) -> some View {
        VStack(spacing: 0) {
            // Window picker
            if vm.availableWindows.count > 1 {
                HStack {
                    Button(action: {
                        let next = vm.selectedWindow + 1
                        if next < vm.availableWindows.count {
                            vm.loadWindow(at: next)
                        }
                    }) {
                        Image(systemName: "chevron.left")
                    }
                    .disabled(vm.selectedWindow >= vm.availableWindows.count - 1)

                    Spacer()
                    Text(vm.currentWindowLabel)
                        .font(.headline)
                    Spacer()

                    Button(action: {
                        let prev = vm.selectedWindow - 1
                        if prev >= 0 {
                            vm.loadWindow(at: prev)
                        }
                    }) {
                        Image(systemName: "chevron.right")
                    }
                    .disabled(vm.selectedWindow <= 0)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            } else {
                Text(vm.currentWindowLabel)
                    .font(.headline)
                    .padding(.vertical, 8)
            }

            // Summary stats
            HStack(spacing: 16) {
                StatBadge(value: "\(vm.cards.count)", label: "users")
                if vm.redFlagCount > 0 {
                    StatBadge(value: "\(vm.redFlagCount)", label: "flags", color: .red)
                }
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 6)

            // Search
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(
                    text: $searchText,
                    placeholder: "Filter people..."
                )
                .frame(height: 22)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // User list
            ScrollView {
                LazyVStack(spacing: 0) {
                    // Card summary (team summary)
                    if let cs = vm.cardSummary, searchText.isEmpty {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("Team Summary")
                                .font(.caption)
                                .fontWeight(.bold)
                                .foregroundStyle(.secondary)

                            Text(cs.summary)
                                .font(.subheadline)
                                .lineLimit(4)

                            let attention = cs.parsedAttention
                            if !attention.isEmpty {
                                ForEach(attention, id: \.self) { item in
                                    HStack(alignment: .top, spacing: 4) {
                                        Image(systemName: "exclamationmark.circle.fill")
                                            .foregroundStyle(.orange)
                                            .font(.caption)
                                        Text(item)
                                            .font(.caption)
                                    }
                                }
                            }

                            let tips = cs.parsedTips
                            if !tips.isEmpty {
                                ForEach(tips, id: \.self) { tip in
                                    HStack(alignment: .top, spacing: 4) {
                                        Image(systemName: "lightbulb.fill")
                                            .foregroundStyle(.yellow)
                                            .font(.caption)
                                        Text(tip)
                                            .font(.caption)
                                    }
                                }
                            }
                        }
                        .padding(10)
                        .background(Color.orange.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
                        .padding(.horizontal, 8)
                        .padding(.bottom, 8)

                        Divider()
                            .padding(.horizontal, 8)
                    }

                    // My Card — pinned at top
                    if let my = myCard, let vm = viewModel, searchText.isEmpty {
                        myCardRow(my, vm: vm)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                selectedUserID = my.userID
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(
                                selectedUserID == my.userID
                                    ? Color.accentColor.opacity(0.15)
                                    : Color.accentColor.opacity(0.05),
                                in: RoundedRectangle(cornerRadius: 8)
                            )
                            .overlay(
                                RoundedRectangle(cornerRadius: 8)
                                    .strokeBorder(Color.accentColor.opacity(0.3), lineWidth: 1)
                            )
                            .padding(.horizontal, 4)
                            .padding(.bottom, 4)

                        Divider()
                            .padding(.horizontal, 8)
                    }

                    ForEach(filteredCards) { card in
                        personRow(card, vm: vm)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                selectedUserID = card.userID
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(
                                selectedUserID == card.userID
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
        .frame(minWidth: 300, idealWidth: 360)
    }

    private func myCardRow(_ card: PeopleCard, vm: PeopleViewModel) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(card.styleEmoji)
                Text("My Card")
                    .fontWeight(.bold)
                    .foregroundStyle(Color.accentColor)
                Text("@\(vm.userName(for: card.userID))")
                    .foregroundStyle(.secondary)
                Spacer()
                Text("\(card.messageCount) msgs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 8) {
                if let profile = vm.currentProfile {
                    if !profile.role.isEmpty {
                        Text(profile.role)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if !profile.team.isEmpty {
                        Text(profile.team)
                            .font(.caption)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.accentColor.opacity(0.1), in: Capsule())
                    }
                }

                Text(card.communicationStyle)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.accentColor.opacity(0.1), in: Capsule())
            }

            if !vm.interactions.isEmpty {
                let connCount = vm.interactions.count
                let orgCount = (vm.currentProfile?.decodedReports.count ?? 0)
                    + (vm.currentProfile?.decodedPeers.count ?? 0)
                    + (vm.currentProfile?.manager.isEmpty == false ? 1 : 0)
                Text("\(connCount) connections \u{00B7} \(orgCount) org links")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }

    private func personRow(_ card: PeopleCard, vm: PeopleViewModel) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(card.styleEmoji)
                Text("@\(vm.userName(for: card.userID))")
                    .fontWeight(.medium)

                StarToggleButton(isStarred: vm.isPersonStarred(card.userID)) {
                    vm.toggleStarredPerson(card.userID)
                }

                Spacer()

                if card.hasRedFlags {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .font(.caption)
                }

                Text("\(card.messageCount) msgs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 8) {
                Text(card.communicationStyle)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.accentColor.opacity(0.1), in: Capsule())

                Text(card.decisionRole)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.secondary.opacity(0.1), in: Capsule())

                if card.volumeChangePct != 0 {
                    Text(String(format: "%+.0f%%", card.volumeChangePct))
                        .font(.caption)
                        .foregroundStyle(card.volumeChangePct < -30 ? .red : .secondary)
                }
            }

            if !card.summary.isEmpty {
                Text(card.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 4)
    }
}

struct StatBadge: View {
    let value: String
    let label: String
    var color: Color = .accentColor

    var body: some View {
        HStack(spacing: 4) {
            Text(value)
                .fontWeight(.bold)
                .foregroundStyle(color)
            Text(label)
                .foregroundStyle(.secondary)
        }
        .font(.caption)
    }
}
