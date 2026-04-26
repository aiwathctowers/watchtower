import SwiftUI
import AppKit

struct MessageBubble: View {
    let message: ChatMessage

    @State private var didCopy = false

    var body: some View {
        switch message.role {
        case .user:
            HStack(spacing: 0) {
                Spacer(minLength: 40)
                Text(message.text)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.top, 10)
                    .padding(.bottom, message.text.isEmpty ? 10 : 28)
                    .foregroundStyle(.white)
                    .background(Color.accentColor, in: RoundedRectangle(cornerRadius: 16))
                    .overlay(alignment: .bottomTrailing) {
                        if !message.text.isEmpty {
                            cornerCopyButton(
                                background: Color.white.opacity(0.18),
                                tint: .white
                            )
                            .padding(6)
                        }
                    }
                    .contextMenu { copyMenuItem }
            }

        case .assistant:
            VStack(alignment: .leading, spacing: 6) {
                if message.isStreaming {
                    Text(message.text)
                        .textSelection(.enabled)
                } else {
                    MarkdownText(text: message.text)
                }
                if message.isStreaming {
                    StreamingIndicator()
                } else if !message.text.isEmpty {
                    cornerCopyButton(
                        background: Color.secondary.opacity(0.15),
                        tint: .secondary
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contextMenu { copyMenuItem }

        case .system:
            Text(message.text)
                .font(.caption)
                .foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 4)
        }
    }

    private func cornerCopyButton(background: Color, tint: Color) -> some View {
        Button(action: copyMessage) {
            Image(systemName: didCopy ? "checkmark" : "doc.on.doc")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(tint)
                .frame(width: 22, height: 22)
                .background(background, in: Circle())
                .contentTransition(.symbolEffect(.replace))
        }
        .buttonStyle(.plain)
        .help("Copy message")
    }

    @ViewBuilder
    private var copyMenuItem: some View {
        Button("Copy message") { copyMessage() }
    }

    private func copyMessage() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(message.text, forType: .string)
        withAnimation(.easeInOut(duration: 0.15)) { didCopy = true }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) {
            withAnimation(.easeInOut(duration: 0.15)) { didCopy = false }
        }
    }
}
