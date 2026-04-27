import SwiftUI

struct MeetingNotesView: View {
    let eventID: String
    @Environment(AppState.self) private var appState

    @State private var notes: [MeetingNote] = []
    @State private var newQuestionText = ""
    @State private var newNoteText = ""
    @State private var creatingTaskForID: Int64?
    @State private var errorMessage: String?
    @State private var showExtractSheet = false
    @State private var recap: MeetingRecap?
    @State private var showRecapSheet = false
    @State private var addedActionItems: Set<Int> = []

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if let error = errorMessage {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal)
            }
            recapSection
            questionsSection
            notesSection
        }
        .onAppear { loadNotes() }
        .sheet(isPresented: $showExtractSheet) {
            ExtractMeetingTopicsSheet(
                eventID: eventID,
                existingTopicSortOrderCeiling: questions.last?.sortOrder ?? -1,
                onCreated: { loadNotes() }
            )
        }
        .sheet(isPresented: $showRecapSheet) {
            GenerateRecapSheet(
                eventID: eventID,
                prefilledText: recap?.sourceText ?? "",
                onCompleted: { loadNotes() }
            )
        }
    }

    // MARK: - Questions Section

    private var questionsSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "checklist")
                    .foregroundStyle(.blue)
                Text("Discussion Topics")
                    .font(.headline)
                Spacer()
                Button {
                    showExtractSheet = true
                } label: {
                    Label("Paste and extract", systemImage: "sparkles")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
            .padding(.top, 4)

            VStack(alignment: .leading, spacing: 6) {
                ForEach(questions) { note in
                    questionRow(note)
                }
            }

            addTopicRow
        }
    }

    private func questionRow(_ note: MeetingNote) -> some View {
        let accent: Color = note.isChecked ? .green : .blue
        return HStack(alignment: .top, spacing: 0) {
            Rectangle()
                .fill(accent)
                .frame(width: 3)

            HStack(alignment: .top, spacing: 8) {
                Button {
                    toggleChecked(note)
                } label: {
                    Image(systemName: note.isChecked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(note.isChecked ? .green : .secondary)
                        .font(.callout)
                }
                .buttonStyle(.plain)
                .padding(.top, 1)

                Text(note.text)
                    .font(.callout)
                    .textSelection(.enabled)
                    .strikethrough(note.isChecked)
                    .foregroundStyle(note.isChecked ? .secondary : .primary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)

                noteActions(note)
            }
            .padding(10)
        }
        .background(accent.opacity(0.05), in: RoundedRectangle(cornerRadius: 6))
    }

    private var addTopicRow: some View {
        HStack(spacing: 6) {
            Image(systemName: "plus.circle")
                .foregroundStyle(.secondary)
                .font(.caption)
            TextField("Add a topic...", text: $newQuestionText, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.callout)
                .lineLimit(1...3)
                .onSubmit { addQuestion() }
        }
        .padding(.vertical, 4)
        .padding(.horizontal, 10)
        .background(
            Color.secondary.opacity(0.04),
            in: RoundedRectangle(cornerRadius: 6)
        )
    }

    // MARK: - Notes Section

    private var notesSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "note.text")
                    .foregroundStyle(.blue)
                Text("Meeting Notes")
                    .font(.headline)
                Spacer()
                Button {
                    showRecapSheet = true
                } label: {
                    Label("Recap from text", systemImage: "sparkles")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
            .padding(.top, 4)

            VStack(alignment: .leading, spacing: 6) {
                ForEach(freeformNotes) { note in
                    noteRow(note)
                }
            }

            addNoteRow
        }
    }

    private func noteRow(_ note: MeetingNote) -> some View {
        HStack(alignment: .top, spacing: 0) {
            Rectangle()
                .fill(Color.orange)
                .frame(width: 3)

            HStack(alignment: .top, spacing: 8) {
                Text(note.text)
                    .font(.callout)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)

                noteActions(note)
            }
            .padding(10)
        }
        .background(Color.orange.opacity(0.05), in: RoundedRectangle(cornerRadius: 6))
    }

    private var addNoteRow: some View {
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: "plus.circle")
                .foregroundStyle(.secondary)
                .font(.caption)
                .padding(.top, 4)
            TextField("Add a note...", text: $newNoteText, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.callout)
                .lineLimit(1...5)
                .onSubmit { addNote() }
        }
        .padding(.vertical, 4)
        .padding(.horizontal, 10)
        .background(
            Color.secondary.opacity(0.04),
            in: RoundedRectangle(cornerRadius: 6)
        )
    }

    // MARK: - Actions

    private func noteActions(_ note: MeetingNote) -> some View {
        HStack(spacing: 6) {
            if let taskID = note.taskID {
                Label("#\(taskID)", systemImage: "checkmark.circle.fill")
                    .labelStyle(.titleAndIcon)
                    .font(.caption2)
                    .foregroundStyle(.green)
            } else {
                Button {
                    createTask(from: note)
                } label: {
                    Image(systemName: creatingTaskForID == note.id ? "hourglass" : "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(.blue)
                }
                .buttonStyle(.plain)
                .disabled(creatingTaskForID == note.id)
                .help("Create task from this note")
            }

            Button {
                deleteNote(note)
            } label: {
                Image(systemName: "trash")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .help("Delete")
        }
    }

    // MARK: - Filtered Lists

    private var questions: [MeetingNote] {
        notes.filter { $0.type == .question }
    }

    private var freeformNotes: [MeetingNote] {
        notes.filter { $0.type == .note }
    }

    // MARK: - Recap Section

    @ViewBuilder
    private var recapSection: some View {
        if let recap, let content = recap.parsed {
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 6) {
                    Image(systemName: "sparkles")
                        .foregroundStyle(.purple)
                    Text("AI Recap")
                        .font(.headline)
                    Spacer()
                    Button {
                        showRecapSheet = true
                    } label: {
                        Label("Re-generate", systemImage: "arrow.triangle.2.circlepath")
                            .font(.caption)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }

                if !content.summary.isEmpty {
                    Text(content.summary)
                        .font(.callout)
                        .textSelection(.enabled)
                }

                if !content.keyDecisions.isEmpty {
                    recapSubsection(title: "Decisions", items: content.keyDecisions)
                }

                if !content.actionItems.isEmpty {
                    actionItemsSubsection(items: content.actionItems)
                }

                if !content.openQuestions.isEmpty {
                    recapSubsection(title: "Open questions", items: content.openQuestions)
                }

                Text("Generated \(formattedTime(recap.updatedAt))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(10)
            .background(Color.purple.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    private func recapSubsection(title: String, items: [String]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.subheadline)
                .fontWeight(.medium)
            ForEach(Array(items.enumerated()), id: \.offset) { _, text in
                HStack(alignment: .top, spacing: 6) {
                    Text("•").foregroundStyle(.secondary)
                    Text(text)
                        .font(.callout)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    private func actionItemsSubsection(items: [String]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Action items")
                .font(.subheadline)
                .fontWeight(.medium)
            ForEach(Array(items.enumerated()), id: \.offset) { idx, text in
                HStack(alignment: .top, spacing: 6) {
                    Text("•").foregroundStyle(.secondary)
                    Text(text)
                        .font(.callout)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)

                    if addedActionItems.contains(idx) {
                        Label("Added", systemImage: "checkmark")
                            .labelStyle(.titleAndIcon)
                            .font(.caption2)
                            .foregroundStyle(.green)
                    } else {
                        Button {
                            addActionItemToNotes(idx: idx, text: text)
                        } label: {
                            Label("+ to notes", systemImage: "plus")
                                .font(.caption2)
                        }
                        .buttonStyle(.borderless)
                    }
                }
            }
        }
    }

    private func formattedTime(_ iso: String) -> String {
        let f = ISO8601DateFormatter()
        guard let date = f.date(from: iso) else { return iso }
        let rel = RelativeDateTimeFormatter()
        rel.unitsStyle = .abbreviated
        return rel.localizedString(for: date, relativeTo: Date())
    }

    private func addActionItemToNotes(idx: Int, text: String) {
        guard let db = appState.databaseManager else { return }
        do {
            let nextSort = (freeformNotes.last?.sortOrder ?? -1) + 1
            _ = try db.dbPool.write { dbConn in
                try MeetingNoteQueries.create(
                    dbConn,
                    eventID: eventID,
                    type: .note,
                    text: text,
                    sortOrder: nextSort
                )
            }
            addedActionItems.insert(idx)
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // MARK: - Data Operations

    private func loadNotes() {
        guard let db = appState.databaseManager else { return }
        do {
            notes = try db.dbPool.read { dbConn in
                try MeetingNoteQueries.fetchForEvent(dbConn, eventID: eventID)
            }
            recap = try? db.dbPool.read { dbConn in
                try MeetingRecapQueries.fetch(dbConn, eventID: eventID)
            }
            addedActionItems = []
        } catch {
            // Silent: table may not exist yet on older DB schema versions
        }
    }

    private func addQuestion() {
        let trimmed = newQuestionText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        guard let db = appState.databaseManager else { return }
        do {
            let sortOrder = (questions.last?.sortOrder ?? -1) + 1
            _ = try db.dbPool.write { dbConn in
                try MeetingNoteQueries.create(
                    dbConn, eventID: eventID, type: .question,
                    text: trimmed, sortOrder: sortOrder
                )
            }
            newQuestionText = ""
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func addNote() {
        let trimmed = newNoteText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        guard let db = appState.databaseManager else { return }
        do {
            let sortOrder = (freeformNotes.last?.sortOrder ?? -1) + 1
            _ = try db.dbPool.write { dbConn in
                try MeetingNoteQueries.create(
                    dbConn, eventID: eventID, type: .note,
                    text: trimmed, sortOrder: sortOrder
                )
            }
            newNoteText = ""
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func toggleChecked(_ note: MeetingNote) {
        guard let id = note.id else { return }
        guard let db = appState.databaseManager else { return }
        do {
            try db.dbPool.write { dbConn in
                try MeetingNoteQueries.toggleChecked(dbConn, id: id)
            }
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func deleteNote(_ note: MeetingNote) {
        guard let id = note.id else { return }
        guard let db = appState.databaseManager else { return }
        do {
            try db.dbPool.write { dbConn in
                try MeetingNoteQueries.delete(dbConn, id: id)
            }
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func createTask(from note: MeetingNote) {
        guard let noteID = note.id else { return }
        guard let db = appState.databaseManager else { return }

        creatingTaskForID = noteID
        do {
            let taskID = try db.dbPool.write { dbConn in
                let today = TargetQueries.todayDateString()
                let id = try TargetQueries.create(
                    dbConn,
                    text: note.text,
                    level: "day",
                    periodStart: today,
                    periodEnd: today,
                    sourceType: "manual",
                    sourceID: "meeting_note:\(noteID)"
                )
                try MeetingNoteQueries.setTaskID(dbConn, noteID: noteID, taskID: Int64(id))
                return id
            }
            _ = taskID
            loadNotes()
        } catch {
            errorMessage = error.localizedDescription
        }
        creatingTaskForID = nil
    }
}
