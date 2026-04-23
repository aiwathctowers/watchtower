import SwiftUI

struct InboxFeedbackSheet: View {
    let item: InboxItem
    var onSubmit: (Int, String) async -> Void
    @State private var selectedReason: String = "source_noise"

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Why is this not helpful?").font(.headline)
            Picker("Reason", selection: $selectedReason) {
                Text("Source usually noise").tag("source_noise")
                Text("Wrong priority").tag("wrong_priority")
                Text("Wrong class").tag("wrong_class")
                Text("Never show me this").tag("never_show")
            }.pickerStyle(.radioGroup)
            HStack {
                Button("Cancel") { Task { await onSubmit(0, "") } }
                Spacer()
                Button("Apply") { Task { await onSubmit(-1, selectedReason) } }.buttonStyle(.borderedProminent)
            }
        }.padding().frame(width: 320)
    }
}
