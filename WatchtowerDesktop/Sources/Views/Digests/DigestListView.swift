import SwiftUI

struct DigestListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: DigestViewModel?
    @State private var selectedDigestID: Int?
    @State private var selectedDecisionEntryID: String?
    @State private var searchText = ""
    @State private var showAllDigests = false
    @State private var showAllDecisions = false
    @State private var activeTab: DigestTab = .digests

    enum DigestTab: String, CaseIterable {
        case digests = "Digests"
        case decisions = "Decisions"
    }

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }

            if let vm = viewModel {
                detailPanel(vm)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedDigestID)
        .animation(.easeInOut(duration: 0.25), value: selectedDecisionEntryID)
        .navigationTitle("Digests")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = DigestViewModel(dbManager: db)
                viewModel?.load()
            }
            if let id = appState.pendingDigestID {
                activeTab = .digests
                showAllDigests = true
                selectedDigestID = id
                appState.pendingDigestID = nil
            }
        }
        .onChange(of: selectedDigestID) { _, newID in
            if let id = newID {
                viewModel?.markDigestRead(id)
            }
        }
        .onChange(of: appState.pendingDigestID) { _, newID in
            if let id = newID {
                activeTab = .digests
                showAllDigests = true
                selectedDigestID = id
                appState.pendingDigestID = nil
            }
        }
        .onChange(of: selectedDecisionEntryID) { _, newID in
            if let id = newID,
               let entry = viewModel?.decisionEntries.first(where: { $0.id == id }),
               !entry.isRead {
                viewModel?.markDecisionRead(digestID: entry.digestID, decisionIdx: entry.decisionIdx)
            }
        }
    }

    @ViewBuilder
    private func detailPanel(_ vm: DigestViewModel) -> some View {
        switch activeTab {
        case .digests:
            if let id = selectedDigestID, let digest = vm.digestByID(id) {
                Divider()
                DigestDetailView(
                    digest: digest,
                    channelName: vm.channelName(for: digest),
                    viewModel: vm
                )
                .id(id)
                .frame(minWidth: 400, idealWidth: 500)
                .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        case .decisions:
            if let entryID = selectedDecisionEntryID,
               let entry = vm.decisionEntries.first(where: { $0.id == entryID }) {
                Divider()
                DecisionDetailView(entry: entry, viewModel: vm)
                    .id(entryID)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
    }

    private var filteredDigests: [Digest] {
        guard let vm = viewModel else { return [] }
        var items = vm.digests
        if !showAllDigests {
            items = items.filter { !$0.isRead }
        }
        if !searchText.isEmpty {
            let q = searchText.lowercased()
            items = items.filter { digest in
                if digest.summary.lowercased().contains(q) { return true }
                if let name = vm.channelName(for: digest), name.lowercased().contains(q) { return true }
                if digest.parsedTopics.contains(where: { $0.lowercased().contains(q) }) { return true }
                return false
            }
        }
        return items
    }

    private func listPanel(_ vm: DigestViewModel) -> some View {
        VStack(spacing: 0) {
            // Tab picker
            Picker("", selection: $activeTab) {
                ForEach(DigestTab.allCases, id: \.self) { tab in
                    Text(tabLabel(tab, vm: vm)).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 12)
            .padding(.top, 10)
            .padding(.bottom, 6)
            .onChange(of: activeTab) {
                selectedDigestID = nil
                selectedDecisionEntryID = nil
                searchText = ""
            }

            // Search field + read filter
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(
                    text: $searchText,
                    placeholder: activeTab == .digests ? "Filter digests..." : "Filter decisions..."
                )
                .frame(height: 22)

                Picker("", selection: activeTab == .digests ? $showAllDigests : $showAllDecisions) {
                    Text("Unread").tag(false)
                    Text("All").tag(true)
                }
                .pickerStyle(.segmented)
                .frame(width: 120)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // List content based on active tab
            switch activeTab {
            case .digests:
                digestsList(vm)
            case .decisions:
                DecisionsListView(
                    viewModel: vm,
                    selectedEntryID: $selectedDecisionEntryID,
                    searchText: $searchText,
                    showAll: $showAllDecisions
                )
            }
        }
        .frame(minWidth: 300, idealWidth: 360)
    }

    private func digestsList(_ vm: DigestViewModel) -> some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(filteredDigests) { digest in
                    digestRow(digest, vm: vm)
                        .contentShape(Rectangle())
                        .onTapGesture {
                            selectedDigestID = digest.id
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(
                            selectedDigestID == digest.id
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

    private func digestRow(_ digest: Digest, vm: DigestViewModel) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            // Top: type badge + channel + date
            HStack(alignment: .center, spacing: 6) {
                // Unread indicator
                if !digest.isRead {
                    Circle()
                        .fill(.blue)
                        .frame(width: 8, height: 8)
                }

                Text(digestTypeLabel(digest.type))
                    .font(.caption2)
                    .fontWeight(.semibold)
                    .foregroundStyle(digestTypeColor(digest.type))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(digestTypeColor(digest.type).opacity(0.12), in: Capsule())

                Text(vm.channelName(for: digest).map { "#\($0)" } ?? "Cross-channel")
                    .font(.subheadline)
                    .fontWeight(digest.isRead ? .regular : .medium)
                    .lineLimit(1)

                Spacer()

                Text(TimeFormatting.shortDateTime(fromUnix: digest.periodTo))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            // Summary preview
            if !digest.summary.isEmpty {
                Text(digest.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            // Bottom: stats row
            HStack(spacing: 10) {
                if digest.messageCount > 0 {
                    Label("\(digest.messageCount)", systemImage: "message")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                let topicCount = digest.parsedTopics.count
                if topicCount > 0 {
                    Label("\(topicCount)", systemImage: "tag")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                Spacer()

                let decisionCount = digest.parsedDecisions.count
                if decisionCount > 0 {
                    Label("\(decisionCount)", systemImage: "arrow.triangle.branch")
                        .font(.caption2)
                        .foregroundStyle(.orange)
                }

                let actionCount = digest.parsedActionItems.count
                if actionCount > 0 {
                    Label("\(actionCount)", systemImage: "checkmark.circle")
                        .font(.caption2)
                        .foregroundStyle(.green)
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func digestTypeLabel(_ type: String) -> String {
        switch type {
        case "channel": "Channel"
        case "daily": "Daily"
        case "weekly": "Weekly"
        default: type.capitalized
        }
    }

    private func digestTypeColor(_ type: String) -> Color {
        switch type {
        case "channel": .blue
        case "daily": .purple
        case "weekly": .indigo
        default: .secondary
        }
    }

    private func tabLabel(_ tab: DigestTab, vm: DigestViewModel) -> String {
        switch tab {
        case .digests:
            let n = vm.unreadDigestCount
            return n > 0 ? "\(tab.rawValue) (\(n))" : tab.rawValue
        case .decisions:
            let n = vm.unreadDecisionCount
            return n > 0 ? "\(tab.rawValue) (\(n))" : tab.rawValue
        }
    }
}
