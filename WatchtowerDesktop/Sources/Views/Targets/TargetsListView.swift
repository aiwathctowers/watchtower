import SwiftUI

struct TargetsListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: TargetsViewModel?
    @State private var selectedItemID: Int?
    @State private var showCreateSheet = false
    @State private var searchText = ""
    @State private var pendingDeleteTarget: Target?

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let id = selectedItemID, let item = vm.itemByID(id) {
                    Divider()
                    TargetDetailView(target: item, viewModel: vm) {
                        selectedItemID = nil
                    }
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
            initViewModel()
            if let id = appState.pendingTargetID {
                selectedItemID = id
                appState.pendingTargetID = nil
            }
        }
        .onChange(of: appState.isDBAvailable) { initViewModel() }
        .onChange(of: appState.pendingTargetID) { _, newID in
            if let id = newID {
                selectedItemID = id
                appState.pendingTargetID = nil
            }
        }
        .sheet(isPresented: $showCreateSheet) {
            CreateTargetSheet()
        }
        .background {
            Button("") { showCreateSheet = true }
                .keyboardShortcut("n", modifiers: .command)
                .hidden()
        }
        .confirmationDialog(
            pendingDeleteTarget.map { target in
                let label = target.text.count > 60
                    ? String(target.text.prefix(60)) + "…"
                    : target.text
                return "Delete \"\(label)\"?"
            } ?? "",
            isPresented: Binding(
                get: { pendingDeleteTarget != nil },
                set: { if !$0 { pendingDeleteTarget = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingDeleteTarget
        ) { target in
            Button("Delete", role: .destructive) {
                if selectedItemID == target.id { selectedItemID = nil }
                viewModel?.deleteTarget(target)
                pendingDeleteTarget = nil
            }
            Button("Cancel", role: .cancel) {
                pendingDeleteTarget = nil
            }
        } message: { _ in
            Text("This action cannot be undone.")
        }
    }

    private func initViewModel() {
        guard viewModel == nil, let db = appState.databaseManager else { return }
        let vm = TargetsViewModel(dbManager: db)
        viewModel = vm
        vm.startObserving()
    }

    // MARK: - List Panel

    private func listPanel(_ vm: TargetsViewModel) -> some View {
        VStack(spacing: 0) {
            toolbar(vm)
            Divider()

            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.tertiary)
                    .font(.caption)
                TextField("Search targets...", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.callout)
                    .onChange(of: searchText) { _, newValue in
                        vm.searchText = newValue
                        vm.load()
                    }
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                        vm.searchText = ""
                        vm.load()
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.tertiary)
                            .font(.caption)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)

            Divider()

            if vm.todayTargets.isEmpty && vm.allTargets.isEmpty {
                if searchText.isEmpty {
                    emptyState
                } else {
                    VStack(spacing: 8) {
                        Image(systemName: "magnifyingglass")
                            .font(.system(size: 30))
                            .foregroundStyle(.tertiary)
                        Text("No results for \"\(searchText)\"")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        todaySection(vm.todayTargets, vm: vm)
                        allSection(vm.allTargets, vm: vm)
                    }
                }
            }
        }
        .frame(minWidth: 300, idealWidth: 350)
    }

    // MARK: - Toolbar

    private func toolbar(_ vm: TargetsViewModel) -> some View {
        HStack {
            Text("Targets")
                .font(.headline)

            if vm.overdueCount > 0 {
                Text("\(vm.overdueCount) overdue")
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            Spacer()

            filterMenu(vm)

            Button {
                showCreateSheet = true
            } label: {
                Image(systemName: "plus")
            }
            .buttonStyle(.borderless)
            .help("New Target (⌘N)")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private func filterMenu(_ vm: TargetsViewModel) -> some View {
        Menu {
            Toggle("Show completed", isOn: Binding(
                get: { vm.showDone },
                set: { vm.showDone = $0; vm.load() }
            ))

            Divider()

            Menu("Level") {
                Button("All") { vm.levelFilter = nil; vm.load() }
                ForEach(["quarter", "month", "week", "day", "custom"], id: \.self) { level in
                    Button(level.capitalized) {
                        vm.levelFilter = level; vm.load()
                    }
                }
            }

            Menu("Priority") {
                Button("All") { vm.priorityFilter = nil; vm.load() }
                ForEach(["high", "medium", "low"], id: \.self) { priority in
                    Button(priority.capitalized) {
                        vm.priorityFilter = priority; vm.load()
                    }
                }
            }
        } label: {
            Image(systemName: "line.3.horizontal.decrease.circle")
                .foregroundStyle(hasActiveFilter(vm) ? .blue : .secondary)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private func hasActiveFilter(_ vm: TargetsViewModel) -> Bool {
        vm.priorityFilter != nil || vm.levelFilter != nil || vm.showDone
    }

    // MARK: - Sections

    @ViewBuilder
    private func todaySection(_ targets: [Target], vm: TargetsViewModel) -> some View {
        if !targets.isEmpty {
            sectionHeader("Today", count: targets.count)
            ForEach(targets) { target in
                targetRow(target, vm: vm, depth: 0)
            }
        }
    }

    @ViewBuilder
    private func allSection(_ targets: [Target], vm: TargetsViewModel) -> some View {
        if !targets.isEmpty {
            sectionHeader("All Targets", count: targets.count)
            ForEach(targets) { target in
                targetRow(target, vm: vm, depth: 0)
            }
        }
    }

    private func sectionHeader(_ title: String, count: Int) -> some View {
        HStack {
            Text(title)
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
            Text("\(count)")
                .font(.caption2)
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    // MARK: - Target Row

    private func targetRow(_ target: Target, vm: TargetsViewModel, depth: Int) -> some View {
        let isSelected = selectedItemID == target.id
        let indent = CGFloat(min(depth, 4)) * 16
        return Button {
            selectedItemID = isSelected ? nil : target.id
        } label: {
            HStack(spacing: 8) {
                if indent > 0 {
                    Spacer().frame(width: indent)
                }

                Button {
                    if target.isActive {
                        vm.markDone(target)
                    }
                } label: {
                    Image(systemName: target.statusIcon)
                        .font(.body)
                        .foregroundStyle(statusColor(target))
                }
                .buttonStyle(.plain)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 4) {
                        levelBadge(target)
                        Text(target.text)
                            .font(.callout)
                            .lineLimit(2)
                            .strikethrough(target.status == "done")
                            .foregroundStyle(target.status == "done" ? .secondary : .primary)
                    }

                    HStack(spacing: 6) {
                        priorityDot(target)
                        if let due = target.dueDateFormatted {
                            Text(due)
                                .font(.caption2)
                                .foregroundStyle(target.isOverdue ? .red : .secondary)
                        }
                        if let progress = target.subItemsProgress {
                            Text(progress)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        periodLabel(target)
                    }
                }

                Spacer()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(
                isSelected ? Color.accentColor.opacity(0.1) : Color.clear,
                in: RoundedRectangle(cornerRadius: 6)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu { contextMenu(target, vm: vm) }
    }

    // MARK: - Context Menu

    @ViewBuilder
    private func contextMenu(_ target: Target, vm: TargetsViewModel) -> some View {
        if target.isActive {
            Button("Mark Done") { vm.markDone(target) }
            Button("Dismiss") { vm.dismiss(target) }

            Menu("Snooze") {
                Button("Tomorrow") { snoozeTarget(target, vm: vm, days: 1) }
                Button("In 3 days") { snoozeTarget(target, vm: vm, days: 3) }
                Button("In a week") { snoozeTarget(target, vm: vm, days: 7) }
            }

            Divider()
        }
        Menu("Status") {
            ForEach(
                ["todo", "in_progress", "blocked", "done", "dismissed"],
                id: \.self
            ) { status in
                Button(status.replacingOccurrences(of: "_", with: " ").capitalized) {
                    vm.updateStatus(target, to: status)
                }
            }
        }
        Menu("Priority") {
            ForEach(["high", "medium", "low"], id: \.self) { priority in
                Button(priority.capitalized) {
                    vm.updatePriority(target, to: priority)
                }
            }
        }
        Divider()
        Button("Delete…", role: .destructive) {
            pendingDeleteTarget = target
        }
    }

    private func snoozeTarget(_ target: Target, vm: TargetsViewModel, days: Int) {
        let date = Calendar.current.date(byAdding: .day, value: days, to: Date()) ?? Date()
        vm.snooze(target, until: date)
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "scope")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("No targets yet")
                .font(.headline)
                .foregroundStyle(.secondary)
            Text("Create targets from tracks, digests, or briefings")
                .font(.callout)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
            Button("New Target") { showCreateSheet = true }
                .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    // MARK: - Helpers

    private func statusColor(_ target: Target) -> Color {
        switch target.status {
        case "todo": return .secondary
        case "in_progress": return .blue
        case "blocked": return .red
        case "done": return .green
        case "dismissed": return .gray
        case "snoozed": return .purple
        default: return .secondary
        }
    }

    private func priorityDot(_ target: Target) -> some View {
        Circle()
            .fill(priorityColor(target))
            .frame(width: 6, height: 6)
    }

    private func priorityColor(_ target: Target) -> Color {
        switch target.priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .blue
        default: return .orange
        }
    }

    private func levelBadge(_ target: Target) -> some View {
        let label: String
        switch target.level {
        case "quarter": label = "Q"
        case "month": label = "M"
        case "week": label = "W"
        case "day": label = "D"
        case "custom": label = target.customLabel.isEmpty ? "C" : String(target.customLabel.prefix(1))
        default: label = "?"
        }
        return Text(label)
            .font(.caption2)
            .fontWeight(.semibold)
            .foregroundStyle(.white)
            .frame(width: 16, height: 16)
            .background(levelColor(target.level), in: RoundedRectangle(cornerRadius: 3))
    }

    private func levelColor(_ level: String) -> Color {
        switch level {
        case "quarter": return .purple
        case "month": return .blue
        case "week": return .teal
        case "day": return .green
        default: return .gray
        }
    }

    @ViewBuilder
    private func periodLabel(_ target: Target) -> some View {
        if !target.periodStart.isEmpty {
            Text(target.periodStart)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
    }
}
