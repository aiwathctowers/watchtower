import SwiftUI

struct TrackDetailView: View {
    let item: Track
    let viewModel: TracksViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState
    @State private var history: [TrackHistoryEntry] = []
    @State private var chatVM: TrackChatViewModel?
    @State private var showSnoozePopover = false
    @State private var snoozeDate = Calendar.current.date(byAdding: .day, value: 1, to: Date()) ?? Date()

    var body: some View {
        VSplitView {
            // Top: item details + history
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    headerSection
                    subItemsSection
                    contextSection
                    blockingSection
                    decisionSection
                    decisionOptionsSection
                    participantsSection
                    sourceRefsSection
                    relatedDigestsSection
                    actionsSection
                    historySection
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
            if item.hasUpdates {
                viewModel.markUpdateRead(item)
            }
            history = viewModel.fetchHistory(for: item.id)
            if let db = appState.databaseManager {
                chatVM = TrackChatViewModel(
                    item: item,
                    viewModel: viewModel,
                    dbManager: db
                )
            }
        }
        // Note: .onChange(of: item.id) is not needed because .id(id) on the parent
        // causes SwiftUI to destroy and recreate this view when id changes.
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center) {
                priorityBadge
                statusBadge
                categoryBadge

                if item.hasUpdates {
                    Label("Updated", systemImage: "bell.badge.fill")
                        .font(.caption)
                        .foregroundStyle(.orange)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.orange.opacity(0.12), in: Capsule())
                }

                Spacer()

                Text(item.sourceDate, style: .relative)
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

