import SwiftUI

// MARK: - CardSize

enum CardSize {
    case compact
    case medium
    case pinned
}

// MARK: - InboxCardView

struct InboxCardView: View {
    let item: InboxItem
    let size: CardSize
    var senderName: String? = nil
    var userNames: [String: String] = [:]
    var isExpanded: Bool = false
    var conversation: [InboxConversationMessage] = []
    var conversationLoaded: Bool = false
    let onToggle: () -> Void
    let onSnooze: (SnoozeOption) -> Void
    let onDismiss: () -> Void
    let onCreateTask: () -> Void
    let onFeedback: (Int, String) -> Void

    // MARK: - Snooze Options

    enum SnoozeOption {
        case oneHour
        case tillTomorrow
        case tillMonday
    }

    // MARK: - Body

    var body: some View {
        switch size {
        case .compact: compactView
        case .medium:  mediumView
        case .pinned:  pinnedView
        }
    }

    // MARK: - Compact (1 line — ambient feed)

    private var compactView: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(action: onToggle) {
                HStack(spacing: 6) {
                    triggerIcon
                    Text(snippetAttributed)
                        .lineLimit(isExpanded ? nil : 1)
                        .font(.callout)
                    Spacer(minLength: 0)
                    Text(item.messageDate, style: .relative)
                        .foregroundStyle(.secondary)
                        .font(.caption)
                    feedbackButtons
                }
                .padding(.vertical, 4)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if isExpanded {
                conversationSection
            }
        }
    }

    // MARK: - Medium (2 lines — non-pinned actionable)

    private var mediumView: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(action: onToggle) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        triggerIcon
                        Text(senderDisplay)
                            .fontWeight(.semibold)
                            .lineLimit(1)
                        Spacer()
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                        Text(item.messageDate, style: .relative)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Text(snippetAttributed)
                        .lineLimit(isExpanded ? nil : 2)
                        .font(.callout)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if isExpanded {
                conversationSection
            }

            actionBar
        }
        .padding(8)
        .background(RoundedRectangle(cornerRadius: 6).fill(.background.secondary))
    }

    // MARK: - Pinned (full card)

    private var pinnedView: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button(action: onToggle) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 6) {
                        priorityDot
                        triggerIcon
                        Text(senderDisplay)
                            .fontWeight(.semibold)
                            .lineLimit(1)
                        Spacer()
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                    Text(snippetAttributed)
                        .lineLimit(isExpanded ? nil : 4)
                        .font(.body)
                    if !item.aiReason.isEmpty {
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "sparkles")
                                .foregroundStyle(.yellow)
                                .font(.caption)
                            Text(SlackTextParser.toAttributedString(item.aiReason, userNames: userNames))
                                .font(.caption)
                        }
                        .padding(8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.yellow.opacity(0.08), in: RoundedRectangle(cornerRadius: 6))
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if isExpanded {
                conversationSection
            }

            actionBar
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 8).fill(.background.secondary))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(priorityColor, lineWidth: 1.5))
    }

    // MARK: - Inline Conversation

    @ViewBuilder
    private var conversationSection: some View {
        if !conversationLoaded {
            HStack(spacing: 6) {
                ProgressView().controlSize(.small)
                Text("Loading conversation…")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 4)
        } else if conversation.isEmpty {
            Text("No conversation messages found locally yet.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.vertical, 4)
        } else {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(conversation.enumerated()), id: \.element.id) { idx, msg in
                    let prevAuthor = idx > 0 ? conversation[idx - 1].author : nil
                    chatBubble(msg, showAuthor: prevAuthor != msg.author)
                }
            }
            .padding(.top, 4)
        }
    }

    private func chatBubble(_ msg: InboxConversationMessage, showAuthor: Bool) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if showAuthor {
                Text(msg.author)
                    .font(.caption2)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                    .padding(.top, 6)
            }
            Text(msg.text)
                .font(.callout)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(
                    msg.isTrigger
                        ? Color.accentColor.opacity(0.15)
                        : Color(nsColor: .controlBackgroundColor),
                    in: RoundedRectangle(cornerRadius: 12)
                )
                .overlay(
                    msg.isTrigger
                        ? RoundedRectangle(cornerRadius: 12)
                            .strokeBorder(Color.accentColor.opacity(0.3), lineWidth: 1)
                        : nil
                )
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: - Shared Sub-views

    private var triggerIcon: some View {
        Image(systemName: triggerSymbol)
            .foregroundStyle(triggerColor)
            .frame(width: 16, height: 16)
    }

    private var triggerSymbol: String {
        switch item.triggerType {
        case "mention":               return "at"
        case "dm":                    return "envelope"
        case "thread_reply":          return "bubble.left.and.bubble.right"
        case "reaction":              return "eye"
        case "jira_assigned":         return "ticket"
        case "jira_comment_mention":  return "bubble.left"
        case "calendar_invite":       return "calendar.badge.plus"
        case "calendar_time_change":  return "clock.arrow.circlepath"
        case "calendar_cancelled":    return "calendar.badge.minus"
        case "decision_made":         return "paperplane"
        case "briefing_ready":        return "sun.max"
        default:                      return "circle"
        }
    }

    private var triggerColor: Color {
        switch item.triggerType {
        case "mention":              return .blue
        case "dm":                   return .green
        case "thread_reply":         return .purple
        case "reaction":             return .yellow
        case "jira_assigned",
             "jira_comment_mention": return .orange
        case "calendar_invite",
             "calendar_time_change",
             "calendar_cancelled":   return .teal
        case "decision_made":        return .indigo
        case "briefing_ready":       return .yellow
        default:                     return .secondary
        }
    }

    private var priorityDot: some View {
        Circle()
            .fill(priorityColor)
            .frame(width: 8, height: 8)
    }

    private var priorityColor: Color {
        switch item.priority {
        case "high":   return .red
        case "medium": return .orange
        default:       return .gray
        }
    }

    /// Display name for the sender — resolved via `senderName` injection when available.
    private var senderDisplay: String {
        if let name = senderName, !name.isEmpty { return name }
        if let resolved = userNames[item.senderUserID], !resolved.isEmpty { return resolved }
        return item.senderUserID.isEmpty ? "Unknown" : item.senderUserID
    }

    /// Snippet rendered as AttributedString: resolves `<@U…>` mentions to display names via
    /// `userNames`, autolinks bare URLs, and preserves inline markdown.
    private var snippetAttributed: AttributedString {
        SlackTextParser.toAttributedString(item.snippet, userNames: userNames)
    }

    // MARK: - Action Bar

    private var actionBar: some View {
        HStack(spacing: 8) {
            if item.itemClass == .actionable {
                Menu("Snooze") {
                    Button("1 hour")        { onSnooze(.oneHour) }
                    Button("Till tomorrow") { onSnooze(.tillTomorrow) }
                    Button("Till Monday")   { onSnooze(.tillMonday) }
                }
                Button("Dismiss", role: .destructive, action: onDismiss)
                if !item.hasLinkedTarget {
                    Button("Create Task", action: onCreateTask)
                }
            }
            Spacer()
            feedbackButtons
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
    }

    // MARK: - Feedback Buttons

    private var feedbackButtons: some View {
        HStack(spacing: 2) {
            Button {
                onFeedback(1, "")
            } label: {
                Image(systemName: "hand.thumbsup")
            }
            .buttonStyle(.plain)

            Button {
                onFeedback(-1, "")
            } label: {
                Image(systemName: "hand.thumbsdown")
            }
            .buttonStyle(.plain)
        }
        .foregroundStyle(.secondary)
    }
}
