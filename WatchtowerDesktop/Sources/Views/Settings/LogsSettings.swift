import SwiftUI

struct LogsSettings: View {
    @State private var selectedLog: LogFile = .sync
    @State private var logContent: String = ""
    @State private var isLoading = false
    @State private var autoScroll = true
    @State private var lineCount: Int = 0

    private let workspaceDir = Self.resolveWorkspaceDir()

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
            // Toolbar
            HStack(spacing: 12) {
                Picker("", selection: $selectedLog) {
                    ForEach(LogFile.allCases) { log in
                        Label(log.label, systemImage: log.icon).tag(log)
                    }
                }
                .pickerStyle(.segmented)
                .frame(maxWidth: 250)

                Spacer()

                if lineCount > 0 {
                    Text("\(lineCount) lines")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }

                if let size = fileSize(for: selectedLog) {
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
                    revealInFinder()
                } label: {
                    Image(systemName: "folder")
                }
                .help("Reveal in Finder")
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)

            Divider()

            // Log content
            if isLoading {
                Spacer()
                ProgressView()
                Spacer()
            } else if logContent.isEmpty {
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
                        Text(logContent)
                            .font(.system(size: 11, design: .monospaced))
                            .textSelection(.enabled)
                            .padding(8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .id("logBottom")
                    }
                    .background(Color(nsColor: .textBackgroundColor))
                    .onChange(of: logContent) {
                        if autoScroll {
                            proxy.scrollTo("logBottom", anchor: .bottom)
                        }
                    }
                }
            }
        }
        .onChange(of: selectedLog) { loadLog() }
        .onAppear { loadLog() }
    }

    // L2 fix: read only the tail of large files to avoid memory spikes.
    private func loadLog() {
        guard let path = logPath(for: selectedLog) else {
            logContent = ""
            lineCount = 0
            return
        }

        isLoading = true
        Task.detached {
            let content: String
            let lines: Int

            // Cap how much we read: last 512KB for large files
            let maxBytes = 512 * 1024
            let url = URL(fileURLWithPath: path)
            guard let attrs = try? FileManager.default.attributesOfItem(atPath: path),
                  let fileSize = attrs[.size] as? Int64 else {
                await MainActor.run { logContent = ""; lineCount = 0; isLoading = false }
                return
            }

            if fileSize <= maxBytes {
                // Small file: read entirely
                if let data = FileManager.default.contents(atPath: path),
                   let str = String(data: data, encoding: .utf8) {
                    let allLines = str.components(separatedBy: "\n")
                    lines = allLines.count
                    content = str
                } else {
                    content = ""; lines = 0
                }
            } else {
                // Large file: read only the tail
                do {
                    let handle = try FileHandle(forReadingFrom: url)
                    defer { try? handle.close() }
                    let offset = UInt64(fileSize) - UInt64(maxBytes)
                    try handle.seek(toOffset: offset)
                    let data = handle.readData(ofLength: maxBytes)
                    if var str = String(data: data, encoding: .utf8) {
                        // Drop the first partial line
                        if let newline = str.firstIndex(of: "\n") {
                            str = String(str[str.index(after: newline)...])
                        }
                        let allLines = str.components(separatedBy: "\n")
                        let tailLines = Array(allLines.suffix(2000))
                        lines = tailLines.count
                        content = "... (showing last \(lines) lines of ~\(ByteCountFormatter.string(fromByteCount: fileSize, countStyle: .file)) file)\n\n"
                            + tailLines.joined(separator: "\n")
                    } else {
                        content = ""; lines = 0
                    }
                } catch {
                    content = "Error reading log: \(error.localizedDescription)"; lines = 0
                }
            }
            await MainActor.run {
                logContent = content
                lineCount = lines
                isLoading = false
            }
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
