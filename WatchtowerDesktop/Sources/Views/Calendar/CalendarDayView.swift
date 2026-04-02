import SwiftUI
import GRDB

struct CalendarDayView: View {
    let events: [CalendarEvent]
    let title: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.headline)

            if events.isEmpty {
                Text("No events")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(8)
            } else {
                ForEach(events) { event in
                    CalendarEventRow(event: event)
                }
            }
        }
    }
}
