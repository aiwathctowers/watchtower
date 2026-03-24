import SwiftUI

struct LogsSettings: View {
    @State private var selectedLog: LogFile = .sync
    @State private var logLines: [String] = []
    @State private var isLoading = false
    @State private var isLoadingMore = false
    @State private var autoScroll = true
    @State private var totalLineCount: Int = 0
    @State private var hasMore = false
    @State private var fileSizeText: String?
    @State private var loadTask: Task<Void, Never>?

    private let workspaceDir = Self.resolveWorkspaceDir()
    private let pageSize = 200
    private let loadMoreThreshold = 5

    enum LogFile: String, CaseIterable, Identifiable {
        case sync = "watchtower.log"
        case daemon = "daemon.log"

        var id: String { rawValue }

        var label: String {
            switch self {
            case .sync: "Sync Log"
            case .daemon: "Daemon Log"
            }
        }

        var icon: String {
            switch self {
            case .sync: "arrow.triangle.2.circlepath"
            case .daemon: "gear"
            }
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            logToolbar
            Divider()
            logContent
        }
        .onChange(of: selectedLog) { loadLog() }
        .onAppear { loadLog() }
    }

    private var logToolbar: some View {
        HStack(spacing: 12) {
            Picker("", selection: $selectedLog) {
                ForEach(LogFile.allCases) { log in
                    Label(log.label, systemImage: log.icon).tag(log)
                }
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 250)

            Spacer()

            if !logLines.isEmpty {
                Text("\(logLines.count)\(hasMore ? "+" : "") of \(totalLineCount) lines")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }

            if let size = fileSizeText {
                Text(size)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }

            Button {
                loadLog()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .help("Reload")

            Button {
                openInEditor()
            } label: {
                Image(systemName: "doc.richtext")
            }
            .help("Open in Default Editor")
            .disabled(logPath(for: selectedLog) == nil)

            Button {
                revealInFinder()
            } label: {
                Image(systemName: "folder")
            }
            .help("Open Log Folder")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var logContent: some View {
        if isLoading {
            Spacer()
            ProgressView()
            Spacer()
        } else if logLines.isEmpty {
            Spacer()
            VStack(spacing: 8) {
                Image(systemName: "doc.text")
                    .font(.largeTitle)
                    .foregroundStyle(.quaternary)
                Text(logPath(for: selectedLog).map { "No content in \($0)" } ?? "Log file not found")
                    .foregroundStyle(.secondary)
            }
            Spacer()
        } else {
            ScrollViewReader { proxy in
                ScrollView([.horizontal, .vertical]) {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        if hasMore {
                            if isLoadingMore {
                                ProgressView()
                                    .frame(maxWidth: .infinity)
                                    .padding(4)
                            } else {
                                Color.clear
                                    .frame(height: 1)
                                    .id("loadMoreSentinel")
                                    .onAppear { loadMore() }
                            }
                        }

                        ForEach(Array(logLines.enumerated()), id: \.offset) { index, line in
                            Text(line)
                                .font(.system(size: 11, design: .monospaced))
                                .textSelection(.enabled)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 0.5)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .id(index)
                        }
                    }
                }
                .background(Color(nsColor: .textBackgroundColor))
                .onChange(of: logLines.count) {
                    if autoScroll {
                        proxy.scrollTo(logLines.count - 1, anchor: .bottom)
                    }
                }
            }
        }
    }

    private func loadLog() {
        loadTask?.cancel()

        guard let path = logPath(for: selectedLog) else {
            logLines = []
            totalLineCount = 0
            hasMore = false
            fileSizeText = nil
            return
        }

        isLoading = true
        let count = pageSize
        loadTask = Task.detached {
            let result = Self.readTail(path: path, lineCount: count)
            guard !Task.isCancelled else { return }
            await MainActor.run {
                logLines = result.lines
                totalLineCount = result.totalLines
                hasMore = result.totalLines > result.lines.count
                fileSizeText = result.fileSizeText
                isLoading = false
            }
        }
    }

    private func loadMore() {
        guard hasMore, !isLoadingMore, let path = logPath(for: selectedLog) else { return }

        isLoadingMore = true
        let currentCount = logLines.count
        let nextCount = currentCount + pageSize
        Task.detached {
            let result = Self.readTail(path: path, lineCount: nextCount)
            await MainActor.run {
                // Prepend older lines, keep scroll position stable
                let newLines = result.lines
                logLines = newLines
                hasMore = result.totalLines > newLines.count
                totalLineCount = result.totalLines
                isLoadingMore = false
                autoScroll = false
            }
        }
    }

    nonisolated private static func readTail(path: String, lineCount: Int) -> TailResult {
        let fileSizeText: String?
        let totalLines: Int

        if let attrs = try? FileManager.default.attributesOfItem(atPath: path),
           let size = attrs[.size] as? Int64 {
            fileSizeText = ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
        } else {
            fileSizeText = nil
        }

        totalLines = countLines(path: path)
        let lines = tailLines(path: path, count: lineCount)

        return TailResult(
            lines: lines,
            totalLines: totalLines,
            fileSizeText: fileSizeText
        )
    }

    nonisolated private static func tailLines(path: String, count: Int) -> [String] {
        let process = Process()
        let pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/tail")
        process.arguments = ["-n", "\(count)", path]
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            guard let output = String(data: data, encoding: .utf8) else { return [] }
            var lines = output.components(separatedBy: "\n")
            if lines.last?.isEmpty == true { lines.removeLast() }
            return lines
        } catch {
            return ["Error reading log: \(error.localizedDescription)"]
        }
    }

