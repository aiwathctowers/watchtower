import SwiftUI

struct StreamingIndicator: View {
    @State private var animatingIndex = 0

    let timer = Timer.publish(every: 0.4, on: .main, in: .common).autoconnect()

    var body: some View {
        HStack(spacing: 4) {
            ForEach(0..<3, id: \.self) { i in
                Circle()
                    .fill(.secondary)
                    .frame(width: 6, height: 6)
                    .opacity(i == animatingIndex ? 1.0 : 0.3)
                    .animation(.easeInOut(duration: 0.3), value: animatingIndex)
            }
        }
        .onReceive(timer) { _ in
            animatingIndex = (animatingIndex + 1) % 3
        }
    }
}
