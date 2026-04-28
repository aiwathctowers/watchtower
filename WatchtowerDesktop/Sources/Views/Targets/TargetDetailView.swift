import SwiftUI

struct TargetDetailView: View {
    let target: Target
    let viewModel: TargetsViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState

    @State private var selectedTab: Tab = .details
    @State private var editingText: String = ""
    @State private var editingIntent: String = ""
    @State private var editingBlocking: String = ""
    @State private var editingBallOn: String = ""
    @State private var hasDueDate: Bool = false
    @State private var dueDate: Date = Date()
    @State private var showSnoozePopover = false
    @State private var snoozeCustomDate = Date()
    @State private var newSubItemText: String = ""
    @State private var editingSubItemIndex: Int?
    @State private var editingSubItemText: String = ""
    @State private var subItemDueDateIndex: Int?
    @State private var subItemDueDate: Date = Date()
    @State private var promotingSubItem: PromotingSubItemContext?
    @State private var newNoteText: String = ""
    @State private var jiraIssue: JiraIssue?
    @State private var jiraConnected = false
    @State private var jiraSiteURL: String?
    @State private var links: [TargetLink] = []
    @State private var showSuggestLinksSheet = false
    @State private var showDeleteConfirm = false
    @State private var suggestedLinks: SuggestedLinksResult?
    @State private var isSuggestingLinks = false
    @State private var suggestLinksError: String?
    @FocusState private var focusedField: Field?

    enum Field: Hashable {
        case text
        case intent
    }

    enum Tab: String, CaseIterable {
        case details = "Details"
        case links = "Links"
        case activity = "Activity"
    }

    /// Identifies a sub-item the user is currently promoting via `PromoteSubItemSheet`.
    /// Uses a fresh UUID per presentation so SwiftUI's `.sheet(item:)` always
    /// re-presents — even when the user dismisses and immediately reopens at
    /// the same sub-item position.
    struct PromotingSubItemContext: Identifiable {
        let id = UUID()
        let index: Int
        let item: TargetSubItem
    }

