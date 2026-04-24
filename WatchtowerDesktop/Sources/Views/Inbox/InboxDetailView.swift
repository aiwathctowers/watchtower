import SwiftUI

struct InboxDetailView: View {
    let item: InboxItem
    let viewModel: InboxViewModel
    var onClose: (() -> Void)?
    @Environment(AppState.self) private var appState

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                headerSection
                aiReasonSection
                messageSection
                contextSection
                resolvedReasonSection
                actionsSection
                metaSection
                feedbackSection
            }
            .padding()
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center, spacing: 8) {
                Image(systemName: item.triggerIcon)
                    .foregroundStyle(triggerColor)
                Text(triggerLabel)
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Text("#\(viewModel.channelName(for: item))")
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(.quaternary, in: Capsule())

                priorityBadge

                Spacer()

                Text(item.messageDate, style: .relative)
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                if let close = onClose {
                    Button { close() } label: {
                        Image(systemName: "xmark")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }

            HStack(alignment: .firstTextBaseline, spacing: 6) {
                let waitingNames = item.decodedWaitingUserIDs.map { viewModel.userName(for: $0) }
                if waitingNames.count > 1 {
                    Text(waitingNames.joined(separator: ", "))
                        .font(.title3)
                        .fontWeight(.semibold)
                } else {
                    Text(viewModel.senderName(for: item))
                        .font(.title3)
                        .fontWeight(.semibold)
                }

                Label(item.status.capitalized, systemImage: item.statusIcon)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var priorityBadge: some View {
        Text(item.priority.capitalized)
            .font(.caption)
            .fontWeight(.medium)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(badgeColor.opacity(0.15), in: Capsule())
            .foregroundStyle(badgeColor)
    }

    private var triggerLabel: String {
        switch item.triggerType {
        case "dm": return "Direct Message"
        case "mention": return "@Mention"
        case "thread_reply": return "Thread Reply"
        case "reaction": return "Reaction"
        default: return item.triggerType.capitalized
        }
    }

    private var triggerColor: Color {
        switch item.triggerType {
        case "mention": return .blue
        case "dm": return .green
        case "thread_reply": return .purple
        case "reaction": return .yellow
        default: return .secondary
        }
    }

    private var badgeColor: Color {
        switch item.priority {
        case "high": return .red
        case "medium": return .orange
        case "low": return .secondary
        default: return .orange
        }
    }

    // MARK: - AI Reason (prominent, right after header)

    @ViewBuilder
    private var aiReasonSection: some View {
        if !item.aiReason.isEmpty {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "sparkles")
                    .foregroundStyle(.yellow)
                    .font(.callout)
                Text(SlackTextParser.toPlainText(item.aiReason))
                    .font(.callout)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.yellow.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Conversation (message + thread as chat bubbles)

    @ViewBuilder
    private var messageSection: some View {
        EmptyView()
    }

    @ViewBuilder
    private var contextSection: some View {
        let messages = conversationMessages
        if !messages.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(messages.enumerated()), id: \.offset) { idx, msg in
                    let prevAuthor = idx > 0 ? messages[idx - 1].author : nil
                    let sameAuthor = prevAuthor == msg.author
                    chatBubble(msg, showAuthor: !sameAuthor)
                }
            }
        }
    }

    private struct ChatMessage {
        let author: String
        let text: String
        let isHighlighted: Bool // the trigger message
    }

    private var conversationMessages: [ChatMessage] {
        var result: [ChatMessage] = []

        let snippetText = SlackTextParser.toPlainText(item.snippet)
        let senderName = viewModel.senderName(for: item)

        // Thread context lines first; highlight if it matches the trigger snippet
        for line in contextLines {
            let isSnippet = line.author == senderName && line.text == snippetText
            result.append(ChatMessage(author: line.author, text: line.text, isHighlighted: isSnippet))
        }

        // If snippet wasn't found in context, append it as the trigger message
        if !snippetText.isEmpty && !result.contains(where: { $0.isHighlighted }) {
            result.append(ChatMessage(author: senderName, text: snippetText, isHighlighted: true))
        }

        return result
    }

    private func chatBubble(_ msg: ChatMessage, showAuthor: Bool) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if showAuthor {
                Text(msg.author)
                    .font(.caption2)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .padding(.top, 8)
            }

            Text(msg.text)
                .font(.callout)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(
                    msg.isHighlighted
                        ? Color.accentColor.opacity(0.15)
                        : Color(nsColor: .controlBackgroundColor),
                    in: RoundedRectangle(cornerRadius: 12)
                )
                .overlay(
                    msg.isHighlighted
                        ? RoundedRectangle(cornerRadius: 12)
                            .strokeBorder(Color.accentColor.opacity(0.3), lineWidth: 1)
                        : nil
                )
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private struct ContextLine {
        let author: String
        let text: String
    }

    private var contextLines: [ContextLine] {
        item.context.split(separator: "\n", omittingEmptySubsequences: true).compactMap { rawLine in
            let line = String(rawLine)
            guard line.hasPrefix("["),
                  let closeBracket = line.firstIndex(of: "]") else {
                return ContextLine(author: "", text: line)
            }
            let author = String(line[line.index(after: line.startIndex)..<closeBracket])
            let text = String(line[line.index(after: closeBracket)...]).trimmingCharacters(in: .whitespaces)
            return ContextLine(author: author, text: SlackTextParser.toPlainText(text))
        }
    }

    // MARK: - Resolved Reason

    @ViewBuilder
    private var resolvedReasonSection: some View {
        if !item.resolvedReason.isEmpty {
            HStack(spacing: 8) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text(item.resolvedReason)
                    .font(.callout)
                    .foregroundStyle(.green)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.green.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Actions

    private var actionsSection: some View {
        VStack(spacing: 8) {
            if let slackURL = viewModel.slackMessageURL(for: item) {
                Button {
                    NSWorkspace.shared.open(slackURL)
                } label: {
                    Label("Open in Slack", systemImage: "arrow.up.right.square")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
            }

            if item.isPending {
                Button {
                    viewModel.resolve(item)
                } label: {
                    Label("Done", systemImage: "checkmark")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)

                if !item.hasLinkedTarget {
                    Button {
                        viewModel.createTask(from: item)
                    } label: {
                        Label("Create Target", systemImage: "scope")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
    }

    // MARK: - Meta

    private var metaSection: some View {
        HStack(spacing: 16) {
            if !item.threadTS.isEmpty {
                Label("Thread", systemImage: "bubble.left.and.bubble.right")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            if let linkedTargetID = item.targetID {
                Label("Target #\(linkedTargetID)", systemImage: "scope")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            Spacer()
        }
    }

    // MARK: - Feedback

    private var feedbackSection: some View {
        HStack {
            Spacer()
            if let db = appState.databaseManager {
                FeedbackButtons(
                    entityType: "inbox",
                    entityID: "\(item.id)",
                    dbManager: db
                )
            }
        }
    }
}
