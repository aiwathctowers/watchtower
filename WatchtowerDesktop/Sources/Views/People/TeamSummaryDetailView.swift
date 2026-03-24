import SwiftUI

struct TeamSummaryDetailView: View {
    let summary: PeopleCardSummary
    var onClose: (() -> Void)?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                summarySection
                attentionSection
                tipsSection
                metadataSection
            }
            .padding(20)
        }
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Image(systemName: "person.3.fill")
                    .font(.largeTitle)
                    .foregroundStyle(.orange)
                VStack(alignment: .leading) {
                    Text("Team Summary")
                        .font(.title2)
                        .fontWeight(.bold)
                    Text("\(periodFromDate.formatted(.dateTime.month().day())) – \(periodToDate.formatted(.dateTime.month().day()))")
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
        }
    }

    // MARK: - Summary

    @ViewBuilder
    private var summarySection: some View {
        if !summary.summary.isEmpty {
            GroupBox("Summary") {
                Text(summary.summary)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
        }
    }

    // MARK: - Attention

    @ViewBuilder
    private var attentionSection: some View {
        let items = summary.parsedAttention
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
                Label("Needs Attention", systemImage: "exclamationmark.circle")
                    .foregroundStyle(.orange)
            }
        }
    }

    // MARK: - Tips

    @ViewBuilder
    private var tipsSection: some View {
        let items = summary.parsedTips
        if !items.isEmpty {
            GroupBox {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(items, id: \.self) { tip in
                        HStack(alignment: .top, spacing: 6) {
                            Image(systemName: "lightbulb.fill")
                                .foregroundStyle(.yellow)
                                .font(.caption)
                            Text(tip)
                                .font(.subheadline)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
            } label: {
                Label("Tips", systemImage: "lightbulb")
                    .foregroundStyle(.yellow)
            }
        }
    }

    // MARK: - Metadata

    private var metadataSection: some View {
        GroupBox {
            HStack(spacing: 16) {
                metadataItem(label: "Model", value: summary.model)
                metadataItem(label: "Tokens", value: "\(summary.inputTokens + summary.outputTokens)")
                metadataItem(label: "Cost", value: String(format: "$%.4f", summary.costUSD))
            }
            .padding(4)
        }
    }

    private func metadataItem(label: String, value: String) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.caption)
                .fontWeight(.medium)
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Helpers

    private var periodFromDate: Date {
        Date(timeIntervalSince1970: summary.periodFrom)
    }

    private var periodToDate: Date {
        Date(timeIntervalSince1970: summary.periodTo)
    }
}
