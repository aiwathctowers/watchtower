import SwiftUI

struct UserOverridesWrapper: Codable {
    let staleThresholds: [String: Int]?
    let terminalStages: [String: Bool]?
    let phaseOverrides: [String: String]?

    enum CodingKeys: String, CodingKey {
        case staleThresholds = "stale_thresholds"
        case terminalStages = "terminal_stages"
        case phaseOverrides = "phase_overrides"
    }
}

struct JiraBoardProfileView: View {
    @Environment(AppState.self) private var appState
    let board: JiraBoard

    @State private var currentBoard: JiraBoard?
    @State private var profile: BoardProfileDisplay?
    @State private var overrides: [String: Int] = [:]
    @State private var terminalOverrides: [String: Bool] = [:]
    @State private var phaseOverrides: [String: String] = [:]
    @State private var isAnalyzing = false
    @State private var analyzeError: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                headerSection
                if let profile {
                    if !profile.workflowSummary.isEmpty {
                        Text(profile.workflowSummary)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal)
                    }
                    HStack(alignment: .top, spacing: 16) {
                        // Left column: Workflow
                        VStack(alignment: .leading, spacing: 0) {
                            sectionHeader("Workflow")
                            workflowFlow(profile.workflowStages)
                        }
                        .frame(maxWidth: .infinity)

                        // Right column: Data
                        VStack(alignment: .leading, spacing: 12) {
                            iterationSection(profile)
                            estimationSection(profile)
                            staleThresholdsSection(profile)
                            customFieldsSection(profile)
                        }
                        .frame(maxWidth: .infinity)
                    }
                    .padding(.horizontal)

