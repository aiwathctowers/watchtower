import SwiftUI

struct CreateTaskSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    var prefillText: String = ""
    var prefillIntent: String = ""
    var prefillSourceType: String = "manual"
    var prefillSourceID: String = ""

    @State private var text: String = ""
    @State private var intent: String = ""
    @State private var priority: String = "medium"
    @State private var ownership: String = "mine"
    @State private var dueDate: Date?
    @State private var hasDueDate: Bool = false
    @State private var subItems: [TaskSubItem] = []
    @State private var newSubItemText: String = ""
    @State private var errorMessage: String?

    var body: some View {
        VStack(spacing: 0) {
            sheetHeader
            Divider()
            formContent
            Divider()
            sheetFooter
        }
        .frame(width: 480, height: 520)
        .onAppear {
            text = prefillText
            intent = prefillIntent
        }
    }

    private var sheetHeader: some View {
        HStack {
            Text("New Task")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    private var formContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                textField
                intentField
                pickersRow
                dueDateRow
                checklistSection
                sourceInfo
                errorRow
            }
            .padding()
        }
    }

    private var textField: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("What needs to be done?")
                .font(.subheadline)
                .fontWeight(.medium)
            TextField("Task description", text: $text, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .lineLimit(1...4)
        }
    }

    private var intentField: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Why? (optional)")
                .font(.subheadline)
                .fontWeight(.medium)
            TextField("Context or intent", text: $intent)
                .textFieldStyle(.roundedBorder)
        }
    }

    private var pickersRow: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Priority")
                    .font(.subheadline)
                    .fontWeight(.medium)
                Picker("Priority", selection: $priority) {
                    Text("High").tag("high")
                    Text("Medium").tag("medium")
                    Text("Low").tag("low")
                }
                .labelsHidden()
                .pickerStyle(.segmented)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Ownership")
                    .font(.subheadline)
                    .fontWeight(.medium)
                Picker("Ownership", selection: $ownership) {
                    Text("Mine").tag("mine")
                    Text("Delegated").tag("delegated")
                    Text("Watching").tag("watching")
                }
                .labelsHidden()
                .pickerStyle(.segmented)
            }
        }
    }

    private var dueDateRow: some View {
        HStack {
            Toggle("Due date", isOn: $hasDueDate)
                .font(.subheadline)
                .fontWeight(.medium)
            if hasDueDate {
                DatePicker(
                    "",
                    selection: Binding(
                        get: { dueDate ?? Date() },
                        set: { dueDate = $0 }
                    ),
                    displayedComponents: .date
                )
                .labelsHidden()
            }
        }
    }

    private var checklistSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Checklist")
                .font(.subheadline)
                .fontWeight(.medium)

            ForEach(Array(subItems.enumerated()), id: \.offset) { index, item in
                HStack(spacing: 8) {
                    Image(systemName: "circle")
                        .foregroundStyle(.secondary)
                        .font(.caption)
                    Text(item.text)
                        .font(.callout)
                    Spacer()
                    Button {
                        subItems.remove(at: index)
                    } label: {
                        Image(systemName: "xmark.circle")
                            .foregroundStyle(.secondary)
                            .font(.caption)
                    }
                    .buttonStyle(.plain)
                }
            }

            HStack(spacing: 8) {
                Image(systemName: "plus.circle")
                    .foregroundStyle(.secondary)
                    .font(.caption)
                TextField("Add checklist item...", text: $newSubItemText)
                    .font(.callout)
                    .textFieldStyle(.plain)
                    .onSubmit {
                        let trimmed = newSubItemText.trimmingCharacters(in: .whitespacesAndNewlines)
                        if !trimmed.isEmpty {
                            subItems.append(TaskSubItem(text: trimmed, done: false))
                            newSubItemText = ""
                        }
                    }
            }
        }
    }

    @ViewBuilder
    private var sourceInfo: some View {
        if prefillSourceType != "manual" {
            HStack(spacing: 4) {
                Image(systemName: sourceIcon)
                    .foregroundStyle(.secondary)
                Text("From \(prefillSourceType) #\(prefillSourceID)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var errorRow: some View {
        if let errorMessage {
            Text(errorMessage)
                .font(.caption)
                .foregroundStyle(.red)
        }
    }

    private var sheetFooter: some View {
        HStack {
            Spacer()
            Button("Create") {
                createTask()
            }
            .keyboardShortcut(.defaultAction)
            .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }

    private var sourceIcon: String {
        switch prefillSourceType {
        case "track": return "binoculars"
        case "digest": return "doc.text.magnifyingglass"
        case "briefing": return "sun.max"
        default: return "square.and.pencil"
        }
    }

    private func createTask() {
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }

        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }

        let dueDateStr: String
        if hasDueDate, let dueDate {
            let fmt = DateFormatter()
            fmt.dateFormat = "yyyy-MM-dd"
            fmt.locale = Locale(identifier: "en_US_POSIX")
            dueDateStr = fmt.string(from: dueDate)
        } else {
            dueDateStr = ""
        }

        let subItemsJSON: String
        if subItems.isEmpty {
            subItemsJSON = "[]"
        } else if let data = try? JSONEncoder().encode(subItems),
                  let json = String(data: data, encoding: .utf8) {
            subItemsJSON = json
        } else {
            subItemsJSON = "[]"
        }

        do {
            _ = try db.dbPool.write { dbConn in
                try TaskQueries.create(
                    dbConn,
                    text: trimmed,
                    intent: intent.trimmingCharacters(in: .whitespacesAndNewlines),
                    priority: priority,
                    ownership: ownership,
                    dueDate: dueDateStr,
                    sourceType: prefillSourceType,
                    sourceID: prefillSourceID,
                    subItems: subItemsJSON
                )
            }
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
