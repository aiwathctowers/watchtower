import SwiftUI

struct DayPlanItemRow: View {
    let item: DayPlanItem
    let onToggle: () -> Void
    let onDelete: () -> Void
    let onNavigateSource: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            // Checkbox toggle
            Button(action: onToggle) {
                Image(systemName: item.isDone ? "checkmark.circle.fill" : "circle")
                    .font(.title3)
                    .foregroundStyle(item.isDone ? .green : .secondary)
            }
            .buttonStyle(.plain)

            // Title + source badge + rationale
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.title)
                        .font(.callout)
                        .fontWeight(.medium)
                        .strikethrough(item.isDone, color: .secondary)
                        .foregroundStyle(item.isDone ? .secondary : .primary)
                        .lineLimit(2)

                    if item.isManual {
                        Image(systemName: "pin.fill")
                            .font(.caption2)
                            .foregroundStyle(.orange)
                    }

                    if let label = sourceBadgeLabel {
                        Button(action: onNavigateSource) {
                            HStack(spacing: 3) {
                                Image(systemName: sourceBadgeIcon)
                                    .font(.caption2)
                                Text(label)
                                    .font(.caption2)
                                    .fontWeight(.medium)
                            }
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(sourceColor.opacity(0.15), in: Capsule())
                            .foregroundStyle(sourceColor)
                        }
                        .buttonStyle(.plain)
                        .help("Open source: \(label)")
                    }
                }

                if let rationale = item.rationale, !rationale.isEmpty {
                    Text(rationale)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }

            Spacer()

            // Priority badge
            if let priority = item.priority {
                priorityBadge(priority)
            }

            // Context menu trigger (3-dot)
            Menu {
                if item.isDone {
                    Button("Mark Pending") { onToggle() }
                } else {
                    Button("Mark Done") { onToggle() }
                }
                if sourceBadgeLabel != nil {
                    Divider()
                    Button("Open source") { onNavigateSource() }
                }
                Divider()
                Button("Delete", role: .destructive) { onDelete() }
                    .disabled(item.isReadOnly)
            } label: {
                Image(systemName: "ellipsis.circle")
                    .foregroundStyle(.secondary)
                    .font(.body)
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .contentShape(Rectangle())
    }

    // MARK: - Source badge

    private var sourceBadgeLabel: String? {
        switch item.sourceType {
        case .task:
            guard let sid = item.sourceId, !sid.isEmpty else { return "task" }
            return "task:\(sid)"
        case .jira:
            return item.sourceId
        case .briefingAttention:
            return "briefing"
        case .focus:
            return "focus"
        case .manual, .calendar:
            return nil
        }
    }

    private var sourceBadgeIcon: String {
        switch item.sourceType {
        case .calendar:          return "calendar"
        case .focus:             return "brain.head.profile"
        case .task:              return "checkmark.circle"
        case .jira:              return "ticket"
        case .briefingAttention: return "sun.max"
        case .manual:            return "pin.fill"
        }
    }

    private var sourceColor: Color {
        switch item.sourceType {
        case .calendar:          return .gray
        case .focus:             return .blue
        case .task:              return .green
        case .jira:              return .purple
        case .briefingAttention: return .yellow
        case .manual:            return .orange
        }
    }

    // MARK: - Priority

    private func priorityBadge(_ priority: String) -> some View {
        Text(priority.capitalized)
            .font(.caption2)
            .fontWeight(.medium)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(priorityColor(priority).opacity(0.15), in: Capsule())
            .foregroundStyle(priorityColor(priority))
    }

    private func priorityColor(_ priority: String) -> Color {
        switch priority {
        case "high":   return .red
        case "medium": return .orange
        case "low":    return .blue
        default:       return .secondary
        }
    }
}
