import Foundation
import GRDB

@MainActor
@Observable
final class DigestViewModel {
    var digests: [Digest] = []
    var decisionEntries: [DecisionEntry] = []
    var selectedType: String?
    var isLoading = false
    var errorMessage: String?
    var unreadDigestCount: Int = 0
    var unreadDecisionCount: Int = 0

    // M9: pre-fetched caches (avoids DB read per row in view body)
    private var channelNameCache: [String: String] = [:]
    private(set) var workspaceDomain: String?
    private(set) var workspaceTeamID: String?
    private(set) var starredChannelIDs: Set<String> = []
    private(set) var currentUserID: String?
    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    /// Start observing the digests table for live updates.
    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM digests") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
    }

    private struct LoadResult {
        let digests: [Digest]
        let withDecisions: [Digest]
        let channelNames: [String: String]
        let domain: String?
        let teamID: String?
        let readIndices: [Int: Set<Int>]
        let unreadDigests: Int
        let importanceCorrections: [String: String]
        let starredChannels: Set<String>
        let currentUserID: String?
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db -> LoadResult in
                let digests = try DigestQueries.fetchAll(db, type: selectedType)
                let withDecisions = try DigestQueries.fetchWithDecisions(db, limit: 200)
                let ws = try WorkspaceQueries.fetchWorkspace(db)

                // Pre-fetch user names for DM resolution
                let users = try UserQueries.fetchAll(db, activeOnly: false)
                var userNames: [String: String] = [:]
                for user in users {
                    let name = user.displayName.isEmpty ? user.name : user.displayName
                    userNames[user.id] = name
                }

                // Pre-fetch channel names, resolving DMs to user names
                let allChannelIDs = Set((digests + withDecisions).map(\.channelID).filter { !$0.isEmpty })
                var nameMap: [String: String] = [:]
                for cid in allChannelIDs {
                    if let ch = try ChannelQueries.fetchByID(db, id: cid) {
                        if ch.type == "dm" || ch.type == "im" {
                            // Try dm_user_id first, then fall back to name if it looks like a user ID
                            let resolvedUID = ch.dmUserID ?? (ch.name.hasPrefix("U") ? ch.name : nil)
                            if let uid = resolvedUID, let userName = userNames[uid] {
                                nameMap[cid] = "DM: \(userName)"
                            } else {
                                nameMap[cid] = ch.name
                            }
                        } else {
                            nameMap[cid] = ch.name
                        }
                    }
                }

                // Pre-fetch decision read states
                let digestIDs = withDecisions.map(\.id)
                let readIndices = try DigestQueries.readDecisionIndices(db, digestIDs: digestIDs)

                let unreadDigests = try DigestQueries.unreadDigestCount(db)
                let importanceCorrections = try ImportanceCorrectionQueries.allCorrections(db)

                let profile = try ProfileQueries.fetchCurrentProfile(db)
                let starred = Set(profile?.decodedStarredChannels ?? [])
                let uid = profile?.slackUserID

                return LoadResult(
                    digests: digests,
                    withDecisions: withDecisions,
                    channelNames: nameMap,
                    domain: ws?.domain,
                    teamID: ws?.id,
                    readIndices: readIndices,
                    unreadDigests: unreadDigests,
                    importanceCorrections: importanceCorrections,
                    starredChannels: starred,
                    currentUserID: uid
                )
            }
            digests = result.digests
            channelNameCache = result.channelNames
            workspaceDomain = result.domain
            workspaceTeamID = result.teamID
            starredChannelIDs = result.starredChannels
            currentUserID = result.currentUserID
            decisionEntries = buildDecisionEntries(
                from: result.withDecisions,
                readIndices: result.readIndices,
                importanceCorrections: result.importanceCorrections
            )
            unreadDigestCount = result.unreadDigests
            unreadDecisionCount = decisionEntries.filter { !$0.isRead }.count
            errorMessage = nil
        } catch {
            digests = []
            decisionEntries = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    private func buildDecisionEntries(
        from digests: [Digest],
        readIndices: [Int: Set<Int>],
        importanceCorrections: [String: String] = [:]
    ) -> [DecisionEntry] {
        // Separate digests by type: prefer higher-level (daily/weekly) over channel
        let dailyWeekly = digests.filter { $0.type == "daily" || $0.type == "weekly" }
        let channelOnly = digests.filter { $0.type == "channel" }

        var entries: [DecisionEntry] = []
        var seenStems: [Set<String>] = []

        // Extract word stems (first 4 chars of words >= 4 chars) for fuzzy dedup
        func wordStems(_ text: String) -> Set<String> {
            let cleaned = text.lowercased()
                .replacingOccurrences(of: "[^\\p{L}\\p{N}]+", with: " ", options: .regularExpression)
            let words = cleaned.split(separator: " ").map(String.init).filter { $0.count >= 4 }
            return Set(words.map { String($0.prefix(4)) })
        }

        // Containment similarity: fraction of shorter set's stems found in longer set
        func isDuplicate(_ text: String) -> Bool {
            let stems = wordStems(text)
            guard stems.count >= 2 else { return false }
            for seen in seenStems {
                let intersection = stems.intersection(seen).count
                let minSize = min(stems.count, seen.count)
                if minSize > 0 && Double(intersection) / Double(minSize) > 0.6 {
                    return true
                }
            }
            return false
        }

        func addDecision(_ decision: Decision, idx: Int, from digest: Digest) {
            guard !isDuplicate(decision.text) else { return }
            seenStems.append(wordStems(decision.text))
            // Use the decision's source message timestamp when available,
            // falling back to the digest's periodTo.
            let date: Date
            if let ts = decision.messageTS,
               let dot = ts.firstIndex(of: "."),
               let unix = Double(ts[ts.startIndex..<dot]) {
                date = Date(timeIntervalSince1970: unix)
            } else if let ts = decision.messageTS, let unix = Double(ts) {
                date = Date(timeIntervalSince1970: unix)
            } else {
                date = Date(timeIntervalSince1970: digest.periodTo)
            }
            let chName = digest.channelID.isEmpty ? nil : channelNameCache[digest.channelID]
            let isRead = readIndices[digest.id]?.contains(idx) ?? false
            let correctionKey = "\(digest.id):\(idx)"
            let corrected = importanceCorrections[correctionKey]
            entries.append(DecisionEntry(
                decision: decision,
                digestID: digest.id,
                decisionIdx: idx,
                channelID: digest.channelID,
                channelName: chName,
                digestSummary: digest.summary,
                digestType: digest.type,
                date: date,
                messageTS: decision.messageTS,
                isRead: isRead,
                correctedImportance: corrected
            ))
        }

        // First pass: add decisions from daily/weekly rollups (preferred)
        for digest in dailyWeekly {
            for (idx, decision) in digest.parsedDecisions.enumerated() {
                addDecision(decision, idx: idx, from: digest)
            }
        }

        // Second pass: add channel-level decisions only if not already covered
        for digest in channelOnly {
            for (idx, decision) in digest.parsedDecisions.enumerated() {
                addDecision(decision, idx: idx, from: digest)
            }
        }

        entries.sort { $0.date > $1.date }
        return entries
    }

    // MARK: - Read tracking

    func markDigestRead(_ digestID: Int) {
        do {
            try dbManager.dbPool.write { db in
                try DigestQueries.markDigestRead(db, id: digestID)
            }
            // Update local state
            if let idx = digests.firstIndex(where: { $0.id == digestID && !$0.isRead }) {
                unreadDigestCount = max(0, unreadDigestCount - 1)
                // Reload to get updated readAt
                if let updated = digestByID(digestID) {
                    digests[idx] = updated
                }
            }
        } catch {
            // Non-critical — just log
            print("Failed to mark digest read: \(error)")
        }
    }

    func markDecisionRead(digestID: Int, decisionIdx: Int) {
        do {
            try dbManager.dbPool.write { db in
                try DigestQueries.markDecisionRead(db, digestID: digestID, decisionIdx: decisionIdx)
            }
            // Update local state
            if let idx = decisionEntries.firstIndex(where: {
                $0.digestID == digestID && $0.decisionIdx == decisionIdx && !$0.isRead
            }) {
                decisionEntries[idx] = decisionEntries[idx].with(isRead: true)
                unreadDecisionCount = max(0, unreadDecisionCount - 1)
            }
        } catch {
            print("Failed to mark decision read: \(error)")
        }
    }

    // MARK: - Batch operations

    func markDigestsRead(_ ids: Set<Int>) {
        do {
            try dbManager.dbPool.write { db in
                for id in ids {
                    try DigestQueries.markDigestRead(db, id: id)
                }
            }
            for id in ids {
                if let idx = digests.firstIndex(where: { $0.id == id && !$0.isRead }) {
                    if let updated = digestByID(id) {
                        digests[idx] = updated
                    }
                    unreadDigestCount = max(0, unreadDigestCount - 1)
                }
            }
        } catch {
            print("Failed to mark digests read: \(error)")
        }
    }

    func markDecisionsRead(_ entries: [DecisionEntry]) {
        do {
            try dbManager.dbPool.write { db in
                for entry in entries {
                    try DigestQueries.markDecisionRead(db, digestID: entry.digestID, decisionIdx: entry.decisionIdx)
                }
            }
            for entry in entries {
                if let idx = decisionEntries.firstIndex(where: {
                    $0.digestID == entry.digestID && $0.decisionIdx == entry.decisionIdx && !$0.isRead
                }) {
                    decisionEntries[idx] = decisionEntries[idx].with(isRead: true)
                    unreadDecisionCount = max(0, unreadDecisionCount - 1)
                }
            }
        } catch {
            print("Failed to mark decisions read: \(error)")
        }
    }

    func submitBatchFeedback(entityType: String, entityIDs: [String], rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                for entityID in entityIDs {
                    try FeedbackQueries.addFeedback(db, entityType: entityType, entityID: entityID, rating: rating)
                }
            }
        } catch {
            print("Failed to submit batch feedback: \(error)")
        }
    }

    // MARK: - Importance corrections

    func setDecisionImportance(_ entry: DecisionEntry, newImportance: String) {
        let originalImportance = entry.decision.resolvedImportance
        guard newImportance != originalImportance else {
            // User reverted to original — delete correction
            do {
                try dbManager.dbPool.write { db in
                    try ImportanceCorrectionQueries.delete(db, digestID: entry.digestID, decisionIdx: entry.decisionIdx)
                }
                updateEntryImportance(entry, corrected: nil)
            } catch {
                print("Failed to delete importance correction: \(error)")
            }
            return
        }
        do {
            try dbManager.dbPool.write { db in
                try ImportanceCorrectionQueries.upsert(
                    db,
                    digestID: entry.digestID,
                    decisionIdx: entry.decisionIdx,
                    decisionText: entry.decision.text,
                    originalImportance: originalImportance,
                    newImportance: newImportance
                )
            }
            updateEntryImportance(entry, corrected: newImportance)
        } catch {
            print("Failed to save importance correction: \(error)")
        }
    }

    private func updateEntryImportance(_ entry: DecisionEntry, corrected: String?) {
        if let idx = decisionEntries.firstIndex(where: {
            $0.digestID == entry.digestID && $0.decisionIdx == entry.decisionIdx
        }) {
            decisionEntries[idx] = decisionEntries[idx].with(correctedImportance: corrected)
        }
    }

    func digestByID(_ id: Int) -> Digest? {
        do {
            return try dbManager.dbPool.read { db in
                try DigestQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func channelName(for digest: Digest) -> String? {
        guard !digest.channelID.isEmpty else { return nil }
        return channelNameCache[digest.channelID]
    }

    /// Returns contributing channel digests for a cross-channel digest (daily/weekly).
    func contributingChannels(for digest: Digest) -> [(name: String, channelID: String)] {
        guard digest.channelID.isEmpty, digest.type == "daily" || digest.type == "weekly" else {
            return []
        }
        do {
            let channels = try dbManager.dbPool.read { db in
                try DigestQueries.fetchAll(db, type: "channel")
                    .filter { $0.periodFrom >= digest.periodFrom && $0.periodTo <= digest.periodTo }
            }
            return channels.compactMap { digest in
                guard !digest.channelID.isEmpty else { return nil }
                let name = channelNameCache[digest.channelID] ?? digest.channelID
                return (name: name, channelID: digest.channelID)
            }
        } catch {
            return []
        }
    }

    /// Build Slack channel deep link (opens Slack app directly)
    func slackChannelURL(channelID: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)")
    }

    /// Build Slack message deep link (opens Slack app directly)
    func slackMessageURL(channelID: String, messageTS: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)&message=\(messageTS)")
    }

    // MARK: - Starred Channels Management

    /// Toggle a channel's starred status
    func toggleStarredChannel(_ channelID: String) {
        guard let userID = currentUserID else { return }
        let wasStarred = starredChannelIDs.contains(channelID)
        // Optimistic update
        if wasStarred {
            starredChannelIDs.remove(channelID)
        } else {
            starredChannelIDs.insert(channelID)
        }
        do {
            if wasStarred {
                try dbManager.removeStarredChannel(channelID, for: userID)
            } else {
                try dbManager.addStarredChannel(channelID, for: userID)
            }
        } catch {
            // Revert on failure
            if wasStarred {
                starredChannelIDs.insert(channelID)
            } else {
                starredChannelIDs.remove(channelID)
            }
            errorMessage = "Failed to update starred channel: \(error.localizedDescription)"
        }
    }

    /// Check if a channel is starred
    func isChannelStarred(_ channelID: String) -> Bool {
        starredChannelIDs.contains(channelID)
    }
}
