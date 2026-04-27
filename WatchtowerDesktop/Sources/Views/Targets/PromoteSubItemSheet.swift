import SwiftUI

/// Sheet that lets the user pick optional overrides for a sub-item being
/// promoted to a standalone child target. Defaults match the parent target
/// (level/priority/period/ownership) and the sub-item itself (text/due_date).
/// Hitting "Convert" calls the ViewModel which forwards to the Go CLI.
///
/// "Refine with AI" reuses `targets extract --json` (same as CreateTargetSheet)
/// to normalize the user's text into structured fields. The first proposed
/// target wins — extra ones (rare for a single sub-item) go into `errorMessage`
/// as a non-fatal hint so the user is aware their paste was multi-target.
struct PromoteSubItemSheet: View {
    @Environment(\.dismiss) private var dismiss

    let parent: Target
    let subItem: TargetSubItem
    let subItemIndex: Int
    let viewModel: TargetsViewModel
    /// Optional injected runner for tests; production uses ProcessCLIRunner.makeDefault().
    let cliRunner: CLIRunnerProtocol?

    @State private var text: String
    @State private var intent: String
    @State private var level: String
    @State private var priority: String
    @State private var ownership: String
    @State private var hasDueDate: Bool
    @State private var dueDate: Date
    @State private var isPromoting = false
    @State private var isRefining = false
    @State private var showIntent: Bool
    @State private var errorMessage: String?

    init(
        parent: Target,
        subItem: TargetSubItem,
        subItemIndex: Int,
        viewModel: TargetsViewModel,
        cliRunner: CLIRunnerProtocol? = nil
    ) {
        self.parent = parent
        self.subItem = subItem
        self.subItemIndex = subItemIndex
        self.viewModel = viewModel
        self.cliRunner = cliRunner
        _text = State(initialValue: subItem.text)
        _intent = State(initialValue: parent.intent)
        _level = State(initialValue: parent.level)
        _priority = State(initialValue: parent.priority)
        _ownership = State(initialValue: parent.ownership)

        let inheritedDueRaw = subItem.dueDate?.isEmpty == false ? subItem.dueDate : (parent.dueDate.isEmpty ? nil : parent.dueDate)
        _hasDueDate = State(initialValue: inheritedDueRaw != nil)
        _dueDate = State(initialValue: Target.parseDueDate(inheritedDueRaw ?? "") ?? Date())
        _showIntent = State(initialValue: !parent.intent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ScrollView {
                form.padding()
            }
            Divider()
            footer
        }
        // Match CreateTargetSheet width so the two sheets in the same flow look consistent.
        .frame(width: 520, height: 520)
    }

    // MARK: - Sections

    private var header: some View {
        HStack {
            Text("Convert to sub-target")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .keyboardShortcut(.cancelAction)
                .disabled(isPromoting)
        }
        .padding(.horizontal)
        .padding(.vertical, 12)
    }

    @ViewBuilder
    private var form: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("From parent #\(parent.id) — \(parent.text)")
                .font(.caption)
                .foregroundStyle(.secondary)

            textFieldWithAI

            VStack(alignment: .leading, spacing: 4) {
                Text("Level").font(.subheadline).fontWeight(.medium)
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

            VStack(alignment: .leading, spacing: 4) {
                Text("Priority").font(.subheadline).fontWeight(.medium)
                Picker("Priority", selection: $priority) {
                    Text("High").tag("high")
                    Text("Medium").tag("medium")
                    Text("Low").tag("low")
                }
                .labelsHidden()
                .pickerStyle(.segmented)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Ownership").font(.subheadline).fontWeight(.medium)
                Picker("Ownership", selection: $ownership) {
                    Text("Mine").tag("mine")
                    Text("Delegated").tag("delegated")
                    Text("Watching").tag("watching")
                }
                .labelsHidden()
                .pickerStyle(.segmented)
            }

            VStack(alignment: .leading, spacing: 4) {
                Toggle("Due date", isOn: $hasDueDate)
                    .font(.subheadline)
                    .fontWeight(.medium)
                if hasDueDate {
                    DatePicker(
                        "",
                        selection: $dueDate,
                        displayedComponents: [.date, .hourAndMinute]
                    )
                    .labelsHidden()
                }
            }

            intentSection

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
    }

