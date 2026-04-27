import SwiftUI

/// Paste-and-process sheet for AI-generated meeting recap. The user pastes
/// raw notes (transcript fragment, hand-written summary, scratchpad), Watchtower's
/// AI returns a structured summary which is persisted by the CLI directly.
struct GenerateRecapSheet: View {
    @Environment(\.dismiss) private var dismiss

    let eventID: String
    var prefilledText: String = ""
    var onCompleted: () -> Void = {}

    @State private var text: String = ""
    @State private var isGenerating = false
    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            editor
            Divider()
            footer
        }
        .frame(width: 580, height: 580)
        .onAppear {
            if text.isEmpty {
                text = prefilledText
            }
        }
    }

    private var header: some View {
        HStack {
            Label("AI Recap", systemImage: "sparkles")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    private var editor: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Paste a recap, transcript fragment, or rough notes. The AI will produce a structured summary (decisions, action items, open questions).")
                .font(.callout)
                .foregroundStyle(.secondary)

            TextEditor(text: $text)
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

    private var footer: some View {
        HStack {
            Spacer()
            Button {
                Task { await runGenerate() }
            } label: {
                if isGenerating {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.small)
                        Text("Generating…")
                    }
                } else {
                    Label(prefilledText.isEmpty ? "Generate" : "Re-generate",
                          systemImage: "sparkles")
                }
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
            .disabled(isGenerating || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }

    private func runGenerate() async {
        guard let runner = ProcessCLIRunner.makeDefault() else {
            errorMessage = "watchtower CLI not found in PATH"
            return
        }
        isGenerating = true
        errorMessage = nil
        defer { isGenerating = false }

        let svc = MeetingRecapService(runner: runner)
        do {
            try await svc.generate(eventID: eventID, text: text)
            onCompleted()
            dismiss()
        } catch {
            errorMessage = "Generation failed: \(error.localizedDescription)"
        }
    }
}
