import SwiftUI

struct GuideDetailView: View {
    let guide: CommunicationGuide
    let userName: String
    var onClose: (() -> Void)? = nil

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                statsGrid
                summarySection
                preferencesSection
                availabilitySection
                decisionSection
                tacticsSection
                approachesSection
                recommendationsSection
                activityHoursChart
            }
            .padding(20)
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("@\(userName)")
                    .font(.title2.bold())
                Text("Communication Guide")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                let from = guide.periodFromDate
                let to = guide.periodToDate
                let fmt = DateFormatter()
                let _ = fmt.dateFormat = "MMM d, yyyy"
                Text("\(fmt.string(from: from)) – \(fmt.string(from: to))")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
            Spacer()
            if let onClose {
                Button(action: onClose) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.title3)
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
    }

    // MARK: - Stats

    private var statsGrid: some View {
        LazyVGrid(columns: [
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible()),
        ], spacing: 8) {
            statCard("Messages", value: "\(guide.messageCount)")
            statCard("Channels", value: "\(guide.channelsActive)")
            statCard("Volume", value: String(format: "%+.0f%%", guide.volumeChangePct))
        }
    }

    private func statCard(_ title: String, value: String) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.title3.bold().monospacedDigit())
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .background(.quaternary.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - Sections

    @ViewBuilder
    private var summarySection: some View {
        if !guide.summary.isEmpty {
            section("How to Work With Them") {
                Text(guide.summary)
                    .font(.body)
            }
        }
    }

    @ViewBuilder
    private var preferencesSection: some View {
        if !guide.communicationPreferences.isEmpty {
            section("Communication Preferences") {
                Text(guide.communicationPreferences)
                    .font(.callout)
            }
        }
    }

    @ViewBuilder
    private var availabilitySection: some View {
        if !guide.availabilityPatterns.isEmpty {
            section("Availability Patterns") {
                Text(guide.availabilityPatterns)
                    .font(.callout)
            }
        }
    }

    @ViewBuilder
    private var decisionSection: some View {
        if !guide.decisionProcess.isEmpty {
            section("Decision Process") {
                Text(guide.decisionProcess)
                    .font(.callout)
            }
        }
    }

    @ViewBuilder
    private var tacticsSection: some View {
        let items = guide.parsedSituationalTactics
        if !items.isEmpty {
            section("Situational Tactics") {
                ForEach(items, id: \.self) { item in
                    Label(item, systemImage: "lightbulb")
                        .font(.callout)
                        .padding(.vertical, 2)
                }
            }
        }
    }

    @ViewBuilder
    private var approachesSection: some View {
        let items = guide.parsedEffectiveApproaches
        if !items.isEmpty {
            section("What Works Well") {
                ForEach(items, id: \.self) { item in
                    Label(item, systemImage: "checkmark.circle")
                        .font(.callout)
                        .foregroundStyle(.green)
                        .padding(.vertical, 2)
                }
            }
        }
    }

    @ViewBuilder
    private var recommendationsSection: some View {
        let items = guide.parsedRecommendations
        if !items.isEmpty {
            section("Recommendations") {
                ForEach(items, id: \.self) { item in
                    Label(item, systemImage: "arrow.right.circle")
                        .font(.callout)
                        .padding(.vertical, 2)
                }
            }
        }
    }

    @ViewBuilder
    private var activityHoursChart: some View {
        let hours = guide.parsedActiveHours
        if !hours.isEmpty {
            section("Active Hours (UTC)") {
                HStack(alignment: .bottom, spacing: 2) {
                    ForEach(0..<24, id: \.self) { hour in
                        let count = hours[String(hour)] ?? 0
                        let maxCount = hours.values.max() ?? 1
                        let height = maxCount > 0 ? CGFloat(count) / CGFloat(maxCount) * 40 : 0
                        VStack(spacing: 1) {
                            RoundedRectangle(cornerRadius: 1)
                                .fill(count > 0 ? Color.accentColor : Color.gray.opacity(0.2))
                                .frame(width: 8, height: max(2, height))
                            if hour % 6 == 0 {
                                Text("\(hour)")
                                    .font(.system(size: 8))
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Helpers

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.headline)
            content()
        }
    }
}
