import SwiftUI

struct PersonDetailView: View {
    let analysis: UserAnalysis
    let userName: String
    let history: [UserAnalysis]
    let userNameResolver: (String) -> String

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                statsGrid
                summarySection
                accomplishmentsSection
                styleDetailsSection
                redFlagsSection
                concernsSection
                highlightsSection
                recommendationsSection
                activityHoursChart
                historySection
            }
            .padding(20)
        }
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(analysis.styleEmoji)
                    .font(.largeTitle)
                VStack(alignment: .leading) {
                    Text("@\(userName)")
                        .font(.title2)
                        .fontWeight(.bold)
                    Text("\(analysis.periodFromDate.formatted(.dateTime.month().day())) – \(analysis.periodToDate.formatted(.dateTime.month().day()))")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }

            HStack(spacing: 8) {
                Badge(text: analysis.communicationStyle, color: .accentColor)
                Badge(text: analysis.decisionRole, color: .purple)
            }
        }
    }

    // MARK: - Stats

    private var statsGrid: some View {
        LazyVGrid(columns: [
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible()),
        ], spacing: 12) {
            StatCard(title: "Messages", value: "\(analysis.messageCount)",
                     detail: analysis.volumeChangePct != 0
                        ? String(format: "%+.0f%% vs prev", analysis.volumeChangePct)
                        : nil,
                     detailColor: analysis.volumeChangePct < -30 ? .red : .secondary)
            StatCard(title: "Channels", value: "\(analysis.channelsActive)", detail: nil)
            StatCard(title: "Avg Length", value: "\(Int(analysis.avgMessageLength))", detail: "chars")
            StatCard(title: "Threads Started", value: "\(analysis.threadsInitiated)", detail: nil)
            StatCard(title: "Thread Replies", value: "\(analysis.threadsReplied)", detail: nil)
        }
    }

    // MARK: - Summary

    @ViewBuilder
    private var summarySection: some View {
        if !analysis.summary.isEmpty {
            GroupBox("Summary") {
                Text(analysis.summary)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
        }
    }

    // MARK: - Accomplishments

    @ViewBuilder
    private var accomplishmentsSection: some View {
        let items = analysis.parsedAccomplishments
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

    // MARK: - Style Details

    @ViewBuilder
    private var styleDetailsSection: some View {
        if !analysis.styleDetails.isEmpty {
            GroupBox("Communication Style Analysis") {
                Text(analysis.styleDetails)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
        }
    }

    // MARK: - Red Flags

    @ViewBuilder
    private var redFlagsSection: some View {
        let flags = analysis.parsedRedFlags
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

    // MARK: - Concerns

    @ViewBuilder
    private var concernsSection: some View {
        let items = analysis.parsedConcerns
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { item in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "exclamationmark.circle.fill")
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
                Label("Concerns", systemImage: "exclamationmark.circle")
                    .foregroundStyle(.orange)
            }
        }
    }

    // MARK: - Highlights

    @ViewBuilder
    private var highlightsSection: some View {
        let items = analysis.parsedHighlights
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

    // MARK: - Recommendations

    @ViewBuilder
    private var recommendationsSection: some View {
        let items = analysis.parsedRecommendations
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { item in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "lightbulb.fill")
                                .foregroundStyle(.yellow)
                                .font(.caption)
                            Text(item)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Recommendations", systemImage: "lightbulb")
                    .foregroundStyle(.yellow)
            }
        }
    }

    // MARK: - Activity Hours

    @ViewBuilder
    private var activityHoursChart: some View {
        let hours = analysis.parsedActiveHours
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
                            Text("\(entry.periodFromDate.formatted(.dateTime.month().day())) – \(entry.periodToDate.formatted(.dateTime.month().day()))")
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
