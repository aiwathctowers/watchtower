import Foundation
import GRDB

@MainActor
@Observable
final class DigestWatcher {
    private var watchTask: Task<Void, Never>?
    private let dbPool: DatabasePool
    private let notificationService: NotificationService
    private var lastCheckedDigestID: Int
    private var lastCheckedBriefingID: Int

    init(dbPool: DatabasePool, notificationService: NotificationService = .shared) {
        self.dbPool = dbPool
        self.notificationService = notificationService
        self.lastCheckedDigestID = UserDefaults.standard.integer(forKey: "lastCheckedDigestID")
        self.lastCheckedBriefingID = UserDefaults.standard.integer(forKey: "lastCheckedBriefingID")
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

        if lastCheckedBriefingID == 0 {
            do {
                let maxID = try dbPool.read { db in
                    try BriefingQueries.maxID(db)
                }
                lastCheckedBriefingID = maxID
                UserDefaults.standard.set(maxID, forKey: "lastCheckedBriefingID")
            } catch {}
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
        let notifyDecisions = UserDefaults.standard.bool(forKey: "notifyDecisions")
        // notifyDecisions defaults to true — AppStorage default is true, but UserDefaults returns false
        // if the key was never set. Check if key exists; if not, treat as enabled.
        let decisionsEnabled = UserDefaults.standard.object(forKey: "notifyDecisions") == nil || notifyDecisions
        let quietHours = UserDefaults.standard.bool(forKey: "quietHoursEnabled")

        guard !quietHours else { return }

        do {
            let newDigests = try dbPool.read { db in
                try DigestQueries.fetchNewSince(db, afterID: lastCheckedDigestID)
            }

            var notificationCount = 0
            for digest in newDigests {
                if decisionsEnabled {
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
                }
                lastCheckedDigestID = digest.id
            }

            if lastCheckedDigestID > 0 {
                UserDefaults.standard.set(lastCheckedDigestID, forKey: "lastCheckedDigestID")
            }
        } catch {
            print("[DigestWatcher] poll error: \(error.localizedDescription)")
        }

        // Poll for new briefings
        do {
            let newBriefings = try dbPool.read { db in
                try BriefingQueries.fetchNewSince(db, afterID: lastCheckedBriefingID)
            }
            for briefing in newBriefings {
                notificationService.sendBriefingNotification(
                    attentionCount: briefing.parsedAttention.count
                )
                lastCheckedBriefingID = briefing.id
            }
            if lastCheckedBriefingID > 0 {
                UserDefaults.standard.set(lastCheckedBriefingID, forKey: "lastCheckedBriefingID")
            }
        } catch {
            print("[DigestWatcher] briefing poll error: \(error.localizedDescription)")
        }
    }

    nonisolated private func resolveChannelName(for digest: Digest) -> String {
        guard !digest.channelID.isEmpty else { return "cross-channel" }
        do {
            return try dbPool.read { db in
                guard let ch = try ChannelQueries.fetchByID(db, id: digest.channelID) else {
                    return digest.channelID
                }
                if ch.type == "dm" || ch.type == "im" {
                    let uid = ch.dmUserID ?? (ch.name.hasPrefix("U") ? ch.name : nil)
                    if let uid,
                       let user = try UserQueries.fetchByID(db, id: uid) {
                        let displayName = user.displayName.isEmpty ? user.name : user.displayName
                        return "DM: \(displayName)"
                    }
                }
                return ch.name
            }
        } catch {
            return digest.channelID
        }
    }
}
