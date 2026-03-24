import SwiftUI
import GRDB

struct UsageView: View {
    @Environment(AppState.self) private var appState
    @State private var selectedTab: Tab = .progress
    @State private var selectedDate: Date = Calendar.current.startOfDay(for: Date())
    @State private var usage: TokenUsageSummary?
    @State private var isLoading = false

    enum Tab: String, CaseIterable {
        case progress = "Progress"
        case usage = "Usage"
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("", selection: $selectedTab) {
                ForEach(Tab.allCases, id: \.self) { tab in
                    Text(tab.rawValue).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 8)

            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    switch selectedTab {
                    case .progress:
                        ProgressDetailContent()
                            .environment(appState)
                    case .usage:
                        historicalSection
                    }
                }
                .padding(20)
            }
        }
        .onAppear { loadUsage() }
    }

    // MARK: - Historical Section

    @ViewBuilder
    private var historicalSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Historical Usage")
                    .font(.title2)
                    .fontWeight(.bold)

                Spacer()

                dayPicker
            }

            if let usage, !usage.rows.isEmpty {
                totalSummary(usage)
                byModelSection(usage)
                byFeatureSection(usage)
            } else if isLoading {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
            } else {
                Text("No AI usage data yet.")
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var dayPicker: some View {
        HStack(spacing: 8) {
            Button {
                selectedDate = Calendar.current.date(
                    byAdding: .day, value: -1, to: selectedDate
                ) ?? selectedDate
                loadUsage()
            } label: {
                Image(systemName: "chevron.left")
            }
            .buttonStyle(.borderless)

            Text(selectedDate, style: .date)
                .font(.subheadline)
                .monospacedDigit()
                .frame(minWidth: 100)

            Button {
                let next = Calendar.current.date(
                    byAdding: .day, value: 1, to: selectedDate
                ) ?? selectedDate
                let today = Calendar.current.startOfDay(for: Date())
                if next <= today {
                    selectedDate = next
                    loadUsage()
                }
            } label: {
                Image(systemName: "chevron.right")
            }
            .buttonStyle(.borderless)
            .disabled(Calendar.current.isDateInToday(selectedDate))

            Button("Today") {
                selectedDate = Calendar.current.startOfDay(for: Date())
                loadUsage()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
            .disabled(Calendar.current.isDateInToday(selectedDate))
        }
    }

    private func totalSummary(_ usage: TokenUsageSummary) -> some View {
        GroupBox("Total") {
            HStack(spacing: 24) {
                costItem(label: "AI Calls", value: "\(usage.totalCalls)")
                costItem(label: "Input (clean)", value: formatTokens(usage.totalInputTokens), tooltip: "Estimated tokens from Watchtower prompts (~4 chars/token)")
                costItem(label: "Output", value: formatTokens(usage.totalOutputTokens))
                costItem(label: "Cost", value: formatCost(usage.totalCost))
            }
            .padding(4)
        }
    }

    private func byModelSection(_ usage: TokenUsageSummary) -> some View {
        GroupBox("By Model") {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(usage.byModel, id: \.model) { entry in
                    HStack {
                        Text(entry.model)
                            .font(.body.monospaced())
                        Spacer()
                        Text("\(entry.calls) calls")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(formatCost(entry.cost))
                            .monospacedDigit()
                            .fontWeight(.medium)
                    }
                }
            }
            .padding(4)
        }
    }

    private func byFeatureSection(_ usage: TokenUsageSummary) -> some View {
        let bySource = groupBySource(usage.rows)
        return GroupBox("By Feature") {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(bySource, id: \.source) { entry in
                    HStack {
                        Text(entry.label)
                        Spacer()
                        Text("\(entry.calls) calls")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(formatCost(entry.cost))
                            .monospacedDigit()
                            .fontWeight(.medium)
                    }
                }
            }
            .padding(4)
        }
    }

    // MARK: - Data Loading

    private func loadUsage() {
        guard let db = appState.databaseManager else { return }
        isLoading = true
        let date = selectedDate
        Task.detached {
            let result = try? await db.dbPool.read { db in
                try TokenUsageQueries.fetchUsage(db, on: date)
            }
            await MainActor.run {
                usage = result
                isLoading = false
            }
        }
    }

    // MARK: - Helpers

    private func costItem(label: String, value: String, tooltip: String? = nil) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.headline)
                .fontWeight(.bold)
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .help(tooltip ?? "")
    }

    private func formatTokens(_ count: Int) -> String {
        if count >= 1_000_000 {
            return String(format: "%.1fM", Double(count) / 1_000_000)
        } else if count >= 1_000 {
            return String(format: "%.1fK", Double(count) / 1_000)
        }
        return "\(count)"
    }

    private func formatCost(_ cost: Double) -> String {
        if cost < 0.01 { return String(format: "$%.4f", cost) }
        return String(format: "$%.2f", cost)
    }

    private struct SourceEntry: Identifiable {
        let source: String
        let label: String
        let calls: Int
        let cost: Double
        var id: String { source }
    }

    private func groupBySource(_ rows: [TokenUsageRow]) -> [SourceEntry] {
        var map: [String: (calls: Int, cost: Double)] = [:]
        for row in rows {
            let existing = map[row.source, default: (0, 0)]
            map[row.source] = (existing.calls + row.calls, existing.cost + row.costUSD)
        }

        let labels: [String: String] = [
            "digests": "Digests",
            "people": "People Analytics",
            "summaries": "Period Summaries",
            "tracks": "Tracks"
        ]

        return map
            .map {
                SourceEntry(
                    source: $0.key,
                    label: labels[$0.key] ?? $0.key,
                    calls: $0.value.calls,
                    cost: $0.value.cost
                )
            }
            .sorted { $0.cost > $1.cost }
    }
}
