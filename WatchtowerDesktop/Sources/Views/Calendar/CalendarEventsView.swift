import SwiftUI

struct CalendarEventsView: View {
    @Environment(AppState.self) private var appState
    @State private var meetingPrepVM = MeetingPrepViewModel()
    @State private var selectedEventID: String?
    @State private var showMeetingPrep = false
    @State private var googleAuth = GoogleAuthService()

    var body: some View {
        Group {
            if googleAuth.isConnected, let calVM = appState.calendarViewModel {
                eventsList(calVM)
            } else {
                notConnectedView
            }
        }
    }

    private func eventsList(_ vm: CalendarViewModel) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                todaySection(vm.todayEvents)
                tomorrowSection(vm.tomorrowEvents)

                if vm.todayEvents.isEmpty && vm.tomorrowEvents.isEmpty {
                    emptyState
                }
            }
            .padding()
        }
        .sheet(isPresented: $showMeetingPrep) {
            if let eventID = selectedEventID {
                MeetingPrepView(eventID: eventID)
            }
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

    // MARK: - Today

    @ViewBuilder
    private func todaySection(_ events: [CalendarEvent]) -> some View {
        if !events.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Today")
                    .font(.headline)

                ForEach(events) { event in
                    eventRow(event)
                }
            }
        }
    }

    // MARK: - Tomorrow

    @ViewBuilder
    private func tomorrowSection(_ events: [CalendarEvent]) -> some View {
        if !events.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Tomorrow")
                    .font(.headline)
                    .foregroundStyle(.secondary)

                ForEach(events) { event in
                    eventRow(event)
                }
            }
        }
    }

    private func eventRow(_ event: CalendarEvent) -> some View {
        HStack {
            CalendarEventRow(event: event)
            Button {
                selectedEventID = event.id
                showMeetingPrep = true
            } label: {
                Label("Prepare", systemImage: "doc.text.magnifyingglass")
                    .font(.caption)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.blue)
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