            Text(item.text)
                .font(.title3)
                .fontWeight(.semibold)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)

            HStack(spacing: 12) {
                if !item.sourceChannelName.isEmpty {
                    if let url = viewModel.slackChannelURL(channelID: item.channelID) {
                        Link(destination: url) {
                            Label("#\(item.sourceChannelName)", systemImage: "number")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                    } else {
                        Label("#\(item.sourceChannelName)", systemImage: "number")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                if !item.requesterName.isEmpty {
                    Label("from \(item.requesterName)", systemImage: "person.fill")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                if let due = item.dueDateFormatted {
                    Label("Due: \(due)", systemImage: "calendar")
                        .font(.caption)
                        .foregroundStyle(item.isOverdue ? .red : .secondary)
                }
            }

            let itemTags = item.decodedTags
            if !itemTags.isEmpty {
                HStack(spacing: 4) {
                    ForEach(itemTags, id: \.self) { tag in
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

    // MARK: - Sub-Items

    @ViewBuilder
    private var subItemsSection: some View {
        let subs = item.decodedSubItems
        if !subs.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Checklist")
                        .font(.headline)
                    let progress = item.subItemsProgress
                    Text("\(progress.done)/\(progress.total)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Spacer()
                    if progress.total > 0 {
                        ProgressView(value: Double(progress.done), total: Double(progress.total))
                            .frame(width: 80)
                    }
                }

                ForEach(Array(subs.enumerated()), id: \.offset) { index, sub in
                    HStack(alignment: .top, spacing: 8) {
                        Button {
                            viewModel.toggleSubItem(item, subItemIndex: index)
                        } label: {
                            Image(systemName: sub.isDone ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(sub.isDone ? .green : .secondary)
                                .font(.body)
                        }
                        .buttonStyle(.borderless)

                        Text(sub.text)
                            .font(.subheadline)
                            .strikethrough(sub.isDone)
                            .foregroundStyle(sub.isDone ? .secondary : .primary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    // MARK: - Context

    @ViewBuilder
    private var contextSection: some View {
        if !item.context.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Context")
                    .font(.headline)
                Text(item.context)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Participants

    @ViewBuilder
    private var participantsSection: some View {
        let people = item.decodedParticipants
        if !people.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Participants")
                    .font(.headline)

                ForEach(people) { person in
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "person.circle.fill")
                            .foregroundStyle(.secondary)
                            .font(.subheadline)
                            .frame(width: 20)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(person.name)
                                .font(.subheadline)
                                .fontWeight(.medium)
                            if let stance = person.stance, !stance.isEmpty {
                                Text(stance)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Source References

    @ViewBuilder
    private var sourceRefsSection: some View {
        let refs = item.decodedSourceRefs
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
                            Text(ref.author)
                                .font(.caption)
                                .fontWeight(.semibold)
                                .foregroundStyle(.secondary)
                            Text(ref.text)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }

                        Spacer()

                        if let url = viewModel.slackMessageURL(channelID: item.channelID, messageTS: ref.ts) {
                            Link(destination: url) {
                                Image(systemName: "arrow.up.right.square")
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                            .buttonStyle(.borderless)
                            .help("Open in Slack")
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    // MARK: - Blocking

    @ViewBuilder
    private var blockingSection: some View {
        if !item.blocking.isEmpty {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                    .font(.subheadline)
                Text(item.blocking)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.orange.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Decision Summary

    @ViewBuilder
    private var decisionSection: some View {
        if !item.decisionSummary.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Decision Path")
                    .font(.headline)
                Text(item.decisionSummary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Decision Options

    @ViewBuilder
    private var decisionOptionsSection: some View {
        let options = item.decodedDecisionOptions
        if !options.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Options Under Consideration")
                    .font(.headline)

                ForEach(options) { opt in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(opt.option)
                            .font(.subheadline)
                            .fontWeight(.medium)

                        if let supporters = opt.supporters, !supporters.isEmpty {
                            Text("Supporters: \(supporters.joined(separator: ", "))")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }

                        HStack(spacing: 16) {
                            if let pros = opt.pros, !pros.isEmpty {
                                Label(pros, systemImage: "plus.circle")
                                    .font(.caption)
                                    .foregroundStyle(.green)
                            }
                            if let cons = opt.cons, !cons.isEmpty {
                                Label(cons, systemImage: "minus.circle")
                                    .font(.caption)
                                    .foregroundStyle(.red)
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

    // MARK: - Related Digests

    @ViewBuilder
    private var relatedDigestsSection: some View {
        let digestIDs = item.decodedRelatedDigestIDs
        if !digestIDs.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("Related Digests")
                    .font(.headline)

                FlowLayout(spacing: 6) {
                    ForEach(digestIDs, id: \.self) { digestID in
                        Button {
                            appState.navigateToDigest(digestID)
                        } label: {
                            Text("Digest #\(digestID)")
                                .font(.caption)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 4)
                                .background(.blue.opacity(0.1), in: Capsule())
                                .foregroundStyle(.blue)
                        }
                        .buttonStyle(.plain)
                        .onHover { hovering in
                            if hovering {
                                NSCursor.pointingHand.push()
                            } else {
                                NSCursor.pop()
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Actions

    private func refreshHistory() {
        history = viewModel.fetchHistory(for: item.id)
    }

    private var actionsSection: some View {
        HStack(spacing: 8) {
            if item.isInbox {
                Button("Accept") { viewModel.accept(item); refreshHistory() }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                Button("Dismiss") { viewModel.dismiss(item); refreshHistory() }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                snoozeButton
            } else if item.isActive {
                Button("Done") { viewModel.markDone(item); refreshHistory() }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .tint(.green)
                Button("Dismiss") { viewModel.dismiss(item); refreshHistory() }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                snoozeButton
            } else {
                Button("Move to Inbox") { viewModel.reopen(item); refreshHistory() }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
            }

            Spacer()

            if !item.sourceMessageTS.isEmpty, !item.channelID.isEmpty,
               let url = viewModel.slackMessageURL(channelID: item.channelID, messageTS: item.sourceMessageTS) {
                Link(destination: url) {
                    Label("View in Slack", systemImage: "arrow.up.right.square")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            }

            if let dbManager = appState.databaseManager {
                FeedbackButtons(
                    entityType: "track",
                    entityID: String(item.id),
                    dbManager: dbManager
                )
            }
        }
    }

    private var snoozeButton: some View {
        Button("Snooze") { showSnoozePopover = true }
            .buttonStyle(.bordered)
            .controlSize(.small)
            .popover(isPresented: $showSnoozePopover) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Snooze until").font(.headline)
                    Button("Later today (+4h)") {
                        viewModel.snooze(item, until: Date().addingTimeInterval(4 * 3600))
                        showSnoozePopover = false
                    }
                    .buttonStyle(.borderless)
                    Button("Tomorrow 9:00") {
                        guard let tomorrow = Calendar.current.date(byAdding: .day, value: 1, to: Date()),
                          let at9 = Calendar.current.date(bySettingHour: 9, minute: 0, second: 0, of: tomorrow) else { return }
                        viewModel.snooze(item, until: at9)
                        showSnoozePopover = false
                    }
                    .buttonStyle(.borderless)
                    Button("Next week") {
                        guard let monday = Calendar.current.nextDate(after: Date(), matching: DateComponents(weekday: 2), matchingPolicy: .nextTime),
                              let at9 = Calendar.current.date(bySettingHour: 9, minute: 0, second: 0, of: monday) else { return }
                        viewModel.snooze(item, until: at9)
                        showSnoozePopover = false
                    }
                    .buttonStyle(.borderless)
                    Divider()
                    DatePicker("Pick date", selection: $snoozeDate, in: Date()..., displayedComponents: [.date, .hourAndMinute])
                        .datePickerStyle(.compact)
                    Button("Snooze") {
                        viewModel.snooze(item, until: snoozeDate)
                        showSnoozePopover = false
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding()
                .frame(width: 250)
            }
    }

    // MARK: - History

    @ViewBuilder
    private var historySection: some View {
        if !history.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("History")
                    .font(.headline)

                ForEach(history) { entry in
                    HStack(alignment: .top, spacing: 8) {
                        Circle()
                            .fill(historyColor(entry.event))
                            .frame(width: 8, height: 8)
                            .padding(.top, 4)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(entry.displayText)
                                .font(.caption)
                            if let detail = entry.detailText {
                                Text(detail)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(2)
                            }
                            Text(TimeFormatting.shortDateTime(from: entry.createdDate))
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Helpers

    private var priorityBadge: some View {
        HStack(spacing: 4) {
            Circle()
                .fill(priorityColor)
                .frame(width: 8, height: 8)
            Text(item.priority.capitalized)
                .font(.caption)
                .foregroundStyle(priorityColor)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(priorityColor.opacity(0.1), in: Capsule())
    }

    private var statusBadge: some View {
        Text(item.status.capitalized)
            .font(.caption)
            .fontWeight(.semibold)
            .foregroundStyle(statusColor)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(statusColor.opacity(0.12), in: Capsule())
    }

    @ViewBuilder
    private var categoryBadge: some View {
        let label = item.categoryLabel
        if !label.isEmpty {
            Text(label)
                .font(.caption)
                .foregroundStyle(categoryColor)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(categoryColor.opacity(0.1), in: Capsule())
        }
    }

    private var categoryColor: Color {
        switch item.category {
        case "code_review": return .purple
        case "decision_needed": return .orange
        case "info_request": return .blue
        case "approval": return .yellow
        case "follow_up": return .teal
        case "bug_fix": return .red
        case "discussion": return .indigo
        default: return .secondary
        }
    }

    private var priorityColor: Color {
        switch item.priority {
        case "high": .red
        case "low": .blue
        default: .orange
        }
    }

    private var statusColor: Color {
        switch item.status {
        case "inbox": .cyan
        case "active": .green
        case "done": .blue
        case "dismissed": .gray
        case "snoozed": .purple
        default: .secondary
        }
    }

    private static let historyColorMap: [String: Color] = [
        "created": .green,
        "accepted": .teal,
        "status_changed": .blue,
        "snoozed": .purple,
        "reactivated": .orange,
        "update_detected": .yellow,
        "update_read": .gray,
        "reopened": .orange,
        "priority_changed": .pink,
        "re_extracted": .cyan,
        "decision_evolved": .indigo,
        "digest_linked": .blue,
        "sub_items_updated": .mint,
        "context_updated": .teal,
        "due_date_changed": .orange
    ]

    private func historyColor(_ event: String) -> Color {
        Self.historyColorMap[event] ?? .secondary
    }
}
