import SwiftUI

struct BriefingDetailView: View {
    let briefing: Briefing
    @Environment(AppState.self) private var appState
    @State private var showCreateTask = false
    @State private var taskPrefillText = ""
    @State private var taskPrefillIntent = ""
    @State private var calendarEvents: [CalendarEvent] = []
    @State private var jiraConnected = false
    @State private var jiraSiteURL: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                calendarSection
                attentionSection
                yourDaySection
                whatHappenedSection
                teamPulseSection
                coachingSection
            }
            .padding()
        }
        .sheet(isPresented: $showCreateTask) {
            CreateTaskSheet(
                prefillText: taskPrefillText,
                prefillIntent: taskPrefillIntent,
                prefillSourceType: "briefing",
                prefillSourceID: String(briefing.id)
            )
        }
        .onAppear {
            jiraConnected = JiraQueries.isConnected()
            jiraSiteURL = JiraConfigHelper.readSiteURL()
            loadCalendarEvents()
        }
    }

    // MARK: - Calendar

    @ViewBuilder
    private var calendarSection: some View {
        if !calendarEvents.isEmpty {
            sectionView(
                title: "Today's Schedule",
                icon: "calendar",
                iconColor: .blue
            ) {
                ForEach(calendarEvents) { event in
                    CalendarEventRow(event: event)
                }
            }
        }
    }

    private func loadCalendarEvents() {
        guard let db = appState.databaseManager else { return }
        calendarEvents = (try? db.dbPool.read { db in
            try CalendarQueries.fetchTodayEvents(db)
        }) ?? []
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Image(systemName: "sun.max.fill")
                    .foregroundStyle(.orange)
                Text(briefing.dateLabel)
                    .font(.title2)
                    .fontWeight(.bold)
            }

            if !briefing.role.isEmpty {
                Text(briefing.role)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            if briefing.isRead {
                Label("Read", systemImage: "checkmark.circle")
                    .font(.caption)
                    .foregroundStyle(.green)
            }
        }
    }

    // MARK: - Attention

    @ViewBuilder
    private var attentionSection: some View {
        let items = briefing.parsedAttention
        if !items.isEmpty {
            sectionView(
                title: "Needs Attention",
                icon: "exclamationmark.triangle.fill",
                iconColor: .orange
            ) {
                ForEach(items) { item in
                    attentionCard(item)
                }
            }
        }
    }

    @ViewBuilder
    private func attentionCard(_ item: AttentionItem) -> some View {
        Button {
            navigateToSource(type: item.sourceType, id: item.sourceID)
        } label: {
            HStack(alignment: .top, spacing: 8) {
                priorityIcon(item.priority)
                    .frame(width: 16)
                    .padding(.top, 2)

                VStack(alignment: .leading, spacing: 3) {
                    Text(item.text)
                        .font(.callout)
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)

                    if let reason = item.reason, !reason.isEmpty {
                        Text(reason)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    HStack(spacing: 4) {
                        if let sourceType = item.sourceType {
                            sourceLabel(type: sourceType)
                        }
                        jiraKeyBadges(in: item.text)
                        if let reason = item.reason {
                            jiraKeyBadges(in: reason)
                        }
                    }
                }

                Spacer()

                if item.sourceType != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                Color.orange.opacity(0.06),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
        .buttonStyle(.plain)

        if item.suggestTask == true {
            HStack {
                Spacer()
                Button {
                    taskPrefillText = item.text
                    taskPrefillIntent = item.reason ?? ""
                    showCreateTask = true
                } label: {
                    Label("Create task", systemImage: "plus.circle")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            }
            .padding(.trailing, 4)
            .padding(.top, 2)
        }
    }

    // MARK: - Your Day

    @ViewBuilder
    private var yourDaySection: some View {
        let items = briefing.parsedYourDay
        sectionView(
            title: "Your Day",
            icon: "calendar",
            iconColor: .green
        ) {
            if items.isEmpty {
                Text("No tracks yet. Tracks are auto-generated from digests.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(
                        Color.green.opacity(0.04),
                        in: RoundedRectangle(cornerRadius: 8)
                    )
            } else {
                ForEach(items) { item in
                    yourDayCard(item)
                }
            }
        }
    }

    private func yourDayCard(_ item: YourDayItem) -> some View {
        Button {
            if let taskID = item.taskID {
                appState.navigateToTask(taskID)
            } else if let trackID = item.trackID {
                navigateToSource(type: "track", id: String(trackID))
            }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                priorityIcon(item.priority)
                    .frame(width: 16)
                    .padding(.top, 2)

                VStack(alignment: .leading, spacing: 3) {
                    Text(item.text)
                        .font(.callout)
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)

                    HStack(spacing: 6) {
                        if let status = item.status, !status.isEmpty {
                            statusBadge(status)
                        }
                        if let ownership = item.ownership, !ownership.isEmpty {
                            Text(ownership)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        if let due = item.dueDate, !due.isEmpty {
                            Label(due, systemImage: "clock")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        jiraKeyBadges(in: item.text)
                    }
                }

                Spacer()

                if item.trackID != nil || item.taskID != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                Color.green.opacity(0.04),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
        .buttonStyle(.plain)
    }

    // MARK: - What Happened

    @ViewBuilder
    private var whatHappenedSection: some View {
        let items = briefing.parsedWhatHappened
        if !items.isEmpty {
            sectionView(
                title: "What Happened",
                icon: "newspaper.fill",
                iconColor: .blue
            ) {
                ForEach(items) { item in
                    whatHappenedCard(item)
                }
            }
        }
    }

    private func whatHappenedCard(_ item: WhatHappenedItem) -> some View {
        Button {
            if let digestID = item.digestID {
                navigateToSource(type: "digest", id: String(digestID))
            }
        } label: {
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    if let ch = item.channelName, !ch.isEmpty {
                        Text("#\(ch)")
                            .font(.caption)
                            .fontWeight(.semibold)
                            .foregroundStyle(.blue)
                    }

                    if let itemType = item.itemType, !itemType.isEmpty {
                        Text(itemType)
                            .font(.caption2)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(
                                Color.secondary.opacity(0.15),
                                in: Capsule()
                            )
                    }

                    if let imp = item.importance, !imp.isEmpty {
                        importanceBadge(imp)
                    }

                    Spacer()

                    if item.digestID != nil {
                        Image(systemName: "chevron.right")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }

                Text(item.text)
                    .font(.callout)
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                Color.blue.opacity(0.04),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
        .buttonStyle(.plain)
    }

    // MARK: - Team Pulse

    @ViewBuilder
    private var teamPulseSection: some View {
        let items = briefing.parsedTeamPulse
        if !items.isEmpty {
            sectionView(
                title: "Team Pulse",
                icon: "heart.text.square.fill",
                iconColor: .pink
            ) {
                ForEach(items) { item in
                    teamPulseCard(item)
                }
            }
        }
    }

    private func teamPulseCard(_ item: TeamPulseItem) -> some View {
        Button {
            if let userID = item.userID {
                navigateToSource(type: "people", id: nil, userID: userID)
            }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                signalIcon(item.signalType)
                    .frame(width: 16)
                    .padding(.top, 2)

                VStack(alignment: .leading, spacing: 3) {
                    Text(item.text)
                        .font(.callout)
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)

                    if let detail = item.detail, !detail.isEmpty {
                        Text(detail)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    if let signal = item.signalType, !signal.isEmpty {
                        signalBadge(signal)
                    }
                }

                Spacer()

                if item.userID != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                Color.pink.opacity(0.04),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
        .buttonStyle(.plain)
    }

    // MARK: - Coaching Corner

    @ViewBuilder
    private var coachingSection: some View {
        let items = briefing.parsedCoaching
        if !items.isEmpty {
            sectionView(
                title: "Coaching Corner",
                icon: "lightbulb.fill",
                iconColor: .yellow
            ) {
                ForEach(items) { item in
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "lightbulb.fill")
                            .foregroundStyle(.yellow)
                            .font(.caption)
                            .padding(.top, 2)

                        VStack(alignment: .leading, spacing: 3) {
                            Text(item.text)
                                .font(.callout)

                            if let cat = item.category, !cat.isEmpty {
                                Text(cat.capitalized)
                                    .font(.caption2)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1)
                                    .background(
                                        Color.yellow.opacity(0.2),
                                        in: Capsule()
                                    )
                            }
                        }

                        Spacer()

                        Button {
                            taskPrefillText = item.text
                            taskPrefillIntent = item.category ?? ""
                            showCreateTask = true
                        } label: {
                            Image(systemName: "plus.circle")
                                .foregroundStyle(.secondary)
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                        .help("Add Task")
                    }
                }
            }
        }
    }

    // MARK: - Navigation

    private func navigateToSource(
        type: String?,
        id: String?,
        userID: String? = nil
    ) {
        guard let type else { return }
        switch type {
        case "track":
            appState.selectedDestination = .tracks
        case "digest":
            if let id, let intID = Int(id) {
                appState.navigateToDigest(intID)
            } else {
                appState.selectedDestination = .digests
            }
        case "people":
            appState.selectedDestination = .people
        default:
            break
        }
    }

    // MARK: - Helpers

    private func sectionView<Content: View>(
        title: String,
        icon: String,
        iconColor: Color,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(iconColor)
                Text(title)
                    .font(.headline)
            }

            content()
        }
    }

    private func priorityIcon(_ priority: String?) -> some View {
        let (icon, color): (String, Color) = {
            switch priority {
            case "high": return ("exclamationmark.circle.fill", .red)
            case "low": return ("arrow.down.circle.fill", .gray)
            default: return ("minus.circle.fill", .orange)
            }
        }()
        return Image(systemName: icon)
            .foregroundStyle(color)
            .font(.caption)
    }

    private func sourceLabel(type: String) -> some View {
        let (icon, label): (String, String) = {
            switch type {
            case "track": return ("checklist", "Track")
            case "digest": return ("newspaper", "Digest")
            case "people": return ("person.2", "Person")
            default: return ("questionmark.circle", type)
            }
        }()
        return Label(label, systemImage: icon)
            .font(.caption2)
            .foregroundStyle(.tertiary)
    }

    private func statusBadge(_ status: String) -> some View {
        let color: Color = {
            switch status {
            case "active": return .green
            case "done": return .gray
            default: return .secondary
            }
        }()
        return Text(status.capitalized)
            .font(.caption2)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(color.opacity(0.15), in: Capsule())
    }

    private func importanceBadge(_ importance: String) -> some View {
        let color: Color = {
            switch importance {
            case "high": return .red
            case "low": return .gray
            default: return .orange
            }
        }()
        return Text(importance.capitalized)
            .font(.caption2)
            .foregroundStyle(color)
    }

    private func signalIcon(_ signalType: String?) -> some View {
        let (icon, color): (String, Color) = {
            switch signalType {
            case "volume_spike": return ("arrow.up.circle.fill", .orange)
            case "volume_drop": return ("arrow.down.circle.fill", .blue)
            case "new_red_flag": return ("flag.fill", .red)
            case "highlight": return ("star.fill", .yellow)
            case "conflict": return ("bolt.fill", .red)
            default: return ("circle.fill", .secondary)
            }
        }()
        return Image(systemName: icon)
            .foregroundStyle(color)
            .font(.caption)
    }

    private func signalBadge(_ signal: String) -> some View {
        let label = signal.replacingOccurrences(of: "_", with: " ").capitalized
        return Text(label)
            .font(.caption2)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(Color.pink.opacity(0.12), in: Capsule())
    }

    // MARK: - Jira Helpers

    private func jiraKeyBadges(in text: String) -> some View {
        JiraKeyLinkBadgesView(
            text: text,
            siteURL: jiraSiteURL,
            isConnected: jiraConnected
        )
    }
}
