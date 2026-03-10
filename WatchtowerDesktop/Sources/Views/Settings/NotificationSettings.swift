import SwiftUI

struct NotificationSettings: View {
    @AppStorage("notifyDecisions") private var notifyDecisions = true
    @AppStorage("notifyDailySummary") private var notifyDailySummary = true
    @AppStorage("quietHoursEnabled") private var quietHoursEnabled = false

    var body: some View {
        Form {
            Section("Notification Types") {
                Toggle("Decision notifications", isOn: $notifyDecisions)
                Toggle("Daily summary notifications", isOn: $notifyDailySummary)
            }

            Section("Quiet Hours") {
                Toggle("Enable quiet hours", isOn: $quietHoursEnabled)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}
