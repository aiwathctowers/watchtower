import SwiftUI

/// Detail view for a single decision, showing context from the parent digest.
struct DecisionDetailView: View {
    let entry: DecisionEntry
    let viewModel: DigestViewModel
    @State private var markingRead = false
    @State private var markedRead = false
    @State private var markReadError: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                // Header
                HStack(alignment: .center) {
                    EditableImportanceBadge(
                        importance: entry.effectiveImportance,
                        isCorrected: entry.correctedImportance != nil
                    ) { newImportance in
                        viewModel.setDecisionImportance(entry, newImportance: newImportance)
                    }

                    if let name = entry.channelName {
                        Text("#\(name)")
                            .font(.title3)
                            .fontWeight(.semibold)
                    } else {
                        Text("Cross-channel")
                            .font(.title3)
                            .fontWeight(.semibold)
                    }

                    Spacer()

                    Text(TimeFormatting.shortDateTime(from: entry.date))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Decision text with left bar
                HStack(alignment: .top, spacing: 0) {
                    RoundedRectangle(cornerRadius: 2)
                        .fill(importanceColor)
                        .frame(width: 3)

                    VStack(alignment: .leading, spacing: 8) {
                        Text(entry.decision.text)
                            .font(.body)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)

                        HStack {
                            if let by = entry.decision.by, !by.isEmpty {
                                Label(by, systemImage: "person")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }

                            Spacer()

                            if let ts = entry.messageTS,
                               let url = viewModel.slackMessageURL(channelID: entry.channelID, messageTS: ts) {
                                Link(destination: url) {
                                    Label("View in Slack", systemImage: "arrow.up.right.square")
                                        .font(.caption)
                                }
                                .buttonStyle(.borderless)
                            }
                        }
                    }
                    .padding(.leading, 12)
                }

                Divider()

                // Context: parent digest
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        Text("From digest")
                            .font(.caption)
                            .foregroundStyle(.tertiary)

                        Text(entry.digestType.capitalized)
                            .font(.caption2)
                            .fontWeight(.semibold)
                            .foregroundStyle(typeColor)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(typeColor.opacity(0.12), in: Capsule())

                        Text("#\(entry.digestID)")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }

                    Text(entry.digestSummary)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }

                // Open channel in Slack + mark read
                if !entry.channelID.isEmpty {
                    HStack(spacing: 12) {
                        if let url = viewModel.slackChannelURL(channelID: entry.channelID) {
                            Link(destination: url) {
                                Label("Open channel in Slack", systemImage: "number")
                                    .font(.caption)
                            }
                            .buttonStyle(.borderless)
                        }

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

                        if let err = markReadError {
                            Text(err)
                                .font(.caption2)
                                .foregroundStyle(.red)
                        }
                    }
                }
            }
            .padding()
        }
    }

    private func markChannelRead() {
        markingRead = true
        markReadError = nil
        Task {
            do {
                try await SlackService.markRead(channelID: entry.channelID)
                markedRead = true
            } catch {
                markReadError = error.localizedDescription
            }
            markingRead = false
        }
    }

    private var importanceColor: Color {
        switch entry.effectiveImportance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    private var typeColor: Color {
        switch entry.digestType {
        case "channel": .blue
        case "daily": .purple
        case "weekly": .indigo
        default: .secondary
        }
    }
}

/// Colored badge showing decision importance (read-only).
struct ImportanceBadge: View {
    let importance: String

    private var color: Color {
        switch importance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    private var label: String {
        switch importance {
        case "high": "High"
        case "low": "Low"
        default: "Medium"
        }
    }

    var body: some View {
        HStack(spacing: 4) {
            Circle()
                .fill(color)
                .frame(width: 8, height: 8)
            Text(label)
                .font(.caption)
                .foregroundStyle(color)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(color.opacity(0.1), in: Capsule())
    }
}

/// Clickable importance badge with a picker menu to change importance.
struct EditableImportanceBadge: View {
    let importance: String
    let isCorrected: Bool
    let onChange: (String) -> Void

    private static let levels = ["high", "medium", "low"]

    private var color: Color {
        switch importance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    private var label: String {
        switch importance {
        case "high": "High"
        case "low": "Low"
        default: "Medium"
        }
    }

    private func labelFor(_ level: String) -> String {
        switch level {
        case "high": "High"
        case "low": "Low"
        default: "Medium"
        }
    }

    private func colorFor(_ level: String) -> Color {
        switch level {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    var body: some View {
        Menu {
            ForEach(Self.levels, id: \.self) { level in
                Button {
                    onChange(level)
                } label: {
                    HStack {
                        Circle()
                            .fill(colorFor(level))
                            .frame(width: 8, height: 8)
                        Text(labelFor(level))
                        if level == importance {
                            Image(systemName: "checkmark")
                        }
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Circle()
                    .fill(color)
                    .frame(width: 8, height: 8)
                Text(label)
                    .font(.caption)
                    .foregroundStyle(color)
                if isCorrected {
                    Image(systemName: "pencil.circle.fill")
                        .font(.caption2)
                        .foregroundStyle(color)
                }
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 8))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(color.opacity(0.1), in: Capsule())
        }
        .buttonStyle(.plain)
        .help(isCorrected ? "Importance changed (click to adjust)" : "Click to change importance")
    }
}
