import SwiftUI

struct InboxListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: InboxViewModel?
    @State private var selectedItemID: Int?
    @State private var expandedSenders: Set<String> = []
    @State private var isScanning = false

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let id = selectedItemID, let item = vm.itemByID(id) {
                    Divider()
                    InboxDetailView(item: item, viewModel: vm) {
                        selectedItemID = nil
                    }
                    .id(id)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(
                        .move(edge: .trailing).combined(with: .opacity)
                    )
                }
            } else {
                ProgressView("Loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedItemID)
        .onAppear { initViewModel() }
        .onChange(of: appState.isDBAvailable) { initViewModel() }
    }

    private func initViewModel() {
        guard viewModel == nil, let db = appState.databaseManager else { return }
        let vm = InboxViewModel(dbManager: db)
        viewModel = vm
        vm.startObserving()
    }

    // MARK: - List Panel

    private func listPanel(_ vm: InboxViewModel) -> some View {
        VStack(spacing: 0) {
            toolbar(vm)
            Divider()

            if vm.senderGroups.isEmpty {
                emptyState
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(vm.senderGroups) { group in
                            senderRow(group, vm: vm)
                        }
                    }
                }
            }
        }
        .frame(minWidth: 300, idealWidth: 350)
    }

    // MARK: - Toolbar

    private func toolbar(_ vm: InboxViewModel) -> some View {
        HStack {
            Text("Inbox")
                .font(.headline)

            if vm.unreadCount > 0 {
                Text("\(vm.unreadCount) unread")
                    .font(.caption)
                    .foregroundStyle(.blue)
            }

            Spacer()

            filterMenu(vm)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private func filterMenu(_ vm: InboxViewModel) -> some View {
        Menu {
            Toggle("Show resolved", isOn: Binding(
                get: { vm.showResolved },
                set: { vm.showResolved = $0; vm.load() }
            ))

            Divider()

            Menu("Priority") {
                Button("All") { vm.priorityFilter = nil; vm.load() }
                ForEach(["high", "medium", "low"], id: \.self) { priority in
                    Button(priority.capitalized) {
                        vm.priorityFilter = priority; vm.load()
                    }
                }
            }

            Menu("Type") {
                Button("All") { vm.triggerTypeFilter = nil; vm.load() }
                Button("@Mentions") {
                    vm.triggerTypeFilter = "mention"; vm.load()
                }
                Button("Direct Messages") {
                    vm.triggerTypeFilter = "dm"; vm.load()
                }
            }
        } label: {
            Image(systemName: "line.3.horizontal.decrease.circle")
                .foregroundStyle(hasActiveFilter(vm) ? .blue : .secondary)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private func hasActiveFilter(_ vm: InboxViewModel) -> Bool {
        vm.priorityFilter != nil
            || vm.triggerTypeFilter != nil
            || vm.showResolved
    }

    // MARK: - Sender Row

    private func senderRow(
        _ group: InboxViewModel.SenderGroup,
        vm: InboxViewModel
    ) -> some View {
        let isExpanded = expandedSenders.contains(group.senderUserID)
        return VStack(spacing: 0) {
            // Sender header
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    if isExpanded {
                        expandedSenders.remove(group.senderUserID)
                    } else {
                        expandedSenders.insert(group.senderUserID)
                    }
                }
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .frame(width: 12)

                    priorityDot(group.highestPriority)

                    Text(group.senderName)
                        .font(.callout)
                        .fontWeight(group.unreadCount > 0 ? .semibold : .regular)
                        .foregroundStyle(.primary)
                        .lineLimit(1)

                    if group.hasUrgent {
                        Text("URGENT")
                            .font(.system(size: 9, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 2)
                            .background(.red, in: Capsule())
                    }

                    Spacer()

                    if group.hasUrgent {
                        Text("\(group.urgentCount)")
                            .font(.caption2)
                            .fontWeight(.bold)
                            .foregroundStyle(.white)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.red, in: Capsule())

                        let rest = group.items.count - group.urgentCount
                        if rest > 0 {
                            Text("\(rest)")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    } else if group.unreadCount > 0 {
                        Text("\(group.unreadCount)")
                            .font(.caption2)
                            .fontWeight(.bold)
                            .foregroundStyle(.white)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.blue, in: Capsule())
                    } else {
                        Text("\(group.items.count)")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(
                    group.hasUrgent
                        ? Color.red.opacity(0.06)
                        : Color.clear
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            // Expanded items
            if isExpanded {
                ForEach(group.items) { item in
                    itemButton(item, vm: vm)
                }
            }

            Divider()
                .padding(.leading, 12)
        }
    }

    private func priorityDot(_ priority: String) -> some View {
        Circle()
            .fill(priorityDotColor(priority))
            .frame(width: 8, height: 8)
    }

    private func priorityDotColor(_ priority: String) -> Color {
        switch priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .blue
        default: return .orange
        }
    }

    // MARK: - Item Button

    private func itemButton(
        _ item: InboxItem,
        vm: InboxViewModel
    ) -> some View {
        let isSelected = selectedItemID == item.id
        return Button {
            selectedItemID = isSelected ? nil : item.id
            if !isSelected && item.isUnread {
                vm.markRead(item)
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: item.triggerIcon)
                    .font(.caption)
                    .foregroundStyle(triggerColor(item))
                    .frame(width: 16)

                VStack(alignment: .leading, spacing: 2) {
                    Text(item.snippet.isEmpty ? "Message" : SlackTextParser.toPlainText(item.snippet))
                        .font(.caption)
                        .lineLimit(2)
                        .foregroundStyle(item.isResolved ? .secondary : .primary)

                    HStack(spacing: 4) {
                        Text("#\(vm.channelName(for: item))")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                        Text(timeAgo(item.messageDate))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                        if item.hasLinkedTask {
                            Image(systemName: "checkmark.circle")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer()
            }
            .padding(.leading, 36)
            .padding(.trailing, 12)
            .padding(.vertical, 6)
            .background(
                isSelected
                    ? Color.accentColor.opacity(0.15)
                    : item.isUnread && item.isPending
                        ? Color.blue.opacity(0.06)
                        : Color.clear,
                in: RoundedRectangle(cornerRadius: 6)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu { contextMenu(item, vm: vm) }
    }

    private func triggerColor(_ item: InboxItem) -> Color {
        switch item.triggerType {
        case "mention": return .blue
        case "dm": return .green
        default: return .secondary
        }
    }

    private func timeAgo(_ date: Date) -> String {
        let interval = Date().timeIntervalSince(date)
        if interval < 3600 {
            let mins = max(1, Int(interval / 60))
            return "\(mins)m ago"
        } else if interval < 86400 {
            return "\(Int(interval / 3600))h ago"
        } else {
            return "\(Int(interval / 86400))d ago"
        }
    }

    // MARK: - Context Menu

    @ViewBuilder
    private func contextMenu(
        _ item: InboxItem,
        vm: InboxViewModel
    ) -> some View {
        if item.isPending {
            Button("Resolve") { vm.resolve(item) }
            Button("Dismiss") { vm.dismiss(item) }
            Divider()
            Menu("Snooze") {
                Button("1 day") { vm.snooze(item, until: snoozeDate(days: 1)) }
                Button("3 days") { vm.snooze(item, until: snoozeDate(days: 3)) }
                Button("1 week") { vm.snooze(item, until: snoozeDate(days: 7)) }
            }
            if !item.hasLinkedTask {
                Button("Create Task") { vm.createTask(from: item) }
            }
            Divider()
        }
        if let slackURL = vm.slackMessageURL(for: item) {
            Button("Open in Slack") {
                NSWorkspace.shared.open(slackURL)
            }
        }
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "tray")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("Inbox is clear")
                .font(.headline)
                .foregroundStyle(.secondary)
            Text("Messages requiring your response will appear here")
                .font(.callout)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)

            Button {
                scanInbox()
            } label: {
                HStack(spacing: 6) {
                    if isScanning {
                        ProgressView()
                            .controlSize(.small)
                    } else {
                        Image(systemName: "magnifyingglass")
                    }
                    Text(isScanning ? "Scanning..." : "Scan Inbox")
                }
            }
            .disabled(isScanning)
            .padding(.top, 8)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    private func scanInbox() {
        guard !isScanning, let path = Constants.findCLIPath() else { return }
        isScanning = true
        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.currentDirectoryURL = Constants.processWorkingDirectory()
            process.arguments = ["inbox", "generate"]
            process.environment = Constants.resolvedEnvironment()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            try? process.run()
            process.waitUntilExit()
            await MainActor.run { isScanning = false }
        }
    }

    // MARK: - Helpers

    private func snoozeDate(days: Int) -> String {
        let date = Calendar.current.date(byAdding: .day, value: days, to: Date()) ?? Date()
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt.string(from: date)
    }
}
