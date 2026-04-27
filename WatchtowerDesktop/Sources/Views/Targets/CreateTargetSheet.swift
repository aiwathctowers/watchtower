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
    @State private var subItems: [TargetSubItem] = []
    @State private var newSubItemText: String = ""
    @State private var errorMessage: String?
    @State private var showExtractSheet = false
    @State private var extractedResult: TargetExtractResult?
    @State private var isExtracting = false
    @State private var showMoreOptions: Bool = false
    @State private var showChecklist: Bool = false
    /// Indices into `subItems` that the user marked to be promoted into
    /// standalone child targets right after the parent target is created.
    /// Indices are kept in sync with `subItems` mutations (see `removeSubItem`).
    @State private var pendingPromotions: Set<Int> = []
    @State private var isCreating: Bool = false

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
        .frame(width: 520, height: 480)
        .onAppear {
            text = prefillText
            intent = prefillIntent
            if !prefillIntent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                showMoreOptions = true
            }
            if !subItems.isEmpty {
                showChecklist = true
            }
        }
        .sheet(isPresented: $showExtractSheet) {
            if let result = extractedResult {
                ExtractPreviewSheet(
                    proposed: result.extracted,
                    omittedCount: result.omittedCount,
                    notes: result.notes,
                    onCreateSelected: { _ in
                        dismiss()
                    }
                )
            }
        }
    }

    private var sheetHeader: some View {
        HStack {
            Text("New Target")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .keyboardShortcut(.cancelAction)
        }
        .padding(.horizontal)
        .padding(.vertical, 12)
    }

    private var formContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                textFieldWithAI
                levelPriorityRow
                customPeriodRow
                checklistSection
                moreOptionsSection
                sourceInfo
                errorRow
            }
            .padding()
        }
    }

    private var textFieldWithAI: some View {
        ZStack(alignment: .topLeading) {
            if text.isEmpty {
                Text("What's the goal? Paste a message or write your own…")
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 10)
                    .allowsHitTesting(false)
            }
            TextEditor(text: $text)
                .font(.body)
                .scrollContentBackground(.hidden)
                .padding(6)
                .frame(minHeight: 56, maxHeight: 180)

            if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                HStack {
                    Spacer()
                    Button {
                        Task { await runExtract() }
                    } label: {
                        if isExtracting {
                            HStack(spacing: 4) {
                                ProgressView().controlSize(.small)
                                Text("Extracting…").font(.caption)
                            }
                        } else {
                            Label("Extract with AI", systemImage: "sparkles")
                                .font(.caption)
                                .labelStyle(.titleAndIcon)
                        }
                    }
                    .buttonStyle(.borderless)
                    .disabled(isExtracting)
                    .padding(6)
                }
            }
        }
        .background(Color(nsColor: .textBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var levelPriorityRow: some View {
        HStack(alignment: .center, spacing: 12) {
            Picker("Level", selection: $level) {
                Text("Quarter").tag("quarter")
                Text("Month").tag("month")
                Text("Week").tag("week")
                Text("Day").tag("day")
                Text("Custom").tag("custom")
            }
            .labelsHidden()
            .pickerStyle(.segmented)
            .frame(maxWidth: .infinity)

            Picker("Priority", selection: $priority) {
                Text("High").tag("high")
                Text("Med").tag("medium")
                Text("Low").tag("low")
            }
            .labelsHidden()
            .pickerStyle(.segmented)
            .frame(width: 160)
        }
    }

    @ViewBuilder
    private var customPeriodRow: some View {
        if level == "custom" {
            HStack(spacing: 8) {
                DatePicker("Start", selection: $periodStart, displayedComponents: .date)
                    .labelsHidden()
                Text("→").foregroundStyle(.secondary)
                DatePicker("End", selection: $periodEnd, displayedComponents: .date)
                    .labelsHidden()
                Spacer()
            }
            .font(.callout)
        }
    }

    @ViewBuilder
    private var checklistSection: some View {
        if subItems.isEmpty && !showChecklist {
            Button {
                withAnimation { showChecklist = true }
            } label: {
                Label("Add checklist", systemImage: "plus.circle")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
        } else {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(subItems.enumerated()), id: \.offset) { index, item in
                    HStack(spacing: 8) {
                        Image(systemName: "circle")
                            .foregroundStyle(.secondary)
                            .font(.caption)
                        Text(item.text)
                            .font(.callout)
                        Spacer()
                        Button {
                            togglePromote(at: index)
                        } label: {
                            Image(systemName: pendingPromotions.contains(index)
                                  ? "arrow.up.right.square.fill"
                                  : "arrow.up.right.square")
                                .foregroundStyle(pendingPromotions.contains(index)
                                                 ? Color.accentColor
                                                 : .secondary)
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                        .help(pendingPromotions.contains(index)
                              ? "Will become a sub-target"
                              : "Promote to sub-target on save")
                        Button {
                            removeSubItem(at: index)
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.tertiary)
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                    }
                }
                HStack(spacing: 8) {
                    Image(systemName: "plus.circle")
                        .foregroundStyle(.secondary)
                        .font(.caption)
                    TextField("Add checklist item…", text: $newSubItemText)
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
    }

    private var moreOptionsSection: some View {
        DisclosureGroup(isExpanded: $showMoreOptions) {
            ZStack(alignment: .topLeading) {
                if intent.isEmpty {
                    Text("Why does this matter?")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 10)
                        .allowsHitTesting(false)
                }
                TextEditor(text: $intent)
                    .font(.body)
                    .scrollContentBackground(.hidden)
                    .padding(6)
                    .frame(minHeight: 50, maxHeight: 110)
            }
            .background(Color(nsColor: .textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .padding(.top, 6)
        } label: {
            Text("Add context")
                .font(.callout)
                .foregroundStyle(.secondary)
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
            if !pendingPromotions.isEmpty {
                Text("\(pendingPromotions.count) checklist item\(pendingPromotions.count == 1 ? "" : "s") will become sub-target\(pendingPromotions.count == 1 ? "" : "s")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                Task { await createTargetAndPromote() }
            } label: {
                if isCreating {
                    HStack(spacing: 4) {
                        ProgressView().controlSize(.small)
                        Text("Creating…")
                    }
                } else {
                    Text("Create")
                }
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
            .disabled(isCreating || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding(.horizontal)
        .padding(.vertical, 12)
    }

    private var sourceIcon: String {
        switch prefillSourceType {
        case "track": return "binoculars"
        case "digest": return "doc.text.magnifyingglass"
        case "briefing": return "sun.max"
        default: return "square.and.pencil"
        }
    }

    /// Toggles the "promote on save" mark for the sub-item at `index`.
    private func togglePromote(at index: Int) {
        if pendingPromotions.contains(index) {
            pendingPromotions.remove(index)
        } else {
            pendingPromotions.insert(index)
        }
    }

    /// Removes a sub-item and shifts pending-promotion indices so they keep
    /// pointing at the same items after the removal.
    private func removeSubItem(at index: Int) {
        subItems.remove(at: index)
        var rebuilt: Set<Int> = []
        for i in pendingPromotions where i != index {
            rebuilt.insert(i < index ? i : i - 1)
        }
        pendingPromotions = rebuilt
    }

    private func createTargetAndPromote() async {
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }

        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }

        isCreating = true
        errorMessage = nil
        defer { isCreating = false }

        let today = dateFormatter.string(from: Date())
        let useCustom = level == "custom"
        let start = useCustom ? dateFormatter.string(from: periodStart) : today
        let end = useCustom ? dateFormatter.string(from: periodEnd) : today

        let subItemsJSON: String
        if subItems.isEmpty {
            subItemsJSON = "[]"
        } else if let data = try? JSONEncoder().encode(subItems),
                  let json = String(data: data, encoding: .utf8) {
            subItemsJSON = json
        } else {
            subItemsJSON = "[]"
        }

        // Snapshot @MainActor-isolated values into Sendable locals before
        // they're captured by the async write closure.
        let intentCopy = intent.trimmingCharacters(in: .whitespacesAndNewlines)
        let levelCopy = level
        let priorityCopy = priority
        let sourceTypeCopy = prefillSourceType
        let sourceIDCopy = prefillSourceID

        // 1. Insert the parent target.
        let newID: Int
        do {
            newID = try await db.dbPool.write { dbConn -> Int in
                try TargetQueries.create(
                    dbConn,
                    text: trimmed,
                    intent: intentCopy,
                    level: levelCopy,
                    periodStart: start,
                    periodEnd: end,
                    priority: priorityCopy,
                    subItems: subItemsJSON,
                    sourceType: sourceTypeCopy,
                    sourceID: sourceIDCopy
                )
            }
        } catch {
            errorMessage = error.localizedDescription
            return
        }

        // 2. If the user marked any sub-items for promotion, delegate to the
        //    canonical batch-promote on TargetsViewModel — single source of
        //    truth for the descending-index contract.
        if !pendingPromotions.isEmpty {
            let vm = TargetsViewModel(dbManager: db)
            let items = pendingPromotions.map { (index: $0, overrides: PromoteSubItemOverrides()) }
            do {
                try await vm.promoteSubItemsAfterCreate(parentID: newID, items: items)
            } catch {
                // Parent persisted; surface the partial failure and keep the
                // sheet open so the user can retry or close manually.
                errorMessage = "Target created but some sub-items failed to promote: \(error.localizedDescription)"
                return
            }
        }

        dismiss()
    }

    private func runExtract() async {
        guard let runner = ProcessCLIRunner.makeDefault() else {
            errorMessage = "watchtower CLI not found in PATH"
            return
        }
        isExtracting = true
        errorMessage = nil
        defer { isExtracting = false }
        do {
            let service = TargetExtractService(runner: runner)
            let result = try await service.extract(text: text)
            if result.extracted.isEmpty {
                errorMessage = "AI returned no extracted targets"
                return
            }
            extractedResult = result
            showExtractSheet = true
        } catch {
            errorMessage = "Extract failed: \(error.localizedDescription)"
        }
    }
}
