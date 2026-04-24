import SwiftUI

struct CreateTargetSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    var prefillText: String = ""
    var prefillIntent: String = ""
    var prefillSourceType: String = "manual"
    var prefillSourceID: String = ""

    @State private var text: String = ""
    @State private var intent: String = ""
    @State private var level: String = "day"
    @State private var priority: String = "medium"
    @State private var periodStart: Date = Date()
    @State private var periodEnd: Date = Date()
    @State private var hasPeriod: Bool = false
    @State private var subItems: [TargetSubItem] = []
    @State private var newSubItemText: String = ""
    @State private var errorMessage: String?
    // V1: hidden — pending Swift→Go CLI bridge (see spec "Out of Scope V2")
    // @State private var showExtractSheet = false
    // @State private var highlightExtract: Bool = false

    private let dateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    var body: some View {
        VStack(spacing: 0) {
            sheetHeader
            Divider()
            formContent
            Divider()
            sheetFooter
        }
        .frame(width: 500, height: 580)
        .onAppear {
            text = prefillText
            intent = prefillIntent
        }
    }

    private var sheetHeader: some View {
        HStack {
            Text("New Target")
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
                // V1: "Paste and extract" button hidden — pending Swift→Go CLI bridge (see spec "Out of Scope V2")
                intentField
                levelRow
                priorityRow
                periodRow
                checklistSection
                sourceInfo
                errorRow
            }
            .padding()
        }
    }

    private var textField: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("What is the goal?")
                .font(.subheadline)
                .fontWeight(.medium)
            TextField("Target description", text: $text, axis: .vertical)
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

    private var levelRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Level")
                .font(.subheadline)
                .fontWeight(.medium)
            Picker("Level", selection: $level) {
                Text("Quarter").tag("quarter")
                Text("Month").tag("month")
                Text("Week").tag("week")
                Text("Day").tag("day")
                Text("Custom").tag("custom")
            }
            .labelsHidden()
            .pickerStyle(.segmented)
        }
    }

    private var priorityRow: some View {
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
    }

    private var periodRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            Toggle("Custom period", isOn: $hasPeriod)
                .font(.subheadline)
                .fontWeight(.medium)
            if hasPeriod {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Start").font(.caption).foregroundStyle(.secondary)
                        DatePicker("", selection: $periodStart, displayedComponents: .date)
                            .labelsHidden()
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        Text("End").font(.caption).foregroundStyle(.secondary)
                        DatePicker("", selection: $periodEnd, displayedComponents: .date)
                            .labelsHidden()
                    }
                }
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
                            subItems.append(TargetSubItem(text: trimmed, done: false))
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
                createTarget()
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

    // MARK: - Create

    private func createTarget() {
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }

        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }

        let today = dateFormatter.string(from: Date())
        let start = hasPeriod ? dateFormatter.string(from: periodStart) : today
        let end = hasPeriod ? dateFormatter.string(from: periodEnd) : today

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
                try TargetQueries.create(
                    dbConn,
                    text: trimmed,
                    intent: intent.trimmingCharacters(in: .whitespacesAndNewlines),
                    level: level,
                    periodStart: start,
                    periodEnd: end,
                    priority: priority,
                    subItems: subItemsJSON,
                    sourceType: prefillSourceType,
                    sourceID: prefillSourceID
                )
            }
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
