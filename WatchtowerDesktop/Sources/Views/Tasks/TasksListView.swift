import SwiftUI

struct TasksListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: TasksViewModel?
    @State private var selectedItemID: Int?
    @State private var showCreateSheet = false
    @State private var searchText = ""
    @State private var jiraConnected = false

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let id = selectedItemID, let item = vm.itemByID(id) {
                    Divider()
                    TaskDetailView(task: item, viewModel: vm) {
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
        .onAppear {
            initViewModel()
            jiraConnected = JiraQueries.isConnected()
            if let id = appState.pendingTaskID {
                selectedItemID = id
                appState.pendingTaskID = nil
            }
        }
        .onChange(of: appState.isDBAvailable) { initViewModel() }
        .onChange(of: appState.pendingTaskID) { _, newID in
            if let id = newID {
                selectedItemID = id
                appState.pendingTaskID = nil
            }
        }
        .sheet(isPresented: $showCreateSheet) {
            CreateTaskSheet()
        }
        .background {
            Button("") { showCreateSheet = true }
                .keyboardShortcut("n", modifiers: .command)
                .hidden()
        }
    }

    private func initViewModel() {
        guard viewModel == nil, let db = appState.databaseManager else { return }
        let vm = TasksViewModel(dbManager: db)
        viewModel = vm
        vm.startObserving()
    }

    // MARK: - List Panel

    private func listPanel(_ vm: TasksViewModel) -> some View {
        VStack(spacing: 0) {
            toolbar(vm)
            Divider()

            // Search bar
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.tertiary)
                    .font(.caption)
                TextField("Search tasks...", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.callout)
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
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

            let filteredToday = filterTasks(vm.todayTasks)
            let filteredAll = filterTasks(vm.allTasks)

            if filteredToday.isEmpty && filteredAll.isEmpty {
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
                        todaySection(filteredToday, vm: vm)
                        allSection(filteredAll, vm: vm)
                    }
                }
            }
        }
        .frame(minWidth: 300, idealWidth: 350)
    }

    private func filterTasks(_ tasks: [TaskItem]) -> [TaskItem] {
        guard !searchText.isEmpty else { return tasks }
        return tasks.filter {
            $0.text.localizedCaseInsensitiveContains(searchText)
        }
    }

    // MARK: - Toolbar

    private func toolbar(_ vm: TasksViewModel) -> some View {
        HStack {
            Text("Tasks")
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
            .help("New Task (⌘N)")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private func filterMenu(_ vm: TasksViewModel) -> some View {
        Menu {
            Toggle("Show completed", isOn: Binding(
                get: { vm.showDone },
                set: { vm.showDone = $0; vm.load() }
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

            Menu("Source") {
                ForEach(sourceFilterOptions, id: \.self) { filter in
                    Button {
                        vm.sourceFilter = filter; vm.load()
                    } label: {
                        HStack {
                            Text(filter.rawValue)
                            if vm.sourceFilter == filter {
                                Image(systemName: "checkmark")
                            }
                        }
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

    private var sourceFilterOptions: [TasksViewModel.SourceFilter] {
        var options: [TasksViewModel.SourceFilter] = [.all]
        if jiraConnected {
            options.append(.jira)
        }
        options.append(contentsOf: [.slack, .manual])
        return options
    }

    private func hasActiveFilter(_ vm: TasksViewModel) -> Bool {
        vm.priorityFilter != nil
            || vm.showDone
            || vm.sourceFilter != .all
    }

    // MARK: - Sections

    @ViewBuilder
    private func todaySection(_ tasks: [TaskItem], vm: TasksViewModel) -> some View {
        if !tasks.isEmpty {
            sectionHeader("Today", count: tasks.count)
            ForEach(tasks) { task in
                taskRow(task, vm: vm)
            }
        }
    }

    @ViewBuilder
    private func allSection(_ tasks: [TaskItem], vm: TasksViewModel) -> some View {
        if !tasks.isEmpty {
            sectionHeader("All Tasks", count: tasks.count)
            ForEach(tasks) { task in
                taskRow(task, vm: vm)
            }
        }
    }

    private func sectionHeader(
        _ title: String,
        count: Int
    ) -> some View {
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

    // MARK: - Task Row

    private func taskRow(_ task: TaskItem, vm: TasksViewModel) -> some View {
        let isSelected = selectedItemID == task.id
        return Button {
            selectedItemID = isSelected ? nil : task.id
        } label: {
            HStack(spacing: 8) {
                // Status toggle
                Button {
                    if task.isActive {
                        vm.markDone(task)
                    }
                } label: {
                    Image(systemName: task.statusIcon)
                        .font(.body)
                        .foregroundStyle(statusColor(task))
                }
                .buttonStyle(.plain)

                // Content
                VStack(alignment: .leading, spacing: 2) {
                    Text(task.text)
                        .font(.callout)
                        .lineLimit(2)
                        .strikethrough(task.status == "done")
                        .foregroundStyle(
                            task.status == "done" ? .secondary : .primary
                        )

                    HStack(spacing: 6) {
                        priorityDot(task)
                        if let due = task.dueDateFormatted {
                            Text(due)
                                .font(.caption2)
                                .foregroundStyle(
                                    task.isOverdue ? .red : .secondary
                                )
                        }
                        if let progress = task.subItemsProgress {
                            Text(progress)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        if task.sourceType == "jira" {
                            jiraBadge(task)
                        } else if task.sourceType != "manual" {
                            Image(systemName: sourceIcon(task))
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
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
        .contextMenu { contextMenu(task, vm: vm) }
    }

    // MARK: - Context Menu

    @ViewBuilder
    private func contextMenu(
        _ task: TaskItem,
        vm: TasksViewModel
    ) -> some View {
        if task.isActive {
            Button("Mark Done") { vm.markDone(task) }
            Button("Dismiss") { vm.dismiss(task) }

            Menu("Snooze") {
                Button("Tomorrow") {
                    snoozeTask(task, vm: vm, days: 1)
                }
                Button("In 3 days") {
                    snoozeTask(task, vm: vm, days: 3)
                }
                Button("In a week") {
                    snoozeTask(task, vm: vm, days: 7)
                }
            }

            Divider()
        }
        Menu("Status") {
            ForEach(
                ["todo", "in_progress", "blocked", "done", "dismissed"],
                id: \.self
            ) { status in
                Button(status.replacingOccurrences(of: "_", with: " ")
                    .capitalized) {
                    vm.updateStatus(task, to: status)
                }
            }
        }
        Menu("Priority") {
            ForEach(["high", "medium", "low"], id: \.self) { priority in
                Button(priority.capitalized) {
                    vm.updatePriority(task, to: priority)
                }
            }
        }
        Divider()
        Button("Delete", role: .destructive) { vm.deleteTask(task) }
    }

    private func snoozeTask(_ task: TaskItem, vm: TasksViewModel, days: Int) {
        let date = Calendar.current.date(byAdding: .day, value: days, to: Date()) ?? Date()
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        vm.snooze(task, until: fmt.string(from: date))
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "checkmark.circle")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("No tasks yet")
                .font(.headline)
                .foregroundStyle(.secondary)
            Text("Create tasks from tracks, digests, or briefings")
                .font(.callout)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
            Button("New Task") { showCreateSheet = true }
                .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    // MARK: - Helpers

    private func statusColor(_ task: TaskItem) -> Color {
        switch task.status {
        case "todo": return .secondary
        case "in_progress": return .blue
        case "blocked": return .red
        case "done": return .green
        case "dismissed": return .gray
        case "snoozed": return .purple
        default: return .secondary
        }
    }

    private func priorityDot(_ task: TaskItem) -> some View {
        Circle()
            .fill(priorityColor(task))
            .frame(width: 6, height: 6)
    }

    private func priorityColor(_ task: TaskItem) -> Color {
        switch task.priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .blue
        default: return .orange
        }
    }

    private func sourceIcon(_ task: TaskItem) -> String {
        switch task.sourceType {
        case "track": return "binoculars"
        case "digest": return "doc.text.magnifyingglass"
        case "briefing": return "sun.max"
        case "chat": return "bubble.left.and.bubble.right"
        case "jira": return "tray.full"
        default: return "square.and.pencil"
        }
    }

    @ViewBuilder
    private func jiraBadge(_ task: TaskItem) -> some View {
        HStack(spacing: 3) {
            Image(systemName: "tray.full")
                .font(.caption2)
            Text(task.sourceID)
                .font(.caption2)
                .fontWeight(.medium)
        }
        .foregroundStyle(.blue)
        .padding(.horizontal, 5)
        .padding(.vertical, 1)
        .background(.blue.opacity(0.1), in: Capsule())
    }
}
