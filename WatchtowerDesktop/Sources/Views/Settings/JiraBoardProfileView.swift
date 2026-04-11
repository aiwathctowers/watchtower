import SwiftUI

struct UserOverridesWrapper: Codable {
    let staleThresholds: [String: Int]?

    enum CodingKeys: String, CodingKey {
        case staleThresholds = "stale_thresholds"
    }
}

struct JiraBoardProfileView: View {
    let board: JiraBoard

    @State private var profile: BoardProfileDisplay?
    @State private var overrides: [String: Int] = [:]
    @State private var isAnalyzing = false
    @State private var analyzeError: String?

    var body: some View {
        Form {
            headerSection
            if let profile {
                workflowSection(profile)
                staleThresholdsSection(profile)
                iterationSection(profile)
                estimationSection(profile)
                healthSignalsSection(profile)
            } else {
                notAnalyzedSection
            }
            reAnalyzeSection
        }
        .formStyle(.grouped)
        .navigationTitle(board.name)
        .onAppear { parseProfile() }
    }

    // MARK: - Header

    private var headerSection: some View {
        Section {
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
        }
    }

    @ViewBuilder
    private var configChangedBadge: some View {
        if board.isConfigChanged {
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
            ScrollView(.horizontal, showsIndicators: false) {
                workflowChain(profile.workflowStages)
            }
        }
    }

    private func workflowChain(
        _ stages: [WorkflowStageDisplay]
    ) -> some View {
        HStack(spacing: 4) {
            ForEach(Array(stages.enumerated()), id: \.element.id) { idx, stage in
                if idx > 0 {
                    Image(systemName: "arrow.right")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                stageCapsule(stage)
            }
        }
        .padding(.vertical, 4)
    }

    private func stageCapsule(
        _ stage: WorkflowStageDisplay
    ) -> some View {
        VStack(spacing: 2) {
            Text(stage.name)
                .font(.caption)
                .fontWeight(.medium)
            if !stage.typicalDurationSignal.isEmpty {
                Text(stage.typicalDurationSignal)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(phaseColor(stage.phase).opacity(0.2), in: Capsule())
        .overlay(Capsule().stroke(phaseColor(stage.phase).opacity(0.5), lineWidth: 1))
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

    // MARK: - Stale Thresholds

    private func staleThresholdsSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        Section("Stale Thresholds (days)") {
            let nonTerminal = profile.workflowStages.filter { !$0.isTerminal }
            ForEach(nonTerminal) { stage in
                staleSliderRow(stage: stage, profile: profile)
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
        Section("Iterations") {
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
            }
        }
    }

    // MARK: - Estimation

    private func estimationSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        Section("Estimation") {
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

    // MARK: - Health Signals

    private func healthSignalsSection(
        _ profile: BoardProfileDisplay
    ) -> some View {
        Section("Health Signals") {
            if profile.healthSignals.isEmpty {
                Text("No signals")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(profile.healthSignals, id: \.self) { signal in
                    Label(signal, systemImage: "exclamationmark.triangle")
                        .font(.callout)
                        .foregroundStyle(.orange)
                }
            }
        }
    }

    // MARK: - Not Analyzed

    private var notAnalyzedSection: some View {
        Section {
            Text("Board profile not yet analyzed. Tap Re-analyze to generate.")
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Re-analyze

    private var reAnalyzeSection: some View {
        Section {
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
    }

    // MARK: - Actions

    private func parseProfile() {
        guard !board.llmProfileJSON.isEmpty else { return }
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        guard let data = board.llmProfileJSON.data(using: .utf8),
              let decoded = try? decoder.decode(
                BoardProfileDisplay.self, from: data
              ) else {
            return
        }
        profile = decoded
        // Seed overrides from user overrides JSON (wrapper matches Go format)
        if !board.userOverridesJSON.isEmpty,
           let overData = board.userOverridesJSON.data(using: .utf8),
           let wrapper = try? JSONDecoder().decode(
               UserOverridesWrapper.self, from: overData
           ) {
            overrides = wrapper.staleThresholds ?? [:]
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
            process.standardOutput = Pipe()
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
            process.standardOutput = Pipe()
            process.standardError = Pipe()

            try? process.run()
            process.waitUntilExit()
        }
    }
}
