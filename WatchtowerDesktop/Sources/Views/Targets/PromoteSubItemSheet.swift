import SwiftUI

/// Sheet that lets the user pick optional overrides for a sub-item being
/// promoted to a standalone child target. Defaults match the parent target
/// (level/priority/period/ownership) and the sub-item itself (text/due_date).
/// Hitting "Convert" calls the ViewModel which forwards to the Go CLI.
struct PromoteSubItemSheet: View {
    @Environment(\.dismiss) private var dismiss

    let parent: Target
    let subItem: TargetSubItem
    let subItemIndex: Int
    let viewModel: TargetsViewModel

    @State private var text: String
    @State private var level: String
    @State private var priority: String
    @State private var ownership: String
    @State private var hasDueDate: Bool
    @State private var dueDate: Date
    @State private var isPromoting = false
    @State private var errorMessage: String?

    private static let dateOnlyFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    init(parent: Target, subItem: TargetSubItem, subItemIndex: Int, viewModel: TargetsViewModel) {
        self.parent = parent
        self.subItem = subItem
        self.subItemIndex = subItemIndex
        self.viewModel = viewModel
        _text = State(initialValue: subItem.text)
        _level = State(initialValue: parent.level)
        _priority = State(initialValue: parent.priority)
        _ownership = State(initialValue: parent.ownership)

        let inheritedDueRaw = subItem.dueDate?.isEmpty == false ? subItem.dueDate : (parent.dueDate.isEmpty ? nil : parent.dueDate)
        _hasDueDate = State(initialValue: inheritedDueRaw != nil)
        _dueDate = State(initialValue: Target.parseDueDate(inheritedDueRaw ?? "") ?? Date())
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
        .frame(width: 460, height: 480)
    }

    // MARK: - Sections

    private var header: some View {
        HStack {
            Text("Convert to sub-target")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
                .disabled(isPromoting)
        }
        .padding()
    }

    @ViewBuilder
    private var form: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("From parent #\(parent.id) — \(parent.text)")
                .font(.caption)
                .foregroundStyle(.secondary)

            VStack(alignment: .leading, spacing: 4) {
                Text("Text").font(.subheadline).fontWeight(.medium)
                TextField("Sub-target text", text: $text)
                    .textFieldStyle(.roundedBorder)
            }

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

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
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
            .disabled(isPromoting || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding()
    }

    // MARK: - Action

    private func runPromote() async {
        isPromoting = true
        errorMessage = nil
        defer { isPromoting = false }

        var overrides = PromoteSubItemOverrides()
        let trimmedText = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedText != subItem.text {
            overrides.text = trimmedText
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
}
