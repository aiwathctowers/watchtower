import SwiftUI

struct PersonDetailView: View {
    let card: PeopleCard
    let userName: String
    let history: [PeopleCard]
    let userNameResolver: (String) -> String
    var onClose: (() -> Void)?
    var isCurrentUser: Bool = false
    var profile: UserProfile?
    var interactions: [UserInteraction] = []
    var onUpdateConnections: (([String], [String], String) -> Void)?
    var allCards: [PeopleCard] = []

    @State private var selectedTab = 0  // 0 = Overview, 1 = Connections

    var body: some View {
        VStack(spacing: 0) {
            if isCurrentUser {
                Picker("", selection: $selectedTab) {
                    Text("Overview").tag(0)
                    Text("Connections").tag(1)
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, 20)
                .padding(.top, 12)
                .padding(.bottom, 4)
            }

            if selectedTab == 0 || !isCurrentUser {
                overviewContent
            } else {
                ConnectionsView(
                    interactions: interactions,
                    profile: profile,
                    allCards: allCards,
                    userNameResolver: userNameResolver,
                    onNavigateToPerson: { _ in
                        // Navigation is handled by parent (PeopleListView)
                    },
                    onUpdateConnections: onUpdateConnections
                )
            }
        }
    }

    private var overviewContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                statsGrid
                if isCurrentUser { profileContextSection }
                insufficientDataBanner
                summarySection
                accomplishmentsSection
                communicationGuideSection
                decisionStyleSection
                tacticsSection
                redFlagsSection
                highlightsSection
                activityHoursChart
                historySection
            }
            .padding(20)
        }
    }

    // MARK: - Profile Context (My Card only)

    @ViewBuilder
    private var profileContextSection: some View {
        if let profile, !profile.customPromptContext.isEmpty {
            GroupBox {
                Text(profile.customPromptContext)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            } label: {
                Label("How the system understands you", systemImage: "brain")
                    .foregroundStyle(.purple)
            }
        }
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(card.styleEmoji)
                    .font(.largeTitle)
                VStack(alignment: .leading) {
                    Text("@\(userName)")
                        .font(.title2)
                        .fontWeight(.bold)
                    Text("\(card.periodFromDate.formatted(.dateTime.month().day())) – \(card.periodToDate.formatted(.dateTime.month().day()))")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                if let onClose {
                    Button { onClose() } label: {
                        Image(systemName: "xmark.circle.fill")
                            .symbolRenderingMode(.hierarchical)
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }

            HStack(spacing: 8) {
                Badge(text: card.communicationStyle, color: .accentColor)
                Badge(text: card.decisionRole, color: .purple)
            }
        }
    }

    // MARK: - Stats

    private var statsGrid: some View {
        LazyVGrid(columns: [
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible())
        ], spacing: 12) {
            StatCard(
                title: "Messages",
                value: "\(card.messageCount)",
                detail: card.volumeChangePct != 0
                    ? String(format: "%+.0f%% vs prev", card.volumeChangePct)
                    : nil,
                detailColor: card.volumeChangePct < -30 ? .red : .secondary
            )
            StatCard(title: "Channels", value: "\(card.channelsActive)", detail: nil)
            StatCard(title: "Avg Length", value: "\(Int(card.avgMessageLength))", detail: "chars")
            StatCard(title: "Threads Started", value: "\(card.threadsInitiated)", detail: nil)
            StatCard(title: "Thread Replies", value: "\(card.threadsReplied)", detail: nil)
        }
    }

    // MARK: - Summary

    @ViewBuilder
    private var summarySection: some View {
        if !card.summary.isEmpty {
            GroupBox("Summary") {
                Text(card.summary)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
        }
    }

    // MARK: - Accomplishments

    @ViewBuilder
    private var accomplishmentsSection: some View {
        let items = card.parsedAccomplishments
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { item in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                                .font(.caption)
                            Text(item)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Accomplishments", systemImage: "checkmark.circle")
                    .foregroundStyle(.green)
            }
        }
    }

    // MARK: - Insufficient Data Banner

    @ViewBuilder
    private var insufficientDataBanner: some View {
        if card.isInsufficientData {
            HStack(spacing: 8) {
                Image(systemName: "info.circle.fill")
                    .foregroundStyle(.orange)
                Text("Not enough data for full analysis. Results will improve as more messages are collected.")
                    .font(.subheadline)
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    // MARK: - Communication Guide

    @ViewBuilder
    private var communicationGuideSection: some View {
        if !card.communicationGuide.isEmpty {
            GroupBox {
                Text(card.communicationGuide)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            } label: {
                Label("Communication Guide", systemImage: "bubble.left.and.text.bubble.right")
                    .foregroundStyle(.blue)
            }
        }
    }

    // MARK: - Decision Style

    @ViewBuilder
    private var decisionStyleSection: some View {
        if !card.decisionStyle.isEmpty {
            GroupBox {
                Text(card.decisionStyle)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            } label: {
                Label("Decision Style", systemImage: "arrow.triangle.branch")
                    .foregroundStyle(.purple)
            }
        }
    }

    // MARK: - Tactics

    @ViewBuilder
    private var tacticsSection: some View {
        let items = card.parsedTactics
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { item in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "lightbulb.fill")
                                .foregroundStyle(.orange)
                                .font(.caption)
                            Text(item)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Tactics", systemImage: "lightbulb")
                    .foregroundStyle(.orange)
            }
        }
    }

    // MARK: - Red Flags

    @ViewBuilder
    private var redFlagsSection: some View {
        let flags = card.parsedRedFlags
        if !flags.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(flags, id: \.self) { flag in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .foregroundStyle(.red)
                                .font(.caption)
                            Text(flag)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Red Flags", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.red)
            }
        }
    }

    // MARK: - Highlights

    @ViewBuilder
    private var highlightsSection: some View {
        let items = card.parsedHighlights
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { item in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "star.fill")
                                .foregroundStyle(.green)
                                .font(.caption)
                            Text(item)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Highlights", systemImage: "star")
                    .foregroundStyle(.green)
            }
        }
    }

    // MARK: - Activity Hours

    @ViewBuilder
    private var activityHoursChart: some View {
        let hours = card.parsedActiveHours
        if !hours.isEmpty {
            let maxCount = max(hours.values.max() ?? 1, 1)
            GroupBox("Activity Hours (UTC)") {
                HStack(alignment: .bottom, spacing: 0) {
                    ForEach(0..<24, id: \.self) { hour in
                        let count = hours[String(hour)] ?? 0
                        let ratio = CGFloat(count) / CGFloat(maxCount)

                        VStack(spacing: 4) {
                            if count > 0 {
                                Text("\(count)")
                                    .font(.system(size: 9))
                                    .foregroundStyle(.secondary)
                            }

                            RoundedRectangle(cornerRadius: 3)
                                .fill(count > 0
                                    ? Color.accentColor.opacity(0.3 + ratio * 0.7)
                                    : Color.secondary.opacity(0.08))
                                .frame(height: count > 0 ? max(ratio * 100, 8) : 4)

                            Text("\(hour)")
                                .font(.system(size: 9, design: .monospaced))
                                .foregroundStyle(count > 0 ? .primary : .quaternary)
                        }
                        .frame(maxWidth: .infinity)
                    }
                }
                .frame(height: 130)
                .padding(.horizontal, 4)
                .padding(.vertical, 8)
            }
        }
    }

    // MARK: - History

    @ViewBuilder
    private var historySection: some View {
        if history.count > 1 {
            GroupBox("History") {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(history) { entry in
                        HStack {
                            let from = entry.periodFromDate
                                .formatted(.dateTime.month().day())
                            let to = entry.periodToDate
                                .formatted(.dateTime.month().day())
                            Text("\(from) – \(to)")
                                .font(.caption)
                                .foregroundStyle(.secondary)

                            Spacer()

                            Text("\(entry.messageCount) msgs")
                                .font(.caption)

                            if entry.volumeChangePct != 0 {
                                Text(String(format: "%+.0f%%", entry.volumeChangePct))
                                    .font(.caption)
                                    .foregroundStyle(entry.volumeChangePct < -30 ? .red : .secondary)
                            }

                            Text(entry.communicationStyle)
                                .font(.caption2)
                                .padding(.horizontal, 4)
                                .padding(.vertical, 1)
                                .background(Color.accentColor.opacity(0.1), in: Capsule())
                        }
                    }
                }
                .padding(4)
            }
        }
    }
}

// MARK: - Supporting Views

struct Badge: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption)
            .fontWeight(.medium)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(color.opacity(0.15), in: Capsule())
            .foregroundStyle(color)
    }
}

struct StatCard: View {
    let title: String
    let value: String
    let detail: String?
    var detailColor: Color = .secondary

    var body: some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.title3)
                .fontWeight(.bold)
            Text(title)
                .font(.caption2)
                .foregroundStyle(.secondary)
            if let detail {
                Text(detail)
                    .font(.caption2)
                    .foregroundStyle(detailColor)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(8)
        .background(Color.secondary.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
    }
}
