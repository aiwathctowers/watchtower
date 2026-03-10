import SwiftUI

/// Flat list of all decisions across digests, deduplicated and sorted by date.
struct DecisionsListView: View {
    let viewModel: DigestViewModel
    @Binding var selectedEntryID: String?
    @Binding var searchText: String
    @Binding var showAll: Bool

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

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(filteredEntries) { entry in
                    decisionRow(entry)
                        .contentShape(Rectangle())
                        .onTapGesture { selectedEntryID = entry.id }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(
                            selectedEntryID == entry.id
                                ? Color.accentColor.opacity(0.15)
                                : Color.clear,
                            in: RoundedRectangle(cornerRadius: 6)
                        )
                        .padding(.horizontal, 4)
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func decisionRow(_ entry: DecisionEntry) -> some View {
        HStack(alignment: .top, spacing: 0) {
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
                .fill(importanceColor(entry.decision.resolvedImportance))
                .frame(width: 3)
                .padding(.vertical, 2)

            VStack(alignment: .leading, spacing: 4) {
                // Channel + importance badge
                HStack {
                    Text(entry.channelName.map { "#\($0)" } ?? "Cross-channel")
                        .font(.caption)
                        .fontWeight(entry.isRead ? .regular : .medium)
                        .foregroundStyle(.secondary)

                    Spacer()

                    Text(importanceLabel(entry.decision.resolvedImportance))
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .foregroundStyle(importanceColor(entry.decision.resolvedImportance))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(importanceColor(entry.decision.resolvedImportance).opacity(0.12), in: Capsule())
                }

                // Decision text
                Text(entry.decision.text)
                    .font(.subheadline)
                    .lineLimit(3)

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
            }
            .padding(.leading, 8)
        }
        .padding(.vertical, 4)
    }

    private func importanceColor(_ importance: String) -> Color {
        switch importance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    private func importanceLabel(_ importance: String) -> String {
        switch importance {
        case "high": "High"
        case "low": "Low"
        default: "Medium"
        }
    }
}
