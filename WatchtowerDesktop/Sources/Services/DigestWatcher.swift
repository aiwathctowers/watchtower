import Foundation
import GRDB

@MainActor
@Observable
final class DigestWatcher {
    private var watchTask: Task<Void, Never>?
    private let dbPool: DatabasePool
    private let notificationService: NotificationService
    private var lastCheckedDigestID: Int

    init(dbPool: DatabasePool, notificationService: NotificationService = .shared) {
        self.dbPool = dbPool
        self.notificationService = notificationService
        self.lastCheckedDigestID = UserDefaults.standard.integer(forKey: "lastCheckedDigestID")
    }

    func start() {
        // Initialize lastCheckedDigestID if first run
        if lastCheckedDigestID == 0 {
            do {
                let maxID = try dbPool.read { db in
                    try DigestQueries.maxID(db)
                }
                lastCheckedDigestID = maxID
                UserDefaults.standard.set(maxID, forKey: "lastCheckedDigestID")
            } catch {
                // Will start from 0
            }
        }

        watchTask?.cancel()
        watchTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { break }
                self.poll()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    func stop() {
        watchTask?.cancel()
        watchTask = nil
    }

    private func poll() {
        do {
            let newDigests = try dbPool.read { db in
                try DigestQueries.fetchNewSince(db, afterID: lastCheckedDigestID)
            }

            var notificationCount = 0
            for digest in newDigests {
                let channelName = resolveChannelName(for: digest)
                for decision in digest.parsedDecisions {
                    guard notificationCount < 5 else { break }
                    notificationService.sendDecisionNotification(
                        decision: decision,
                        channelName: channelName,
                        digestID: digest.id
                    )
                    notificationCount += 1
                }
                lastCheckedDigestID = digest.id
            }

            if lastCheckedDigestID > 0 {
                UserDefaults.standard.set(lastCheckedDigestID, forKey: "lastCheckedDigestID")
            }
        } catch {
            // M18: log instead of silently swallowing
            print("[DigestWatcher] poll error: \(error.localizedDescription)")
        }
    }

    private nonisolated func resolveChannelName(for digest: Digest) -> String {
        guard !digest.channelID.isEmpty else { return "cross-channel" }
        do {
            return try dbPool.read { db in
                try ChannelQueries.fetchByID(db, id: digest.channelID)?.name ?? digest.channelID
            }
        } catch {
            return digest.channelID
        }
    }
}
