import SwiftUI
import GRDB

/// Mini dialog that shows AI-proposed parent + secondary links, lets the user
/// deselect individual items, then applies the chosen set to the DB in a
/// single write transaction.
struct SuggestLinksSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    let targetID: Int
    @State var suggestions: SuggestedLinksResult

    @State private var applyParent: Bool = true
    @State private var selectedLinks: Set<Int> = []
    @State private var errorMessage: String?

    init(targetID: Int, suggestions: SuggestedLinksResult) {
        self.targetID = targetID
        self._suggestions = State(initialValue: suggestions)
        self._selectedLinks = State(initialValue: Set(suggestions.secondaryLinks.indices))
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ScrollView {
                content.padding()
            }
            Divider()
            footer
        }
        .frame(width: 480, height: 420)
    }

    private var header: some View {
        HStack {
            Text("Suggested links")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    @ViewBuilder
    private var content: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let parentID = suggestions.parentID {
                Toggle(isOn: $applyParent) {
                    Text("Set parent to target #\(parentID)")
                        .font(.callout)
                }
            }

            if !suggestions.secondaryLinks.isEmpty {
                Divider().padding(.vertical, 4)
                Text("Secondary links")
                    .font(.subheadline)
                    .fontWeight(.medium)
                ForEach(Array(suggestions.secondaryLinks.enumerated()), id: \.offset) { idx, link in
                    linkRow(idx: idx, link: link)
                }
            }

            if let errorMessage {
                Text(errorMessage)
                    .foregroundStyle(.red)
                    .font(.caption)
            }
        }
    }

    @ViewBuilder
    private func linkRow(idx: Int, link: ProposedLink) -> some View {
        HStack(spacing: 8) {
            Toggle("", isOn: Binding(
                get: { selectedLinks.contains(idx) },
                set: { on in
                    if on {
                        selectedLinks.insert(idx)
                    } else {
                        selectedLinks.remove(idx)
                    }
                }
            ))
            .labelsHidden()
            Text(link.relation.replacingOccurrences(of: "_", with: " "))
                .font(.caption)
            if let tid = link.targetId {
                Text("→ target #\(tid)")
                    .font(.caption)
                    .fontWeight(.semibold)
            } else if !link.externalRef.isEmpty {
                Text("→ \(link.externalRef)")
                    .font(.caption)
                    .fontWeight(.semibold)
            }
            Spacer()
        }
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button("Apply") { apply() }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(!hasAnythingToApply)
        }
        .padding()
    }

    private var hasAnythingToApply: Bool {
        (suggestions.parentID != nil && applyParent) || !selectedLinks.isEmpty
    }

    private func apply() {
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }
        do {
            try db.dbPool.write { dbConn in
                if applyParent, let parentID = suggestions.parentID {
                    try dbConn.execute(
                        sql: "UPDATE targets SET parent_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?",
                        arguments: [parentID, targetID]
                    )
                }
                for idx in selectedLinks.sorted() {
                    let link = suggestions.secondaryLinks[idx]
                    // target_links CHECK requires at least one of target_target_id / external_ref.
                    guard link.targetId != nil || !link.externalRef.isEmpty else { continue }
                    try dbConn.execute(
                        sql: """
                            INSERT OR IGNORE INTO target_links
                              (source_target_id, target_target_id, external_ref, relation, created_by)
                            VALUES (?, ?, ?, ?, 'ai')
                            """,
                        arguments: [targetID, link.targetId, link.externalRef, link.relation]
                    )
                }
            }
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
