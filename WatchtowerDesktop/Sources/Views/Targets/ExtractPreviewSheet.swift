import SwiftUI

// MARK: - Proposed Target (input model for the preview)

struct ProposedTarget: Identifiable {
    var id = UUID()
    var text: String
    var intent: String
    var level: String           // "quarter","month","week","day","custom"
    var customLabel: String
    var levelConfidence: Double? // AI-assigned confidence (0-1); nil for manual
    var periodStart: String      // YYYY-MM-DD
    var periodEnd: String        // YYYY-MM-DD
    var priority: String         // "high","medium","low"
    var parentId: Int?
    var secondaryLinks: [ProposedLink]
    var isSelected: Bool = true
}

struct ProposedLink: Identifiable {
    var id = UUID()
    var targetId: Int?
    var externalRef: String
    var relation: String
}

// MARK: - ExtractPreviewSheet

/// Preview sheet for extracted targets (US-002 AI pipeline).
/// Accepts a pre-built list of `ProposedTarget` items so the sheet can be
/// unit-tested in isolation. Wire to the US-002 extractor when that story ships.
struct ExtractPreviewSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    @State private var items: [ProposedTarget]
    let onCreateSelected: ([ProposedTarget]) -> Void

    // Footer info (set by US-002 extractor caller)
    var omittedCount: Int = 0
    var notes: String = ""

    init(
        proposed: [ProposedTarget],
        omittedCount: Int = 0,
        notes: String = "",
        onCreateSelected: @escaping ([ProposedTarget]) -> Void
    ) {
        _items = State(initialValue: proposed)
        self.omittedCount = omittedCount
        self.notes = notes
        self.onCreateSelected = onCreateSelected
    }

    private let dateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    var selectedItems: [ProposedTarget] { items.filter(\.isSelected) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if items.isEmpty {
                emptyState
            } else {
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach($items) { $item in
                            ProposedTargetCard(item: $item)
                        }
                    }
                    .padding()
                }
            }
            Divider()
            footer
        }
        .frame(width: 620, height: 640)
    }

    // MARK: - Header

    private var header: some View {
        HStack {
            Text("Extracted Targets")
                .font(.headline)
            Text("(\(items.count) found)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "sparkles")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("No targets extracted")
                .font(.headline)
                .foregroundStyle(.secondary)
            Text("Paste text with action items and click 'Paste and extract'.")
                .font(.callout)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    // MARK: - Footer

    private var footer: some View {
        VStack(spacing: 6) {
            if omittedCount > 0 {
                Text("AI omitted \(omittedCount) more item\(omittedCount == 1 ? "" : "s").")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !notes.isEmpty {
                Text(notes)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
            }
            HStack {
                Text("\(selectedItems.count) of \(items.count) selected")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Create selected") {
                    createSelected()
                }
                .buttonStyle(.borderedProminent)
                .disabled(selectedItems.isEmpty)
                .keyboardShortcut(.defaultAction)
            }
            .padding()
        }
    }

    // MARK: - Create

    private func createSelected() {
        guard let db = appState.databaseManager else { return }
        let toCreate = selectedItems
        do {
            try db.dbPool.write { dbConn in
                for item in toCreate {
                    let today = dateFormatter.string(from: Date())
                    let start = item.periodStart.isEmpty ? today : item.periodStart
                    let end = item.periodEnd.isEmpty ? today : item.periodEnd
                    let newID = try TargetQueries.create(
                        dbConn,
                        text: item.text,
                        intent: item.intent,
                        level: item.level,
                        customLabel: item.customLabel,
                        periodStart: start,
                        periodEnd: end,
                        parentId: item.parentId,
                        priority: item.priority,
                        sourceType: "extract",
                        aiLevelConfidence: item.levelConfidence
                    )
                    for link in item.secondaryLinks {
                        if link.targetId != nil || !link.externalRef.isEmpty {
                            try dbConn.execute(
                                sql: """
                                    INSERT OR IGNORE INTO target_links
                                        (source_target_id, target_target_id, external_ref, relation, created_by)
                                    VALUES (?, ?, ?, ?, 'ai')
                                    """,
                                arguments: [newID, link.targetId, link.externalRef, link.relation]
                            )
                        }
                    }
                }
            }
        } catch {
            // Surface error if needed — for now proceed to dismiss
        }
        onCreateSelected(toCreate)
        dismiss()
    }
}

// MARK: - ProposedTargetCard

private struct ProposedTargetCard: View {
    @Binding var item: ProposedTarget

    private let levels = ["quarter", "month", "week", "day", "custom"]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Checkbox + text
            HStack(alignment: .top, spacing: 8) {
                Toggle("", isOn: $item.isSelected)
                    .labelsHidden()
                    .toggleStyle(.checkbox)

                VStack(alignment: .leading, spacing: 6) {
                    TextField("Target text", text: $item.text, axis: .vertical)
                        .font(.callout)
                        .fontWeight(.semibold)
                        .textFieldStyle(.roundedBorder)
                        .lineLimit(1...3)

                    if !item.intent.isEmpty || item.isSelected {
                        TextField("Intent (optional)", text: $item.intent)
                            .font(.caption)
                            .textFieldStyle(.roundedBorder)
                            .foregroundStyle(.secondary)
                    }
                }
            }

            // Level picker with AI confidence highlight
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 4) {
                        Text("Level")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if let conf = item.levelConfidence {
                            Text("AI guess · \(Int(conf * 100))%")
                                .font(.caption2)
                                .foregroundStyle(.yellow)
                        }
                    }
                    Picker("Level", selection: $item.level) {
                        ForEach(levels, id: \.self) { lvl in
                            Text(lvl.capitalized).tag(lvl)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                    .fixedSize()
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text("Priority")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Picker("Priority", selection: $item.priority) {
                        Text("High").tag("high")
                        Text("Medium").tag("medium")
                        Text("Low").tag("low")
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                    .fixedSize()
                }

                Spacer()

                VStack(alignment: .trailing, spacing: 2) {
                    Text("Period")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text("\(item.periodStart) – \(item.periodEnd)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }

            // Secondary links chip list
            if !item.secondaryLinks.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach($item.secondaryLinks) { $link in
                            HStack(spacing: 4) {
                                Text(link.relation.replacingOccurrences(of: "_", with: " "))
                                    .font(.caption2)
                                if let tid = link.targetId {
                                    Text("#\(tid)")
                                        .font(.caption2)
                                        .fontWeight(.semibold)
                                } else if !link.externalRef.isEmpty {
                                    Text(link.externalRef)
                                        .font(.caption2)
                                        .fontWeight(.semibold)
                                }
                                Button {
                                    item.secondaryLinks.removeAll { $0.id == link.id }
                                } label: {
                                    Image(systemName: "xmark")
                                        .font(.system(size: 8))
                                }
                                .buttonStyle(.plain)
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(.blue.opacity(0.1), in: Capsule())
                        }
                    }
                }
            }
        }
        .padding(12)
        .background(
            item.isSelected
                ? Color.accentColor.opacity(0.05)
                : Color(nsColor: .controlBackgroundColor),
            in: RoundedRectangle(cornerRadius: 10)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .strokeBorder(
                    item.isSelected ? Color.accentColor.opacity(0.3) : Color.clear,
                    lineWidth: 1
                )
        )
        .opacity(item.isSelected ? 1 : 0.6)
    }
}
