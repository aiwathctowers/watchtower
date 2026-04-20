import SwiftUI

struct TaskDetailView: View {
    let task: TaskItem
    let viewModel: TasksViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState

    @State private var editingText: String = ""
    @State private var editingIntent: String = ""
    @State private var editingBlocking: String = ""
    @State private var editingBallOn: String = ""
    @State private var hasDueDate: Bool = false
    @State private var dueDate: Date = Date()
    @State private var showSnoozePopover = false
    @State private var snoozeCustomDate = Date()
    @State private var newSubItemText: String = ""
    @State private var editingSubItemIndex: Int? = nil
    @State private var editingSubItemText: String = ""
    @State private var subItemDueDateIndex: Int? = nil
    @State private var subItemDueDate: Date = Date()
    @State private var newNoteText: String = ""
    @State private var aiInstruction: String = ""
    @State private var isAIUpdating = false
    @State private var aiErrorMessage: String?
    @State private var jiraIssue: JiraIssue?
    @State private var jiraConnected = false
    @State private var jiraSiteURL: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                headerSection
                intentSection
                statusSection
                dueDateSection
                detailsSection
                subItemsSection
                notesSection
                aiUpdateSection
                metaSection
                sourceSection
                jiraIssueSection
                actionsSection
            }
            .padding()
        }
        .onAppear {
            jiraConnected = JiraQueries.isConnected()
            jiraSiteURL = JiraConfigHelper.readSiteURL()
            syncState()
            loadJiraIssue()
        }
        .onChange(of: task.id) { syncState(); loadJiraIssue() }
    }

    private func syncState() {
        editingText = task.text
        editingIntent = task.intent
        editingBlocking = task.blocking
        editingBallOn = task.ballOn
        hasDueDate = !task.dueDate.isEmpty
        if let date = TaskItem.parseDueDate(task.dueDate) {
            dueDate = date
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                priorityMenu
                Spacer()
                if let onClose {
                    Button { onClose() } label: {
                        Image(systemName: "xmark")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }

            TextField("Task description", text: $editingText, axis: .vertical)
                .font(.title3)
                .fontWeight(.semibold)
                .textFieldStyle(.plain)
                .lineLimit(1...5)
                .onSubmit { commitText() }
                .onChange(of: editingText) { _, newValue in
                    // Commit on focus loss is handled by onSubmit; also debounce-commit
                }

            HStack(spacing: 12) {
                statusLabel
                if let progress = task.subItemsProgress {
                    Label(progress, systemImage: "checklist")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // MARK: - Intent

    private var intentSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Intent")
                .font(.headline)
            TextField("Why this task matters...", text: $editingIntent)
                .font(.callout)
                .textFieldStyle(.plain)
                .foregroundStyle(.secondary)
                .onSubmit { commitIntent() }
        }
    }

    // MARK: - Status

    private var statusSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Status")
                .font(.headline)
            HStack(spacing: 8) {
                ForEach(
                    ["todo", "in_progress", "blocked", "done"],
                    id: \.self
                ) { status in
                    Button {
                        viewModel.updateStatus(task, to: status)
                    } label: {
                        Text(statusDisplayName(status))
                            .font(.caption)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 5)
                            .background(
                                task.status == status
                                    ? statusButtonColor(status).opacity(0.15)
                                    : Color.clear,
                                in: Capsule()
                            )
                            .overlay(
                                Capsule()
                                    .strokeBorder(
                                        statusButtonColor(status).opacity(0.3),
                                        lineWidth: 1
                                    )
                            )
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    // MARK: - Due Date

    private var dueDateSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Due Date")
                .font(.headline)
            HStack {
                Toggle("", isOn: $hasDueDate)
                    .labelsHidden()
                    .onChange(of: hasDueDate) { _, newValue in
                        if newValue {
                            commitDueDate()
                        } else {
                            viewModel.updateDueDate(task, to: "")
                        }
                    }
                if hasDueDate {
                    DatePicker(
                        "",
                        selection: $dueDate,
                        displayedComponents: [.date, .hourAndMinute]
                    )
                    .labelsHidden()
                    .onChange(of: dueDate) { _, _ in
                        commitDueDate()
                    }
                    if task.isOverdue {
                        Text("Overdue")
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                } else {
                    Text("No due date")
                        .font(.callout)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }

    // MARK: - Details (blocking, ball_on)

    private var detailsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Blocking")
                    .font(.headline)
                TextField("What is this blocking?", text: $editingBlocking)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .foregroundStyle(.secondary)
                    .onSubmit { commitBlocking() }
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Ball On")
                    .font(.headline)
                TextField("Who has the ball?", text: $editingBallOn)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .foregroundStyle(.secondary)
                    .onSubmit { commitBallOn() }
            }
        }
    }

    // MARK: - Sub Items

    private var subItemsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Checklist")
                .font(.headline)

            let items = task.decodedSubItems
            ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                subItemRow(index: index, item: item)
            }

            // Add new sub-item
            HStack(spacing: 8) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                TextField("Add item...", text: $newSubItemText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.addSubItem(task, text: newSubItemText)
                        newSubItemText = ""
                    }
            }
        }
    }

    @ViewBuilder
    private func subItemRow(index: Int, item: TaskSubItem) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "line.3.horizontal")
                .font(.caption2)
                .foregroundStyle(.tertiary)

            Button {
                viewModel.toggleSubItem(task, index: index)
            } label: {
                Image(systemName: item.done ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(item.done ? .green : .secondary)
            }
            .buttonStyle(.plain)

            subItemContent(index: index, item: item)

            Spacer(minLength: 0)

            subItemActions(index: index, item: item)
        }
        .padding(.vertical, 2)
        .draggable(String(index)) {
            HStack(spacing: 8) {
                Image(systemName: "line.3.horizontal")
                    .font(.caption2)
                Text(item.text)
                    .font(.callout)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(.background, in: RoundedRectangle(cornerRadius: 6))
        }
        .dropDestination(for: String.self) { droppedItems, _ in
            guard let fromStr = droppedItems.first,
                  let from = Int(fromStr),
                  from != index else { return false }
            let dest = from < index ? index + 1 : index
            viewModel.moveSubItem(task, from: IndexSet(integer: from), to: dest)
            return true
        }
    }

    @ViewBuilder
    private func subItemContent(index: Int, item: TaskSubItem) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if editingSubItemIndex == index {
                TextField("Sub-item", text: $editingSubItemText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.editSubItem(task, index: index, newText: editingSubItemText)
                        editingSubItemIndex = nil
                    }
                    .onExitCommand { editingSubItemIndex = nil }
            } else {
                Text(item.text)
                    .font(.callout)
                    .strikethrough(item.done)
                    .foregroundStyle(item.done ? .secondary : .primary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        editingSubItemIndex = index
                        editingSubItemText = item.text
                    }
            }

            subItemDueDateRow(index: index, item: item)
        }
    }

    @ViewBuilder
    private func subItemDueDateRow(index: Int, item: TaskSubItem) -> some View {
        if subItemDueDateIndex == index {
            HStack(spacing: 4) {
                DatePicker("", selection: $subItemDueDate, displayedComponents: [.date, .hourAndMinute])
                    .labelsHidden()
                    .controlSize(.small)
                Button("Set") {
                    let dateStr = TaskItem.formatDueDate(subItemDueDate)
                    viewModel.updateSubItemDueDate(task, index: index, dueDate: dateStr)
                    subItemDueDateIndex = nil
                }
                .controlSize(.small)
                if item.dueDate != nil {
                    Button("Clear") {
                        viewModel.updateSubItemDueDate(task, index: index, dueDate: nil)
                        subItemDueDateIndex = nil
                    }
                    .controlSize(.small)
                }
                Button("Cancel") { subItemDueDateIndex = nil }
                    .controlSize(.small)
            }
        } else if let dueStr = item.dueDate, !dueStr.isEmpty {
            Text("Due: \(subItemDueDateFormatted(dueStr))")
                .font(.caption2)
                .foregroundStyle(item.isOverdue ? .red : .secondary)
                .onTapGesture {
                    subItemDueDateIndex = index
                    subItemDueDate = item.dueDateParsed ?? Date()
                }
        }
    }

    @ViewBuilder
    private func subItemActions(index: Int, item: TaskSubItem) -> some View {
        Button {
            if subItemDueDateIndex == index {
                subItemDueDateIndex = nil
            } else {
                subItemDueDateIndex = index
                subItemDueDate = item.dueDateParsed ?? Date()
            }
        } label: {
            Image(systemName: "calendar")
                .font(.caption)
                .foregroundStyle(item.dueDate != nil ? Color.blue : Color.gray.opacity(0.5))
        }
        .buttonStyle(.plain)

        Button {
            viewModel.removeSubItem(task, index: index)
        } label: {
            Image(systemName: "xmark.circle")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .buttonStyle(.plain)
    }

    // MARK: - Notes

    private var notesSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Notes")
                .font(.headline)

            let notes = task.decodedNotes
            if notes.isEmpty {
                Text("No notes yet")
                    .font(.callout)
                    .foregroundStyle(.tertiary)
            } else {
                ForEach(Array(notes.enumerated()), id: \.element.id) { index, note in
                    HStack(alignment: .top, spacing: 8) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(note.text)
                                .font(.callout)
                            if let date = note.createdDate {
                                Text(date, style: .relative)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            } else {
                                Text(note.createdAt)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                        }
                        Spacer()
                        Button {
                            viewModel.removeNote(task, index: index)
                        } label: {
                            Image(systemName: "xmark.circle")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                    .padding(.vertical, 2)
                    if index < notes.count - 1 {
                        Divider()
                    }
                }
            }

            HStack(spacing: 8) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                TextField("Add note...", text: $newNoteText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.addNote(task, text: newNoteText)
                        newNoteText = ""
                    }
            }
        }
    }

    // MARK: - AI Update

    private var aiUpdateSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("AI Update")
                .font(.headline)

            HStack(spacing: 8) {
                TextField("Describe what to change...", text: $aiInstruction)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit { runAIUpdate() }
                    .disabled(isAIUpdating)

                Button {
                    runAIUpdate()
                } label: {
                    if isAIUpdating {
                        ProgressView()
                            .controlSize(.small)
                    } else {
                        Image(systemName: "sparkles")
                    }
                }
                .buttonStyle(.bordered)
                .disabled(aiInstruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isAIUpdating)
            }

            if let error = aiErrorMessage {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    // MARK: - Meta

    @ViewBuilder
    private var metaSection: some View {
        let tags = task.decodedTags
        if !tags.isEmpty {
            HStack(spacing: 4) {
                Text("Tags:")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                ForEach(tags, id: \.self) { tag in
                    Text(tag)
                        .font(.caption)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            .blue.opacity(0.1),
                            in: Capsule()
                        )
                }
            }
        }
    }

    // MARK: - Source

    @ViewBuilder
    private var sourceSection: some View {
        if task.sourceType != "manual" {
            VStack(alignment: .leading, spacing: 4) {
                Text("Source")
                    .font(.headline)
                Button {
                    navigateToSource()
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: sourceIcon)
                            .foregroundStyle(.blue)
                        Text("\(task.sourceType.capitalized) #\(task.sourceID)")
                            .font(.callout)
                            .foregroundStyle(.blue)
                        Image(systemName: "chevron.right")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .buttonStyle(.plain)
            }
        }
    }

    // MARK: - Actions

    private var actionsSection: some View {
        HStack(spacing: 8) {
            if task.isActive {
                Button {
                    viewModel.markDone(task)
                } label: {
                    Label("Done", systemImage: "checkmark")
                }
                .buttonStyle(.borderedProminent)
                .tint(.green)

                Button {
                    viewModel.dismiss(task)
                } label: {
                    Label("Dismiss", systemImage: "xmark")
                }
                .buttonStyle(.bordered)

                Button {
                    showSnoozePopover = true
                } label: {
                    Label("Snooze", systemImage: "moon")
                }
                .buttonStyle(.bordered)
                .popover(isPresented: $showSnoozePopover) {
                    snoozePopover
                }
            }

            Spacer()

            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "task",
                    entityID: String(task.id),
                    dbManager: dbManager
                )
            }
        }
    }

    // MARK: - Snooze Popover

    private var snoozePopover: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Snooze until")
                .font(.headline)
                .padding(.bottom, 4)

            Button("Tomorrow") {
                snoozeFor(days: 1)
            }
            Button("In 3 days") {
                snoozeFor(days: 3)
            }
            Button("In a week") {
                snoozeFor(days: 7)
            }

            Divider()

            DatePicker("Pick date", selection: $snoozeCustomDate, displayedComponents: [.date, .hourAndMinute])
                .labelsHidden()

            Button("Snooze to selected date") {
                let dateStr = TaskItem.formatDueDate(snoozeCustomDate)
                viewModel.snooze(task, until: dateStr)
                showSnoozePopover = false
            }
            .buttonStyle(.borderedProminent)
        }
        .padding()
        .frame(width: 220)
    }

    // MARK: - Priority & Ownership Menus

    private var priorityMenu: some View {
        Menu {
            ForEach(["high", "medium", "low"], id: \.self) { p in
                Button {
                    viewModel.updatePriority(task, to: p)
                } label: {
                    HStack {
                        Text(p.capitalized)
                        if task.priority == p {
                            Image(systemName: "checkmark")
                        }
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Circle()
                    .fill(priorityColor)
                    .frame(width: 8, height: 8)
                Text(task.priority.capitalized)
                    .font(.caption)
                    .fontWeight(.medium)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(priorityColor.opacity(0.12), in: Capsule())
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private var ownershipMenu: some View {
        Menu {
            ForEach(["mine", "delegated", "watching"], id: \.self) { o in
                Button {
                    viewModel.updateOwnership(task, to: o)
                } label: {
                    HStack {
                        Text(o.capitalized)
                        if task.ownership == o {
                            Image(systemName: "checkmark")
                        }
                    }
                }
            }
        } label: {
            Text(task.ownership.capitalized)
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(.secondary.opacity(0.1), in: Capsule())
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    // MARK: - Helpers

    private var statusLabel: some View {
        HStack(spacing: 4) {
            Image(systemName: task.statusIcon)
                .font(.caption)
            Text(statusDisplayName(task.status))
                .font(.caption)
        }
        .foregroundStyle(statusButtonColor(task.status))
    }

    private var priorityColor: Color {
        switch task.priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .blue
        default: return .orange
        }
    }

    private var sourceIcon: String {
        switch task.sourceType {
        case "track": return "binoculars"
        case "digest": return "doc.text.magnifyingglass"
        case "briefing": return "sun.max"
        case "chat": return "bubble.left.and.bubble.right"
        case "jira": return "tray.full"
        default: return "square.and.pencil"
        }
    }

    private func statusDisplayName(_ status: String) -> String {
        switch status {
        case "todo": return "To Do"
        case "in_progress": return "In Progress"
        case "blocked": return "Blocked"
        case "done": return "Done"
        case "dismissed": return "Dismissed"
        case "snoozed": return "Snoozed"
        default: return status.capitalized
        }
    }

    private func statusButtonColor(_ status: String) -> Color {
        switch status {
        case "todo": return .secondary
        case "in_progress": return .blue
        case "blocked": return .red
        case "done": return .green
        case "dismissed": return .gray
        case "snoozed": return .purple
        default: return .secondary
        }
    }

    // MARK: - Jira Issue

    @ViewBuilder
    private var jiraIssueSection: some View {
        if task.sourceType == "jira", let issue = jiraIssue {
            VStack(alignment: .leading, spacing: 8) {
                Text("Jira Issue")
                    .font(.headline)

                // Clickable key -> opens in browser
                Button {
                    openJiraIssue()
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "tray.full")
                            .foregroundStyle(.blue)
                        Text(issue.key)
                            .font(.callout)
                            .fontWeight(.medium)
                            .foregroundStyle(.blue)
                        Image(systemName: "arrow.up.right.square")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .buttonStyle(.plain)

                // Status with color indicator
                HStack(spacing: 6) {
                    Circle()
                        .fill(jiraStatusColor(issue.statusCategory))
                        .frame(width: 8, height: 8)
                    Text(issue.status)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }

                // Assignee
                if !issue.assigneeDisplayName.isEmpty {
                    HStack(spacing: 6) {
                        Text("Assignee:")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(issue.assigneeDisplayName)
                            .font(.callout)
                    }
                }

                // Sprint
                if !issue.sprintName.isEmpty {
                    HStack(spacing: 6) {
                        Text("Sprint:")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(issue.sprintName)
                            .font(.callout)
                    }
                }

                // Due date
                if !issue.dueDate.isEmpty {
                    HStack(spacing: 6) {
                        Text("Due:")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(issue.dueDate)
                            .font(.callout)
                    }
                }
            }
        }
    }

    private func loadJiraIssue() {
        if task.sourceType == "jira" {
            jiraIssue = viewModel.fetchJiraIssue(key: task.sourceID)
        } else {
            jiraIssue = nil
        }
    }

    private func openJiraIssue() {
        guard let siteURL = jiraSiteURL,
              !siteURL.isEmpty else { return }
        let urlString = "\(siteURL)/browse/\(task.sourceID)"
        if let url = URL(string: urlString) {
            NSWorkspace.shared.open(url)
        }
    }

    private func jiraStatusColor(
        _ statusCategory: String
    ) -> Color {
        switch statusCategory {
        case "done": return .green
        case "in_progress": return .blue
        case "todo": return .secondary
        default: return .secondary
        }
    }

    private func navigateToSource() {
        switch task.sourceType {
        case "track":
            appState.selectedDestination = .tracks
        case "digest":
            if let id = Int(task.sourceID) {
                appState.navigateToDigest(id)
            }
        case "briefing":
            appState.selectedDestination = .briefings
        case "jira":
            openJiraIssue()
        default:
            break
        }
    }

    // MARK: - Commit Helpers

    private func commitText() {
        let trimmed = editingText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != task.text else { return }
        viewModel.updateText(task, to: trimmed)
    }

    private func commitIntent() {
        let trimmed = editingIntent.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != task.intent else { return }
        viewModel.updateIntent(task, to: trimmed)
    }

    private func commitDueDate() {
        let dateStr = TaskItem.formatDueDate(dueDate)
        guard dateStr != task.dueDate else { return }
        viewModel.updateDueDate(task, to: dateStr)
    }

    private func commitBlocking() {
        let trimmed = editingBlocking.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != task.blocking else { return }
        viewModel.updateBlocking(task, to: trimmed)
    }

    private func commitBallOn() {
        let trimmed = editingBallOn.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != task.ballOn else { return }
        viewModel.updateBallOn(task, to: trimmed)
    }

    private static let subItemDateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateStyle = .short
        fmt.timeStyle = .short
        return fmt
    }()

    private func subItemDueDateFormatted(_ dateStr: String) -> String {
        guard let date = TaskItem.parseDueDate(dateStr) else { return dateStr }
        return Self.subItemDateFormatter.string(from: date)
    }

    private func runAIUpdate() {
        let instruction = aiInstruction.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instruction.isEmpty else { return }

        guard let cliPath = Constants.findCLIPath() else {
            aiErrorMessage = "watchtower binary not found"
            return
        }

        isAIUpdating = true
        aiErrorMessage = nil

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["tasks", "ai-update", "\(task.id)", "--instruction", instruction]
            process.environment = Constants.resolvedEnvironment()

            let stdout = Pipe()
            let stderr = Pipe()
            process.standardOutput = stdout
            process.standardError = stderr

            do {
                try process.run()

                // Read pipes BEFORE waitUntilExit to avoid deadlock if buffer fills.
                let outData = stdout.fileHandleForReading.readDataToEndOfFile()
                let errData = stderr.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()

                let outStr = String(data: outData, encoding: .utf8)?
                    .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                let errStr = String(data: errData, encoding: .utf8)?
                    .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

                await MainActor.run {
                    isAIUpdating = false

                    if process.terminationStatus != 0 {
                        aiErrorMessage = errStr.isEmpty ? "AI update failed" : errStr
                        return
                    }

                    applyAIUpdateResult(outStr)
                    aiInstruction = ""
                }
            } catch {
                await MainActor.run {
                    isAIUpdating = false
                    aiErrorMessage = error.localizedDescription
                }
            }
        }
    }

    private func applyAIUpdateResult(_ jsonStr: String) {
        guard let data = jsonStr.data(using: .utf8) else {
            aiErrorMessage = "Invalid response from AI"
            return
        }

        struct UpdatedTask: Decodable {
            let text: String?
            let intent: String?
            let priority: String?
            // swiftlint:disable:next identifier_name
            let due_date: String?
            // swiftlint:disable:next identifier_name
            let sub_items: [TaskSubItem]?
        }

        do {
            let result = try JSONDecoder().decode(UpdatedTask.self, from: data)

            if let t = result.text, !t.isEmpty, t != task.text {
                viewModel.updateText(task, to: t)
                editingText = t
            }
            if let i = result.intent {
                viewModel.updateIntent(task, to: i)
                editingIntent = i
            }
            if let p = result.priority, ["high", "medium", "low"].contains(p) {
                viewModel.updatePriority(task, to: p)
            }
            if let d = result.due_date, !d.isEmpty {
                viewModel.updateDueDate(task, to: d)
                if let date = TaskItem.parseDueDate(d) {
                    dueDate = date
                    hasDueDate = true
                }
            }
            if let items = result.sub_items, !items.isEmpty {
                viewModel.replaceSubItems(task, items: items)
            }
        } catch {
            aiErrorMessage = "Failed to parse AI response: \(error.localizedDescription)"
        }
    }

    private func snoozeFor(days: Int) {
        let date = Calendar.current.date(byAdding: .day, value: days, to: Date()) ?? Date()
        let dateStr = TaskItem.formatDueDate(date)
        viewModel.snooze(task, until: dateStr)
        showSnoozePopover = false
    }
}
