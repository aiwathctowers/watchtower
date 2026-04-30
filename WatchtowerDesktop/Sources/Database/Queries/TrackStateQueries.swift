import Foundation
import GRDB

enum TrackStateQueries {

    // MARK: - Fetch

    /// Returns the history of narrative-state snapshots for a track, most
    /// recent first. Empty array if the track has no history.
    /// See docs/inventory/tracks.md TRACKS-06.
    static func fetchByTrackID(_ db: Database, trackID: Int) throws -> [TrackState] {
        try TrackState.fetchAll(
            db,
            sql: """
                SELECT * FROM track_states
                WHERE track_id = ?
                ORDER BY created_at DESC, id DESC
                """,
            arguments: [trackID]
        )
    }

    /// Returns the count of state-history rows for a track.
    static func count(_ db: Database, trackID: Int) throws -> Int {
        try Int.fetchOne(
            db,
            sql: "SELECT COUNT(*) FROM track_states WHERE track_id = ?",
            arguments: [trackID]
        ) ?? 0
    }
}