    /// Mirrors CreateTargetSheet's textFieldWithAI: a multi-line editor with a
    /// floating "Refine with AI" button at the bottom-right.
    private var textFieldWithAI: some View {
        ZStack(alignment: .topLeading) {
            if text.isEmpty {
                Text("Sub-target text — paste a goal or write your own…")
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 10)
                    .allowsHitTesting(false)
            }
            TextEditor(text: $text)
                .font(.body)
                .scrollContentBackground(.hidden)
                .padding(6)
                .frame(minHeight: 56, maxHeight: 160)

            if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                HStack {
                    Spacer()
                    Button {
                        Task { await runRefine() }
                    } label: {
                        if isRefining {
                            HStack(spacing: 4) {
                                ProgressView().controlSize(.small)
                                Text("Refining…").font(.caption)
                            }
                        } else {
                            Label("Refine with AI", systemImage: "sparkles")
                                .font(.caption)
                                .labelStyle(.titleAndIcon)
                        }
                    }
                    .buttonStyle(.borderless)
                    .disabled(isRefining || isPromoting)
                    .padding(6)
                }
            }
        }
        .background(Color(nsColor: .textBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var intentSection: some View {
        DisclosureGroup(isExpanded: $showIntent) {
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
            Text("Intent")
                .font(.subheadline)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)
        }
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button {
                Task { await runPromote() }
            } label: {
                if isPromoting {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.small)
                        Text("Converting…")
                    }
                } else {
                    Text("Convert")
                }
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
            .disabled(isPromoting || isRefining || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }

    // MARK: - Actions

    private func runPromote() async {
        isPromoting = true
        errorMessage = nil
        defer { isPromoting = false }

        var overrides = PromoteSubItemOverrides()
        let trimmedText = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedText != subItem.text {
            overrides.text = trimmedText
        }
        let trimmedIntent = intent.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedIntent != parent.intent {
            overrides.intent = trimmedIntent
        }
        if level != parent.level {
            overrides.level = level
        }
        if priority != parent.priority {
            overrides.priority = priority
        }
        if ownership != parent.ownership {
            overrides.ownership = ownership
        }
        if hasDueDate {
            overrides.dueDate = Target.formatDueDate(dueDate)
        } else if !(subItem.dueDate?.isEmpty ?? true) || !parent.dueDate.isEmpty {
            // User explicitly cleared an inherited due date.
            overrides.dueDate = ""
        }

        do {
            _ = try await viewModel.promoteSubItem(parent, index: subItemIndex, overrides: overrides)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    /// Runs `targets extract --json` against the current text, takes the first
    /// proposed target, and folds its structured fields into the sheet state.
    /// Extra proposed targets (uncommon for a single sub-item) are surfaced as
    /// a non-fatal hint via `errorMessage` instead of being silently dropped.
    private func runRefine() async {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        guard let runner = cliRunner ?? ProcessCLIRunner.makeDefault() else {
            errorMessage = "watchtower CLI not found in PATH"
            return
        }

        isRefining = true
        errorMessage = nil
        defer { isRefining = false }

        do {
            let svc = TargetExtractService(runner: runner)
            let result = try await svc.extract(text: trimmed)
            guard let first = result.extracted.first else {
                errorMessage = "AI returned no structured target — keep your text and edit fields manually"
                return
            }
            applyRefined(first)
            if result.extracted.count > 1 {
                errorMessage = "AI proposed \(result.extracted.count) targets; using the first. Use New Target to capture the rest."
            }
        } catch {
            errorMessage = "Refine failed: \(error.localizedDescription)"
        }
    }

    /// Folds AI-proposed fields into local @State, only overwriting fields the
    /// AI actually populated so user-typed values aren't blanked. due_date is
    /// not propagated because the existing extract Swift wire schema doesn't
    /// surface it — the user keeps whatever date they had pre-refine.
    private func applyRefined(_ p: ProposedTarget) {
        if !p.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            text = p.text
        }
        if !p.intent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            intent = p.intent
            showIntent = true
        }
        if ["quarter", "month", "week", "day", "custom"].contains(p.level) {
            level = p.level
        }
        if ["high", "medium", "low"].contains(p.priority) {
            priority = p.priority
        }
    }
}
