import SwiftUI

// MARK: - Meeting Prep Detail Panel (right side)

struct MeetingPrepDetailView: View {
    let eventID: String
    @Bindable var viewModel: MeetingPrepViewModel
    @Binding var userNotes: String
    let onClose: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            toolbar
            Divider()

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
        .onAppear {
            if viewModel.result == nil && !viewModel.isLoading {
                viewModel.generate(eventID: eventID)
            }
        }
    }

    // MARK: - Toolbar

    private var toolbar: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("Meeting Prep")
                    .font(.headline)
                if viewModel.isCached {
                    Text("Cached")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            Button {
                viewModel.regenerate(eventID: eventID, userNotes: userNotes)
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
                    .font(.caption)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.blue)
            .disabled(viewModel.isLoading)

            Button {
                onClose()
            } label: {
                Image(systemName: "xmark")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    // MARK: - Content

    private func prepContent(_ result: MeetingPrepResult) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                titleHeader(result)

                // Context gaps — prompt user for input
                contextGapsSection(result.contextGaps)

                // User notes input
                userNotesSection

                talkingPointsSection(result.talkingPoints)
                openItemsSection(result.openItems)
                peopleNotesSection(result.peopleNotes)
                recommendationsSection(result.recommendations ?? [])
                suggestedPrepSection(result.suggestedPrep)

                Divider()
                    .padding(.vertical, 4)

                MeetingNotesView(eventID: eventID)
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

    // MARK: - Context Gaps

    @ViewBuilder
    private func contextGapsSection(_ gaps: [String]?) -> some View {
        if let gaps, !gaps.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 4) {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text("Missing Context")
                        .font(.headline)
                }
                ForEach(Array(gaps.enumerated()), id: \.offset) { _, gap in
                    HStack(alignment: .top, spacing: 6) {
                        Image(systemName: "info.circle")
                            .font(.caption)
                            .foregroundStyle(.orange)
                            .padding(.top, 2)
                        Text(gap).font(.callout)
                    }
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                Color.orange.opacity(0.06),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
    }

    // MARK: - User Notes Input

    private var userNotesSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            sectionHeader("Your Notes", icon: "square.and.pencil")
            TextEditor(text: $userNotes)
                .font(.callout)
                .frame(minHeight: 60, maxHeight: 100)
                .padding(4)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.secondary.opacity(0.2), lineWidth: 1)
                )
            if !userNotes.isEmpty {
                HStack {
                    Spacer()
                    Button {
                        viewModel.regenerate(eventID: eventID, userNotes: userNotes)
                    } label: {
                        Label("Regenerate with notes", systemImage: "arrow.clockwise")
                            .font(.caption)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .disabled(viewModel.isLoading)
                }
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

    // MARK: - Recommendations

    @ViewBuilder
    private func recommendationsSection(_ recommendations: [MeetingRecommendation]) -> some View {
        if !recommendations.isEmpty {
            sectionHeader("Recommendations", icon: "sparkles")
            ForEach(recommendations) { rec in
                HStack(alignment: .top, spacing: 6) {
                    priorityDot(rec.priority)
                        .padding(.top, 6)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(rec.text).font(.callout)
                        Text(rec.category)
                            .font(.caption2)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(
                                categoryColor(rec.category).opacity(0.12),
                                in: Capsule()
                            )
                            .foregroundStyle(categoryColor(rec.category))
                    }
                }
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

    private func categoryColor(_ category: String) -> Color {
        switch category {
        case "agenda": return .red
        case "format": return .purple
        case "participants": return .blue
        case "followup": return .green
        case "preparation": return .orange
        default: return .secondary
        }
    }

    // MARK: - States

    private var loadingView: some View {
        VStack(spacing: 16) {
            VStack(spacing: 8) {
                ProgressView()
                    .controlSize(.regular)
                Text(viewModel.statusMessage.isEmpty ? "Preparing..." : viewModel.statusMessage)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            // Progress steps
            VStack(alignment: .leading, spacing: 6) {
                progressStep("Loading event data", done: true)
                progressStep("Analyzing attendee activity", done: !viewModel.statusMessage.contains("Gathering"))
                progressStep("Generating meeting brief", done: false)
            }
            .padding()
            .frame(maxWidth: 280)
            .background(
                Color.secondary.opacity(0.04),
                in: RoundedRectangle(cornerRadius: 8)
            )
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func progressStep(_ label: String, done: Bool) -> some View {
        HStack(spacing: 8) {
            if done {
                Image(systemName: "checkmark.circle.fill")
                    .font(.caption)
                    .foregroundStyle(.green)
            } else {
                ProgressView()
                    .controlSize(.small)
                    .scaleEffect(0.7)
            }
            Text(label)
                .font(.caption)
                .foregroundStyle(done ? .secondary : .primary)
        }
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
