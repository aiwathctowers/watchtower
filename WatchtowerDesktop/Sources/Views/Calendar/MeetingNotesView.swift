import SwiftUI

struct MeetingNotesView: View {
    let eventID: String
    @Environment(AppState.self) private var appState

    @State private var notes: [MeetingNote] = []
    @State private var newQuestionText = ""
    @State private var newNoteText = ""
    @State private var editingNoteID: Int64?
    @State private var creatingTaskForID: Int64?
    @State private var errorMessage: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let error = errorMessage {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal)
            }
            questionsSection
            notesSection
        }
        .onAppear { loadNotes() }
    }

    // MARK: - Questions Section

    private var questionsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 4) {
                Image(systemName: "checklist")
                    .foregroundStyle(.blue)
                Text("Discussion Topics")
                    .font(.headline)
            }
            .padding(.top, 4)

            ForEach(questions) { note in
                questionRow(note)
            }

            HStack(spacing: 6) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                    .font(.caption)
                TextField("Add a topic...", text: $newQuestionText)
                    .textFieldStyle(.plain)
                    .font(.callout)
                    .onSubmit { addQuestion() }
            }
            .padding(.vertical, 2)
        }
    }

    private func questionRow(_ note: MeetingNote) -> some View {
        HStack(spacing: 6) {
            Button {
                toggleChecked(note)
            } label: {
                Image(systemName: note.isChecked ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(note.isChecked ? .green : .secondary)
                    .font(.callout)
            }
            .buttonStyle(.plain)

            Text(note.text)
                .font(.callout)
                .strikethrough(note.isChecked)
                .foregroundStyle(note.isChecked ? .secondary : .primary)

            Spacer()

            noteActions(note)
        }
        .padding(.vertical, 2)
    }

    // MARK: - Notes Section

    private var notesSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 4) {
                Image(systemName: "note.text")
                    .foregroundStyle(.blue)
                Text("Meeting Notes")
                    .font(.headline)
            }
            .padding(.top, 4)

            ForEach(freeformNotes) { note in
                noteRow(note)
            }

            HStack(alignment: .top, spacing: 6) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                    .font(.caption)
                    .padding(.top, 4)
                TextField("Add a note...", text: $newNoteText, axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(.callout)
                    .lineLimit(1...4)
                    .onSubmit { addNote() }
            }
            .padding(.vertical, 2)
        }
    }

    private func noteRow(_ note: MeetingNote) -> some View {
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: "circle.fill")
                .font(.system(size: 4))
                .padding(.top, 7)
                .foregroundStyle(.secondary)

            Text(note.text)
                .font(.callout)
                .frame(maxWidth: .infinity, alignment: .leading)

            noteActions(note)
        }
        .padding(.vertical, 2)
    }

    // MARK: - Actions

    private func noteActions(_ note: MeetingNote) -> some View {
        HStack(spacing: 4) {
            if let taskID = note.taskID {
                Label("Task #\(taskID)", systemImage: "checkmark.circle.fill")
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

    // MARK: - Data Operations

    private func loadNotes() {
        guard let db = appState.databaseManager else { return }
        do {
            notes = try db.dbPool.read { dbConn in
                try MeetingNoteQueries.fetchForEvent(dbConn, eventID: eventID)
            }
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
