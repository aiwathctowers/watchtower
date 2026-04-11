import SwiftUI

struct GanttChartView: View {
    let epics: [ProjectMapViewModel.EpicItem]

    private let rowHeight: CGFloat = 32
    private let rowSpacing: CGFloat = 4
    private let labelWidth: CGFloat = 180
    private let dayWidth: CGFloat = 12
    private let headerHeight: CGFloat = 40

    private var today: Date { Date() }
    private var calendar: Calendar { Calendar.current }

    // MARK: - Timeline bounds

    private var timelineEpics: [ProjectMapViewModel.EpicItem] {
        epics.filter { $0.startDate != nil && $0.endDate != nil }
    }

    private var fallbackEpics: [ProjectMapViewModel.EpicItem] {
        epics.filter { $0.startDate == nil || $0.endDate == nil }
    }

    private var timelineStart: Date {
        let starts = timelineEpics.compactMap(\.startDate)
        let earliest = starts.min() ?? today
        // Round down to start of month
        return calendar.date(from: calendar.dateComponents([.year, .month], from: earliest)) ?? earliest
    }

    private var timelineEnd: Date {
        let ends = timelineEpics.compactMap(\.endDate)
        let latest = ends.max() ?? today
        // Add 2 weeks buffer
        return calendar.date(byAdding: .weekOfYear, value: 2, to: latest) ?? latest
    }

    private var totalDays: Int {
        max(1, calendar.dateComponents([.day], from: timelineStart, to: timelineEnd).day ?? 1)
    }

    private var chartWidth: CGFloat {
        CGFloat(totalDays) * dayWidth
    }

    // MARK: - Body

