import SwiftUI

struct MessageBubble: View {
    let message: ChatMessage

    var body: some View {
        switch message.role {
        case .user:
            HStack {
                Spacer(minLength: 40)
                Text(message.text)
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .foregroundStyle(.white)
                    .background(Color.accentColor, in: RoundedRectangle(cornerRadius: 16))
            }

        case .assistant:
            VStack(alignment: .leading, spacing: 4) {
                if message.isStreaming {
                    Text(message.text)
                        .textSelection(.enabled)
                } else {
                    MarkdownText(text: message.text)
                }
                if message.isStreaming {
                    StreamingIndicator()
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

        case .system:
            Text(message.text)
                .font(.caption)
                .foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 4)
        }
    }
}
