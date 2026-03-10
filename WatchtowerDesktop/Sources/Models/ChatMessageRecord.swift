import Foundation
import GRDB

struct ChatMessageRecord: FetchableRecord, Decodable, Identifiable {
    let id: Int64
    let conversationID: Int64
    let role: String
    let text: String
    let createdAt: Double

    enum CodingKeys: String, CodingKey {
        case id
        case conversationID = "conversation_id"
        case role
        case text
        case createdAt = "created_at"
    }

    func toChatMessage() -> ChatMessage {
        let msgRole: ChatMessage.Role = switch role {
        case "user": .user
        case "system": .system
        default: .assistant
        }
        return ChatMessage(
            id: UUID(),
            role: msgRole,
            text: text,
            timestamp: Date(timeIntervalSince1970: createdAt),
            isStreaming: false
        )
    }
}