    var body: some View {
        if epics.isEmpty {
            emptyState
        } else if timelineEpics.isEmpty {
            // No date data at all — show fallback bars
            fallbackView
        } else {
            VStack(spacing: 0) {
                if !timelineEpics.isEmpty {
                    timelineChart
                }
                if !fallbackEpics.isEmpty {
                    Divider().padding(.vertical, 8)
                    Text("Epics without timeline data")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 4)
                    fallbackSection
                }
            }
        }
    }

    // MARK: - Timeline Chart

    private var timelineChart: some View {
        ScrollView(.horizontal, showsIndicators: true) {
            VStack(alignment: .leading, spacing: 0) {
                // Date header
                dateHeader
                    .frame(height: headerHeight)

                Divider()

                // Rows
                ScrollView(.vertical, showsIndicators: true) {
                    ZStack(alignment: .topLeading) {
                        // Today marker
                        todayMarker

                        // Month grid lines
                        monthGridLines

                        // Epic rows
                        VStack(spacing: rowSpacing) {
                            ForEach(timelineEpics) { epic in
                                timelineRow(epic)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
            .frame(minWidth: labelWidth + chartWidth)
        }
        .padding(.horizontal, 8)
    }

    // MARK: - Date Header

    private var dateHeader: some View {
        HStack(spacing: 0) {
            // Label spacer
            Color.clear.frame(width: labelWidth)

            // Month markers
            ZStack(alignment: .topLeading) {
                ForEach(monthMarkers, id: \.offset) { marker in
                    Text(marker.label)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .offset(x: marker.offset)
                }
            }
            .frame(width: chartWidth, alignment: .leading)
        }
    }

    private var monthMarkers: [(label: String, offset: CGFloat)] {
        var markers: [(String, CGFloat)] = []
        let fmt = DateFormatter()
        fmt.dateFormat = "MMM yyyy"

        var current = timelineStart
        while current < timelineEnd {
            let days = calendar.dateComponents([.day], from: timelineStart, to: current).day ?? 0
            let offset = CGFloat(days) * dayWidth
            markers.append((fmt.string(from: current), offset))
            current = calendar.date(byAdding: .month, value: 1, to: current) ?? timelineEnd
        }
        return markers
    }

    // MARK: - Today Marker

    private var todayMarker: some View {
        let days = calendar.dateComponents([.day], from: timelineStart, to: today).day ?? 0
        let xOffset = labelWidth + CGFloat(days) * dayWidth

        return Rectangle()
            .fill(.clear)
            .frame(width: labelWidth + chartWidth)
            .overlay(alignment: .topLeading) {
                if days >= 0, CGFloat(days) * dayWidth <= chartWidth {
                    Rectangle()
                        .fill(Color.red)
                        .frame(width: 1.5)
                        .overlay(alignment: .top) {
                            Text("Today")
                                .font(.system(size: 9))
                                .foregroundStyle(.red)
                                .offset(x: 0, y: -14)
                        }
                        .offset(x: xOffset)
                }
            }
    }

    // MARK: - Grid Lines

    private var monthGridLines: some View {
        let markers = monthMarkers
        return ZStack(alignment: .topLeading) {
            ForEach(markers, id: \.offset) { marker in
                Rectangle()
                    .fill(Color.secondary.opacity(0.1))
                    .frame(width: 0.5)
                    .offset(x: labelWidth + marker.offset)
            }
        }
        .frame(
            width: labelWidth + chartWidth,
            height: CGFloat(timelineEpics.count) * (rowHeight + rowSpacing)
        )
    }

    // MARK: - Timeline Row

    private func timelineRow(_ epic: ProjectMapViewModel.EpicItem) -> some View {
        HStack(spacing: 0) {
            // Epic name
            Text(epic.name)
                .font(.caption)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(width: labelWidth, alignment: .leading)
                .padding(.horizontal, 4)

            // Bar area
            ZStack(alignment: .leading) {
                // Background track
                Color.clear.frame(width: chartWidth, height: rowHeight)

                if let start = epic.startDate, let end = epic.endDate {
                    let startDays = max(
                        0,
                        calendar.dateComponents([.day], from: timelineStart, to: start).day ?? 0
                    )
                    let endDays = max(
                        startDays + 1,
                        calendar.dateComponents([.day], from: timelineStart, to: end).day ?? 0
                    )
                    let barOffset = CGFloat(startDays) * dayWidth
                    let barWidth = max(20, CGFloat(endDays - startDays) * dayWidth)

                    ganttBar(epic: epic, width: barWidth)
                        .offset(x: barOffset)
                }
            }
            .frame(width: chartWidth, height: rowHeight)
        }
        .frame(height: rowHeight)
    }

    // MARK: - Gantt Bar

    private func ganttBar(
        epic: ProjectMapViewModel.EpicItem,
        width: CGFloat
    ) -> some View {
        let color = badgeColor(epic.statusBadge)
        let fillWidth = width * epic.progressPct

        return ZStack(alignment: .leading) {
            // Background
            RoundedRectangle(cornerRadius: 4)
                .fill(color.opacity(0.2))
                .frame(width: width, height: 20)

            // Progress fill
            if fillWidth > 0 {
                RoundedRectangle(cornerRadius: 4)
                    .fill(color.opacity(0.7))
                    .frame(width: max(4, fillWidth), height: 20)
            }

            // Percentage label
            Text("\(Int(epic.progressPct * 100))%")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.primary)
                .padding(.leading, 4)
        }
        .frame(width: width)
    }

    // MARK: - Fallback Views

    private var fallbackView: some View {
        ScrollView(.vertical, showsIndicators: true) {
            VStack(spacing: rowSpacing) {
                ForEach(epics) { epic in
                    fallbackRow(epic)
                }
            }
            .padding(12)
        }
    }

    private var fallbackSection: some View {
        VStack(spacing: rowSpacing) {
            ForEach(fallbackEpics) { epic in
                fallbackRow(epic)
            }
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }

    private func fallbackRow(_ epic: ProjectMapViewModel.EpicItem) -> some View {
        let color = badgeColor(epic.statusBadge)
        return HStack(spacing: 8) {
            Text(epic.name)
                .font(.caption)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(width: labelWidth, alignment: .leading)

            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 4)
                        .fill(Color.secondary.opacity(0.12))

                    RoundedRectangle(cornerRadius: 4)
                        .fill(color.opacity(0.7))
                        .frame(width: max(0, geo.size.width * epic.progressPct))

                    Text("\(Int(epic.progressPct * 100))%")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(.primary)
                        .padding(.leading, 4)
                }
            }
            .frame(height: 20)

            Text("\(epic.doneIssues)/\(epic.totalIssues)")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .frame(width: 40, alignment: .trailing)
        }
        .frame(height: rowHeight)
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "chart.bar.xaxis.ascending")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text("No epics to display")
                .font(.title3)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Helpers

    private func badgeColor(_ badge: ProjectMapViewModel.EpicStatusBadge) -> Color {
        switch badge {
        case .onTrack: .green
        case .atRisk: .orange
        case .behind: .red
        }
    }
}
