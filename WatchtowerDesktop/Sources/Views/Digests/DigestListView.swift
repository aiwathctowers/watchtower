import SwiftUI

struct DigestListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: DigestViewModel?
    @State private var chainsViewModel: ChainsViewModel?
    @State private var selectedDigestID: Int?
    @State private var selectedDecisionEntryID: String?
    @State private var selectedChainID: Int?
    @State private var searchText = ""
    @State private var showAllDigests = false
    @State private var showAllDecisions = false
    @State private var showAllChains = false
    @State private var activeTab: ChainTab = .chains
    @State private var expandedDigestIDs: Set<Int> = []
    @State private var expandedDecisionIDs: Set<String> = []
    @State private var expandedChainIDs: Set<Int> = []
    @State private var isSelectMode = false
    @State private var checkedDigestIDs: Set<Int> = []
    @State private var checkedDecisionIDs: Set<String> = []
    @State private var checkedChainIDs: Set<Int> = []
    @State private var chainStatusFilter: String? = nil

    enum ChainTab: String, CaseIterable {
        case chains = "Chains"
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
        .animation(.easeInOut(duration: 0.25), value: selectedChainID)
        .navigationTitle("Chains")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = DigestViewModel(dbManager: db)
                viewModel?.load()
                chainsViewModel = ChainsViewModel(dbManager: db)
                chainsViewModel?.load()
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
        .onChange(of: selectedChainID) { _, newID in
            if let id = newID {
                chainsViewModel?.markChainRead(id)
            }
        }
    }

    @ViewBuilder
    private func detailPanel(_ vm: DigestViewModel) -> some View {
        switch activeTab {
        case .chains:
            if let chainsVM = chainsViewModel, let id = selectedChainID,
               let chain = chainsVM.chains.first(where: { $0.id == id }) {
                Divider()
                ChainDetailView(chain: chain, viewModel: chainsVM, onClose: { selectedChainID = nil })
                    .id(id)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        case .digests:
            if let id = selectedDigestID, let digest = vm.digestByID(id) {
                Divider()
                DigestDetailView(
                    digest: digest,
                    channelName: vm.channelName(for: digest),
                    viewModel: vm,
                    onClose: { selectedDigestID = nil }
                )
                .id(id)
                .frame(minWidth: 400, idealWidth: 500)
                .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        case .decisions:
            if let entryID = selectedDecisionEntryID,
               let entry = vm.decisionEntries.first(where: { $0.id == entryID }) {
                Divider()
                DecisionDetailView(entry: entry, viewModel: vm, onClose: { selectedDecisionEntryID = nil })
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

    private var filteredChains: [Chain] {
        guard let vm = chainsViewModel else { return [] }
        var items = vm.chains
        if !showAllChains {
            items = items.filter { !$0.isRead }
        }
        if !searchText.isEmpty {
            let q = searchText.lowercased()
            items = items.filter {
                $0.title.lowercased().contains(q) ||
                $0.summary.lowercased().contains(q) ||
                $0.slug.lowercased().contains(q)
            }
        }
        return items
    }

    private func listPanel(_ vm: DigestViewModel) -> some View {
        VStack(spacing: 0) {
            // Tab picker
            Picker("", selection: $activeTab) {
                ForEach(ChainTab.allCases, id: \.self) { tab in
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
                selectedChainID = nil
                searchText = ""
                isSelectMode = false
                checkedDigestIDs.removeAll()
                checkedDecisionIDs.removeAll()
                checkedChainIDs.removeAll()
            }

            // Search field + read filter
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(
                    text: $searchText,
                    placeholder: searchPlaceholder
                )
                .frame(height: 22)

                Picker("", selection: activeReadBinding) {
                    Text("Unread").tag(false)
                    Text("All").tag(true)
                }
                .pickerStyle(.segmented)
                .frame(width: 120)
                .id(activeTab)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, activeTab == .chains ? 4 : 8)
            .background(Color(nsColor: .windowBackgroundColor))

            // Status filter for chains
            if activeTab == .chains {
                HStack(spacing: 8) {
                    Picker("Status", selection: $chainStatusFilter) {
                        Text("All").tag(String?.none)
                        Label("Active", systemImage: "link.circle.fill").tag(String?.some("active"))
                        Label("Resolved", systemImage: "checkmark.circle.fill").tag(String?.some("resolved"))
                        Label("Stale", systemImage: "moon.zzz.fill").tag(String?.some("stale"))
                    }
                    .frame(maxWidth: 160)

                    Spacer()
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .onChange(of: chainStatusFilter) {
                    chainsViewModel?.statusFilter = chainStatusFilter
                    chainsViewModel?.load()
                }
            }

            Divider()

            // Selection toolbar
            selectionToolbar(vm)

            // List content based on active tab
            switch activeTab {
            case .chains:
                chainsListContent
            case .digests:
                digestsList(vm)
            case .decisions:
                DecisionsListView(
                    viewModel: vm,
                    selectedEntryID: $selectedDecisionEntryID,
                    expandedEntryIDs: $expandedDecisionIDs,
                    searchText: $searchText,
                    showAll: $showAllDecisions,
                    isSelectMode: $isSelectMode,
                    checkedIDs: $checkedDecisionIDs
                )
            }
        }
        .frame(minWidth: 300, idealWidth: 360)
    }

    private var searchPlaceholder: String {
        switch activeTab {
        case .chains: "Filter chains..."
        case .digests: "Filter digests..."
        case .decisions: "Filter decisions..."
        }
    }

    private var activeReadBinding: Binding<Bool> {
        switch activeTab {
        case .chains: $showAllChains
        case .digests: $showAllDigests
        case .decisions: $showAllDecisions
        }
    }

    // MARK: - Chains List

    @ViewBuilder
    private var chainsListContent: some View {
        if filteredChains.isEmpty {
            ContentUnavailableView(
                "No Chains",
                systemImage: "link.circle",
                description: Text("Chains are created automatically when related decisions and digests are detected.")
            )
        } else {
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(filteredChains) { chain in
                        chainListItem(chain)
                    }
                }
                .padding(.vertical, 8)
                .padding(.horizontal, 8)
            }
        }
    }

    private func chainListItem(_ chain: Chain) -> some View {
        let isChecked = checkedChainIDs.contains(chain.id)
        let isSelected = selectedChainID == chain.id && !isSelectMode

        return HStack(spacing: 0) {
            if isSelectMode {
                Button {
                    toggleChainChecked(chain.id)
                } label: {
                    Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
                        .font(.body)
                }
                .buttonStyle(.borderless)
                .padding(.leading, 8)
            }

            chainRow(chain)
                .contentShape(Rectangle())
                .onTapGesture {
                    if isSelectMode {
                        toggleChainChecked(chain.id)
                    } else {
                        selectedChainID = chain.id
                    }
                }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            isSelected
                ? Color.accentColor.opacity(0.12)
                : isChecked
                    ? Color.accentColor.opacity(0.08)
                    : !chain.isRead
                        ? Color.blue.opacity(0.06)
                        : Color(nsColor: .controlBackgroundColor),
            in: RoundedRectangle(cornerRadius: 8)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(
                    isSelected
                        ? Color.accentColor.opacity(0.3)
                        : !chain.isRead
                            ? Color.blue.opacity(0.25)
                            : Color.primary.opacity(0.06),
                    lineWidth: 1
                )
        )
    }

    private func chainRow(_ chain: Chain) -> some View {
        let expanded = expandedChainIDs.contains(chain.id)
        let children = chainsViewModel?.children(for: chain.id) ?? []

        return VStack(alignment: .leading, spacing: 6) {
            // Top: status icon + title + date
            HStack(alignment: .center, spacing: 6) {
                // Unread indicator
                if !chain.isRead {
                    Circle()
                        .fill(.blue)
                        .frame(width: 8, height: 8)
                }

                chainStatusIcon(chain.status)

                Text(chain.title)
                    .font(.subheadline)
                    .fontWeight(chain.isRead ? .regular : .medium)
                    .lineLimit(1)

                Spacer()

                Text(chain.lastSeenDate, style: .relative)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                // Expand/collapse chevron
                Button {
                    toggleChainExpanded(chain.id)
                } label: {
                    Image(systemName: expanded ? "chevron.up" : "chevron.down")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .frame(width: 16, height: 16)
                }
                .buttonStyle(.borderless)
            }

            // Summary preview
            if !chain.summary.isEmpty {
                Text(chain.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(expanded ? nil : 2)
            }

            // Bottom: stats row
            HStack(spacing: 10) {
                Label("\(chain.itemCount)", systemImage: "number")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                // Channel badges
                let channelIDs = chain.decodedChannelIDs
                if !channelIDs.isEmpty {
                    let names = channelIDs.prefix(3).map { "#" + (chainsViewModel?.channelName(for: $0) ?? $0) }
                    Text(names.joined(separator: ", ") + (channelIDs.count > 3 ? " +\(channelIDs.count - 3)" : ""))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                // Children count
                if !children.isEmpty {
                    Label("\(children.count)", systemImage: "arrow.triangle.branch")
                        .font(.caption2)
                        .foregroundStyle(.blue)
                }

                chainStatusBadge(chain.status)
            }

            // Expanded content: children + summary details
            if expanded {
                chainExpandedContent(chain, children: children)
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private func chainExpandedContent(_ chain: Chain, children: [Chain]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Divider()

            // Child chains
            if !children.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Label("Sub-chains", systemImage: "arrow.triangle.branch")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.blue)

                    ForEach(children) { child in
                        HStack(spacing: 6) {
                            chainStatusIcon(child.status)
                                .font(.caption2)
                            Text(child.title)
                                .font(.caption)
                                .lineLimit(1)
                            Spacer()
                            Text("\(child.itemCount)")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            selectedChainID = child.id
                        }
                    }
                }
            }

            // Channels
            let channelIDs = chain.decodedChannelIDs
            if !channelIDs.isEmpty {
                FlowLayout(spacing: 4) {
                    ForEach(channelIDs, id: \.self) { chID in
                        Text("#" + (chainsViewModel?.channelName(for: chID) ?? chID))
                            .font(.caption2)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.blue.opacity(0.1), in: Capsule())
                    }
                }
            }

            // Open detail button
            Button {
                selectedChainID = chain.id
            } label: {
                Label("Open details", systemImage: "arrow.right.circle")
                    .font(.caption)
            }
            .buttonStyle(.borderless)
        }
        .padding(.top, 2)
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    @ViewBuilder
    private func chainStatusIcon(_ status: String) -> some View {
        switch status {
        case "active":
            Image(systemName: "link.circle.fill")
                .foregroundStyle(.blue)
        case "resolved":
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.green)
        case "stale":
            Image(systemName: "moon.zzz.fill")
                .foregroundStyle(.gray)
        default:
            Image(systemName: "link.circle")
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func chainStatusBadge(_ status: String) -> some View {
        let (text, color): (String, Color) = switch status {
        case "active": ("Active", .blue)
        case "resolved": ("Resolved", .green)
        case "stale": ("Stale", .gray)
        default: (status, .secondary)
        }
        Text(text)
            .font(.caption2)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.12), in: Capsule())
            .foregroundStyle(color)
    }

    private func toggleChainChecked(_ id: Int) {
        if checkedChainIDs.contains(id) {
            checkedChainIDs.remove(id)
        } else {
            checkedChainIDs.insert(id)
        }
    }

    private func toggleChainExpanded(_ id: Int) {
        withAnimation(.easeInOut(duration: 0.2)) {
            if expandedChainIDs.contains(id) {
                expandedChainIDs.remove(id)
            } else {
                expandedChainIDs.insert(id)
            }
        }
    }

    // MARK: - Selection Toolbar

    @ViewBuilder
    private func selectionToolbar(_ vm: DigestViewModel) -> some View {
        if isSelectMode {
            let count: Int = switch activeTab {
            case .chains: checkedChainIDs.count
            case .digests: checkedDigestIDs.count
            case .decisions: checkedDecisionIDs.count
            }
            HStack(spacing: 8) {
                Button {
                    toggleSelectAll()
                } label: {
                    let allSelected: Bool = switch activeTab {
                    case .chains: checkedChainIDs.count == filteredChains.count && !filteredChains.isEmpty
                    case .digests: checkedDigestIDs.count == filteredDigests.count && !filteredDigests.isEmpty
                    case .decisions: checkedDecisionIDs.count == (viewModel?.decisionEntries.count ?? 0)
                    }
                    Label(
                        allSelected ? "Deselect All" : "Select All",
                        systemImage: allSelected ? "checkmark.circle.fill" : "circle"
                    )
                    .font(.caption)
                }
                .buttonStyle(.borderless)

                if count > 0 {
                    Text("\(count) selected")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    Spacer()

                    Button {
                        markSelectedRead(vm)
                    } label: {
                        Label("Read", systemImage: "eye")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)
                    .help("Mark selected as read")

                    Button {
                        submitSelectedFeedback(vm, rating: 1)
                    } label: {
                        Image(systemName: "hand.thumbsup")
                            .foregroundStyle(.green)
                    }
                    .buttonStyle(.borderless)
                    .help("Rate selected as good")

                    Button {
                        submitSelectedFeedback(vm, rating: -1)
                    } label: {
                        Image(systemName: "hand.thumbsdown")
                            .foregroundStyle(.red)
                    }
                    .buttonStyle(.borderless)
                    .help("Rate selected as bad")
                } else {
                    Spacer()
                }

                Button {
                    isSelectMode = false
                    checkedDigestIDs.removeAll()
                    checkedDecisionIDs.removeAll()
                    checkedChainIDs.removeAll()
                } label: {
                    Text("Cancel")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(Color.accentColor.opacity(0.06))
        } else {
            HStack {
                Spacer()
                Button {
                    isSelectMode = true
                } label: {
                    Label("Select", systemImage: "checkmark.circle")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 4)
        }
    }

    private func toggleSelectAll() {
        switch activeTab {
        case .chains:
            if checkedChainIDs.count == filteredChains.count {
                checkedChainIDs.removeAll()
            } else {
                checkedChainIDs = Set(filteredChains.map(\.id))
            }
        case .digests:
            if checkedDigestIDs.count == filteredDigests.count {
                checkedDigestIDs.removeAll()
            } else {
                checkedDigestIDs = Set(filteredDigests.map(\.id))
            }
        case .decisions:
            guard let vm = viewModel else { return }
            if checkedDecisionIDs.count == vm.decisionEntries.count {
                checkedDecisionIDs.removeAll()
            } else {
                checkedDecisionIDs = Set(vm.decisionEntries.map(\.id))
            }
        }
    }

    private func markSelectedRead(_ vm: DigestViewModel) {
        switch activeTab {
        case .chains:
            chainsViewModel?.markChainsRead(checkedChainIDs)
            checkedChainIDs.removeAll()
        case .digests:
            vm.markDigestsRead(checkedDigestIDs)
            checkedDigestIDs.removeAll()
        case .decisions:
            let entries = vm.decisionEntries.filter { checkedDecisionIDs.contains($0.id) }
            vm.markDecisionsRead(entries)
            checkedDecisionIDs.removeAll()
        }
    }

    private func submitSelectedFeedback(_ vm: DigestViewModel, rating: Int) {
        switch activeTab {
        case .chains:
            chainsViewModel?.submitBatchFeedback(chainIDs: checkedChainIDs.map { $0 }, rating: rating)
            checkedChainIDs.removeAll()
        case .digests:
            let ids = checkedDigestIDs.map { String($0) }
            vm.submitBatchFeedback(entityType: "digest", entityIDs: ids, rating: rating)
            checkedDigestIDs.removeAll()
        case .decisions:
            let entries = vm.decisionEntries.filter { checkedDecisionIDs.contains($0.id) }
            let ids = entries.map { "\($0.digestID):\($0.decisionIdx)" }
            vm.submitBatchFeedback(entityType: "decision", entityIDs: ids, rating: rating)
            checkedDecisionIDs.removeAll()
        }
        isSelectMode = false
    }

    // MARK: - Digests List

    private func digestsList(_ vm: DigestViewModel) -> some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(filteredDigests) { digest in
                    digestListItem(digest, vm: vm)
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func digestListItem(_ digest: Digest, vm: DigestViewModel) -> some View {
        let isChecked = checkedDigestIDs.contains(digest.id)
        let isSelected = selectedDigestID == digest.id && !isSelectMode
        let bgColor: Color = isSelected
            ? Color.accentColor.opacity(0.15)
            : isChecked ? Color.accentColor.opacity(0.08) : Color.clear

        return HStack(spacing: 0) {
            if isSelectMode {
                Button {
                    toggleDigestChecked(digest.id)
                } label: {
                    Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
                        .font(.body)
                }
                .buttonStyle(.borderless)
                .padding(.leading, 8)
            }

            digestRow(digest, vm: vm)
                .contentShape(Rectangle())
                .onTapGesture {
                    if isSelectMode {
                        toggleDigestChecked(digest.id)
                    } else {
                        selectedDigestID = digest.id
                    }
                }
        }
        .padding(.horizontal, isSelectMode ? 4 : 10)
        .padding(.vertical, 6)
        .background(bgColor, in: RoundedRectangle(cornerRadius: 6))
        .padding(.horizontal, 4)
    }

    private func toggleDigestChecked(_ id: Int) {
        if checkedDigestIDs.contains(id) {
            checkedDigestIDs.remove(id)
        } else {
            checkedDigestIDs.insert(id)
        }
    }

    private func toggleDigestExpanded(_ id: Int) {
        withAnimation(.easeInOut(duration: 0.2)) {
            if expandedDigestIDs.contains(id) {
                expandedDigestIDs.remove(id)
            } else {
                expandedDigestIDs.insert(id)
            }
        }
    }

    private func digestRow(_ digest: Digest, vm: DigestViewModel) -> some View {
        let expanded = expandedDigestIDs.contains(digest.id)
        return VStack(alignment: .leading, spacing: 6) {
            // Top: type badge + channel + date
            HStack(alignment: .center, spacing: 6) {
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

                if !digest.channelID.isEmpty {
                    StarToggleButton(isStarred: vm.isChannelStarred(digest.channelID)) {
                        vm.toggleStarredChannel(digest.channelID)
                    }
                }

                Spacer()

                Text(TimeFormatting.shortDateTime(fromUnix: digest.periodTo))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                Button {
                    toggleDigestExpanded(digest.id)
                } label: {
                    Image(systemName: expanded ? "chevron.up" : "chevron.down")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .frame(width: 16, height: 16)
                }
                .buttonStyle(.borderless)
            }

            if !digest.summary.isEmpty {
                Text(digest.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(expanded ? nil : 2)
            }

            // Stats row
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

                let actionCount = digest.parsedTracks.count
                if actionCount > 0 {
                    Label("\(actionCount)", systemImage: "checkmark.circle")
                        .font(.caption2)
                        .foregroundStyle(.green)
                }
            }

            if expanded {
                digestExpandedContent(digest, vm: vm)
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private func digestExpandedContent(_ digest: Digest, vm: DigestViewModel) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Divider()

            let topics = digest.parsedTopics
            if !topics.isEmpty {
                FlowLayout(spacing: 4) {
                    ForEach(topics, id: \.self) { topic in
                        Text(topic)
                            .font(.caption2)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.accentColor.opacity(0.1), in: Capsule())
                    }
                }
            }

            let decisions = digest.parsedDecisions
            if !decisions.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Label("Decisions", systemImage: "arrow.triangle.branch")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.orange)

                    ForEach(decisions) { decision in
                        HStack(alignment: .top, spacing: 6) {
                            RoundedRectangle(cornerRadius: 1)
                                .fill(decisionImportanceColor(decision.resolvedImportance))
                                .frame(width: 2, height: 14)
                                .padding(.top, 2)
                            VStack(alignment: .leading, spacing: 1) {
                                Text(decision.text)
                                    .font(.caption)
                                    .lineLimit(2)
                                if let by = decision.by, !by.isEmpty {
                                    Text(by)
                                        .font(.caption2)
                                        .foregroundStyle(.tertiary)
                                }
                            }
                        }
                    }
                }
            }

            let actions = digest.parsedTracks
            if !actions.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Label("Tracks", systemImage: "checkmark.circle")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.green)

                    ForEach(actions) { item in
                        HStack(alignment: .top, spacing: 4) {
                            Image(systemName: item.status == "done" ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(item.status == "done" ? .green : .secondary)
                                .font(.caption2)
                                .padding(.top, 1)
                            Text(item.text)
                                .font(.caption)
                                .lineLimit(2)
                        }
                    }
                }
            }

            Button {
                selectedDigestID = digest.id
            } label: {
                Label("Open details", systemImage: "arrow.right.circle")
                    .font(.caption)
            }
            .buttonStyle(.borderless)
        }
        .padding(.top, 2)
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    // MARK: - Helpers

    private func decisionImportanceColor(_ importance: String) -> Color {
        switch importance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
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

    private func tabLabel(_ tab: ChainTab, vm: DigestViewModel) -> String {
        switch tab {
        case .chains:
            let n = chainsViewModel?.unreadChainCount ?? 0
            return n > 0 ? "\(tab.rawValue) (\(n))" : tab.rawValue
        case .digests:
            let n = vm.unreadDigestCount
            return n > 0 ? "\(tab.rawValue) (\(n))" : tab.rawValue
        case .decisions:
            let n = vm.unreadDecisionCount
            return n > 0 ? "\(tab.rawValue) (\(n))" : tab.rawValue
        }
    }
}
