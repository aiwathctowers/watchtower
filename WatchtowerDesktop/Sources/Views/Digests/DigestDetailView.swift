import SwiftUI

struct DigestDetailView: View {
    let digest: Digest
    let channelName: String?
    let viewModel: DigestViewModel
    @Environment(AppState.self) private var appState

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

                // Decisions
                decisionsSection

                // Action Items
                actionItemsSection

                Divider()

                // Stats footer
                statsFooter
            }
            .padding()
        }
        .navigationTitle(channelName.map { "#\($0)" } ?? "Digest")
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                // Type badge
                Text(digest.type.capitalized)
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(typeColor)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(typeColor.opacity(0.12), in: Capsule())

                if let name = channelName {
                    Text("#\(name)")
                        .font(.title3)
                        .fontWeight(.semibold)
                } else {
                    Text("Cross-channel")
                        .font(.title3)
                        .fontWeight(.semibold)
                }

                Spacer()
            }

            // Date range + message count
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

            // Action buttons
            HStack(spacing: 12) {
                if !digest.channelID.isEmpty {
                    Button {
                        appState.navigateToChannel(digest.channelID)
                    } label: {
                        Label("View channel", systemImage: "text.bubble")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)
                }

                if !digest.channelID.isEmpty,
                   let url = viewModel.slackChannelURL(channelID: digest.channelID) {
                    Link(destination: url) {
                        Label("Open in Slack", systemImage: "arrow.up.right.square")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)
                }
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
        let topics = digest.parsedTopics
        if !topics.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Topics")
                    .font(.headline)
                FlowLayout(spacing: 6) {
                    ForEach(topics, id: \.self) { topic in
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

    @ViewBuilder
    private var decisionsSection: some View {
        let decisions = digest.parsedDecisions
        if !decisions.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Decisions")
                    .font(.headline)
                ForEach(decisions) { decision in
                    DecisionCard(
                        decision: decision,
                        slackURL: decision.messageTS.flatMap { ts in
                            viewModel.slackMessageURL(channelID: digest.channelID, messageTS: ts)
                        }
                    )
                }
            }
        }
    }

    @ViewBuilder
    private var actionItemsSection: some View {
        let actions = digest.parsedActionItems
        if !actions.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Action Items")
                    .font(.headline)
                ForEach(actions) { item in
                    HStack(alignment: .top) {
                        Image(systemName: item.status == "done" ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(item.status == "done" ? .green : .secondary)
                            .font(.subheadline)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.text)
                                .font(.subheadline)
                            if let assignee = item.assignee {
                                Text(assignee)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
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
