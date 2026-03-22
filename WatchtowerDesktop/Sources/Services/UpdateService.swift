import Foundation
import AppKit

/// Handles checking for updates via GitHub Releases API, downloading, and installing.
@MainActor
@Observable
final class UpdateService {
    enum UpdateState: Equatable {
        case idle
        case checking
        case available(version: String, notes: String, downloadURL: URL)
        case downloading(progress: Double)
        case readyToInstall(appPath: URL)
        case installing
        case error(String)
    }

    var state: UpdateState = .idle

    var isUpdateAvailable: Bool {
        if case .available = state { return true }
        if case .readyToInstall = state { return true }
        return false
    }

    private static let repo = "vadimtrunov/watchtower"
    private static let lastCheckKey = "lastUpdateCheckDate"
    private static let cacheDir: URL = {
        guard let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first else {
            return FileManager.default.temporaryDirectory.appendingPathComponent("com.watchtower.desktop/updates", isDirectory: true)
        }
        return caches.appendingPathComponent("com.watchtower.desktop/updates", isDirectory: true)
    }()

    // MARK: - Check for Updates

    func checkForUpdates() async {
        state = .checking

        do {
            let release = try await fetchLatestRelease()
            let current = Constants.appVersion
            guard Self.isNewer(release.tagName, than: current) else {
                state = .idle
                UserDefaults.standard.set(Date(), forKey: Self.lastCheckKey)
                return
            }

            guard let asset = release.assets.first(where: { $0.name.hasSuffix(".zip") }) else {
                state = .error("No ZIP asset in release \(release.tagName)")
                return
            }

            guard let url = URL(string: asset.browserDownloadURL) else {
                state = .error("Invalid download URL")
                return
            }

            state = .available(
                version: release.tagName,
                notes: release.body ?? "",
                downloadURL: url
            )
            UserDefaults.standard.set(Date(), forKey: Self.lastCheckKey)
        } catch {
            state = .error(error.localizedDescription)
        }
    }

    /// Check if 24 hours have passed since last check, and if so, check for updates.
    func checkIfNeeded() async {
        if let last = UserDefaults.standard.object(forKey: Self.lastCheckKey) as? Date,
           Date().timeIntervalSince(last) < 86400 {
            return
        }
        await checkForUpdates()
    }

    // MARK: - Download

    func downloadUpdate() async {
        guard case .available(_, _, let downloadURL) = state else { return }

        state = .downloading(progress: 0)

        do {
            let fm = FileManager.default
            try fm.createDirectory(at: Self.cacheDir, withIntermediateDirectories: true)

            // Clean previous downloads
            let zipPath = Self.cacheDir.appendingPathComponent("update.zip")
            let extractDir = Self.cacheDir.appendingPathComponent("extracted")
            try? fm.removeItem(at: zipPath)
            try? fm.removeItem(at: extractDir)

            // Download with progress
            let (localURL, _) = try await downloadWithProgress(from: downloadURL)

            try fm.moveItem(at: localURL, to: zipPath)

            state = .downloading(progress: 0.9)

            // Extract using ditto (handles macOS resource forks correctly)
            try fm.createDirectory(at: extractDir, withIntermediateDirectories: true)
            let exitCode = try await runProcess(
                path: "/usr/bin/ditto",
                arguments: ["-xk", zipPath.path, extractDir.path]
            )
            guard exitCode == 0 else {
                state = .error("Failed to extract update (exit \(exitCode))")
                return
            }

            // Find the .app inside extracted directory
            guard let appName = try fm.contentsOfDirectory(atPath: extractDir.path)
                .first(where: { $0.hasSuffix(".app") }) else {
                state = .error("No .app found in downloaded archive")
                return
            }

            let appPath = extractDir.appendingPathComponent(appName)
            state = .readyToInstall(appPath: appPath)
        } catch {
            state = .error("Download failed: \(error.localizedDescription)")
        }
    }

    // MARK: - Install

    func install(daemonManager: DaemonManager) async {
        guard case .readyToInstall(let newAppPath) = state else { return }

        state = .installing

        // 1. Stop daemon
        await daemonManager.stopDaemon()

        // 2. Determine current app location
        guard let currentAppPath = Self.currentAppBundlePath() else {
            state = .error("Cannot determine current app location")
            return
        }

        // 3. Generate and run helper script
        let script = Self.generateHelperScript(
            currentAppPath: currentAppPath,
            newAppPath: newAppPath.path,
            pid: ProcessInfo.processInfo.processIdentifier
        )

        let scriptPath = Self.cacheDir.appendingPathComponent("update.sh")
        do {
            try script.write(to: scriptPath, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes(
                [.posixPermissions: 0o755],
                ofItemAtPath: scriptPath.path
            )
        } catch {
            state = .error("Failed to write update script: \(error.localizedDescription)")
            return
        }

        // 4. Launch helper script and exit
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = [scriptPath.path]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        // Detach from parent process group so it survives our exit
        process.qualityOfService = .userInitiated
        do {
            try process.run()
        } catch {
            state = .error("Failed to launch updater: \(error.localizedDescription)")
            return
        }

        // 5. Quit the app — the helper script takes over
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            NSApplication.shared.terminate(nil)
        }
    }

