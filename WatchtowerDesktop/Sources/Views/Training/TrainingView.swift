import SwiftUI
import GRDB

struct TrainingView: View {
    @Environment(AppState.self) private var appState
    @State private var prompts: [PromptTemplate] = []
    @State private var feedbackStats: [FeedbackStats] = []
    @State private var recentFeedback: [Feedback] = []
    @State private var selectedPromptID: PromptID?
    @State private var isLoading = true
    @State private var isTuning = false
    @State private var tuneOutput: String = ""
    @State private var tuneError: String?
    @State private var showTuneOutput = false
    @State private var importanceCorrectionCount: Int = 0
    @State private var showManualTune = false

    private var totalFeedback: Int { feedbackStats.reduce(0) { $0 + $1.total } }
    private var totalPositive: Int { feedbackStats.reduce(0) { $0 + $1.positive } }
    private var totalNegative: Int { feedbackStats.reduce(0) { $0 + $1.negative } }
    private var qualityPercent: Int { totalFeedback > 0 ? totalPositive * 100 / totalFeedback : 0 }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            header
                .padding(.horizontal, 24)
                .padding(.top, 20)
                .padding(.bottom, 16)

            Divider()

            if isLoading {
                Spacer()
                ProgressView()
                    .controlSize(.large)
                Spacer()
            } else {
                ScrollView {
                    VStack(spacing: 24) {
                        // Dashboard cards
                        dashboardCards
                            .padding(.horizontal, 24)
                            .padding(.top, 20)

                        // Tune section
                        tuneCard
                            .padding(.horizontal, 24)

                        // Prompts grid
                        promptsSection
                            .padding(.horizontal, 24)
                            .padding(.bottom, 24)
                    }
                }
            }
        }
        .background(Color(nsColor: .controlBackgroundColor))
        .onAppear { reload() }
        .sheet(isPresented: $showTuneOutput) {
            TuneOutputSheet(output: tuneOutput)
        }
        .sheet(isPresented: $showManualTune) {
            ManualTuneSheet(prompts: prompts) { output in
                tuneOutput = output
                showTuneOutput = true
                reload()
            }
        }
        .sheet(item: $selectedPromptID) { item in
            if let prompt = prompts.first(where: { $0.id == item.id }) {
                PromptEditorSheet(prompt: prompt, dbManager: appState.databaseManager) {
                    reload()
                }
            }
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Training")
                    .font(.title)
                    .fontWeight(.bold)
                Text("Fine-tune AI prompts based on your feedback")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer()

            Button {
                reload()
            } label: {
                Image(systemName: "arrow.clockwise")
                    .font(.body)
            }
            .buttonStyle(.bordered)
            .controlSize(.regular)
        }
    }

    // MARK: - Dashboard Cards

    private var dashboardCards: some View {
        HStack(spacing: 12) {
            // Quality score
            TrainingStatCard(title: "Quality", icon: "chart.bar.fill", accent: qualityColor) {
                Text(totalFeedback > 0 ? "\(qualityPercent)%" : "—")
                    .font(.system(size: 28, weight: .bold, design: .rounded))
                    .foregroundStyle(qualityColor)
                Text(qualityLabel)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            // Total feedback
            TrainingStatCard(title: "Feedback", icon: "hand.thumbsup", accent: .blue) {
                Text("\(totalFeedback)")
                    .font(.system(size: 28, weight: .bold, design: .rounded))
                HStack(spacing: 8) {
                    HStack(spacing: 2) {
                        Image(systemName: "plus")
                            .font(.system(size: 8))
                        Text("\(totalPositive)")
                    }
                    .font(.caption2)
                    .foregroundStyle(.green)
                    HStack(spacing: 2) {
                        Image(systemName: "minus")
                            .font(.system(size: 8))
                        Text("\(totalNegative)")
                    }
                    .font(.caption2)
                    .foregroundStyle(.red)
                }
            }

            // Importance corrections
            TrainingStatCard(title: "Corrections", icon: "arrow.up.arrow.down", accent: .orange) {
                Text("\(importanceCorrectionCount)")
                    .font(.system(size: 28, weight: .bold, design: .rounded))
                    .foregroundStyle(importanceCorrectionCount > 0 ? .orange : .secondary)
                Text(importanceCorrectionCount > 0 ? "pending" : "none")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            // Active prompts
            TrainingStatCard(title: "Prompts", icon: "doc.text", accent: .purple) {
                Text("\(prompts.count)")
                    .font(.system(size: 28, weight: .bold, design: .rounded))
                let totalVersions = prompts.reduce(0) { $0 + $1.version }
                Text("\(totalVersions) versions")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxHeight: 110)
    }

    private var qualityColor: Color {
        if totalFeedback == 0 { return .gray }
        if qualityPercent >= 80 { return .green }
        if qualityPercent >= 60 { return .orange }
        return .red
    }

    private var qualityLabel: String {
        if totalFeedback == 0 { return "No data" }
        if qualityPercent >= 80 { return "Excellent" }
        if qualityPercent >= 60 { return "Good" }
        if qualityPercent >= 40 { return "Needs work" }
        return "Poor"
    }

    // MARK: - Tune Card

    private var tuneCard: some View {
        HStack(spacing: 16) {
            // Icon
            ZStack {
                Circle()
                    .fill(
                        LinearGradient(
                            colors: [.purple, .blue],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
                    .frame(width: 48, height: 48)
                Image(systemName: "wand.and.stars")
                    .font(.title2)
                    .foregroundStyle(.white)
            }

            // Text
            VStack(alignment: .leading, spacing: 2) {
                Text("Auto-Tune Prompts")
                    .font(.headline)
                Text("Analyze feedback patterns and automatically improve prompt templates using AI")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            Spacer()

            if let err = tuneError {
                Text(err)
                    .font(.caption2)
                    .foregroundStyle(.red)
                    .frame(maxWidth: 150)
                    .lineLimit(2)
            }

            if !tuneOutput.isEmpty {
                Button("View Results") {
                    showTuneOutput = true
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }

            // Manual tune button
            Button {
                showManualTune = true
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "pencil.and.outline")
                    Text("Manual Tune")
                }
                .frame(minWidth: 110)
            }
            .buttonStyle(.bordered)
            .controlSize(.regular)
            .disabled(isTuning || prompts.isEmpty)

            // Auto tune button
            Button {
                runTune()
            } label: {
                HStack(spacing: 6) {
                    if isTuning {
                        ProgressView()
                            .controlSize(.small)
                    }
                    Text(isTuning ? "Tuning..." : "Auto Tune")
                }
                .frame(minWidth: 100)
            }
            .buttonStyle(.borderedProminent)
            .tint(.purple)
            .controlSize(.regular)
            .disabled(isTuning || feedbackStats.isEmpty)
        }
        .padding(16)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12))
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .strokeBorder(Color.purple.opacity(0.2), lineWidth: 1)
        )
    }

    // MARK: - Prompts Section

    private var promptsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Prompt Templates")
                .font(.headline)

            if prompts.isEmpty {
                HStack {
                    Spacer()
                    VStack(spacing: 8) {
                        Image(systemName: "doc.text")
                            .font(.largeTitle)
                            .foregroundStyle(.tertiary)
                        Text("No prompts yet")
                            .foregroundStyle(.secondary)
                        Text("Run a sync to seed default prompt templates")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                    .padding(40)
                    Spacer()
                }
                .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12))
            } else {
                LazyVGrid(columns: [
                    GridItem(.flexible(), spacing: 12),
                    GridItem(.flexible(), spacing: 12),
                ], spacing: 12) {
                    ForEach(prompts) { prompt in
                        PromptCard(prompt: prompt) {
                            selectedPromptID = PromptID(id: prompt.id)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Actions

    private func runTune() {
        guard let cliPath = Constants.findCLIPath() else {
            tuneError = "watchtower binary not found"
            return
        }

        isTuning = true
        tuneError = nil
        tuneOutput = ""

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["tune", "--apply"]

            var env = Constants.resolvedEnvironment()
            // Ensure claude's directory is in PATH (nvm may not be in login-only shell)
            if let claudePath = Constants.findClaudePath() {
                let claudeDir = (claudePath as NSString).deletingLastPathComponent
                let currentPath = env["PATH"] ?? ""
                if !currentPath.contains(claudeDir) {
                    env["PATH"] = claudeDir + ":" + currentPath
                }
            }
            process.environment = env

            let stdout = Pipe()
            let stderr = Pipe()
            process.standardOutput = stdout
            process.standardError = stderr

            do {
                try process.run()
                process.waitUntilExit()

                let outData = stdout.fileHandleForReading.readDataToEndOfFile()
                let errData = stderr.fileHandleForReading.readDataToEndOfFile()
                let outStr = String(data: outData, encoding: .utf8) ?? ""
                let errStr = String(data: errData, encoding: .utf8) ?? ""

                await MainActor.run {
                    if process.terminationStatus == 0 {
                        tuneOutput = outStr
                        tuneError = nil
                    } else {
                        tuneOutput = outStr
                        tuneError = errStr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                            ? "Tuning failed (exit code \(process.terminationStatus))"
                            : String(errStr.prefix(200))
                    }
                    isTuning = false
                    reload()
                }
            } catch {
                await MainActor.run {
                    tuneError = error.localizedDescription
                    isTuning = false
                }
            }
        }
    }

    private func reload() {
        guard let db = appState.databaseManager else {
            isLoading = false
            return
        }
        Task.detached {
            let loadedPrompts = (try? await db.dbPool.read { db in
                try PromptQueries.fetchAll(db)
            }) ?? []
            let loadedStats = (try? await db.dbPool.read { db in
                try FeedbackQueries.getStats(db)
            }) ?? []
            let loadedRecent = (try? await db.dbPool.read { db in
                try FeedbackQueries.getAllFeedback(db, limit: 20)
            }) ?? []
            let corrections = (try? await db.dbPool.read { db in
                try ImportanceCorrectionQueries.allCorrections(db)
            }) ?? [:]
            await MainActor.run {
                prompts = loadedPrompts
                feedbackStats = loadedStats
                recentFeedback = loadedRecent
                importanceCorrectionCount = corrections.count
                isLoading = false
            }
        }
    }

    private func feedbackTypeLabel(_ type: String) -> String {
        switch type {
        case "digest": return "Digests"
        case "track": return "Tracks"
        case "decision": return "Decisions"
        default: return type.capitalized
        }
    }
}

// H6 fix: wrapper struct instead of global String: Identifiable conformance
struct PromptID: Identifiable {
    let id: String
}

// L4: shared prompt label mapping — single source of truth
let sharedPromptLabels: [String: String] = [
    "digest.channel": "Channel Digest",
    "digest.daily": "Daily Rollup",
    "digest.weekly": "Weekly Summary",
    "digest.period": "Period Summary",
    "tracks.extract": "Tracks Extract",
    "tracks.update": "Tracks Update",
    "analysis.user": "User Analysis",
    "analysis.period": "Period Analysis",
]

// MARK: - Stat Card

struct TrainingStatCard<Content: View>: View {
    let title: String
    let icon: String
    let accent: Color
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(spacing: 8) {
            HStack {
                Image(systemName: icon)
                    .font(.caption)
                    .foregroundStyle(accent)
                Text(title)
                    .font(.caption)
                    .fontWeight(.medium)
                    .foregroundStyle(.secondary)
                Spacer()
            }

            Spacer()

            VStack(spacing: 2) {
                content()
            }

            Spacer()
        }
        .padding(12)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .strokeBorder(accent.opacity(0.15), lineWidth: 1)
        )
    }
}

// MARK: - Prompt Card

struct PromptCard: View {
    let prompt: PromptTemplate
    let onTap: () -> Void

    private var categoryColor: Color {
        if prompt.id.hasPrefix("digest") { return .blue }
        if prompt.id.hasPrefix("tracks") { return .orange }
        if prompt.id.hasPrefix("analysis") { return .purple }
        return .gray
    }

    private var categoryLabel: String {
        if prompt.id.hasPrefix("digest") { return "Digest" }
        if prompt.id.hasPrefix("tracks") { return "Tracks" }
        if prompt.id.hasPrefix("analysis") { return "Analysis" }
        return "Other"
    }

    private var promptLabel: String {
        sharedPromptLabels[prompt.id] ?? prompt.id
    }

    var body: some View {
        Button(action: onTap) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    // Category badge
                    Text(categoryLabel)
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(categoryColor)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(categoryColor.opacity(0.12), in: Capsule())

                    Spacer()

                    Text("v\(prompt.version)")
                        .font(.system(size: 11, weight: .medium, design: .monospaced))
                        .foregroundStyle(.secondary)
                }

                Text(promptLabel)
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .foregroundStyle(.primary)
                    .lineLimit(1)

                // Preview of template
                Text(prompt.template.prefix(80) + (prompt.template.count > 80 ? "..." : ""))
                    .font(.system(.caption2, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)

                Spacer(minLength: 0)

                HStack {
                    Image(systemName: "globe")
                        .font(.system(size: 9))
                    Text(prompt.language)
                        .font(.caption2)
                    Spacer()
                    Text(formatDate(prompt.updatedAt))
                        .font(.caption2)
                }
                .foregroundStyle(.secondary)
            }
            .padding(14)
            .frame(maxWidth: .infinity, minHeight: 120, alignment: .topLeading)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 10))
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .strokeBorder(categoryColor.opacity(0.15), lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
    }

    private func formatDate(_ dateStr: String) -> String {
        // Show relative or short date
        let parts = dateStr.split(separator: " ")
        if let datePart = parts.first {
            return String(datePart)
        }
        return dateStr
    }
}

