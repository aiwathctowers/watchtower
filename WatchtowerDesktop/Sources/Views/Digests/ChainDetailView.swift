import SwiftUI

struct ChainDetailView: View {
    let chain: Chain
    let viewModel: ChainsViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                headerSection
                summarySection
                childrenSection
                channelsSection
                actionsSection
                timelineSection
            }
            .padding()
        }
        .onAppear {
            viewModel.loadRefs(for: chain.id)
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                statusBadge
                itemCountBadge

                if !chain.isRead {
                    Label("Unread", systemImage: "circle.fill")
                        .font(.caption)
                        .foregroundStyle(.blue)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.blue.opacity(0.12), in: Capsule())
                }

                Spacer()

                Text(chain.lastSeenDate, style: .relative)
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

            Text(chain.title)
                .font(.title3)
                .fontWeight(.semibold)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)

            HStack(spacing: 12) {
                Label("\(chain.itemCount) items", systemImage: "number")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Label {
                    Text(chain.firstSeenDate, format: .dateTime.month().day())
                } icon: {
                    Image(systemName: "calendar")
                }
                .font(.caption)
                .foregroundStyle(.secondary)

                Text("—")
                    .font(.caption)
                    .foregroundStyle(.tertiary)

                Text(chain.lastSeenDate, format: .dateTime.month().day())
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Summary

    @ViewBuilder
    private var summarySection: some View {
        if !chain.summary.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Summary")
                    .font(.headline)
                Text(chain.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Children

    @ViewBuilder
    private var childrenSection: some View {
        let children = viewModel.children(for: chain.id)
        if !children.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Sub-chains (\(children.count))")
                    .font(.headline)

                ForEach(children) { child in
                    HStack(alignment: .top, spacing: 8) {
                        statusIcon(child.status)
                            .font(.subheadline)
                            .frame(width: 20)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(child.title)
                                .font(.subheadline)
                                .fontWeight(.medium)
                            if !child.summary.isEmpty {
                                Text(child.summary)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(2)
                            }
                        }

                        Spacer()

                        Text("\(child.itemCount)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(.quaternary, in: Capsule())

                        if !child.isRead {
                            Circle()
                                .fill(.blue)
                                .frame(width: 6, height: 6)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    // MARK: - Channels

    @ViewBuilder
    private var channelsSection: some View {
        let ids = chain.decodedChannelIDs
        if !ids.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Channels")
                    .font(.headline)

                FlowLayout(spacing: 6) {
                    ForEach(ids, id: \.self) { chID in
                        if let url = viewModel.slackChannelURL(channelID: chID) {
                            Link(destination: url) {
                                Text("#" + viewModel.channelName(for: chID))
                                    .font(.caption)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 4)
                                    .background(.blue.opacity(0.1), in: Capsule())
                            }
                            .buttonStyle(.borderless)
                        } else {
                            Text("#" + viewModel.channelName(for: chID))
                                .font(.caption)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 4)
                                .background(.blue.opacity(0.1), in: Capsule())
                        }
                    }
                }
            }
        }
    }

    // MARK: - Actions

    private var actionsSection: some View {
        HStack(spacing: 8) {
            if chain.isActive {
                Button("Archive") {
                    viewModel.archiveChain(chain.id)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }

            Spacer()

            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "chain",
                    entityID: String(chain.id),
                    dbManager: dbManager
                )
            }
        }
    }

    // MARK: - Timeline

    @ViewBuilder
    private var timelineSection: some View {
        let refs = viewModel.selectedChainRefs
        VStack(alignment: .leading, spacing: 8) {
            Text("Timeline (\(refs.count) items)")
                .font(.headline)

            if refs.isEmpty {
                Text("Loading...")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(refs) { ref in
                    timelineItem(ref)
                    if ref.id != refs.last?.id {
                        Divider()
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func timelineItem(_ ref: ChainRef) -> some View {
        HStack(alignment: .top, spacing: 10) {
            timelineIcon(ref)
            VStack(alignment: .leading, spacing: 4) {
                timelineHeader(ref)
                timelineContent(ref)
            }
        }
    }

    @ViewBuilder
    private func timelineIcon(_ ref: ChainRef) -> some View {
        if ref.isDecision {
            Image(systemName: "checkmark.seal.fill")
                .foregroundStyle(.orange)
                .frame(width: 20)
        } else if ref.isDigest {
            Image(systemName: "doc.text.fill")
                .foregroundStyle(.purple)
                .frame(width: 20)
        } else {
            Image(systemName: "target")
                .foregroundStyle(.blue)
                .frame(width: 20)
        }
    }

    private func timelineHeader(_ ref: ChainRef) -> some View {
        HStack {
            Text(refTypeLabel(ref))
                .font(.caption.bold())
                .foregroundStyle(refTypeColor(ref))

            if let url = viewModel.slackChannelURL(channelID: ref.channelID) {
                Link(destination: url) {
                    Text("#" + viewModel.channelName(for: ref.channelID))
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            } else {
                Text("#" + viewModel.channelName(for: ref.channelID))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Text(ref.timestampDate, format: .dateTime.month().day().hour().minute())
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
    }

    @ViewBuilder
    private func timelineContent(_ ref: ChainRef) -> some View {
        if ref.isDecision, let decision = viewModel.decisionText(for: ref) {
            Text(decision.text)
                .font(.subheadline)
                .textSelection(.enabled)
            if !decision.by.isEmpty {
                Text("by \(decision.by)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            importanceBadge(decision.importance)
        } else if ref.isDigest, let digest = viewModel.digestSummary(for: ref) {
            Text(digest.summary)
                .font(.subheadline)
                .textSelection(.enabled)
            let topics = digest.parsedTopics
            if !topics.isEmpty {
                FlowLayout(spacing: 4) {
                    ForEach(topics.prefix(5), id: \.self) { topic in
                        Text(topic)
                            .font(.caption2)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(Color.purple.opacity(0.1), in: Capsule())
                    }
                }
            }
            HStack(spacing: 8) {
                if digest.messageCount > 0 {
                    Label("\(digest.messageCount) msgs", systemImage: "message")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                let decCount = digest.parsedDecisions.count
                if decCount > 0 {
                    Label("\(decCount) decisions", systemImage: "arrow.triangle.branch")
                        .font(.caption2)
                        .foregroundStyle(.orange)
                }
            }
        } else if ref.isTrack {
            Text("Track #\(ref.trackID)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Helpers

    private var statusBadge: some View {
        let (text, color) = statusInfo(chain.status)
        return Text(text)
            .font(.caption)
            .fontWeight(.semibold)
            .foregroundStyle(color)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(color.opacity(0.12), in: Capsule())
    }

    private var itemCountBadge: some View {
        HStack(spacing: 4) {
            Image(systemName: "number")
                .font(.caption2)
            Text("\(chain.itemCount)")
                .font(.caption)
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(.quaternary, in: Capsule())
    }

    private func statusInfo(_ status: String) -> (String, Color) {
        switch status {
        case "active": ("Active", .blue)
        case "resolved": ("Resolved", .green)
        case "stale": ("Stale", .gray)
        default: (status.capitalized, .secondary)
        }
    }

    @ViewBuilder
    private func statusIcon(_ status: String) -> some View {
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

    private func refTypeLabel(_ ref: ChainRef) -> String {
        if ref.isDecision { return "Decision" }
        if ref.isDigest { return "Digest" }
        return "Track"
    }

    private func refTypeColor(_ ref: ChainRef) -> Color {
        if ref.isDecision { return .orange }
        if ref.isDigest { return .purple }
        return .blue
    }

    @ViewBuilder
    private func importanceBadge(_ importance: String) -> some View {
        let color: Color = switch importance {
        case "high": .red
        case "medium": .orange
        case "low": .gray
        default: .secondary
        }
        Text(importance)
            .font(.caption2)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.15), in: Capsule())
            .foregroundStyle(color)
    }
}
