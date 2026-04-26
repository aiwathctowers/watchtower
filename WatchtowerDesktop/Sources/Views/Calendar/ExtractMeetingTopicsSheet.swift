import SwiftUI

/// Paste-and-extract sheet for Discussion Topics. The user pastes a raw blob
/// (recap, rambling status, markdown), Watchtower's AI splits it into atomic
/// topics, and the user ticks the ones to add as `meeting_notes(type=question)`.
struct ExtractMeetingTopicsSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    let eventID: String
    var existingTopicSortOrderCeiling: Int = 0
    var onCreated: () -> Void = {}

    @State private var rawText: String = ""
    @State private var isExtracting = false
    @State private var result: MeetingTopicsExtractResult?
    @State private var selected: Set<Int> = []
    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if result == nil {
                pasteStep
            } else {
                previewStep
            }
            Divider()
            footer
        }
        .frame(width: 560, height: 560)
    }

    // MARK: - Header

    private var header: some View {
        HStack {
            Label("Extract topics", systemImage: "sparkles")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    // MARK: - Step 1: paste

    private var pasteStep: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Paste a recap, chat log, or rambling notes. The AI will split it into discrete discussion topics — you pick which to add.")
                .font(.callout)
                .foregroundStyle(.secondary)

            TextEditor(text: $rawText)
                .font(.callout)
                .padding(6)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.secondary.opacity(0.2), lineWidth: 1)
                )

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .padding()
    }

    // MARK: - Step 2: preview

    @ViewBuilder
    private var previewStep: some View {
        if let result {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("AI extracted \(result.topics.count) topic\(result.topics.count == 1 ? "" : "s")")
                        .font(.headline)
                    Spacer()
                    Button("Re-extract") {
                        self.result = nil
                        self.selected = []
                    }
                    .buttonStyle(.borderless)
                    .font(.caption)
                }

                if !result.notes.isEmpty {
                    Text(result.notes)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(6)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 6))
                }

                if result.topics.isEmpty {
                    emptyState
                } else {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(Array(result.topics.enumerated()), id: \.offset) { idx, topic in
                                topicRow(idx: idx, topic: topic)
                            }
                        }
                    }
                }

                if let errorMessage {
                    Text(errorMessage)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
            }
            .padding()
        }
    }

    private func topicRow(idx: Int, topic: MeetingExtractedTopic) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Toggle(
                "",
                isOn: Binding(
                    get: { selected.contains(idx) },
                    set: { on in
                        if on { selected.insert(idx) } else { selected.remove(idx) }
                    }
                )
            )
            .labelsHidden()
            .padding(.top, 1)

            Text(topic.text)
                .font(.callout)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)

            if !topic.priority.isEmpty {
                priorityBadge(topic.priority)
            }
        }
        .padding(8)
        .background(
            Color.secondary.opacity(0.04),
            in: RoundedRectangle(cornerRadius: 6)
        )
    }

    private func priorityBadge(_ priority: String) -> some View {
        let color: Color = {
            switch priority {
            case "high": return .red
            case "low": return .gray
            default: return .orange
            }
        }()
        return Text(priority.capitalized)
            .font(.caption2)
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.12), in: Capsule())
    }

    private var emptyState: some View {
        VStack(spacing: 6) {
            Image(systemName: "tray")
                .font(.title2)
                .foregroundStyle(.secondary)
            Text("AI returned no actionable topics.")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(40)
    }

    // MARK: - Footer

    private var footer: some View {
        HStack {
            Spacer()
            if result == nil {
                Button {
                    Task { await runExtract() }
                } label: {
                    if isExtracting {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Extracting…")
                        }
                    } else {
                        Label("Extract", systemImage: "sparkles")
                    }
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(isExtracting || rawText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            } else {
                Button("Add \(selected.count) selected") { applySelected() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(selected.isEmpty)
            }
        }
        .padding()
    }

    // MARK: - Extract

    private func runExtract() async {
        guard let runner = ProcessCLIRunner.makeDefault() else {
            errorMessage = "watchtower CLI not found in PATH"
            return
        }
        isExtracting = true
        errorMessage = nil
        defer { isExtracting = false }
        do {
            let service = MeetingTopicsExtractService(runner: runner)
            let res = try await service.extract(text: rawText, eventID: eventID)
            result = res
            selected = Set(res.topics.indices)
        } catch {
            errorMessage = "Extract failed: \(error.localizedDescription)"
        }
    }

    // MARK: - Persist

    private func applySelected() {
        guard let result else { return }
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }
        let picked = selected.sorted().map { result.topics[$0] }
        do {
            try db.dbPool.write { dbConn in
                var next = existingTopicSortOrderCeiling + 1
                for topic in picked {
                    _ = try MeetingNoteQueries.create(
                        dbConn,
                        eventID: eventID,
                        type: .question,
                        text: topic.text,
                        sortOrder: next
                    )
                    next += 1
                }
            }
            onCreated()
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