// MARK: - Manual Tune Sheet

struct ManualTuneSheet: View {
    let prompts: [PromptTemplate]
    let onComplete: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selectedPromptID: String = ""
    @State private var instructions: String = ""
    @State private var isRunning = false
    @State private var error: String?

    private var promptLabels: [String: String] { sharedPromptLabels }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Manual Tune")
                        .font(.title2)
                        .fontWeight(.bold)
                    Text("Describe what to change and AI will rewrite the prompt")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding()

            Divider()

            VStack(alignment: .leading, spacing: 16) {
                // Prompt picker
                VStack(alignment: .leading, spacing: 6) {
                    Text("Prompt")
                        .font(.subheadline)
                        .fontWeight(.medium)
                    Picker("", selection: $selectedPromptID) {
                        Text("Select a prompt...").tag("")
                        ForEach(prompts) { prompt in
                            Text(promptLabels[prompt.id] ?? prompt.id).tag(prompt.id)
                        }
                    }
                    .labelsHidden()
                }

                // Instructions
                VStack(alignment: .leading, spacing: 6) {
                    Text("Instructions")
                        .font(.subheadline)
                        .fontWeight(.medium)
                    Text("Describe what you don't like and what to fix")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    TextEditor(text: $instructions)
                        .font(.system(.body, design: .default))
                        .scrollContentBackground(.hidden)
                        .padding(8)
                        .background(
                            RoundedRectangle(cornerRadius: 8)
                                .fill(Color(nsColor: .textBackgroundColor))
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: 8)
                                .strokeBorder(Color.secondary.opacity(0.2), lineWidth: 1)
                        )
                        .frame(minHeight: 120)
                }

