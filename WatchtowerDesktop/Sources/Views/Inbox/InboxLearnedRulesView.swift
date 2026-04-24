import SwiftUI
import GRDB

struct InboxLearnedRulesView: View {
    @State private var vm: InboxLearnedRulesViewModel
    @State private var showAdd = false

    init(db: DatabasePool) { _vm = State(initialValue: InboxLearnedRulesViewModel(db: db)) }

    var body: some View {
        List {
            Section("Mutes (\(vm.mutes.count))") {
                ForEach(vm.mutes) { r in ruleRow(r) }
            }
            Section("Boosts (\(vm.boosts.count))") {
                ForEach(vm.boosts) { r in ruleRow(r) }
            }
        }
        .toolbar { Button { showAdd = true } label: { Image(systemName: "plus") } }
        .task { await vm.load() }
        .sheet(isPresented: $showAdd) {
            AddRuleSheet { scope, weight, type in
                await vm.addRule(ruleType: type, scopeKey: scope, weight: weight)
                showAdd = false
            }
        }
    }

    @ViewBuilder private func ruleRow(_ r: InboxLearnedRule) -> some View {
        HStack {
            Text(r.scopeKey).font(.system(.body, design: .monospaced))
            Spacer()
            Text(String(format: "%+.1f", r.weight)).foregroundStyle(r.weight < 0 ? .red : .green)
            Text(r.source).font(.caption).foregroundStyle(.secondary)
            Button(role: .destructive) { Task { await vm.remove(r) } } label: { Image(systemName: "trash") }
                .buttonStyle(.plain)
        }
    }
}

struct AddRuleSheet: View {
    var onSave: (_ scope: String, _ weight: Double, _ type: String) async -> Void
    @State private var scopeType = "sender"
    @State private var scopeValue = ""
    @State private var weight: Double = -0.8
    @State private var ruleType = "source_mute"

    var body: some View {
        Form {
            Picker("Scope type", selection: $scopeType) {
                Text("Sender").tag("sender")
                Text("Channel").tag("channel")
                Text("Jira label").tag("jira_label")
                Text("Trigger").tag("trigger")
            }
            TextField("Scope value", text: $scopeValue)
            Picker("Rule", selection: $ruleType) {
                Text("Mute").tag("source_mute")
                Text("Boost").tag("source_boost")
                Text("Downgrade class").tag("trigger_downgrade")
                Text("Boost trigger").tag("trigger_boost")
            }
            Slider(value: $weight, in: -1...1, step: 0.1) { Text(String(format: "Weight: %+.1f", weight)) }
            Button("Save") {
                Task { await onSave("\(scopeType):\(scopeValue)", weight, ruleType) }
            }
        }.padding()
    }
}
