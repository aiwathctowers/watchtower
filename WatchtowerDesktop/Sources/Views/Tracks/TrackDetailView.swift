import SwiftUI

struct TrackDetailView: View {
    let track: Track
    let viewModel: TracksViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState
    @State private var chatVM: TrackChatViewModel?
    @State private var showCreateTask = false
    @State private var linkedTasks: [TaskItem] = []
    @State private var jiraIssues: [JiraIssue] = []

    var body: some View {
        VSplitView {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    headerSection
                    textSection
                    requesterSection
                    contextSection
                    blockingSection
                    subItemsSection
                    decisionSection
                    decisionOptionsSection
                    participantsSection
                    sourceRefsSection
                    relatedDigestsSection
                    linkedTasksSection
                    jiraIssuesSection
                    dueDateSection
                    tagsSection
                    actionsSection
                }
                .padding()
            }
            .frame(minHeight: 200)

            // Bottom: embedded chat
            if let chatVM {
                Divider()
                TrackChatSection(chatVM: chatVM)
                    .frame(minHeight: 200, idealHeight: 300)
            }
        }
        .onAppear {
            if let db = appState.databaseManager {
                chatVM = TrackChatViewModel(
                    track: track, viewModel: viewModel, dbManager: db
                )
                loadLinkedTasks(db: db)
                loadJiraIssues(db: db)
            }
        }
        .onChange(of: showCreateTask) { _, isShowing in
            if !isShowing, let db = appState.databaseManager {
                loadLinkedTasks(db: db)
            }
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center, spacing: 8) {
                priorityBadge
                ownershipBadge
                categoryBadge

                if track.hasUpdates {
                    Label("Updated", systemImage: "bell.badge.fill")
                        .font(.caption)
                        .foregroundStyle(.orange)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.orange.opacity(0.12), in: Capsule())
                }

                Spacer()

                Text(track.updatedAgo)
                    .font(.caption)
                    .foregroundStyle(.secondary)

                if let onClose {
                    Button { onClose() } label: {
                        Image(systemName: "xmark.circle.fill")
                            .symbolRenderingMode(.hierarchical)
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }

            // Main track text as title
            Text(viewModel.resolveUserIDs(track.text))
                .font(.title3)
                .fontWeight(.semibold)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: - Text (main body)

    @ViewBuilder
    private var textSection: some View {
        // Channels
        let channels = track.decodedChannelIDs
        if !channels.isEmpty {
            HStack(spacing: 8) {
                ForEach(channels, id: \.self) { chID in
                    let name = viewModel.channelName(for: chID) ?? chID
                    if let url = viewModel.slackChannelURL(channelID: chID) {
                        Link(destination: url) {
                            Label("#\(name)", systemImage: "number")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                    } else {
                        Label("#\(name)", systemImage: "number")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    // MARK: - Requester

    @ViewBuilder
    private var requesterSection: some View {
        if !track.requesterName.isEmpty {
            HStack(spacing: 6) {
                Image(systemName: "person.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text("Requested by: \(viewModel.resolveUserIDs(track.requesterName))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Context

    @ViewBuilder
    private var contextSection: some View {
        if !track.context.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Context")
                    .font(.headline)
                Text(viewModel.resolveUserIDs(track.context))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Blocking

    @ViewBuilder
    private var blockingSection: some View {
        if !track.blocking.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Text("Blocking")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.red)
                Text(viewModel.resolveUserIDs(track.blocking))
                    .font(.subheadline)
                    .textSelection(.enabled)
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Sub-items

    @ViewBuilder
    private var subItemsSection: some View {
        let items = track.decodedSubItems
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                let progress = track.subItemsProgress
                HStack {
                    Text("Sub-items")
                        .font(.headline)
                    Spacer()
                    Text("\(progress.done)/\(progress.total)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Progress bar
                if progress.total > 0 {
                    GeometryReader { geo in
                        ZStack(alignment: .leading) {
                            RoundedRectangle(cornerRadius: 3)
                                .fill(.quaternary)
                            RoundedRectangle(cornerRadius: 3)
                                .fill(.green)
                                .frame(
                                    width: geo.size.width
                                        * CGFloat(progress.done)
                                        / CGFloat(progress.total)
                                )
                        }
                    }
                    .frame(height: 6)
                }

                ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                    Button {
                        viewModel.toggleSubItem(track, at: index)
                    } label: {
                        HStack(spacing: 8) {
                            Image(
                                systemName: item.isDone
                                    ? "checkmark.circle.fill"
                                    : "circle"
                            )
                            .foregroundStyle(item.isDone ? .green : .secondary)
                            .font(.subheadline)

                            Text(item.text)
                                .font(.subheadline)
                                .strikethrough(item.isDone)
                                .foregroundStyle(item.isDone ? .secondary : .primary)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    // MARK: - Decision summary

    @ViewBuilder
    private var decisionSection: some View {
        if !track.decisionSummary.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Text("Decision")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                Text(viewModel.resolveUserIDs(track.decisionSummary))
                    .font(.subheadline)
                    .textSelection(.enabled)
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.blue.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Decision options

    @ViewBuilder
    private var decisionOptionsSection: some View {
        let options = track.decodedDecisionOptions
        if !options.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Options")
                    .font(.headline)

                ForEach(options) { opt in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(opt.option)
                            .font(.subheadline)
                            .fontWeight(.medium)

                        if !opt.supporters.isEmpty {
                            HStack(spacing: 4) {
                                Image(systemName: "hand.thumbsup.fill")
                                    .font(.caption2)
                                    .foregroundStyle(.green)
                                Text(opt.supporters.joined(separator: ", "))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        if !opt.pros.isEmpty {
                            HStack(alignment: .top, spacing: 4) {
                                Text("+")
                                    .font(.caption)
                                    .foregroundStyle(.green)
                                    .frame(width: 12)
                                Text(opt.pros)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        if !opt.cons.isEmpty {
                            HStack(alignment: .top, spacing: 4) {
                                Text("-")
                                    .font(.caption)
                                    .foregroundStyle(.red)
                                    .frame(width: 12)
                                Text(opt.cons)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
                }
            }
        }
    }

    // MARK: - Participants

    @ViewBuilder
    private var participantsSection: some View {
        let people = track.decodedParticipants
        if !people.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Participants")
                    .font(.headline)

                ForEach(people) { person in
                    HStack(spacing: 8) {
                        Image(systemName: "person.circle.fill")
                            .foregroundStyle(stanceColor(person.stance))
                            .font(.subheadline)
                            .frame(width: 20)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(person.name)
                                .font(.subheadline)
                                .fontWeight(.medium)
                            if let stance = person.stance, !stance.isEmpty {
                                Text(stance)
                                    .font(.caption)
                                    .foregroundStyle(stanceColor(stance))
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Source refs (key messages)

    @ViewBuilder
    private var sourceRefsSection: some View {
        let refs = track.decodedSourceRefs
        if !refs.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Key Messages")
                    .font(.headline)

                ForEach(refs) { ref in
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "quote.opening")
                            .foregroundStyle(.tertiary)
                            .font(.caption)
                            .frame(width: 16)
                            .padding(.top, 2)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(viewModel.resolveUserIDs(ref.author))
                                .font(.caption)
                                .fontWeight(.semibold)
                                .foregroundStyle(.secondary)
                            Text(ref.text)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }

                        Spacer()

                        // Slack link for the message — prefer channel_id from ref, fall back to track's first channel
                        let refChannelID = ref.channelID ?? track.decodedChannelIDs.first
                        if let chID = refChannelID, !ref.ts.isEmpty {
                            if let url = viewModel.slackMessageURL(
                                channelID: chID, messageTS: ref.ts
                            ) {
                                Link(destination: url) {
                                    Image(systemName: "arrow.up.right.square")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                                .buttonStyle(.borderless)
                                .help("Open in Slack")
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    // MARK: - Related Digests (expandable)

    @ViewBuilder
    private var relatedDigestsSection: some View {
        let digestIDs = track.decodedRelatedDigestIDs
        if !digestIDs.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Related Digests")
                    .font(.headline)

                ForEach(digestIDs, id: \.self) { digestID in
                    LinkedDigestRow(
                        digestID: digestID,
                        viewModel: viewModel,
                        appState: appState
                    )
                }
            }
        }
    }

    // MARK: - Due date

    @ViewBuilder
    private var dueDateSection: some View {
        if let formatted = track.dueDateFormatted {
            HStack(spacing: 6) {
                Image(systemName: "calendar")
                    .font(.caption)
                    .foregroundStyle(track.isOverdue ? .red : .secondary)
                Text("Due: \(formatted)")
                    .font(.caption)
                    .foregroundStyle(track.isOverdue ? .red : .secondary)
                if track.isOverdue {
                    Text("OVERDUE")
                        .font(.caption2)
                        .fontWeight(.bold)
                        .foregroundStyle(.white)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.red, in: Capsule())
                }
            }
        }
    }

    // MARK: - Tags

    @ViewBuilder
    private var tagsSection: some View {
        let trackTags = track.decodedTags
        if !trackTags.isEmpty {
            HStack(spacing: 4) {
                ForEach(trackTags, id: \.self) { tag in
                    Text(tag)
                        .font(.caption2)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
    }

    // MARK: - Actions

    private var actionsSection: some View {
        HStack(spacing: 8) {
            Button {
                showCreateTask = true
            } label: {
                Label("Take Action", systemImage: "checkmark.circle")
            }
            .buttonStyle(.bordered)
            .sheet(isPresented: $showCreateTask) {
                CreateTaskSheet(
                    prefillText: track.text,
                    prefillIntent: track.context,
                    prefillSourceType: "track",
                    prefillSourceID: String(track.id)
                )
            }

            if track.isDismissed {
                Button {
                    viewModel.restoreTrack(track)
                } label: {
                    Label("Restore", systemImage: "arrow.uturn.backward")
                }
                .buttonStyle(.bordered)
            } else {
                Button {
                    onClose?()
                    viewModel.dismissTrack(track)
                } label: {
                    Label("Dismiss", systemImage: "archivebox")
                }
                .buttonStyle(.bordered)
            }

            Spacer()
            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "track",
                    entityID: String(track.id),
                    dbManager: dbManager
                )
            }
        }
    }

    // MARK: - Jira Issues

    @ViewBuilder
    private var jiraIssuesSection: some View {
        if !jiraIssues.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Linked Jira Issues")
                    .font(.headline)

                ForEach(jiraIssues, id: \.key) { issue in
                    HStack(spacing: 10) {
                        JiraBadgeView(
                            issue: issue,
                            siteURL: viewModel.jiraSiteURL,
                            isExpanded: true
                        )

                        VStack(alignment: .leading, spacing: 2) {
                            Text(issue.summary)
                                .font(.subheadline)
                                .lineLimit(2)

                            HStack(spacing: 8) {
                                if !issue.sprintName.isEmpty {
                                    Label(issue.sprintName, systemImage: "arrow.triangle.2.circlepath")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }

                                if let dueText = jiraIssueDueText(issue) {
                                    Label(dueText, systemImage: "calendar")
                                        .font(.caption2)
                                        .foregroundStyle(
                                            isJiraIssueOverdue(issue) ? .red : .secondary
                                        )
                                }
                            }
                        }

                        Spacer()
                    }
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.indigo.opacity(0.04), in: RoundedRectangle(cornerRadius: 8))
                }
            }
        }
    }

    private func loadJiraIssues(db: DatabaseManager) {
        jiraIssues = (try? db.dbPool.read { database in
            try JiraQueries.fetchIssuesForTrack(database, trackID: track.id)
        }) ?? []
    }

    private func jiraIssueDueText(_ issue: JiraIssue) -> String? {
        guard !issue.dueDate.isEmpty else { return nil }
        // dueDate is typically "YYYY-MM-DD" or ISO8601
        let dateStr = String(issue.dueDate.prefix(10))
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        guard let date = formatter.date(from: dateStr) else { return dateStr }
        let relative = DateFormatter()
        relative.dateStyle = .medium
        relative.timeStyle = .none
        return relative.string(from: date)
    }

    private func isJiraIssueOverdue(_ issue: JiraIssue) -> Bool {
        guard !issue.dueDate.isEmpty,
              issue.statusCategory != "done" else { return false }
        let dateStr = String(issue.dueDate.prefix(10))
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        guard let date = formatter.date(from: dateStr) else { return false }
        return date < Date()
    }

    // MARK: - Linked Tasks

    @ViewBuilder
    private var linkedTasksSection: some View {
        if !linkedTasks.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Tasks")
                    .font(.headline)

                ForEach(linkedTasks) { task in
                    Button {
                        appState.navigateToTask(task.id)
                    } label: {
                        HStack(spacing: 8) {
                            Image(systemName: task.statusIcon)
                                .foregroundStyle(taskStatusColor(task.status))
                                .font(.subheadline)

                            VStack(alignment: .leading, spacing: 2) {
                                Text(task.text)
                                    .font(.subheadline)
                                    .lineLimit(2)
                                    .foregroundStyle(.primary)

                                HStack(spacing: 6) {
                                    Text(task.status.replacingOccurrences(of: "_", with: " ").capitalized)
                                        .font(.caption2)
                                        .padding(.horizontal, 5)
                                        .padding(.vertical, 1)
                                        .background(
                                            taskStatusColor(task.status).opacity(0.15),
                                            in: Capsule()
                                        )

                                    if let due = task.dueDateFormatted {
                                        Label(due, systemImage: "calendar")
                                            .font(.caption2)
                                            .foregroundStyle(task.isOverdue ? .red : .secondary)
                                    }
                                }
                            }

                            Spacer()

                            Image(systemName: "chevron.right")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(.green.opacity(0.04), in: RoundedRectangle(cornerRadius: 8))
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    private func loadLinkedTasks(db: DatabaseManager) {
        linkedTasks = (try? db.dbPool.read { database in
            try TaskQueries.fetchBySourceRef(database, sourceType: "track", sourceID: String(track.id))
        }) ?? []
    }

    private func taskStatusColor(_ status: String) -> Color {
        switch status {
        case "todo": .secondary
        case "in_progress": .blue
        case "blocked": .red
        case "done": .green
        case "dismissed": .gray
        case "snoozed": .purple
        default: .secondary
        }
    }

    // MARK: - Badges

    private var priorityBadge: some View {
        Menu {
            ForEach(["high", "medium", "low"], id: \.self) { priority in
                Button {
                    viewModel.updatePriority(track, to: priority)
                } label: {
                    if priority == track.priority {
                        Label(priority.capitalized, systemImage: "checkmark")
                    } else {
                        Text(priority.capitalized)
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Circle()
                    .fill(priorityColor)
                    .frame(width: 8, height: 8)
                Text(track.priority.capitalized)
                    .font(.caption)
                    .foregroundStyle(priorityColor)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(priorityColor.opacity(0.1), in: Capsule())
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private var ownershipBadge: some View {
        Menu {
            ForEach(["mine", "delegated", "watching"], id: \.self) { own in
                Button {
                    viewModel.updateOwnership(track, to: own)
                } label: {
                    if own == track.ownership {
                        Label(ownershipLabel(own), systemImage: "checkmark")
                    } else {
                        Text(ownershipLabel(own))
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Image(systemName: ownershipIcon)
                    .font(.system(size: 9))
                Text(track.ownershipLabel)
                    .font(.caption)
            }
            .foregroundStyle(ownershipColor)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(ownershipColor.opacity(0.1), in: Capsule())
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private var categoryBadge: some View {
        Text(track.categoryLabel)
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(.secondary.opacity(0.1), in: Capsule())
    }

    // MARK: - Helpers

    private var priorityColor: Color {
        switch track.priority {
        case "high": .red
        case "low": .blue
        default: .orange
        }
    }

    private var ownershipColor: Color {
        switch track.ownership {
        case "mine": .green
        case "delegated": .purple
        case "watching": .secondary
        default: .secondary
        }
    }

    private var ownershipIcon: String {
        switch track.ownership {
        case "mine": "person.fill"
        case "delegated": "arrow.right.circle.fill"
        case "watching": "eye.fill"
        default: "circle"
        }
    }

    private func ownershipLabel(_ value: String) -> String {
        switch value {
        case "mine": return "Mine"
        case "delegated": return "Delegated"
        case "watching": return "Watching"
        default: return value.capitalized
        }
    }

    private func stanceColor(_ stance: String?) -> Color {
        switch stance {
        case "driver": .green
        case "supporter": .blue
        case "blocker": .red
        case "reviewer": .purple
        case "neutral": .secondary
        default: .secondary
        }
    }
}

// MARK: - Linked Digest Row (expandable)

private struct LinkedDigestRow: View {
    let digestID: Int
    let viewModel: TracksViewModel
    let appState: AppState
    @State private var isExpanded = false
    @State private var digest: Digest?
    @State private var channelName: String?

    var body: some View {
        DisclosureGroup(isExpanded: $isExpanded) {
            if let digest {
                VStack(alignment: .leading, spacing: 8) {
                    if !digest.summary.isEmpty {
                        Text(digest.summary)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .lineLimit(6)
                    }

                    let decisions = digest.parsedDecisions
                    if !decisions.isEmpty {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Decisions")
                                .font(.caption)
                                .fontWeight(.semibold)
                            ForEach(decisions) { decision in
                                HStack(alignment: .top, spacing: 6) {
                                    Image(systemName: "arrow.triangle.branch")
                                        .font(.caption2)
                                        .foregroundStyle(.orange)
                                        .frame(width: 12)
                                        .padding(.top, 2)
                                    Text(decision.text)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }

                    if let dbManager = appState.databaseManager {
                        FeedbackButtons(
                            entityType: "digest",
                            entityID: String(digestID),
                            dbManager: dbManager
                        )
                    }
                }
                .padding(.leading, 4)
            } else {
                Text("Loading...")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "doc.text")
                    .font(.caption)
                    .foregroundStyle(.blue)
                if let channelName {
                    Text("#\(channelName)")
                        .font(.caption)
                        .foregroundStyle(.primary)
                }
                Text("Digest #\(digestID)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if let digest {
                    Text(digest.isRead ? "read" : "unread")
                        .font(.caption2)
                        .foregroundStyle(digest.isRead ? .green : .orange)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(
                            (digest.isRead ? Color.green : Color.orange).opacity(0.1),
                            in: Capsule()
                        )
                }
                Spacer()
            }
        }
        .onAppear { loadDigest() }
        .onChange(of: isExpanded) { _, expanded in
            if expanded { loadDigest() }
        }
    }

    private func loadDigest() {
        guard digest == nil else { return }
        digest = viewModel.fetchDigest(id: digestID)
        if let loaded = digest {
            channelName = viewModel.channelName(for: loaded.channelID)
        }
    }
}
