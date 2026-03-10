import Foundation
import UserNotifications

final class NotificationService: Sendable {
    static let shared = NotificationService()

    func requestPermission() async -> Bool {
        do {
            return try await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert, .sound, .badge])
        } catch {
            return false
        }
    }

    func sendDecisionNotification(decision: Decision, channelName: String, digestID: Int) {
        let content = UNMutableNotificationContent()
        content.title = "New decision in #\(channelName)"
        content.body = decision.text
        content.sound = .default
        content.userInfo = ["digestId": digestID, "type": "decision"]

        // M7: stable identifier using digestID + decision hash (not hashValue which varies per launch)
        let stableHash = decision.text.utf8.reduce(0) { $0 &+ Int($1) }
        let request = UNNotificationRequest(
            identifier: "decision-\(digestID)-\(stableHash)",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendActionItemUpdateNotification(text: String, channelName: String, itemID: Int) {
        let content = UNMutableNotificationContent()
        content.title = "Update on action item"
        content.body = "#\(channelName): \(String(text.prefix(200)))"
        content.sound = .default
        content.userInfo = ["type": "action_item_update", "actionItemId": itemID]

        let request = UNNotificationRequest(
            identifier: "action-update-\(itemID)-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendActionItemNotification(text: String, channelName: String, priority: String) {
        let content = UNMutableNotificationContent()
        let prefix = priority == "high" ? "Urgent: " : ""
        content.title = "\(prefix)Action needed in \(channelName)"
        content.body = String(text.prefix(200))
        content.sound = .default
        content.userInfo = ["type": "action_item"]

        let stableHash = text.utf8.reduce(0) { $0 &+ Int($1) }
        let request = UNNotificationRequest(
            identifier: "action-item-\(stableHash)",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendDailySummaryNotification(summary: String) {
        let content = UNMutableNotificationContent()
        content.title = "Daily summary ready"
        content.body = String(summary.prefix(200))
        content.sound = .default
        content.userInfo = ["type": "daily_summary"]

        let request = UNNotificationRequest(
            identifier: "daily-summary-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }
}
