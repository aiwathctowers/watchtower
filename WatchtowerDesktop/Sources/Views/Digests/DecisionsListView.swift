import SwiftUI

/// Flat list of all decisions across digests, deduplicated and sorted by date.
struct DecisionsListView: View {
    let viewModel: DigestViewModel
    @Binding var selectedEntryID: String?
    @Binding var expandedEntryIDs: Set<String>
    @Binding var searchText: String
    @Binding var showAll: Bool
    @Binding var isSelectMode: Bool
    @Binding var checkedIDs: Set<String>

    private var filteredEntries: [DecisionEntry] {
        var items = viewModel.decisionEntries
        if !showAll {
            items = items.filter { !$0.isRead }
        }
        if !searchText.isEmpty {
            let q = searchText.lowercased()
            items = items.filter { entry in
                if entry.decision.text.lowercased().contains(q) { return true }
                if let name = entry.channelName, name.lowercased().contains(q) { return true }
                if let by = entry.decision.by, by.lowercased().contains(q) { return true }
                return false
            }
        }
        return items
    }

    private func toggleExpanded(_ id: String) {
        withAnimation(.easeInOut(duration: 0.2)) {
            if expandedEntryIDs.contains(id) {
                expandedEntryIDs.remove(id)
            } else {
                expandedEntryIDs.insert(id)
            }
        }
    }

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(filteredEntries) { entry in
                    decisionListItem(entry)
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func decisionListItem(_ entry: DecisionEntry) -> some View {
        let isChecked = checkedIDs.contains(entry.id)
        let isSelected = selectedEntryID == entry.id && !isSelectMode
        let bgColor: Color = isSelected
            ? Color.accentColor.opacity(0.15)
            : isChecked ? Color.accentColor.opacity(0.08) : Color.clear

        return HStack(spacing: 0) {
            if isSelectMode {
                Button {
                    toggleChecked(entry.id)
                } label: {
                    Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
                        .font(.body)
                }
                .buttonStyle(.borderless)
                .padding(.leading, 8)
            }

            decisionRow(entry)
                .contentShape(Rectangle())
                .onTapGesture {
                    if isSelectMode {
                        toggleChecked(entry.id)
                    } else {
                        selectedEntryID = entry.id
                    }
                }
        }
        .padding(.horizontal, isSelectMode ? 4 : 10)
        .padding(.vertical, 4)
        .background(bgColor, in: RoundedRectangle(cornerRadius: 6))
        .padding(.horizontal, 4)
    }

    private func toggleChecked(_ id: String) {
        if checkedIDs.contains(id) {
            checkedIDs.remove(id)
        } else {
            checkedIDs.insert(id)
        }
    }

    private func decisionRow(_ entry: DecisionEntry) -> some View {
        let expanded = expandedEntryIDs.contains(entry.id)
        return HStack(alignment: .top, spacing: 0) {
            // Unread indicator
            if !entry.isRead {
                Circle()
                    .fill(.blue)
                    .frame(width: 8, height: 8)
                    .padding(.top, 6)
                    .padding(.trailing, 4)
            }

            // Left importance bar
            RoundedRectangle(cornerRadius: 2)
                .fill(importanceColor(entry.effectiveImportance))
                .frame(width: 3)
                .padding(.vertical, 2)

            VStack(alignment: .leading, spacing: 4) {
                // Channel + importance badge + expand chevron
                HStack {
                    Text(entry.channelName.map { "#\($0)" } ?? "Cross-channel")
                        .font(.caption)
                        .fontWeight(entry.isRead ? .regular : .medium)
                        .foregroundStyle(.secondary)

                    Spacer()

                    EditableImportanceBadge(
                        importance: entry.effectiveImportance,
                        isCorrected: entry.correctedImportance != nil
                    ) { newImportance in
                        viewModel.setDecisionImportance(entry, newImportance: newImportance)
                    }

                    Button {
                        toggleExpanded(entry.id)
                    } label: {
                        Image(systemName: expanded ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .frame(width: 16, height: 16)
                    }
                    .buttonStyle(.borderless)
                }

                // Decision text
                Text(entry.decision.text)
                    .font(.subheadline)
                    .lineLimit(expanded ? nil : 3)

                // Author + date
                HStack {
                    if let by = entry.decision.by, !by.isEmpty {
                        Label(by, systemImage: "person")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }

                    Spacer()

                    Text(TimeFormatting.shortDateTime(from: entry.date))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                // Expanded content
                if expanded {
                    decisionExpandedContent(entry)
                }
            }
            .padding(.leading, 8)
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private func decisionExpandedContent(_ entry: DecisionEntry) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Divider()

            // Parent digest context
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text("From digest")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                    Text(entry.digestType.capitalized)
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(digestTypeColor(entry.digestType))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(digestTypeColor(entry.digestType).opacity(0.12), in: Capsule())
                }
                Text(entry.digestSummary)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }

            // Action buttons
            HStack(spacing: 12) {
                if let ts = entry.messageTS,
                   let url = viewModel.slackMessageURL(channelID: entry.channelID, messageTS: ts) {
                    Link(destination: url) {
                        Label("Slack", systemImage: "arrow.up.right.square")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)
                }

                Button {
                    selectedEntryID = entry.id
                } label: {
                    Label("Open details", systemImage: "arrow.right.circle")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
            }
        }
        .padding(.top, 2)
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    private func digestTypeColor(_ type: String) -> Color {
        switch type {
        case "channel": .blue
        case "daily": .purple
        case "weekly": .indigo
        default: .secondary
        }
    }

    private func importanceColor(_ importance: String) -> Color {
        switch importance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

}