    var body: some View {
        VStack(spacing: 0) {
            // Tab bar
            HStack(spacing: 0) {
                ForEach(Tab.allCases, id: \.self) { tab in
                    tabButton(tab)
                }
                Spacer()
                Menu {
                    Button("Delete…", role: .destructive) {
                        showDeleteConfirm = true
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                        .foregroundStyle(.secondary)
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .padding(.trailing, 8)
                if let onClose {
                    Button { onClose() } label: {
                        Image(systemName: "xmark")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                    .padding(.trailing, 12)
                }
            }
            .padding(.horizontal, 4)
            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    switch selectedTab {
                    case .details:
                        detailsTab
                    case .links:
                        linksTab
                    case .activity:
                        activityTab
                    }
                }
                .padding()
            }
        }
        .onAppear {
            jiraConnected = JiraQueries.isConnected()
            jiraSiteURL = JiraConfigHelper.readSiteURL()
            syncState()
            loadJiraIssue()
            loadLinks()
        }
        .onChange(of: target.id) {
            syncState()
            loadJiraIssue()
            loadLinks()
        }
        .onChange(of: focusedField) { oldValue, _ in
            switch oldValue {
            case .text: commitText()
            case .intent: commitIntent()
            case .none: break
            }
        }
        .sheet(isPresented: $showSuggestLinksSheet) {
            if let suggestedLinks {
                SuggestLinksSheet(
                    targetID: target.id,
                    suggestions: suggestedLinks
                )
            }
        }
        .sheet(item: $promotingSubItem) { ctx in
            let prefill = TargetPrefillBuilder.fromSubItem(
                parent: target,
                subItem: ctx.item,
                index: ctx.index
            )
            PromoteSubItemSheet(
                parent: target,
                subItem: ctx.item,
                subItemIndex: ctx.index,
                viewModel: viewModel,
                prefilledIntent: prefill.intent
            )
        }
        .confirmationDialog(
            {
                let label = target.text.count > 60
                    ? String(target.text.prefix(60)) + "…"
                    : target.text
                return "Delete \"\(label)\"?"
            }(),
            isPresented: $showDeleteConfirm,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                viewModel.deleteTarget(target)
                onClose?()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This action cannot be undone.")
        }
    }

    private func tabButton(_ tab: Tab) -> some View {
        let isSelected = selectedTab == tab
        return Button(tab.rawValue) {
            selectedTab = tab
        }
        .buttonStyle(.plain)
        .font(.callout)
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(isSelected ? Color.accentColor.opacity(0.12) : Color.clear)
        .foregroundStyle(isSelected ? Color.accentColor : Color.secondary)
    }

    private func syncState() {
        editingText = target.text
        editingIntent = target.intent
        editingBlocking = target.blocking
        editingBallOn = target.ballOn
        hasDueDate = !target.dueDate.isEmpty
        if let date = Target.parseDueDate(target.dueDate) {
            dueDate = date
        }
    }

    // MARK: - Details Tab

    private var detailsTab: some View {
        VStack(alignment: .leading, spacing: 20) {
            headerSection
            intentSection
            levelSection
            statusSection
            dueDateSection
            detailsSection
            subItemsSection
            notesSection
            jiraIssueSection
            actionsSection
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                priorityMenu
                Spacer()
            }

            TextField("Target description", text: $editingText, axis: .vertical)
                .font(.title3)
                .fontWeight(.semibold)
                .textFieldStyle(.plain)
                .lineLimit(1...10)
                .padding(10)
                .focused($focusedField, equals: .text)
                .background(Color(nsColor: .textBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 8))

            HStack(spacing: 12) {
                statusLabel
                if let progress = target.subItemsProgress {
                    Label(progress, systemImage: "checklist")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                if target.progress > 0 {
                    Text("\(Int(target.progress * 100))%")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // MARK: - Intent

    private var intentSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Intent")
                .font(.headline)
            ZStack(alignment: .topLeading) {
                if editingIntent.isEmpty {
                    Text("Why this target matters…")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 10)
                        .allowsHitTesting(false)
                }
                TextEditor(text: $editingIntent)
                    .font(.body)
                    .scrollContentBackground(.hidden)
                    .padding(6)
                    .frame(minHeight: 56, maxHeight: 160)
                    .focused($focusedField, equals: .intent)
            }
            .background(Color(nsColor: .textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Level

    private var levelSection: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Level")
                    .font(.headline)
                Text(target.level.capitalized + (target.level == "custom" && !target.customLabel.isEmpty ? " (\(target.customLabel))" : ""))
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .leading, spacing: 4) {
                Text("Period")
                    .font(.headline)
                Text("\(target.periodStart) – \(target.periodEnd)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
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
                        viewModel.updateStatus(target, to: status)
                    } label: {
                        Text(statusDisplayName(status))
                            .font(.caption)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 5)
                            .background(
                                target.status == status
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
                            viewModel.updateDueDate(target, to: "")
                        }
                    }
                if hasDueDate {
                    DatePicker(
                        "",
                        selection: $dueDate,
                        displayedComponents: [.date, .hourAndMinute]
                    )
                    .labelsHidden()
                    .onChange(of: dueDate) { _, _ in commitDueDate() }
                    if target.isOverdue {
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

            let items = target.decodedSubItems
            ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                subItemRow(index: index, item: item)
            }

            HStack(spacing: 8) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                TextField("Add item...", text: $newSubItemText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.addSubItem(target, text: newSubItemText)
                        newSubItemText = ""
                    }
            }
        }
    }

    @ViewBuilder
    private func subItemRow(index: Int, item: TargetSubItem) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "line.3.horizontal")
                .font(.caption2)
                .foregroundStyle(.tertiary)

            Button {
                viewModel.toggleSubItem(target, index: index)
            } label: {
                Image(systemName: item.done ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(item.done ? .green : .secondary)
            }
            .buttonStyle(.plain)

            if editingSubItemIndex == index {
                TextField("Sub-item", text: $editingSubItemText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.editSubItem(target, index: index, newText: editingSubItemText)
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

            Spacer(minLength: 0)

            Button {
                promotingSubItem = PromotingSubItemContext(index: index, item: item)
            } label: {
                Image(systemName: "arrow.up.right.square")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .help("Convert to sub-target")

            Button {
                viewModel.removeSubItem(target, index: index)
            } label: {
                Image(systemName: "xmark.circle")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
        }
        .padding(.vertical, 2)
    }

    // MARK: - Notes

    private var notesSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Notes")
                .font(.headline)

            let notes = target.decodedNotes
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
                            }
                        }
                        Spacer()
                        Button {
                            viewModel.removeNote(target, index: index)
                        } label: {
                            Image(systemName: "xmark.circle")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                    .padding(.vertical, 2)
                    if index < notes.count - 1 { Divider() }
                }
            }

            HStack(spacing: 8) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                TextField("Add note...", text: $newNoteText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        viewModel.addNote(target, text: newNoteText)
                        newNoteText = ""
                    }
            }
        }
    }

    // MARK: - Actions

    private var actionsSection: some View {
        HStack(spacing: 8) {
            if target.isActive {
                Button {
                    viewModel.markDone(target)
                } label: {
                    Label("Done", systemImage: "checkmark")
                }
                .buttonStyle(.borderedProminent)
                .tint(.green)

                Button {
                    viewModel.dismiss(target)
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
                    entityType: "target",
                    entityID: String(target.id),
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
            Button("Tomorrow") { snoozeFor(days: 1) }
            Button("In 3 days") { snoozeFor(days: 3) }
            Button("In a week") { snoozeFor(days: 7) }
            Divider()
            DatePicker("Pick date", selection: $snoozeCustomDate, displayedComponents: [.date, .hourAndMinute])
                .labelsHidden()
            Button("Snooze to selected date") {
                viewModel.snooze(target, until: snoozeCustomDate)
                showSnoozePopover = false
            }
            .buttonStyle(.borderedProminent)
        }
        .padding()
        .frame(width: 220)
    }

    // MARK: - Links Tab

    private var linksTab: some View {
        VStack(alignment: .leading, spacing: 16) {
            let inbound = links.filter { $0.targetTargetId == target.id }
            let outbound = links.filter { $0.sourceTargetId == target.id }

            if inbound.isEmpty && outbound.isEmpty {
                Text("No links yet.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            if !inbound.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Inbound")
                        .font(.headline)
                    ForEach(inbound) { link in
                        linkRow(link)
                    }
                }
            }

            if !outbound.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Outbound")
                        .font(.headline)
                    ForEach(outbound) { link in
                        linkRow(link)
                    }
                }
            }

            HStack {
                Button {
                    Task { await runSuggestLinks() }
                } label: {
                    if isSuggestingLinks {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Suggesting…")
                        }
                    } else {
                        Label("Suggest links", systemImage: "sparkles")
                    }
                }
                .disabled(isSuggestingLinks)
                if let suggestLinksError {
                    Text(suggestLinksError)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                Spacer()
            }
            .padding(.top, 8)
        }
    }

    @ViewBuilder
    private func linkRow(_ link: TargetLink) -> some View {
        HStack(spacing: 8) {
            Text(link.relation.replacingOccurrences(of: "_", with: " ").capitalized)
                .font(.caption)
                .foregroundStyle(.white)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(relationColor(link.relation), in: Capsule())

            if link.isExternalLink {
                if let url = externalURL(link.externalRef) {
                    Link(link.externalRef, destination: url)
                        .font(.callout)
                } else {
                    Text(link.externalRef)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            } else if let peerID = link.targetTargetId == target.id ? link.sourceTargetId : link.targetTargetId {
                Text("Target #\(peerID)")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if link.isAICreated {
                if let conf = link.confidence {
                    Text("\(Int(conf * 100))%")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Image(systemName: "sparkles")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: - Activity Tab

    private var activityTab: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Progress")
                    .font(.headline)
                ProgressView(value: target.progress)
                    .tint(.green)
                Text("\(Int(target.progress * 100))% complete")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if !target.sourceType.isEmpty && target.sourceType != "manual" {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Source")
                        .font(.headline)
                    Text("\(target.sourceType.capitalized) \(target.sourceID)")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Created")
                    .font(.headline)
                Text(target.createdDate, style: .date)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Updated")
                    .font(.headline)
                Text(target.updatedDate, style: .relative)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            let tags = target.decodedTags
            if !tags.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Tags")
                        .font(.headline)
                    FlowLayout(spacing: 6) {
                        ForEach(tags, id: \.self) { tag in
                            Text(tag)
                                .font(.caption)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 3)
                                .background(.blue.opacity(0.1), in: Capsule())
                        }
                    }
                }
            }

            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "target",
                    entityID: String(target.id),
                    dbManager: dbManager
                )
            }
        }
    }

    // MARK: - Jira Issue

    @ViewBuilder
    private var jiraIssueSection: some View {
        if target.sourceType == "jira", let issue = jiraIssue {
            VStack(alignment: .leading, spacing: 8) {
                Text("Jira Issue")
                    .font(.headline)

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

                Text(issue.status)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Priority Menu

    private var priorityMenu: some View {
        Menu {
            ForEach(["high", "medium", "low"], id: \.self) { p in
                Button {
                    viewModel.updatePriority(target, to: p)
                } label: {
                    HStack {
                        Text(p.capitalized)
                        if target.priority == p { Image(systemName: "checkmark") }
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Circle()
                    .fill(priorityColor)
                    .frame(width: 8, height: 8)
                Text(target.priority.capitalized)
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

    // MARK: - Helpers

    private var statusLabel: some View {
        HStack(spacing: 4) {
            Image(systemName: target.statusIcon)
                .font(.caption)
            Text(statusDisplayName(target.status))
                .font(.caption)
        }
        .foregroundStyle(statusButtonColor(target.status))
    }

    private var priorityColor: Color {
        switch target.priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .blue
        default: return .orange
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

    private func relationColor(_ relation: String) -> Color {
        switch relation {
        case "contributes_to": return .green
        case "blocks": return .red
        case "related": return .blue
        case "duplicates": return .orange
        default: return .gray
        }
    }

    private func externalURL(_ ref: String) -> URL? {
        if ref.hasPrefix("jira:") {
            guard let site = jiraSiteURL else { return nil }
            let key = String(ref.dropFirst(5))
            return URL(string: "\(site)/browse/\(key)")
        }
        if ref.hasPrefix("slack:") {
            let parts = ref.dropFirst(6).split(separator: ":")
            guard parts.count >= 2 else { return nil }
            return URL(string: "https://slack.com/archives/\(parts[0])/p\(parts[1].replacingOccurrences(of: ".", with: ""))")
        }
        return nil
    }

    private func loadJiraIssue() {
        if target.sourceType == "jira" {
            jiraIssue = viewModel.fetchJiraIssue(key: target.sourceID)
        } else {
            jiraIssue = nil
        }
    }

    private func loadLinks() {
        links = viewModel.fetchLinks(for: target.id)
    }

    private func openJiraIssue() {
        guard let siteURL = jiraSiteURL, !siteURL.isEmpty else { return }
        let urlString = "\(siteURL)/browse/\(target.sourceID)"
        if let url = URL(string: urlString) {
            NSWorkspace.shared.open(url)
        }
    }

    private func snoozeFor(days: Int) {
        let date = Calendar.current.date(byAdding: .day, value: days, to: Date()) ?? Date()
        viewModel.snooze(target, until: date)
        showSnoozePopover = false
    }

    // MARK: - Commit Helpers

    private func commitText() {
        let trimmed = editingText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != target.text else { return }
        viewModel.updateText(target, to: trimmed)
    }

    private func commitIntent() {
        let trimmed = editingIntent.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != target.intent else { return }
        viewModel.updateIntent(target, to: trimmed)
    }

    private func commitDueDate() {
        let dateStr = Target.formatDueDate(dueDate)
        guard dateStr != target.dueDate else { return }
        viewModel.updateDueDate(target, to: dateStr)
    }

    private func commitBlocking() {
        let trimmed = editingBlocking.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != target.blocking else { return }
        viewModel.updateBlocking(target, to: trimmed)
    }

    private func commitBallOn() {
        let trimmed = editingBallOn.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != target.ballOn else { return }
        viewModel.updateBallOn(target, to: trimmed)
    }

    // MARK: - Actions

    private func runSuggestLinks() async {
        guard let runner = ProcessCLIRunner.makeDefault() else {
            suggestLinksError = "watchtower CLI not found in PATH"
            return
        }
        isSuggestingLinks = true
        suggestLinksError = nil
        defer { isSuggestingLinks = false }
        do {
            let service = TargetSuggestLinksService(runner: runner)
            let result = try await service.suggest(targetID: target.id)
            if result.parentID == nil && result.secondaryLinks.isEmpty {
                suggestLinksError = "AI had no suggestions"
                return
            }
            suggestedLinks = result
            showSuggestLinksSheet = true
        } catch {
            suggestLinksError = "Suggest-links failed: \(error.localizedDescription)"
        }
    }
}
