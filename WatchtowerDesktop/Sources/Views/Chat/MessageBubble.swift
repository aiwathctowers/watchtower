import SwiftUI

struct MessageBubble: View {
    let message: ChatMessage

    var body: some View {
        HStack {
            if message.role == .user { Spacer(minLength: 60) }

            VStack(alignment: message.role == .user ? .trailing : .leading, spacing: 4) {
                if message.role == .assistant {
                    MarkdownText(text: message.text)
                } else {
                    Text(message.text)
                }

                if message.isStreaming {
                    StreamingIndicator()
                }
            }
            .padding(12)
            .background(backgroundColor, in: RoundedRectangle(cornerRadius: 12))
            .foregroundStyle(message.role == .user ? .white : .primary)

            if message.role == .assistant { Spacer(minLength: 60) }
        }
    }

    private var backgroundColor: Color {
        switch message.role {
        case .user: .accentColor
        case .assistant: Color(.controlBackgroundColor)
        case .system: Color(.controlBackgroundColor).opacity(0.5)
        }
    }
}
