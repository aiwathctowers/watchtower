import Foundation
import GRDB

@MainActor
@Observable
final class InboxLearnedRulesViewModel {
    var mutes: [InboxLearnedRule] = []   // weight < 0
    var boosts: [InboxLearnedRule] = []  // weight > 0

    private let queries: InboxLearnedRulesQueries

    init(db: DatabasePool) {
        self.queries = InboxLearnedRulesQueries(dbPool: db)
    }

    func load() async {
        let all = (try? queries.listAll()) ?? []
        mutes = all.filter { $0.weight < 0 }
        boosts = all.filter { $0.weight > 0 }
    }

    func addRule(ruleType: String, scopeKey: String, weight: Double) async {
        try? queries.upsertManual(ruleType: ruleType, scopeKey: scopeKey, weight: weight)
        await load()
    }

    func remove(_ rule: InboxLearnedRule) async {
        try? queries.delete(ruleType: rule.ruleType, scopeKey: rule.scopeKey)
        await load()
    }
}
