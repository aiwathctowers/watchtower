import SwiftUI

struct StarToggleButton: View {
    let isStarred: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: isStarred ? "star.fill" : "star")
                .foregroundStyle(isStarred ? .yellow : .secondary)
                .font(.system(size: 14))
        }
        .buttonStyle(.borderless)
        .help(isStarred ? "Unstar" : "Star")
    }
}

#Preview {
    VStack(spacing: 10) {
        StarToggleButton(isStarred: true) {}
        StarToggleButton(isStarred: false) {}
    }
    .padding()
}