                if let error {
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.red)
                }

                // Run button
                HStack {
                    Spacer()
                    Button {
                        runManualTune()
                    } label: {
                        HStack(spacing: 6) {
                            if isRunning {
                                ProgressView()
                                    .controlSize(.small)
                            } else {
                                Image(systemName: "wand.and.stars")
                            }
                            Text(isRunning ? "Tuning..." : "Run Tune")
                        }
                        .frame(minWidth: 120)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.purple)
                    .controlSize(.regular)
                    .disabled(isRunning || selectedPromptID.isEmpty || instructions.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .padding()

            Spacer()
        }
        .frame(width: 550, height: 420)
        .onAppear {
            if selectedPromptID.isEmpty, let first = prompts.first {
                selectedPromptID = first.id
            }
        }
    }

    private func runManualTune() {
        guard let cliPath = Constants.findCLIPath() else {
            error = "watchtower binary not found"
            return
        }

        isRunning = true
        error = nil

        let promptID = selectedPromptID
        // C1: validate and length-limit user instructions before passing to CLI
        let userInstructions = String(instructions.prefix(2000))

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["tune", promptID, "--instructions", userInstructions, "--apply"]

            var env = Constants.resolvedEnvironment()
            if let claudePath = Constants.findClaudePath() {
                let claudeDir = (claudePath as NSString).deletingLastPathComponent
                let currentPath = env["PATH"] ?? ""
                if !currentPath.contains(claudeDir) {
                    env["PATH"] = claudeDir + ":" + currentPath
                }
            }
            process.environment = env

            let stdout = Pipe()
            let stderr = Pipe()
            process.standardOutput = stdout
            process.standardError = stderr

            do {
                try process.run()
                process.waitUntilExit()

                let outData = stdout.fileHandleForReading.readDataToEndOfFile()
                let errData = stderr.fileHandleForReading.readDataToEndOfFile()
                let outStr = String(data: outData, encoding: .utf8) ?? ""
                let errStr = String(data: errData, encoding: .utf8) ?? ""

                await MainActor.run {
                    isRunning = false
                    if process.terminationStatus == 0 {
                        // M5: call onComplete before dismiss to avoid sheet animation conflict
                        onComplete(outStr)
                        dismiss()
                    } else {
                        error = errStr.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                            ? "Tuning failed (exit code \(process.terminationStatus))"
                            : String(errStr.prefix(300))
                    }
                }
            } catch {
                await MainActor.run {
                    self.error = error.localizedDescription
                    isRunning = false
                }
            }
        }
    }
}

