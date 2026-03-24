import Foundation
import UserNotifications

/// FNV-1a hash for stable, collision-resistant notification identifiers.
private func fnv1aHash(_ string: String) -> UInt64 {
    var hash: UInt64 = 14_695_981_039_346_656_037
    for byte in string.utf8 {
        hash ^= UInt64(byte)
        hash &*= 1_099_511_628_211
    }
    return hash
}

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

        // Stable identifier using digestID + FNV-1a hash of text (collision-resistant).
        let stableHash = fnv1aHash(decision.text)
        let request = UNNotificationRequest(
            identifier: "decision-\(digestID)-\(stableHash)",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendTrackUpdateNotification(text: String, channelName: String, itemID: Int) {
        let content = UNMutableNotificationContent()
        content.title = "Update on track"
        content.body = "#\(channelName): \(String(text.prefix(200)))"
        content.sound = .default
        content.userInfo = ["type": "track_update", "trackId": itemID]

        let request = UNNotificationRequest(
            identifier: "track-update-\(itemID)-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendTrackNotification(text: String, channelName: String, priority: String) {
        let content = UNMutableNotificationContent()
        let prefix = priority == "high" ? "Urgent: " : ""
        content.title = "\(prefix)New track in #\(channelName)"
        content.body = String(text.prefix(200))
        content.sound = .default
        content.userInfo = ["type": "track"]

        let stableHash = fnv1aHash(text)
        let request = UNNotificationRequest(
            identifier: "track-\(stableHash)",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendTestNotification() {
        let content = UNMutableNotificationContent()
        content.title = "Watchtower"
        content.body = "Notifications are working!"
        content.sound = .default
        content.userInfo = ["type": "test"]

        let request = UNNotificationRequest(
            identifier: "test-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    func sendBriefingNotification(attentionCount: Int) {
        let content = UNMutableNotificationContent()
        content.title = "Morning Briefing Ready"
        content.body = attentionCount > 0
            ? "\(attentionCount) items need attention"
            : "Your daily briefing is ready"
        content.sound = .default
        content.userInfo = ["type": "briefing"]

        let request = UNNotificationRequest(
            identifier: "briefing-\(Int(Date().timeIntervalSince1970))",
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