    // MARK: - Helper Script

    /// Escape a string for safe use inside double-quoted shell strings.
    /// Only the four characters special inside double quotes need escaping: " \ ` $
    private static func shellEscape(_ s: String) -> String {
        var result = ""
        for ch in s {
            switch ch {
            case "\"", "\\", "`", "$":
                result.append("\\")
                result.append(ch)
            default:
                result.append(ch)
            }
        }
        return result
    }

    private static func generateHelperScript(currentAppPath: String, newAppPath: String, pid: pid_t) -> String {
        // C1 fix: escape paths for safe shell interpolation
        let escapedCurrent = shellEscape(currentAppPath)
        let escapedNew = shellEscape(newAppPath)
        let escapedCache = shellEscape(Self.cacheDir.path)
        return """
        #!/bin/sh
        # Watchtower auto-update helper script
        # Wait for the app to exit
        while kill -0 \(pid) 2>/dev/null; do
            sleep 0.5
        done

        # Small extra delay to ensure file handles are released
        sleep 1

        # Verify codesign on the new app before replacing
        if ! /usr/bin/codesign --verify --deep --strict "\(escapedNew)" 2>/dev/null; then
            echo "ERROR: Code signature verification failed. Aborting update." >&2
            exit 1
        fi

        # Remove old app
        rm -rf "\(escapedCurrent)"

        # Move new app into place
        mv "\(escapedNew)" "\(escapedCurrent)"

        # Clear quarantine attribute (downloaded file)
        xattr -dr com.apple.quarantine "\(escapedCurrent)" 2>/dev/null

        # Relaunch
        open "\(escapedCurrent)"

        # Cleanup
        rm -rf "\(escapedCache)"
        """
    }

    // MARK: - Helpers

    private static func currentAppBundlePath() -> String? {
        // Bundle.main.bundleURL points to Watchtower.app/
        let bundleURL = Bundle.main.bundleURL
        guard bundleURL.pathExtension == "app" else { return nil }
        return bundleURL.path
    }

    private func fetchLatestRelease() async throws -> GitHubRelease {
        let urlString = "https://api.github.com/repos/\(Self.repo)/releases/latest"
        guard let url = URL(string: urlString) else {
            throw URLError(.badURL)
        }

        var request = URLRequest(url: url)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("Watchtower/\(Constants.appVersion)", forHTTPHeaderField: "User-Agent")
        request.timeoutInterval = 15

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
            let code = (response as? HTTPURLResponse)?.statusCode ?? 0
            throw UpdateError.httpError(code)
        }

        return try JSONDecoder().decode(GitHubRelease.self, from: data)
    }

    private func downloadWithProgress(from url: URL) async throws -> (URL, URLResponse) {
        // Use a simple download — URLSession delegate progress would add complexity
        // For ~12MB ZIP this is fast enough
        let (localURL, response) = try await URLSession.shared.download(from: url)
        await MainActor.run { state = .downloading(progress: 0.8) }
        return (localURL, response)
    }

    nonisolated private func runProcess(path: String, arguments: [String]) async throws -> Int32 {
        try await Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.arguments = arguments
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            try process.run()
            process.waitUntilExit()
            return process.terminationStatus
        }.value
    }

    /// Compare semantic versions. Returns true if `new` is strictly greater than `current`.
    nonisolated static func isNewer(_ new: String, than current: String) -> Bool {
        let parse: (String) -> [Int] = { version in
            let cleaned = version.hasPrefix("v") ? String(version.dropFirst()) : version
            return cleaned.split(separator: ".").compactMap { Int($0) }
        }
        let newParts = parse(new)
        let currentParts = parse(current)

        for i in 0..<max(newParts.count, currentParts.count) {
            let nv = i < newParts.count ? newParts[i] : 0
            let cv = i < currentParts.count ? currentParts[i] : 0
            if nv > cv { return true }
            if nv < cv { return false }
        }
        return false
    }
}

// MARK: - Models

struct GitHubRelease: Decodable {
    let tagName: String
    let name: String?
    let body: String?
    let assets: [GitHubAsset]

    enum CodingKeys: String, CodingKey {
        case tagName = "tag_name"
        case name, body, assets
    }
}

struct GitHubAsset: Decodable {
    let name: String
    let browserDownloadURL: String
    let size: Int

    enum CodingKeys: String, CodingKey {
        case name
        case browserDownloadURL = "browser_download_url"
        case size
    }
}

enum UpdateError: LocalizedError {
    case httpError(Int)

    var errorDescription: String? {
        switch self {
        case .httpError(let code):
            "GitHub API returned status \(code)"
        }
    }
}
