import Foundation
import GRDB

@MainActor
@Observable
final class ChainsViewModel {
    var chains: [Chain] = []
    var childChains: [Int: [Chain]] = [:]  // parentID → children
    var selectedChainRefs: [ChainRef] = []
    var isLoading = false
    var errorMessage: String?
    var activeChainCount: Int = 0
    var unreadChainCount: Int = 0
    var statusFilter: String?   // nil = all, "active", "resolved", "stale"

    // Pre-fetched caches
    private var channelNameCache: [String: String] = [:]
    private var digestCache: [Int: Digest] = [:]
    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    /// Start observing the chains table for live updates.
    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM chains") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
    }

    private(set) var workspaceTeamID: String?

    private struct LoadResult {
        let chains: [Chain]
        let activeCount: Int
        let unreadCount: Int
        let channelNames: [String: String]
        let childChains: [Int: [Chain]]
        let teamID: String?
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db -> LoadResult in
                let chains = try ChainQueries.fetchAll(db, topLevelOnly: true)
                let activeCount = try ChainQueries.fetchActiveCount(db)
                let unreadCount = try ChainQueries.fetchUnreadCount(db)

                var channelNames: [String: String] = [:]
                let allChannels = try ChannelQueries.fetchAll(db)
                for ch in allChannels {
                    channelNames[ch.id] = ch.name
                }

                let teamID = try String.fetchOne(db, sql: "SELECT id FROM workspace LIMIT 1")

                // Pre-fetch children for all parent chains.
                var childMap: [Int: [Chain]] = [:]
                for chain in chains {
                    let children = try ChainQueries.fetchChildren(db, parentID: chain.id)
                    if !children.isEmpty {
                        childMap[chain.id] = children
                    }
                }

                return LoadResult(
                    chains: chains,
                    activeCount: activeCount,
                    unreadCount: unreadCount,
                    channelNames: channelNames,
                    childChains: childMap,
                    teamID: teamID
                )
            }
            if let filter = statusFilter {
                self.chains = result.chains.filter { $0.status == filter }
            } else {
                self.chains = result.chains
            }
            self.activeChainCount = result.activeCount
            self.unreadChainCount = result.unreadCount
            self.channelNameCache = result.channelNames
            self.childChains = result.childChains
            self.workspaceTeamID = result.teamID
        } catch {
            self.errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func loadRefs(for chainID: Int) {
        do {
            let refs = try dbManager.dbPool.read { db in
                try ChainQueries.fetchRefs(db, chainID: chainID)
            }
            self.selectedChainRefs = refs

            // Pre-fetch digests for the refs (decisions and digest refs both need digest data).
            try dbManager.dbPool.read { db in
                for ref in refs {
                    if (ref.isDecision || ref.isDigest) && digestCache[ref.digestID] == nil {
                        if let digest = try DigestQueries.fetchByID(db, id: ref.digestID) {
                            digestCache[ref.digestID] = digest
                        }
                    }
                }
            }
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func markChainRead(_ id: Int) {
        do {
            try dbManager.dbPool.write { db in
                try ChainQueries.markRead(db, id: id)
            }
            // Also mark contained digests as read.
            if let refs = try? dbManager.dbPool.read({ db in try ChainQueries.fetchRefs(db, chainID: id) }) {
                try dbManager.dbPool.write { db in
                    for ref in refs where ref.isDigest || ref.isDecision {
                        if ref.digestID > 0 {
                            try DigestQueries.markDigestRead(db, id: ref.digestID)
                        }
                    }
                }
            }
            // Update local state.
            if chains.contains(where: { $0.id == id }) {
                // Reload to get updated readAt.
                load()
            } else {
                unreadChainCount = max(0, unreadChainCount - 1)
            }
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func markChainsRead(_ ids: Set<Int>) {
        do {
            try dbManager.dbPool.write { db in
                try ChainQueries.markReadBatch(db, ids: ids)
            }
            load()
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func archiveChain(_ id: Int) {
        do {
            try dbManager.dbPool.write { db in
                try ChainQueries.updateStatus(db, id: id, status: "resolved")
            }
            load()
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func submitFeedback(chainID: Int, rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(sql: """
                    INSERT INTO feedback (entity_type, entity_id, rating, created_at)
                    VALUES ('chain', ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
                    """, arguments: [String(chainID), rating])
            }
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func submitBatchFeedback(chainIDs: [Int], rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                for id in chainIDs {
                    try db.execute(sql: """
                        INSERT INTO feedback (entity_type, entity_id, rating, created_at)
                        VALUES ('chain', ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
                        """, arguments: [String(id), rating])
                }
            }
        } catch {
            self.errorMessage = error.localizedDescription
        }
    }

    func slackChannelURL(channelID: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)")
    }

    func channelName(for channelID: String) -> String {
        channelNameCache[channelID] ?? channelID
    }

    func digest(for id: Int) -> Digest? {
        digestCache[id]
    }

    /// Returns the decision text for a decision ref.
    func decisionText(for ref: ChainRef) -> (text: String, by: String, importance: String)? {
        guard ref.isDecision, let digest = digestCache[ref.digestID] else { return nil }
        let decisions = digest.parsedDecisions
        guard ref.decisionIdx < decisions.count else { return nil }
        let decision = decisions[ref.decisionIdx]
        return (text: decision.text, by: decision.by ?? "", importance: decision.resolvedImportance)
    }

    /// Returns the digest summary for a digest ref.
    func digestSummary(for ref: ChainRef) -> Digest? {
        guard ref.isDigest else { return nil }
        return digestCache[ref.digestID]
    }

    /// Returns children for a chain, if any.
    func children(for chainID: Int) -> [Chain] {
        childChains[chainID] ?? []
    }
}
