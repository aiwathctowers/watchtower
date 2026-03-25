import SwiftUI

struct TrackDetailView: View {
    let track: Track
    let viewModel: TracksViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState
    @State private var chatVM: TrackChatViewModel?

    var body: some View {
        VSplitView {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    headerSection
                    currentStatusSection
                    narrativeSection
                    timelineSection
                    participantsSection
                    keyMessagesSection
                    linkedDigestsSection
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
            if track.hasUpdates {
                viewModel.markRead(track)
            }
            if let db = appState.databaseManager {
                chatVM = TrackChatViewModel(
                    track: track, viewModel: viewModel, dbManager: db
                )
            }
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                priorityBadge
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

            Text(track.title)
                .font(.title3)
                .fontWeight(.semibold)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)

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

            // Tags
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
    }

    // MARK: - Current Status (highlighted card)

    @ViewBuilder
    private var currentStatusSection: some View {
        if !track.currentStatus.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Text("Current Status")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                Text(viewModel.resolveUserIDs(track.currentStatus))
                    .font(.subheadline)
                    .textSelection(.enabled)
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.blue.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Narrative

    @ViewBuilder
    private var narrativeSection: some View {
        if !track.narrative.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Narrative")
                    .font(.headline)
                Text(viewModel.resolveUserIDs(track.narrative))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Timeline

    @ViewBuilder
    private var timelineSection: some View {
        let events = track.decodedTimeline
        if !events.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Timeline")
                    .font(.headline)

                ForEach(events) { event in
                    HStack(alignment: .top, spacing: 8) {
                        Circle()
                            .fill(.blue)
                            .frame(width: 8, height: 8)
                            .padding(.top, 5)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(event.event)
                                .font(.caption)
                            HStack(spacing: 6) {
                                Text(event.date)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                                if let ch = event.channel, !ch.isEmpty {
                                    Text("#\(ch)")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
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
                            .foregroundStyle(roleColor(person.role))
                            .font(.subheadline)
                            .frame(width: 20)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(person.name)
                                .font(.subheadline)
                                .fontWeight(.medium)
                            if let role = person.role, !role.isEmpty {
                                Text(role)
                                    .font(.caption)
                                    .foregroundStyle(roleColor(role))
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Key Messages

    @ViewBuilder
    private var keyMessagesSection: some View {
        let messages = track.decodedKeyMessages
        if !messages.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Key Messages")
                    .font(.headline)

                ForEach(messages) { msg in
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "quote.opening")
                            .foregroundStyle(.tertiary)
                            .font(.caption)
                            .frame(width: 16)
                            .padding(.top, 2)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(msg.author)
                                .font(.caption)
                                .fontWeight(.semibold)
                                .foregroundStyle(.secondary)
                            Text(msg.text)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                            if let date = msg.date {
                                Text(date, style: .relative)
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                        }

                        Spacer()

                        if let ch = msg.channel, !ch.isEmpty {
                            if let url = viewModel.slackMessageURL(
                                channelID: ch, messageTS: msg.ts
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

    // MARK: - Linked Digests (expandable via source_refs)

    @ViewBuilder
    private var linkedDigestsSection: some View {
        let digestIDs = track.linkedDigestIDs
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

    // MARK: - Actions

    private var actionsSection: some View {
        HStack(spacing: 8) {
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

    // MARK: - Helpers

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

    private var priorityColor: Color {
        switch track.priority {
        case "high": .red
        case "low": .blue
        default: .orange
        }
    }

    private func roleColor(_ role: String?) -> Color {
        switch role {
        case "driver": .green
        case "reviewer": .blue
        case "blocker": .red
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