                    // Full-width: Health Signals
                    healthSignalsSection(profile)
                        .padding(.horizontal)
                } else {
                    notAnalyzedSection
                }
                reAnalyzeSection
                    .padding(.horizontal)
            }
            .padding(.vertical)
        }
        .onAppear { parseProfile() }
    }

    private func sectionHeader(_ title: String) -> some View {
        Text(title)
            .font(.headline)
            .padding(.bottom, 8)
    }

    // MARK: - Header

    private var headerSection: some View {
        HStack {
            Text(board.projectKey)
                .font(.headline)
            Spacer()
            Text(board.boardType)
                .font(.caption)
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .background(Color.secondary.opacity(0.2), in: Capsule())
            configChangedBadge
        }
        .padding(.horizontal)
    }

    @ViewBuilder
    private var configChangedBadge: some View {
        if (currentBoard ?? board).isConfigChanged {
            Text("Config changed")
                .font(.caption2)
                .foregroundStyle(.orange)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.orange.opacity(0.15), in: Capsule())
        }
    }

    // MARK: - Workflow Visualization

    private func workflowSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        Section("Workflow") {
            if !profile.workflowSummary.isEmpty {
                Text(profile.workflowSummary)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            workflowFlow(profile.workflowStages)
        }
    }

    /// Flatten all statuses from stages, apply phase overrides, regroup by effective phase.
    private func effectiveStatusesByPhase(
        _ stages: [WorkflowStageDisplay]
    ) -> [(phase: String, statuses: [(name: String, isTerminal: Bool)])] {
        // Collect all statuses with their original phase/terminal info.
        struct StatusInfo {
            let name: String
            let originalPhase: String
            let isTerminal: Bool
        }
        var all: [StatusInfo] = []
        for stage in stages {
            for status in stage.originalStatuses {
                all.append(StatusInfo(
                    name: status,
                    originalPhase: stage.phase,
                    isTerminal: stage.isTerminal
                ))
            }
        }

        // Group by effective phase (with overrides applied).
        let phaseOrder = ["backlog", "active_work", "review", "testing", "done", "other"]
        var grouped: [String: [(name: String, isTerminal: Bool)]] = [:]
        for s in all {
            let effectivePhase = phaseOverrides[s.name] ?? s.originalPhase
            grouped[effectivePhase, default: []].append((s.name, s.isTerminal))
        }

        return phaseOrder.compactMap { phase in
            guard let statuses = grouped[phase], !statuses.isEmpty else { return nil }
            return (phase, statuses)
        }
    }

    private func workflowFlow(
        _ stages: [WorkflowStageDisplay]
    ) -> some View {
        let phases = effectiveStatusesByPhase(stages)

        return VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(phases.enumerated()), id: \.offset) { idx, entry in
                if idx > 0 {
                    HStack {
                        Spacer()
                        Image(systemName: "arrow.down")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                    }
                }
                phaseBlock(
                    phase: entry.phase,
                    statuses: entry.statuses
                )
            }
        }
        .padding(.vertical, 4)
    }

    private func phaseBlock(
        phase: String,
        statuses: [(name: String, isTerminal: Bool)]
    ) -> some View {
        let color = phaseColor(phase)
        return VStack(alignment: .leading, spacing: 6) {
            Text(phaseLabel(phase))
                .font(.caption2)
                .fontWeight(.semibold)
                .foregroundStyle(color)
                .textCase(.uppercase)
            FlowLayout(spacing: 6) {
                ForEach(statuses, id: \.name) { status in
                    statusBadge(
                        status: status.name,
                        stageIsTerminal: status.isTerminal,
                        color: color,
                        isOverridden: phaseOverrides[status.name] != nil
                    )
                }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(color.opacity(0.25), lineWidth: 1)
        )
        .dropDestination(for: String.self) { items, _ in
            guard let statusName = items.first else { return false }
            // Don't drop onto the same phase.
            let currentPhase = phaseOverrides[statusName] ?? originalPhase(for: statusName)
            guard currentPhase != phase else { return false }
            phaseOverrides[statusName] = phase
            applyPhaseOverride(boardID: board.id, status: statusName, phase: phase)
            return true
        } isTargeted: { _ in }
    }

    /// Look up the original phase for a status from the LLM profile.
    private func originalPhase(for status: String) -> String? {
        guard let profile else { return nil }
        for stage in profile.workflowStages {
            if stage.originalStatuses.contains(status) {
                return stage.phase
            }
        }
        return nil
    }

    private func statusBadge(
        status: String,
        stageIsTerminal: Bool,
        color: Color,
        isOverridden: Bool = false
    ) -> some View {
        let isTerminal = terminalOverrides[status] ?? stageIsTerminal
        return HStack(spacing: 3) {
            Circle()
                .fill(isTerminal ? Color.red.opacity(0.6) : Color.green.opacity(0.6))
                .frame(width: 6, height: 6)
            Text(status)
                .font(.caption2)
            if isOverridden {
                Image(systemName: "arrow.uturn.right")
                    .font(.system(size: 7))
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 3)
        .background(
            isTerminal ? Color.red.opacity(0.1) : color.opacity(0.1),
            in: Capsule()
        )
        .overlay(
            Capsule().stroke(
                isOverridden ? color.opacity(0.5) : (isTerminal ? Color.red.opacity(0.3) : color.opacity(0.2)),
                lineWidth: isOverridden ? 1.5 : 1
            )
        )
        .draggable(status)
        .onTapGesture {
            let newVal = !isTerminal
            terminalOverrides[status] = newVal
            applyTerminalOverride(boardID: board.id, stage: status, isTerminal: newVal)
        }
        .help(isOverridden
              ? "\(status): moved here (drag to another phase, or re-analyze to reset)"
              : isTerminal
                ? "\(status): terminal (click to toggle)"
                : "\(status): active (click to toggle, drag to move)")
    }

    private func phaseColor(_ phase: String) -> Color {
        switch phase {
        case "backlog": return .gray
        case "active_work": return .blue
        case "review": return .purple
        case "testing": return .orange
        case "done": return .green
        default: return .secondary
        }
    }

    private func phaseLabel(_ phase: String) -> String {
        switch phase {
        case "backlog": return "Backlog"
        case "active_work": return "Active Work"
        case "review": return "Review"
        case "testing": return "Testing"
        case "done": return "Done"
        default: return phase
        }
    }

    // MARK: - Stale Thresholds

    private func staleThresholdsSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        let nonTerminal = profile.workflowStages.filter { !$0.isTerminal }
        return cardSection("Stale Thresholds") {
            VStack(spacing: 6) {
                ForEach(nonTerminal) { stage in
                    staleSliderRow(stage: stage, profile: profile)
                }
            }
        }
    }

    private func staleSliderRow(
        stage: WorkflowStageDisplay,
        profile: BoardProfileDisplay
    ) -> some View {
        let currentValue = Double(
            overrides[stage.name]
            ?? profile.staleThresholds[stage.name]
            ?? 7
        )
        return HStack {
            Text(stage.name)
                .font(.caption)
                .frame(width: 100, alignment: .leading)
            Slider(
                value: Binding(
                    get: { currentValue },
                    set: { newVal in
                        let intVal = Int(newVal.rounded())
                        overrides[stage.name] = intVal
                        applyOverride(
                            boardID: board.id,
                            stage: stage.name,
                            days: intVal
                        )
                    }
                ),
                in: 1...30,
                step: 1
            )
            Text("\(Int(currentValue))d")
                .font(.caption)
                .monospacedDigit()
                .frame(width: 30, alignment: .trailing)
        }
    }

    // MARK: - Iteration Info

    private func iterationSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        cardSection("Iterations") {
            if profile.iterationInfo.hasIterations {
                let weeks = profile.iterationInfo.typicalLengthDays / 7
                let label = weeks > 0
                    ? "\(weeks)-week cycles"
                    : "\(profile.iterationInfo.typicalLengthDays)-day cycles"
                let throughput = profile.iterationInfo.avgThroughput
                HStack {
                    Label(label, systemImage: "arrow.triangle.2.circlepath")
                    Spacer()
                    Text("~\(throughput) SP throughput")
                        .foregroundStyle(.secondary)
                }
                .font(.callout)
            } else {
                Text("No iterations configured")
                    .foregroundStyle(.secondary)
                    .font(.caption)
            }
        }
    }

    // MARK: - Estimation

    private func estimationSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        cardSection("Estimation") {
            HStack {
                if let field = profile.estimationApproach.field,
                   !field.isEmpty {
                    Label(
                        "Story Points (\(field))",
                        systemImage: "number.circle"
                    )
                } else {
                    Label("Count only", systemImage: "number.circle")
                }
            }
            .font(.callout)
        }
    }

    // MARK: - Custom Fields

    private func customFieldsSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        if profile.customFields.isEmpty {
            return AnyView(EmptyView())
        }
        let grouped = Dictionary(
            grouping: profile.customFields,
            by: { roleCategory($0.role) }
        )
        let order = ["Estimation", "Roles", "Categorization",
                     "Tracking", "Planning", "Other"]
        return AnyView(
            cardSection("Custom Fields") {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(order, id: \.self) { category in
                        if let fields = grouped[category] {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(category)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                                    .textCase(.uppercase)
                                ForEach(fields) { field in
                                    HStack {
                                        Text(field.name)
                                            .font(.caption)
                                        Spacer()
                                        Text(field.role.replacingOccurrences(of: "_", with: " "))
                                            .font(.caption2)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        )
    }

    private func roleCategory(_ role: String) -> String {
        switch role {
        case "story_points", "tshirt_size":
            return "Estimation"
        case "qa_assignee", "developer", "product_manager",
             "delivery_manager", "project_manager":
            return "Roles"
        case "area", "team", "severity", "environment",
             "region", "discipline":
            return "Categorization"
        case "branch", "merge_request", "release_notes":
            return "Tracking"
        case "planned_start", "planned_end",
             "hold_reason", "flagged":
            return "Planning"
        default:
            return "Other"
        }
    }

    // MARK: - Health Signals

    private func healthSignalsSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        cardSection("Health Signals") {
            if profile.healthSignals.isEmpty {
                Text("No signals")
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(profile.healthSignals, id: \.self) { signal in
                        Label(signal, systemImage: "exclamationmark.triangle")
                            .font(.caption)
                            .foregroundStyle(.orange)
                    }
                }
            }
        }
    }

    // MARK: - Not Analyzed

    private var notAnalyzedSection: some View {
        Text("Board profile not yet analyzed. Tap Re-analyze to generate.")
            .foregroundStyle(.secondary)
            .padding(.horizontal)
    }

    // MARK: - Re-analyze

    private var reAnalyzeSection: some View {
        HStack {
            Button {
                runAnalyze()
            } label: {
                HStack(spacing: 4) {
                    if isAnalyzing {
                        ProgressView().controlSize(.small)
                    }
                    Text(isAnalyzing ? "Analyzing..." : "Re-analyze Board")
                }
            }
            .disabled(isAnalyzing)

            if let err = analyzeError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(2)
            }
        }
    }

    // MARK: - Card Helper

    private func cardSection<Content: View>(
        _ title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
            content()
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - Actions

    private func parseProfile() {
        let b = currentBoard ?? board
        guard !b.llmProfileJSON.isEmpty else { return }
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        guard let data = b.llmProfileJSON.data(using: .utf8),
              let decoded = try? decoder.decode(
                BoardProfileDisplay.self, from: data
              ) else {
            return
        }
        profile = decoded
        // Seed overrides from user overrides JSON (wrapper matches Go format)
        if !b.userOverridesJSON.isEmpty,
           let overData = b.userOverridesJSON.data(using: .utf8),
           let wrapper = try? JSONDecoder().decode(
               UserOverridesWrapper.self, from: overData
           ) {
            overrides = wrapper.staleThresholds ?? [:]
            terminalOverrides = wrapper.terminalStages ?? [:]
            phaseOverrides = wrapper.phaseOverrides ?? [:]
        }
    }

    private func reloadBoard() async {
        guard let db = appState.databaseManager else { return }
        let updated = try? await db.dbPool.read { db in
            try JiraQueries.fetchBoard(db, id: board.id)
        }
        if let updated {
            currentBoard = updated
            parseProfile()
        }
    }

    private func runAnalyze() {
        guard let cliPath = Constants.findCLIPath() else {
            analyzeError = "Watchtower CLI not found"
            return
        }

        isAnalyzing = true
        analyzeError = nil

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", "analyze", "--force",
                String(board.id),
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()

            let stderrPipe = Pipe()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                await MainActor.run {
                    isAnalyzing = false
                    analyzeError = "Failed to launch CLI"
                }
                return
            }

            let stderrData = stderrPipe.fileHandleForReading
                .readDataToEndOfFile()
            process.waitUntilExit()

            await MainActor.run {
                isAnalyzing = false
                if process.terminationStatus != 0 {
                    let stderr = String(
                        data: stderrData, encoding: .utf8
                    )?.trimmingCharacters(
                        in: .whitespacesAndNewlines
                    ) ?? ""
                    analyzeError = stderr.isEmpty
                        ? "Analysis failed"
                        : String(stderr.prefix(200))
                } else {
                    Task {
                        await reloadBoard()
                    }
                }
            }
        }
    }

    private func applyOverride(
        boardID: Int,
        stage: String,
        days: Int
    ) {
        guard let cliPath = Constants.findCLIPath() else { return }

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", "override",
                String(boardID),
                "--stale", "\(stage)=\(days)"
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = Pipe()

            try? process.run()
            process.waitUntilExit()
        }
    }

    private func applyPhaseOverride(
        boardID: Int,
        status: String,
        phase: String
    ) {
        guard let cliPath = Constants.findCLIPath() else { return }

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", "override",
                String(boardID),
                "--phase", "\(status)=\(phase)"
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = Pipe()

            try? process.run()
            process.waitUntilExit()
        }
    }

    private func applyTerminalOverride(
        boardID: Int,
        stage: String,
        isTerminal: Bool
    ) {
        guard let cliPath = Constants.findCLIPath() else { return }

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", "override",
                String(boardID),
                "--terminal", "\(stage)=\(isTerminal)"
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()
            process.standardOutput = FileHandle.nullDevice
            process.standardError = Pipe()

            try? process.run()
            process.waitUntilExit()
        }
    }
}

