import SwiftUI

/// Detail view for a single decision, showing context from the parent digest.
struct DecisionDetailView: View {
    let entry: DecisionEntry
    let viewModel: DigestViewModel
    var onClose: (() -> Void)? = nil
    @Environment(AppState.self) private var appState
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
                        if let url = viewModel.slackChannelURL(channelID: entry.channelID) {
                            Link(destination: url) {
                                Text("#\(name)")
                                    .font(.title3)
                                    .fontWeight(.semibold)
                            }
                            .buttonStyle(.borderless)
                        } else {
                            Text("#\(name)")
                                .font(.title3)
                                .fontWeight(.semibold)
                        }
                    } else {
                        Text("Cross-channel")
                            .font(.title3)
                            .fontWeight(.semibold)
                    }

                    Spacer()

                    Text(TimeFormatting.shortDateTime(from: entry.date))
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

                        Spacer()

                        if let dbManager = appState.databaseManager {
                            FeedbackButtons(
                                entityType: "decision",
                                entityID: "\(entry.digestID):\(entry.decisionIdx)",
                                dbManager: dbManager
                            )
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

/// Clickable importance badge — single styled control with a popover picker.
struct EditableImportanceBadge: View {
    let importance: String
    let isCorrected: Bool
    let onChange: (String) -> Void

    private static let levels = ["high", "medium", "low"]
    @State private var showPicker = false

    private func colorFor(_ level: String) -> Color {
        switch level {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    private func labelFor(_ level: String) -> String {
        switch level {
        case "high": "High"
        case "low": "Low"
        default: "Medium"
        }
    }

    var body: some View {
        Button {
            showPicker.toggle()
        } label: {
            HStack(spacing: 4) {
                Circle()
                    .fill(colorFor(importance))
                    .frame(width: 8, height: 8)
                Text(labelFor(importance))
                    .font(.caption)
                    .foregroundStyle(colorFor(importance))
                if isCorrected {
                    Image(systemName: "pencil.circle.fill")
                        .font(.caption2)
                        .foregroundStyle(colorFor(importance))
                }
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 8))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(colorFor(importance).opacity(0.1), in: Capsule())
        }
        .buttonStyle(.plain)
        .popover(isPresented: $showPicker, arrowEdge: .bottom) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Self.levels, id: \.self) { level in
                    Button {
                        onChange(level)
                        showPicker = false
                    } label: {
                        HStack(spacing: 6) {
                            Circle()
                                .fill(colorFor(level))
                                .frame(width: 8, height: 8)
                            Text(labelFor(level))
                                .font(.callout)
                            Spacer()
                            if level == importance {
                                Image(systemName: "checkmark")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    if level != Self.levels.last {
                        Divider().padding(.horizontal, 6)
                    }
                }
            }
            .padding(.vertical, 4)
            .frame(width: 140)
        }
        .help(isCorrected ? "Importance changed (click to adjust)" : "Click to change importance")
    }
}