// MARK: - Prompt Editor Sheet

struct PromptEditorSheet: View {
    let prompt: PromptTemplate
    let dbManager: DatabaseManager?
    let onSave: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var editedTemplate: String = ""
    @State private var isEditing = false
    @State private var saveError: String?
    @State private var history: [PromptHistoryEntry] = []
    @State private var showHistory = false

    private var promptLabel: String {
        sharedPromptLabels[prompt.id] ?? prompt.id
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(promptLabel)
                        .font(.title2)
                        .fontWeight(.bold)
                    HStack(spacing: 8) {
                        Text(prompt.id)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.1), in: Capsule())
                        Text("Version \(prompt.version)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(prompt.language)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                Button {
                    showHistory = true
                } label: {
                    Label("History", systemImage: "clock.arrow.circlepath")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)

                if isEditing {
                    Button("Cancel") {
                        isEditing = false
                        editedTemplate = prompt.template
                        saveError = nil
                    }
                    .controlSize(.small)

                    Button("Save") {
                        savePrompt()
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                } else {
                    Button("Edit") {
                        editedTemplate = prompt.template
                        isEditing = true
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }

                Button("Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding()

            if let err = saveError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal)
            }

            Divider()

            // Content
            if isEditing {
                TextEditor(text: $editedTemplate)
                    .font(.system(.caption, design: .monospaced))
                    .scrollContentBackground(.hidden)
                    .padding(8)
            } else {
                ScrollView {
                    Text(prompt.template)
                        .font(.system(.caption, design: .monospaced))
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding()
                }
            }
        }
        .frame(width: 800, height: 600)
        .sheet(isPresented: $showHistory) {
            PromptHistorySheet(promptID: prompt.id, dbManager: dbManager)
        }
    }

    private func savePrompt() {
        guard let db = dbManager else { return }
        do {
            try db.dbPool.write { database in
                try database.execute(
                    sql: """
                        INSERT INTO prompt_history (prompt_id, version, template, reason)
                        SELECT id, version, template, 'manual edit' FROM prompts WHERE id = ?
                        """,
                    arguments: [prompt.id]
                )
                try database.execute(
                    sql: """
                        UPDATE prompts SET template = ?, version = version + 1, updated_at = datetime('now')
                        WHERE id = ?
                        """,
                    arguments: [editedTemplate, prompt.id]
                )
            }
            isEditing = false
            saveError = nil
            onSave()
            dismiss()
        } catch {
            saveError = error.localizedDescription
        }
    }
}
