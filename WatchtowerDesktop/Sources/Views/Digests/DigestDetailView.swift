import SwiftUI

struct DigestDetailView: View {
    let digest: Digest
    let channelName: String?
    let viewModel: DigestViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState
    @State private var markingRead = false
    @State private var markedRead = false
    @State private var markReadError: String?
    @State private var digestTopics: [DigestTopic] = []
    @State private var showCreateTask = false
    @State private var taskPrefillText = ""
    @State private var taskPrefillSourceType = "digest"
    @State private var jiraIssues: [String: JiraIssue] = [:]
    @State private var jiraConnected = false
    @State private var jiraSiteURL: String?
    @State private var withoutJiraEnabled = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                // Header
                header

                // Contributing channels (for cross-channel digests)
                contributingChannelsSection

                // Summary
                if !digest.summary.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Summary")
                            .font(.headline)
                        Text(digest.summary)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }

                // Topics
                topicsSection

                // Ongoing Topics (from running summary)
                ongoingTopicsSection

                // Decisions
                decisionsSection

                // Tracks
                tracksSection

                Divider()

                // Stats footer
                statsFooter
            }
            .padding()
        }
        .sheet(isPresented: $showCreateTask) {
            CreateTaskSheet(
                prefillText: taskPrefillText,
                prefillSourceType: taskPrefillSourceType,
                prefillSourceID: String(digest.id)
            )
        }
        .navigationTitle(channelName.map { "#\($0)" } ?? "Digest")
        .task {
            jiraConnected = JiraQueries.isConnected()
            jiraSiteURL = JiraConfigHelper.readSiteURL()
            withoutJiraEnabled = JiraConfigHelper.readWithoutJiraDetection()

            if let dbManager = appState.databaseManager {
                digestTopics = (try? dbManager.dbPool.read { db in
                    try DigestQueries.fetchTopics(db, digestID: digest.id)
                }) ?? []

                // Load Jira issues linked to this digest
                if jiraConnected {
                    let issues = (try? dbManager.dbPool.read { db in
                        try JiraQueries.fetchIssuesForDigest(
                            db, digestID: digest.id
                        )
                    }) ?? []
                    var map: [String: JiraIssue] = [:]
                    for issue in issues {
                        map[issue.key] = issue
                    }
                    jiraIssues = map
                }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            headerTitleRow
            headerDateRow
            headerActionsRow
        }
    }

    private var headerTitleRow: some View {
        HStack(alignment: .center) {
            Text(digest.type.capitalized)
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(typeColor)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(typeColor.opacity(0.12), in: Capsule())

            if let name = channelName {
                if let url = viewModel.slackChannelURL(channelID: digest.channelID) {
                    Link(destination: url) {
                        Text("#\(name)")
                            .font(.title3)
                            .fontWeight(.semibold)
                    }
                    .buttonStyle(.borderless)
                } else {
                    Text("#\(name)")
                        .font(.title3)
                        .fontWeight(.semibold)
                }
            } else {
                Text("Cross-channel")
                    .font(.title3)
                    .fontWeight(.semibold)
            }

            Spacer()

            if let onClose {
                Button { onClose() } label: {
                    Image(systemName: "xmark.circle.fill")
                        .symbolRenderingMode(.hierarchical)
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.borderless)
            }
        }
    }

    private var headerDateRow: some View {
        HStack(spacing: 12) {
            Label(
                "\(TimeFormatting.shortDateTime(fromUnix: digest.periodFrom)) — \(TimeFormatting.shortDateTime(fromUnix: digest.periodTo))",
                systemImage: "calendar"
            )
            .font(.caption)
            .foregroundStyle(.secondary)

            if digest.messageCount > 0 {
                Label("\(digest.messageCount) messages", systemImage: "message")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()
        }
    }

    private var headerActionsRow: some View {
        HStack(spacing: 12) {
            if !digest.channelID.isEmpty {
                Button {
                    markChannelRead()
                } label: {
                    if markingRead {
                        ProgressView()
                            .controlSize(.mini)
                    } else {
                        Label(
                            markedRead ? "Marked read" : "Mark read in Slack",
                            systemImage: markedRead ? "checkmark.circle.fill" : "eye"
                        )
                        .font(.caption)
                        .foregroundStyle(markedRead ? .green : .accentColor)
                    }
                }
                .buttonStyle(.borderless)
                .disabled(markingRead || markedRead)
            }

            if let err = markReadError {
                Text(err)
                    .font(.caption2)
                    .foregroundStyle(.red)
            }

            Spacer()

            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "digest",
                    entityID: String(digest.id),
                    dbManager: dbManager
                )
            }
        }
    }

    @ViewBuilder
    private var contributingChannelsSection: some View {
        let contributing = viewModel.contributingChannels(for: digest)
        if !contributing.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Channels")
                    .font(.headline)
                FlowLayout(spacing: 6) {
                    ForEach(contributing, id: \.channelID) { item in
                        if let url = viewModel.slackChannelURL(channelID: item.channelID) {
                            Link(destination: url) {
                                Text("#\(item.name)")
                                    .font(.caption)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 4)
                                    .background(Color.blue.opacity(0.1), in: Capsule())
                            }
                            .buttonStyle(.borderless)
                        } else {
                            Text("#\(item.name)")
                                .font(.caption)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 4)
                                .background(Color.secondary.opacity(0.1), in: Capsule())
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var topicsSection: some View {
        if !digestTopics.isEmpty {
            // New structured topics with nested decisions
            ForEach(digestTopics) { topic in
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text(topic.title)
                            .font(.headline)
                        Spacer()
                        Button {
                            taskPrefillText = topic.title + (topic.summary.isEmpty ? "" : ": \(topic.summary)")
                            taskPrefillSourceType = "digest"
                            showCreateTask = true
                        } label: {
                            Image(systemName: "plus.circle")
                                .foregroundStyle(.secondary)
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                        .help("Create task from topic")
                    }
                    if !topic.summary.isEmpty {
                        Text(topic.summary)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    let decisions = topic.parsedDecisions
                    if !decisions.isEmpty {
                        ForEach(Array(decisions.enumerated()), id: \.element.id) { idx, decision in
                            DecisionCard(
                                decision: decision,
                                slackURL: decision.messageTS.flatMap { ts in
                                    viewModel.slackMessageURL(channelID: digest.channelID, messageTS: ts)
                                },
                                feedbackEntityID: "\(digest.id):\(topic.idx):\(idx)",
                                dbManager: appState.databaseManager,
                                jiraIssues: jiraIssues,
                                jiraSiteURL: jiraSiteURL
                            )
                        }
                    }
                    // Action items with Jira enrichment
                    let actionItems = topic.parsedActionItems
                    if !actionItems.isEmpty {
                        actionItemsList(actionItems)
                    }
                }
            }
        } else {
            // Fallback: old-style topic tags for legacy digests
            let topicNames = digest.parsedTopics
            if !topicNames.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Topics")
                        .font(.headline)
                    FlowLayout(spacing: 6) {
                        ForEach(topicNames, id: \.self) { topic in
                            Text(topic)
                                .font(.caption)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 4)
                                .background(Color.accentColor.opacity(0.1), in: Capsule())
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var ongoingTopicsSection: some View {
        if let rs = digest.parsedRunningSummary,
           let activeTopics = rs.activeTopics, !activeTopics.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Ongoing Topics")
                    .font(.headline)
                ForEach(activeTopics) { topic in
                    HStack(alignment: .top, spacing: 8) {
                        Circle()
                            .fill(topicStatusColor(topic.status))
                            .frame(width: 8, height: 8)
                            .padding(.top, 5)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(topic.topic)
                                .font(.subheadline)
                                .fontWeight(.medium)
                            if let topicSummary = topic.summary,
                               !topicSummary.isEmpty {
                                Text(topicSummary)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            HStack(spacing: 8) {
                                if let status = topic.status {
                                    Text(status)
                                        .font(.caption2)
                                        .foregroundStyle(
                                            topicStatusColor(status)
                                        )
                                }
                                if let started = topic.started {
                                    Text("since \(started)")
                                        .font(.caption2)
                                        .foregroundStyle(.tertiary)
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    private func topicStatusColor(_ status: String?) -> Color {
        switch status {
        case "in_progress": .orange
        case "resolved": .green
        case "blocked": .red
        case "stale": .gray
        default: .blue
        }
    }

    @ViewBuilder
    private var decisionsSection: some View {
        // Only show flat decisions section for legacy digests (no topics)
        if digestTopics.isEmpty {
            let decisions = digest.parsedDecisions
            if !decisions.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Decisions")
                        .font(.headline)
                    ForEach(Array(decisions.enumerated()), id: \.element.id) { idx, decision in
                        VStack(spacing: 0) {
                            DecisionCard(
                                decision: decision,
                                slackURL: decision.messageTS.flatMap { ts in
                                    viewModel.slackMessageURL(
                                        channelID: digest.channelID,
                                        messageTS: ts
                                    )
                                },
                                feedbackEntityID: "\(digest.id):\(idx)",
                                dbManager: appState.databaseManager,
                                jiraIssues: jiraIssues,
                                jiraSiteURL: jiraSiteURL
                            )
                            HStack {
                                Spacer()
                                Button {
                                    taskPrefillText = decision.text
                                    taskPrefillSourceType = "digest"
                                    showCreateTask = true
                                } label: {
                                    Label("Create task", systemImage: "plus.circle")
                                        .font(.caption)
                                }
                                .buttonStyle(.plain)
                                .foregroundStyle(.secondary)
                            }
                            .padding(.trailing, 4)
                            .padding(.top, 2)
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var tracksSection: some View {
        let tracks = digest.parsedTracks
        if !tracks.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Tracks")
                    .font(.headline)
                actionItemsList(tracks)
            }
        }
    }

    @ViewBuilder
    private func actionItemsList(
        _ items: [DigestTrack]
    ) -> some View {
        ForEach(items) { item in
            HStack(alignment: .top) {
                Image(
                    systemName: item.status == "done"
                        ? "checkmark.circle.fill"
                        : "circle"
                )
                .foregroundStyle(
                    item.status == "done" ? .green : .secondary
                )
                .font(.subheadline)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 4) {
                        Text(item.text)
                            .font(.subheadline)
                        actionItemJiraBadges(for: item.text)
                    }
                    if let assignee = item.assignee {
                        Text(assignee)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func actionItemJiraBadges(
        for text: String
    ) -> some View {
        let keys = text.extractJiraKeys()
        if jiraConnected {
            if !keys.isEmpty {
                JiraKeyBadgesView(
                    text: text,
                    issues: jiraIssues,
                    siteURL: jiraSiteURL,
                    isConnected: jiraConnected
                )
            } else if withoutJiraEnabled {
                Image(systemName: "exclamationmark.triangle")
                    .font(.caption2)
                    .foregroundStyle(.orange)
                    .help(
                        "This discussion is not tracked in Jira"
                    )
            }
        }
    }

    private var statsFooter: some View {
        HStack {
            if !digest.model.isEmpty {
                Label(digest.model, systemImage: "cpu")
            }
            Spacer()
            if let input = digest.inputTokens, let output = digest.outputTokens {
                Text("\(input + output) tokens")
            }
            if let cost = digest.costUSD {
                Text(String(format: "$%.4f", cost))
            }
        }
        .font(.caption)
        .foregroundStyle(.tertiary)
    }

    private func markChannelRead() {
        markingRead = true
        markReadError = nil
        Task {
            do {
                try await SlackService.markRead(channelID: digest.channelID)
                markedRead = true
            } catch {
                markReadError = error.localizedDescription
            }
            markingRead = false
        }
    }

    private var typeColor: Color {
        switch digest.type {
        case "channel": .blue
        case "daily": .purple
        case "weekly": .indigo
        default: .secondary
        }
    }

}

/// Simple flow layout for topic tags
struct FlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let result = layout(proposal: proposal, subviews: subviews)
        return result.size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let result = layout(proposal: proposal, subviews: subviews)
        for (index, position) in result.positions.enumerated() {
            subviews[index].place(at: CGPoint(x: bounds.minX + position.x, y: bounds.minY + position.y), proposal: .unspecified)
        }
    }

    private func layout(proposal: ProposedViewSize, subviews: Subviews) -> (size: CGSize, positions: [CGPoint]) {
        let maxWidth = proposal.width ?? .infinity
        var positions: [CGPoint] = []
        var x: CGFloat = 0
        var y: CGFloat = 0
        var rowHeight: CGFloat = 0

        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x + size.width > maxWidth, x > 0 {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            positions.append(CGPoint(x: x, y: y))
            rowHeight = max(rowHeight, size.height)
            x += size.width + spacing
        }

        return (CGSize(width: maxWidth, height: y + rowHeight), positions)
    }
}
