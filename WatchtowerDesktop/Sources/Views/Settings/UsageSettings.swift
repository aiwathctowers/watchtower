import SwiftUI

struct UsageSettings: View {
    @Environment(AppState.self) private var appState
    @State private var usage: TokenUsageSummary?
    @State private var isLoading = false

    var body: some View {
        Form {
            if let usage, !usage.rows.isEmpty {
                Section("Total") {
                    LabeledContent("AI Calls") {
                        Text("\(usage.totalCalls)")
                            .monospacedDigit()
                    }
                    LabeledContent("Input Tokens") {
                        Text(formatTokens(usage.totalInputTokens))
                            .monospacedDigit()
                    }
                    LabeledContent("Output Tokens") {
                        Text(formatTokens(usage.totalOutputTokens))
                            .monospacedDigit()
                    }
                    LabeledContent("Total Cost") {
                        Text(formatCost(usage.totalCost))
                            .monospacedDigit()
                            .fontWeight(.semibold)
                    }
                }

                Section("By Model") {
                    ForEach(usage.byModel, id: \.model) { entry in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(shortModelName(entry.model))
                                    .font(.body.monospaced())
                                Spacer()
                                Text(formatCost(entry.cost))
                                    .monospacedDigit()
                                    .fontWeight(.medium)
                            }
                            HStack(spacing: 12) {
                                Label("\(entry.calls) calls", systemImage: "arrow.up.arrow.down")
                                Label("\(formatTokens(entry.input)) in", systemImage: "arrow.down")
                                Label("\(formatTokens(entry.output)) out", systemImage: "arrow.up")
                            }
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        }
                        .padding(.vertical, 2)
                    }
                }

                Section("By Feature") {
                    let bySource = groupBySource(usage.rows)
                    ForEach(bySource, id: \.source) { entry in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(entry.label)
                                Spacer()
                                Text(formatCost(entry.cost))
                                    .monospacedDigit()
                                    .fontWeight(.medium)
                            }
                            HStack(spacing: 12) {
                                Label("\(entry.calls) calls", systemImage: "arrow.up.arrow.down")
                                Label("\(formatTokens(entry.input)) in", systemImage: "arrow.down")
                                Label("\(formatTokens(entry.output)) out", systemImage: "arrow.up")
                            }
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        }
                        .padding(.vertical, 2)
                    }
                }
            } else if isLoading {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
            } else {
                Text("No AI usage data yet. Run a sync with digest generation enabled.")
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .padding()
        .onAppear { loadUsage() }
    }

    private func loadUsage() {
        guard let db = appState.databaseManager else { return }
        isLoading = true
        Task.detached {
            let result = try? await db.dbPool.read { db in
                try TokenUsageQueries.fetchUsage(db)
            }
            await MainActor.run {
                usage = result
                isLoading = false
            }
        }
    }

    private func formatTokens(_ n: Int) -> String {
        if n >= 1_000_000 { return String(format: "%.1fM", Double(n) / 1_000_000) }
        if n >= 1_000 { return String(format: "%.1fK", Double(n) / 1_000) }
        return "\(n)"
    }

    private func formatCost(_ cost: Double) -> String {
        if cost < 0.01 { return String(format: "$%.4f", cost) }
        return String(format: "$%.2f", cost)
    }

    private func shortModelName(_ model: String) -> String {
        // "claude-haiku-4-5-20251001" → "haiku 4.5"
        // "claude-sonnet-4-6" → "sonnet 4.6"
        let m = model.lowercased()
        if m.contains("opus") { return extractVersion(m, family: "opus") }
        if m.contains("sonnet") { return extractVersion(m, family: "sonnet") }
        if m.contains("haiku") { return extractVersion(m, family: "haiku") }
        return model
    }

    private func extractVersion(_ model: String, family: String) -> String {
        // Try to extract version like "4-5" or "4-6" after the family name
        guard let range = model.range(of: family) else { return family }
        let after = model[range.upperBound...]
        // Match "-X-Y" pattern for version
        let parts = after.split(separator: "-").compactMap { Int($0) }
        if parts.count >= 2 {
            return "\(family) \(parts[0]).\(parts[1])"
        } else if parts.count == 1 {
            return "\(family) \(parts[0])"
        }
        return family
    }

    private struct SourceEntry {
        let source: String
        let label: String
        let calls: Int
        let input: Int
        let output: Int
        let cost: Double
    }

    private func groupBySource(_ rows: [TokenUsageRow]) -> [SourceEntry] {
        var map: [String: (calls: Int, input: Int, output: Int, cost: Double)] = [:]
        for row in rows {
            let existing = map[row.source, default: (0, 0, 0, 0)]
            map[row.source] = (existing.calls + row.calls, existing.input + row.inputTokens, existing.output + row.outputTokens, existing.cost + row.costUSD)
        }

        let labels: [String: String] = [
            "digests": "Digests",
            "people": "People Analytics",
            "summaries": "Period Summaries",
            "tracks": "Tracks",
        ]

        return map
            .map { SourceEntry(source: $0.key, label: labels[$0.key] ?? $0.key, calls: $0.value.calls, input: $0.value.input, output: $0.value.output, cost: $0.value.cost) }
            .sorted { $0.cost > $1.cost }
    }
}
