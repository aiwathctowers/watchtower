import SwiftUI

struct DayPlanTimelineView: View {
    let items: [DayPlanItem]
    let allDayEvents: [DayPlanItem]
    let calendarEventsByID: [String: CalendarEvent]
    let onToggle: (DayPlanItem) -> Void
    let onPrepare: (String) -> Void
    let onNavigate: (DayPlanItem) -> Void

    @State private var allDayExpanded = false
    @State private var expandedEventID: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !allDayEvents.isEmpty {
                allDayChip
                Divider()
                    .padding(.leading, 80)
            }

            ForEach(items) { item in
                row(item)
                Divider()
                    .padding(.leading, 80)
            }
        }
    }

    // MARK: - All-day chip

    private var allDayChip: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    allDayExpanded.toggle()
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: allDayExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption2)
                    Text("\(allDayEvents.count) all-day")
                        .font(.caption)
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)

            if allDayExpanded {
                ForEach(allDayEvents) { item in
                    HStack(spacing: 6) {
                        Circle()
                            .fill(.secondary.opacity(0.3))
                            .frame(width: 6, height: 6)
                        Text(item.title)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                        Spacer()
                    }
                    .padding(.leading, 32)
                    .padding(.trailing, 16)
                    .padding(.vertical, 2)
                }
                .padding(.bottom, 4)
            }
        }
    }

    // MARK: - Row dispatch

    @ViewBuilder
    private func row(_ item: DayPlanItem) -> some View {
        if item.sourceType == .calendar,
           let sid = item.sourceId,
           let event = calendarEventsByID[sid] {
            calendarRow(item: item, event: event)
        } else {
            regularRow(item)
        }
    }

    // MARK: - Calendar row (parity with Calendar page)

    private func calendarRow(item: DayPlanItem, event: CalendarEvent) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 6) {
                CalendarEventRow(event: event)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        withAnimation(.easeInOut(duration: 0.15)) {
                            expandedEventID = expandedEventID == event.id ? nil : event.id
                        }
                    }

                Button {
                    onPrepare(event.id)
                } label: {
                    Label("Prepare", systemImage: "doc.text.magnifyingglass")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.blue)
                .padding(.top, 8)
            }

            if expandedEventID == event.id {
                eventDetail(event)
                    .padding(.leading, 96)
                    .padding(.trailing, 12)
                    .padding(.bottom, 6)
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
    }

    private func eventDetail(_ event: CalendarEvent) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if !event.location.isEmpty {
                Label(event.location, systemImage: "mappin")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !event.organizerEmail.isEmpty {
                Label(event.organizerEmail, systemImage: "person")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            let attendees = event.parsedAttendees
            if !attendees.isEmpty {
                Label("\(attendees.count) attendees", systemImage: "person.2")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                ForEach(attendees) { a in
                    HStack(spacing: 4) {
                        Image(systemName: responseIcon(a.responseStatus))
                            .font(.caption2)
                            .foregroundStyle(responseColor(a.responseStatus))
                        Text(a.displayName.isEmpty ? a.email : a.displayName)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.leading, 20)
                }
            }
            let plain = event.plainDescription
            if !plain.isEmpty {
                Text(plain)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(4)
                    .padding(.top, 2)
            }
            if !event.htmlLink.isEmpty {
                Link(destination: URL(string: event.htmlLink) ?? URL(string: "https://calendar.google.com")!) {
                    Label("Open in Google Calendar", systemImage: "arrow.up.right.square")
                        .font(.caption)
                }
                .padding(.top, 2)
            }
        }
    }

    private func responseIcon(_ status: String) -> String {
        switch status {
        case "accepted": return "checkmark.circle.fill"
        case "tentative": return "questionmark.circle"
        case "declined": return "xmark.circle"
        default: return "circle"
        }
    }

    private func responseColor(_ status: String) -> Color {
        switch status {
        case "accepted": return .green
        case "tentative": return .orange
        case "declined": return .red
        default: return .secondary
        }
    }

    // MARK: - Non-calendar row (original timeline look)

    private func regularRow(_ item: DayPlanItem) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Text(item.timeRange ?? "")
                .font(.caption)
                .fontDesign(.monospaced)
                .foregroundStyle(.secondary)
                .frame(width: 70, alignment: .trailing)
                .padding(.top, 2)

            RoundedRectangle(cornerRadius: 2)
                .fill(sourceColor(item.sourceType))
                .frame(width: 3)
                .padding(.vertical, 2)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.title)
                        .font(.callout)
                        .fontWeight(.semibold)
                        .strikethrough(item.isDone, color: .secondary)
                        .foregroundStyle(item.isDone ? .secondary : .primary)
                        .lineLimit(2)

                    if let badge = sourceBadgeLabel(item) {
                        Button {
                            onNavigate(item)
                        } label: {
                            HStack(spacing: 3) {
                                Image(systemName: sourceBadgeIcon(item.sourceType))
                                    .font(.caption2)
                                Text(badge)
                                    .font(.caption2)
                                    .fontWeight(.medium)
                            }
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(
                                sourceColor(item.sourceType).opacity(0.15),
                                in: Capsule()
                            )
                            .foregroundStyle(sourceColor(item.sourceType))
                        }
                        .buttonStyle(.plain)
                    }
                }

                if let rationale = item.rationale, !rationale.isEmpty {
                    Text(rationale)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            .padding(.vertical, 6)

            Spacer()

            if !item.isReadOnly {
                Button(action: { onToggle(item) }) {
                    Image(systemName: item.isDone ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(item.isDone ? .green : .secondary)
                }
                .buttonStyle(.plain)
                .padding(.top, 4)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 4)
    }

    private func sourceColor(_ sourceType: DayPlanItemSourceType) -> Color {
        switch sourceType {
        case .calendar:          return .gray
        case .focus:             return .blue
        case .task:              return .green
        case .jira:              return .purple
        case .briefingAttention: return .yellow
        case .manual:            return .orange
        }
    }

    private func sourceBadgeIcon(_ sourceType: DayPlanItemSourceType) -> String {
        switch sourceType {
        case .calendar:          return "calendar"
        case .focus:             return "brain.head.profile"
        case .task:              return "checkmark.circle"
        case .jira:              return "ticket"
        case .briefingAttention: return "sun.max"
        case .manual:            return "pin.fill"
        }
    }

    private func sourceBadgeLabel(_ item: DayPlanItem) -> String? {
        switch item.sourceType {
        case .task:
            guard let sid = item.sourceId else { return "task" }
            return "task:\(sid)"
        case .jira:
            return item.sourceId
        case .briefingAttention:
            return "briefing"
        case .focus:
            return "focus"
        case .manual, .calendar:
            return nil
        }
    }
}
