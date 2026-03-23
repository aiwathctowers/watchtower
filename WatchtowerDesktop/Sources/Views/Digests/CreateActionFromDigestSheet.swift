import SwiftUI
import GRDB
import UserNotifications

private func encodeToJSON<T: Encodable>(_ value: T?) -> String? {
    guard let value else { return nil }
    guard let data = try? JSONEncoder().encode(value) else { return nil }
    return String(data: data, encoding: .utf8)
}

struct CreateActionFromDigestSheet: View {
    let digest: Digest
    let channelName: String?
    let dbManager: DatabaseManager

    @Environment(\.dismiss) private var dismiss
    @State private var userNote = ""

    private let claudeService = ClaudeService()

    var body: some View {
        VStack(spacing: 16) {
            // Header
            HStack {
                Text("Create Action from Digest")
                    .font(.headline)
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(.borderless)
            }

            // Digest summary preview
            GroupBox {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(digest.type.capitalized)
                            .font(.caption)
                            .fontWeight(.semibold)
                            .foregroundStyle(.blue)
                        if let name = channelName {
                            Text("#\(name)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                    }
                    Text(digest.summary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                }
            }

            // User note input
            VStack(alignment: .leading, spacing: 4) {
                Text("Instructions (optional)")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                TextEditor(text: $userNote)
                    .font(.body)
                    .frame(height: 80)
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.secondary.opacity(0.3))
                    )
                Text("Describe what action you want to create, or leave empty to let AI decide.")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            Spacer()

            // Action button
            HStack {
                Spacer()
                Button {
                    startBackgroundGeneration()
                } label: {
                    Label("Create Action", systemImage: "sparkles")
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding()
        .frame(width: 480, height: 340)
    }

    private func startBackgroundGeneration() {
        let note = userNote.trimmingCharacters(in: .whitespacesAndNewlines)
        let capturedDigest = digest
        let capturedChannelName = channelName
        let capturedDB = dbManager
        let capturedService = claudeService

        // Close sheet immediately
        dismiss()

        // Generate in background
        Task.detached {
            await generateAndSave(
                service: capturedService,
                digest: capturedDigest,
                channelName: capturedChannelName,
                db: capturedDB,
                note: note
            )
        }
    }
}

private func generateAndSave(
    service: ClaudeService,
    digest: Digest,
    channelName: String?,
    db: DatabaseManager,
    note: String
) async {
    do {
        let generated: GeneratedActionItem = try await service.generateActionItem(
            from: digest,
            userNote: note.isEmpty ? nil : note,
            channelName: channelName
        )

        let currentUserID: String = try await db.dbPool.read { db in
            try ActionItemQueries.fetchCurrentUserID(db)
        } ?? ""

        let participantsJSON: String = encodeToJSON(generated.participants) ?? "[]"
        let sourceRefsJSON: String = encodeToJSON(generated.sourceRefs) ?? "[]"
        let tagsJSON: String = encodeToJSON(generated.tags) ?? "[]"
        let decisionOptionsJSON: String = encodeToJSON(generated.decisionOptions) ?? "[]"
        let subItemsJSON: String = encodeToJSON(generated.subItems) ?? "[]"
        let relatedDigestIDs: String = encodeToJSON([digest.id]) ?? "[]"

        let dueDateUnix: Double? = generated.dueDate.flatMap { dd in
            let fmt = DateFormatter()
            fmt.dateFormat = "yyyy-MM-dd"
            fmt.timeZone = TimeZone.current
            return fmt.date(from: dd)?.timeIntervalSince1970
        }

        let channelID: String = digest.channelID
        let srcChannelName: String = channelName ?? ""
        let text: String = generated.text
        let context: String = generated.context
        let priority: String = generated.priority
        let periodFrom: Double = digest.periodFrom
        let periodTo: Double = digest.periodTo
        let reqName: String = generated.requester?.name ?? ""
        let reqUID: String = generated.requester?.userID ?? ""
        let category: String = generated.category
        let blocking: String = generated.blocking ?? ""
        let decSummary: String = generated.decisionSummary ?? ""

        _ = try await db.dbPool.write { db in
            try ActionItemQueries.insertActionItem(
                db,
                channelID: channelID,
                assigneeUserID: currentUserID,
                assigneeRaw: "",
                text: text,
                context: context,
                sourceMessageTS: "",
                sourceChannelName: srcChannelName,
                priority: priority,
                dueDate: dueDateUnix,
                periodFrom: periodFrom,
                periodTo: periodTo,
                model: "claude-sonnet-4-6",
                inputTokens: 0,
                outputTokens: 0,
                costUSD: 0,
                participants: participantsJSON,
                sourceRefs: sourceRefsJSON,
                requesterName: reqName,
                requesterUserID: reqUID,
                category: category,
                blocking: blocking,
                tags: tagsJSON,
                decisionSummary: decSummary,
                decisionOptions: decisionOptionsJSON,
                relatedDigestIDs: relatedDigestIDs,
                subItems: subItemsJSON
            )
        }

        // Notify success
        let channel: String = channelName ?? "digest"
        NotificationService.shared.sendActionItemNotification(
            text: generated.text,
            channelName: channel,
            priority: generated.priority
        )
    } catch {
        // Notify error
        let content = UNMutableNotificationContent()
        content.title = "Action creation failed"
        content.body = error.localizedDescription
        content.sound = .default
        let request = UNNotificationRequest(
            identifier: "action-create-error-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        try? await UNUserNotificationCenter.current().add(request)
    }
}
