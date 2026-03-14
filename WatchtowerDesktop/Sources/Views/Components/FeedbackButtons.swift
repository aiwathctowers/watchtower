import SwiftUI

/// Reusable thumbs up/down feedback buttons for rating AI-generated content.
struct FeedbackButtons: View {
    let entityType: String   // "digest", "track", "decision"
    let entityID: String
    let dbManager: DatabaseManager

    @State private var currentRating: Int? = nil  // nil = not rated, +1 = good, -1 = bad
    @State private var isLoading = false

    var body: some View {
        HStack(spacing: 4) {
            Button {
                submitFeedback(rating: 1)
            } label: {
                Image(systemName: currentRating == 1 ? "hand.thumbsup.fill" : "hand.thumbsup")
                    .foregroundStyle(currentRating == 1 ? .green : .secondary)
            }
            .buttonStyle(.plain)
            .help("Good result")

            Button {
                submitFeedback(rating: -1)
            } label: {
                Image(systemName: currentRating == -1 ? "hand.thumbsdown.fill" : "hand.thumbsdown")
                    .foregroundStyle(currentRating == -1 ? .red : .secondary)
            }
            .buttonStyle(.plain)
            .help("Bad result")
        }
        .disabled(isLoading)
        .task {
            await loadExistingFeedback()
        }
    }

    // M13 fix: use Task (inherits MainActor) instead of Task.detached for safe @State mutation.
    private func submitFeedback(rating: Int) {
        isLoading = true
        Task {
            let success: Bool
            do {
                try await dbManager.dbPool.write { db in
                    try FeedbackQueries.addFeedback(
                        db,
                        entityType: entityType,
                        entityID: entityID,
                        rating: rating
                    )
                }
                success = true
            } catch {
                success = false
            }
            if success {
                currentRating = rating
            }
            isLoading = false
        }
    }

    private func loadExistingFeedback() async {
        let rating: Int? = await Task.detached {
            try? dbManager.dbPool.read { db in
                try FeedbackQueries.getFeedback(db, entityType: entityType, entityID: entityID)?.rating
            }
        }.value
        currentRating = rating
    }
}
