import SwiftUI

struct CalendarEventsView: View {
    @Environment(AppState.self) private var appState
    @State private var meetingPrepVM = MeetingPrepViewModel()
    @State private var selectedEventID: String?
    @State private var googleAuth = GoogleAuthService()
    @State private var expandedAllDayDates: Set<Date> = []
    @State private var expandedEventID: String?
    @State private var userNotes: String = ""

    var body: some View {
        Group {
            if googleAuth.isConnected, let calVM = appState.calendarViewModel {
                HStack(spacing: 0) {
                    eventsList(calVM)
                        .frame(minWidth: 300, idealWidth: 350)

                    if let eventID = selectedEventID {
                        Divider()
                        MeetingPrepDetailView(
                            eventID: eventID,
                            viewModel: meetingPrepVM,
                            userNotes: $userNotes,
                            onClose: { selectedEventID = nil }
                        )
                        .id(eventID)
                        .frame(minWidth: 400, idealWidth: 500)
                        .transition(
                            .move(edge: .trailing).combined(with: .opacity)
                        )
                    }
                }
                .animation(.easeInOut(duration: 0.25), value: selectedEventID)
                .onAppear { calVM.loadEvents() }
            } else {
                notConnectedView
            }
        }
    }

    private func eventsList(_ vm: CalendarViewModel) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header

                ForEach(vm.dailyEvents) { day in
                    daySection(day: day, isToday: day.label == "Today")
                }

                if vm.dailyEvents.isEmpty {
                    emptyState
                }
            }
            .padding()
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(spacing: 6) {
            Image(systemName: "calendar")
                .foregroundStyle(.blue)
            Text("Calendar")
                .font(.title2)
                .fontWeight(.bold)
        }
    }

    // MARK: - Day Section

    private func daySection(day: DayEvents, isToday: Bool) -> some View {
        let timed = day.events.filter { !$0.isAllDay }
        let allDay = day.events.filter { $0.isAllDay }

        return VStack(alignment: .leading, spacing: 8) {
            Text(day.label)
                .font(.headline)
                .foregroundStyle(isToday ? .primary : .secondary)

            ForEach(timed) { event in
                eventRow(event)
            }

            if !allDay.isEmpty {
                allDayChip(allDay, date: day.id)
            }
        }
    }

    // MARK: - All-Day Chip

    private func allDayChip(_ events: [CalendarEvent], date: Date) -> some View {
        let isExpanded = expandedAllDayDates.contains(date)

        return VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    if isExpanded {
                        expandedAllDayDates.remove(date)
                    } else {
                        expandedAllDayDates.insert(date)
                    }
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .font(.caption2)
                    Text("\(events.count) all-day")
                        .font(.caption)
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)

            if isExpanded {
                ForEach(events) { event in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Circle()
                                .fill(.secondary.opacity(0.3))
                                .frame(width: 6, height: 6)
                            Text(event.title)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        .padding(.leading, 16)
                        .contentShape(Rectangle())
                        .onTapGesture {
                            withAnimation(.easeInOut(duration: 0.15)) {
                                expandedEventID = expandedEventID == event.id ? nil : event.id
                            }
                        }

                        if expandedEventID == event.id {
                            eventDetail(event)
                                .padding(.leading, 28)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Event Row

    private func eventRow(_ event: CalendarEvent) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                CalendarEventRow(event: event)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        withAnimation(.easeInOut(duration: 0.15)) {
                            expandedEventID = expandedEventID == event.id ? nil : event.id
                        }
                    }
                Button {
                    if selectedEventID == event.id {
                        selectedEventID = nil
                    } else {
                        selectedEventID = event.id
                        meetingPrepVM.generate(eventID: event.id)
                    }
                } label: {
                    Label("Prepare", systemImage: "doc.text.magnifyingglass")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(selectedEventID == event.id ? Color.accentColor : .blue)
            }

            if expandedEventID == event.id {
                eventDetail(event)
                    .padding(.leading, 88)
            }
        }
    }

    // MARK: - Event Detail

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
        .padding(.vertical, 4)
    }

    // MARK: - Helpers

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

    // MARK: - Empty

    private var emptyState: some View {
        VStack(spacing: 8) {
            Image(systemName: "calendar.badge.checkmark")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
            Text("No upcoming events")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 40)
    }

    private var notConnectedView: some View {
        VStack(spacing: 12) {
            Image(systemName: "calendar.badge.exclamationmark")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
            Text("Google Calendar not connected")
                .font(.headline)
            Text("Connect your Google Calendar to see upcoming meetings and prepare for them.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal)

            if googleAuth.isAuthenticating {
                ProgressView("Connecting...")
                    .padding(.top, 4)
                Button("Cancel") {
                    googleAuth.cancelConnect()
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            } else {
                Button {
                    googleAuth.connect()
                } label: {
                    Label("Connect Google Calendar", systemImage: "calendar.badge.plus")
                }
                .buttonStyle(.borderedProminent)
                .padding(.top, 4)
            }

            if let err = googleAuth.error {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
