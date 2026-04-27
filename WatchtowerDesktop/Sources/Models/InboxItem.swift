import Foundation
import GRDB

// MARK: - ItemClass

enum ItemClass: String, Codable, Equatable {
    case actionable
    case ambient
}

// MARK: - InboxConversationMessage

/// A single message in the live conversation rendered inline inside an expanded `InboxCardView`.
/// Loaded on demand from the local `messages` table — represents the current state
/// of the thread/channel, not the snapshot frozen into `inbox_items.context` at detect time.
struct InboxConversationMessage: Identifiable, Equatable {
    let id: String       // message ts (channel-local)
    let author: String   // resolved display name
    let text: String     // cleaned via SlackTextParser
    let isTrigger: Bool  // matches the inbox item's message_ts
    let date: Date
}

// MARK: - InboxItem

struct InboxItem: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let channelID: String
    let messageTS: String
    let threadTS: String
    let senderUserID: String
    let triggerType: String     // "mention", "dm"
    let snippet: String
    let context: String
    let rawText: String
    let permalink: String
    let status: String          // "pending", "resolved", "dismissed", "snoozed"
    let priority: String        // "high", "medium", "low"
    let aiReason: String
    let resolvedReason: String
    let snoozeUntil: String
    let waitingUserIDs: String  // JSON array e.g. ["U123","U456"]
    let targetID: Int?          // nullable (renamed from task_id in migration v66)
    let readAt: String          // "" = unread
    // New fields (v67)
    let itemClassRaw: String    // column: item_class
    let pinned: Bool            // column: pinned (INTEGER 0/1)
    let archivedAt: Date?       // column: archived_at (nullable TEXT ISO8601)
    let archiveReason: String   // column: archive_reason
    let createdAt: String
    let updatedAt: String

    /// Typed item class derived from `item_class` column.
    var itemClass: ItemClass {
        ItemClass(rawValue: itemClassRaw) ?? .ambient
    }

    init(row: Row) {
        id = row["id"]
        channelID = row["channel_id"] ?? ""
        messageTS = row["message_ts"] ?? ""
        threadTS = row["thread_ts"] ?? ""
        senderUserID = row["sender_user_id"] ?? ""
        triggerType = row["trigger_type"] ?? "mention"
        snippet = row["snippet"] ?? ""
        context = row["context"] ?? ""
        rawText = row["raw_text"] ?? ""
        permalink = row["permalink"] ?? ""
        status = row["status"] ?? "pending"
        priority = row["priority"] ?? "medium"
        aiReason = row["ai_reason"] ?? ""
        resolvedReason = row["resolved_reason"] ?? ""
        snoozeUntil = row["snooze_until"] ?? ""
        waitingUserIDs = row["waiting_user_ids"] ?? ""
        targetID = row["target_id"] as Int?
        readAt = row["read_at"] ?? ""
        itemClassRaw = row["item_class"] ?? "ambient"
        pinned = (row["pinned"] as Int? ?? 0) != 0
        if let archivedAtStr = row["archived_at"] as String?, !archivedAtStr.isEmpty {
            archivedAt = Self.iso8601WithFractional.date(from: archivedAtStr)
                ?? Self.iso8601Standard.date(from: archivedAtStr)
        } else {
            archivedAt = nil
        }
        archiveReason = row["archive_reason"] ?? ""
        createdAt = row["created_at"] ?? ""
        updatedAt = row["updated_at"] ?? ""
    }

    // MARK: - Status Predicates

    var isPending: Bool { status == "pending" }
    var isUnread: Bool { readAt.isEmpty }
    var isResolved: Bool { status == "resolved" }
    var isDismissed: Bool { status == "dismissed" }
    var isSnoozed: Bool { status == "snoozed" }
    var isMention: Bool { triggerType == "mention" }
    var isDM: Bool { triggerType == "dm" }
    var hasLinkedTarget: Bool { targetID != nil }

    // MARK: - Priority

    var priorityOrder: Int {
        switch priority {
        case "high": return 0
        case "medium": return 1
        case "low": return 2
        default: return 1
        }
    }

    // MARK: - Display Helpers

    var isThreadReply: Bool { triggerType == "thread_reply" }
    var isReaction: Bool { triggerType == "reaction" }

    var triggerIcon: String {
        switch triggerType {
        case "mention": return "at"
        case "dm": return "envelope"
        case "thread_reply": return "arrowshape.turn.up.left"
        case "reaction": return "hand.thumbsup"
        default: return "tray"
        }
    }

    var statusIcon: String {
        switch status {
        case "pending": return "tray.full"
        case "resolved": return "checkmark.circle.fill"
        case "dismissed": return "xmark.circle"
        case "snoozed": return "moon.circle"
        default: return "tray.full"
        }
    }

    var priorityColor: String {
        switch priority {
        case "high": return "red"
        case "medium": return "orange"
        case "low": return "secondary"
        default: return "orange"
        }
    }

    // MARK: - Waiting Users

    /// Parsed list of user IDs waiting for response.
    var decodedWaitingUserIDs: [String] {
        guard !waitingUserIDs.isEmpty,
              let data = waitingUserIDs.data(using: .utf8),
              let ids = try? JSONDecoder().decode([String].self, from: data) else {
            // Fallback: just the sender
            return senderUserID.isEmpty ? [] : [senderUserID]
        }
        return ids
    }

    // MARK: - Date Helpers

    private static let iso8601WithFractional: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static let iso8601Standard: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    var createdDate: Date {
        if let date = Self.iso8601WithFractional.date(from: createdAt) { return date }
        return Self.iso8601Standard.date(from: createdAt) ?? Date()
    }

    /// Date of the original Slack message (parsed from message_ts epoch).
    var messageDate: Date {
        guard let ts = Double(messageTS.components(separatedBy: ".").first ?? messageTS) else {
            return createdDate
        }
        return Date(timeIntervalSince1970: ts)
    }
}
