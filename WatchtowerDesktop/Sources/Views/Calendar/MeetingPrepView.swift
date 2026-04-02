import SwiftUI

struct MeetingPrepView: View {
    let eventID: String
    @State private var viewModel = MeetingPrepViewModel()
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if viewModel.isLoading {
                    loadingView
                } else if let result = viewModel.result {
                    prepContent(result)
                } else if let error = viewModel.error {
                    errorView(error)
                } else {
                    loadingView
                }
            }
            .navigationTitle("Meeting Prep")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
            }
        }
        .frame(minWidth: 500, minHeight: 400)
        .onAppear {
            viewModel.generate(eventID: eventID)
        }
    }

    // MARK: - Content

    private func prepContent(_ result: MeetingPrepResult) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                titleHeader(result)
                talkingPointsSection(result.talkingPoints)
                openItemsSection(result.openItems)
                peopleNotesSection(result.peopleNotes)
                suggestedPrepSection(result.suggestedPrep)
            }
            .padding()
        }
    }

    private func titleHeader(_ result: MeetingPrepResult) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(result.title)
                .font(.title2)
                .fontWeight(.bold)
            if !result.startTime.isEmpty {
                Text(result.startTime)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Talking Points

    @ViewBuilder
    private func talkingPointsSection(_ points: [TalkingPoint]) -> some View {
        if !points.isEmpty {
            sectionHeader("Talking Points", icon: "text.bubble")
            ForEach(points) { point in
                HStack(alignment: .top, spacing: 6) {
                    priorityDot(point.priority)
                        .padding(.top, 6)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(point.text).font(.callout)
                        if !point.sourceType.isEmpty {
                            Text("\(point.sourceType) #\(point.sourceID)")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Open Items

    @ViewBuilder
    private func openItemsSection(_ items: [OpenItem]) -> some View {
        if !items.isEmpty {
            sectionHeader("Open Items", icon: "tray.full")
            ForEach(items) { item in
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "circle.fill")
                        .font(.system(size: 4))
                        .padding(.top, 6)
                        .foregroundStyle(.orange)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.text).font(.callout)
                        HStack(spacing: 4) {
                            if !item.personName.isEmpty {
                                Text(item.personName)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                            if !item.type.isEmpty {
                                Text(item.type)
                                    .font(.caption2)
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(
                                        Color.secondary.opacity(0.12),
                                        in: Capsule()
                                    )
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - People Notes

    @ViewBuilder
    private func peopleNotesSection(_ notes: [PersonNote]) -> some View {
        if !notes.isEmpty {
            sectionHeader("People Notes", icon: "person.2")
            ForEach(notes) { note in
                VStack(alignment: .leading, spacing: 3) {
                    Text(note.name)
                        .font(.callout)
                        .fontWeight(.medium)
                    if !note.communicationTip.isEmpty {
                        HStack(alignment: .top, spacing: 4) {
                            Image(systemName: "lightbulb.fill")
                                .font(.caption2)
                                .foregroundStyle(.yellow)
                            Text(note.communicationTip)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    if !note.recentContext.isEmpty {
                        Text(note.recentContext)
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(
                    Color.secondary.opacity(0.04),
                    in: RoundedRectangle(cornerRadius: 6)
                )
            }
        }
    }

    // MARK: - Suggested Prep

    @ViewBuilder
    private func suggestedPrepSection(_ items: [String]) -> some View {
        if !items.isEmpty {
            sectionHeader("Suggested Prep", icon: "book")
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(.green)
                        .padding(.top, 2)
                    Text(item).font(.callout)
                }
            }
        }
    }

    // MARK: - Helpers

    private func sectionHeader(
        _ title: String,
        icon: String
    ) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon)
                .foregroundStyle(.blue)
            Text(title)
                .font(.headline)
        }
        .padding(.top, 4)
    }

    private func priorityDot(_ priority: String) -> some View {
        let color: Color = {
            switch priority {
            case "high": return .red
            case "medium": return .orange
            default: return .blue
            }
        }()
        return Image(systemName: "circle.fill")
            .font(.system(size: 5))
            .foregroundStyle(color)
    }

    // MARK: - States

    private var loadingView: some View {
        VStack(spacing: 12) {
            ProgressView()
            Text("Preparing meeting context...")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func errorView(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundStyle(.orange)
            Text(message)
                .font(.callout)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Retry") {
                viewModel.generate(eventID: eventID)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}