    nonisolated private static func countLines(path: String) -> Int {
        let process = Process()
        let pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/wc")
        process.arguments = ["-l", path]
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            guard let output = String(data: data, encoding: .utf8) else { return 0 }
            let trimmed = output.trimmingCharacters(in: .whitespaces)
            if let spaceIdx = trimmed.firstIndex(of: " "),
               let count = Int(trimmed[trimmed.startIndex..<spaceIdx]) {
                return count
            }
            return Int(trimmed) ?? 0
        } catch {
            return 0
        }
    }

    private func logPath(for log: LogFile) -> String? {
        guard let dir = workspaceDir else { return nil }
        let path = "\(dir)/\(log.rawValue)"
        return FileManager.default.fileExists(atPath: path) ? path : nil
    }

    private func fileSize(for log: LogFile) -> String? {
        guard let path = logPath(for: log),
              let attrs = try? FileManager.default.attributesOfItem(atPath: path),
              let size = attrs[.size] as? Int64 else { return nil }
        return ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
    }

    private func revealInFinder() {
        if let path = logPath(for: selectedLog) {
            NSWorkspace.shared.selectFile(path, inFileViewerRootedAtPath: "")
        } else if let dir = workspaceDir {
            NSWorkspace.shared.selectFile(nil, inFileViewerRootedAtPath: dir)
        }
    }

    private func openInEditor() {
        if let path = logPath(for: selectedLog) {
            let url = URL(fileURLWithPath: path)
            NSWorkspace.shared.open(url)
        }
    }

    private static func resolveWorkspaceDir() -> String? {
        let basePath = Constants.databasePath
        let configPath = Constants.configPath
        let fm = FileManager.default

        // Try active workspace from config
        if let data = fm.contents(atPath: configPath),
           let str = String(data: data, encoding: .utf8) {
            for line in str.components(separatedBy: .newlines) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if trimmed.hasPrefix("active_workspace:") {
                    let ws = trimmed.dropFirst("active_workspace:".count)
                        .trimmingCharacters(in: .whitespaces)
                        .trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
                    if !ws.isEmpty && DatabaseManager.isValidWorkspaceName(ws) {
                        let dir = "\(basePath)/\(ws)"
                        if fm.fileExists(atPath: dir) { return dir }
                    }
                }
            }
        }

        // Fallback: first workspace dir
        if let contents = try? fm.contentsOfDirectory(atPath: basePath) {
            for dir in contents.sorted() where !dir.hasPrefix(".") {
                let full = "\(basePath)/\(dir)"
                var isDir: ObjCBool = false
                if fm.fileExists(atPath: full, isDirectory: &isDir), isDir.boolValue {
                    return full
                }
            }
        }

        return nil
    }
}

private struct TailResult {
    let lines: [String]
    let totalLines: Int
    let fileSizeText: String?
}
