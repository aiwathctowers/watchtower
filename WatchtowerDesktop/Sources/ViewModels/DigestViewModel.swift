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
    private let dbManager: DatabaseManager

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db in
                let d = try DigestQueries.fetchAll(db, type: selectedType)
                let withDecisions = try DigestQueries.fetchWithDecisions(db, limit: 200)
                let ws = try WorkspaceQueries.fetchWorkspace(db)

                // Pre-fetch user names for DM resolution
                let users = try UserQueries.fetchAll(db, activeOnly: false)
                var userNames: [String: String] = [:]
                for u in users {
                    let name = u.displayName.isEmpty ? u.name : u.displayName
                    userNames[u.id] = name
                }

                // Pre-fetch channel names, resolving DMs to user names
                let allChannelIDs = Set((d + withDecisions).map(\.channelID).filter { !$0.isEmpty })
                var nameMap: [String: String] = [:]
                for cid in allChannelIDs {
                    if let ch = try ChannelQueries.fetchByID(db, id: cid) {
                        if (ch.type == "dm" || ch.type == "im"),
                           let dmUID = ch.dmUserID,
                           let userName = userNames[dmUID] {
                            nameMap[cid] = "DM: \(userName)"
                        } else {
                            nameMap[cid] = ch.name
                        }
                    }
                }

                // Pre-fetch decision read states
                let digestIDs = withDecisions.map(\.id)
                let readIndices = try DigestQueries.readDecisionIndices(db, digestIDs: digestIDs)

                let unreadDigests = try DigestQueries.unreadDigestCount(db)

                return (d, withDecisions, nameMap, ws?.domain, readIndices, unreadDigests)
            }
            digests = result.0
            channelNameCache = result.2
            workspaceDomain = result.3
            decisionEntries = buildDecisionEntries(from: result.1, readIndices: result.4)
            unreadDigestCount = result.5
            unreadDecisionCount = decisionEntries.filter { !$0.isRead }.count
            errorMessage = nil
        } catch {
            digests = []
            decisionEntries = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    private func buildDecisionEntries(from digests: [Digest], readIndices: [Int: Set<Int>]) -> [DecisionEntry] {
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
            let date = Date(timeIntervalSince1970: digest.periodTo)
            let chName = digest.channelID.isEmpty ? nil : channelNameCache[digest.channelID]
            let isRead = readIndices[digest.id]?.contains(idx) ?? false
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
                isRead: isRead
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
                decisionEntries[idx] = DecisionEntry(
                    decision: decisionEntries[idx].decision,
                    digestID: decisionEntries[idx].digestID,
                    decisionIdx: decisionEntries[idx].decisionIdx,
                    channelID: decisionEntries[idx].channelID,
                    channelName: decisionEntries[idx].channelName,
                    digestSummary: decisionEntries[idx].digestSummary,
                    digestType: decisionEntries[idx].digestType,
                    date: decisionEntries[idx].date,
                    messageTS: decisionEntries[idx].messageTS,
                    isRead: true
                )
                unreadDecisionCount = max(0, unreadDecisionCount - 1)
            }
        } catch {
            print("Failed to mark decision read: \(error)")
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
            return channels.compactMap { d in
                guard !d.channelID.isEmpty else { return nil }
                let name = channelNameCache[d.channelID] ?? d.channelID
                return (name: name, channelID: d.channelID)
            }
        } catch {
            return []
        }
    }

    /// Build Slack channel URL
    func slackChannelURL(channelID: String) -> URL? {
        guard let domain = workspaceDomain, !domain.isEmpty else { return nil }
        return URL(string: "https://\(domain).slack.com/archives/\(channelID)")
    }

    /// Build Slack message permalink from channel ID and message timestamp
    func slackMessageURL(channelID: String, messageTS: String) -> URL? {
        guard let domain = workspaceDomain, !domain.isEmpty else { return nil }
        let tsForURL = "p" + messageTS.replacingOccurrences(of: ".", with: "")
        return URL(string: "https://\(domain).slack.com/archives/\(channelID)/\(tsForURL)")
    }
}
