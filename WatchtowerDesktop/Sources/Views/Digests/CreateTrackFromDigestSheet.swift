import SwiftUI
import GRDB
import UserNotifications

private func encodeToJSON<T: Encodable>(_ value: T?) -> String? {
    guard let value else { return nil }
    guard let data = try? JSONEncoder().encode(value) else { return nil }
    return String(data: data, encoding: .utf8)
}

struct CreateTrackFromDigestSheet: View {
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
                Text("Create Track from Digest")
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
                Text("Describe what track you want to create, or leave empty to let AI decide.")
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
                    Label("Create Track", systemImage: "sparkles")
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
        let generated: GeneratedTrack = try await service.generateTrack(
            from: digest,
            userNote: note.isEmpty ? nil : note,
            channelName: channelName
        )

        let currentUserID: String = try await db.dbPool.read { db in
            try TrackQueries.fetchCurrentUserID(db)
        } ?? ""

        try await persistTrack(
            generated: generated,
            digest: digest,
            channelName: channelName,
            currentUserID: currentUserID,
            db: db
        )

        NotificationService.shared.sendTrackNotification(
            text: generated.text,
            channelName: channelName ?? "digest",
            priority: generated.priority
        )
    } catch {
        let content = UNMutableNotificationContent()
        content.title = "Track creation failed"
        content.body = error.localizedDescription
        content.sound = .default
        let request = UNNotificationRequest(
            identifier: "track-create-error-\(Int(Date().timeIntervalSince1970))",
            content: content,
            trigger: nil
        )
        try? await UNUserNotificationCenter.current().add(request)
    }
}

private func persistTrack(
    generated: GeneratedTrack,
    digest: Digest,
    channelName: String?,
    currentUserID: String,
    db: DatabaseManager
) async throws {
    let dueDateUnix: Double? = generated.dueDate.flatMap { dd in
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.timeZone = TimeZone.current
        return fmt.date(from: dd)?.timeIntervalSince1970
    }

    let data = TrackInsertData(
        channelID: digest.channelID,
        assigneeUserID: currentUserID,
        text: generated.text,
        context: generated.context,
        sourceChannelName: channelName ?? "",
        priority: generated.priority,
        dueDate: dueDateUnix,
        periodFrom: digest.periodFrom,
        periodTo: digest.periodTo,
        model: "claude",
        participants: encodeToJSON(generated.participants) ?? "[]",
        sourceRefs: encodeToJSON(generated.sourceRefs) ?? "[]",
        requesterName: generated.requester?.name ?? "",
        requesterUserID: generated.requester?.userID ?? "",
        category: generated.category,
        blocking: generated.blocking ?? "",
        tags: encodeToJSON(generated.tags) ?? "[]",
        decisionSummary: generated.decisionSummary ?? "",
        decisionOptions: encodeToJSON(generated.decisionOptions) ?? "[]",
        relatedDigestIDs: encodeToJSON([digest.id]) ?? "[]",
        subItems: encodeToJSON(generated.subItems) ?? "[]"
    )

    _ = try await db.dbPool.write { db in
        try TrackQueries.insertTrack(db, data: data)
    }
}
