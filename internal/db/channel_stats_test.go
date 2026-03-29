package db

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStatsTestData(t *testing.T, db *DB) {
	t.Helper()

	// Create channels
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C1", Name: "general", Type: "public", IsMember: true, NumMembers: 10,
	}))
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C2", Name: "bot-alerts", Type: "public", IsMember: true, NumMembers: 5,
	}))
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C3", Name: "old-project", Type: "public", IsMember: true, NumMembers: 3,
	}))
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C4", Name: "dm-channel", Type: "dm", IsMember: true,
		DMUserID: sql.NullString{String: "U2", Valid: true},
	}))

	// Create users
	_, err := db.Exec(`INSERT INTO users (id, name, is_bot) VALUES ('U1', 'alice', 0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, name, is_bot) VALUES ('U2', 'bob', 0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, name, is_bot) VALUES ('UBOT', 'alertbot', 1)`)
	require.NoError(t, err)

	// Messages in C1 (general): alice posts, bob posts, alice is mentioned
	for i := range 20 {
		userID := "U2"
		text := "hello from bob"
		if i < 5 {
			userID = "U1"
			text = "hello from alice"
		}
		if i == 10 {
			text = "hey <@U1> check this"
		}
		if i == 11 {
			text = "also <@U1> fyi"
		}
		ts := 1700000000.0 + float64(i)
		_, err := db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES (?, ?, ?, ?)`,
			"C1", tsStr(ts), userID, text)
		require.NoError(t, err)
	}

	// Messages in C2 (bot-alerts): all from bot
	for i := range 60 {
		ts := 1700000000.0 + float64(i)
		_, err := db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES (?, ?, ?, ?)`,
			"C2", tsStr(ts), "UBOT", "alert: something")
		require.NoError(t, err)
	}

	// Messages in C3 (old-project): some from bob, old activity
	for i := range 10 {
		ts := 1600000000.0 + float64(i) // old
		_, err := db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES (?, ?, ?, ?)`,
			"C3", tsStr(ts), "U2", "old msg")
		require.NoError(t, err)
	}

	// Add watch on C1
	_, err = db.Exec(`INSERT INTO watch_list (entity_type, entity_id, entity_name) VALUES ('channel', 'C1', 'general')`)
	require.NoError(t, err)
}

func tsStr(ts float64) string {
	sec := int64(ts)
	frac := int64((ts - float64(sec)) * 1e6)
	return fmt.Sprintf("%d.%06d", sec, frac)
}

func TestGetChannelStats(t *testing.T) {
	db := openTestDB(t)
	setupStatsTestData(t, db)

	stats, err := db.GetChannelStats("U1")
	require.NoError(t, err)
	require.NotEmpty(t, stats)

	// Find C1 (general)
	var c1 *ChannelStatRow
	for i := range stats {
		if stats[i].ChannelID == "C1" {
			c1 = &stats[i]
			break
		}
	}
	require.NotNil(t, c1, "C1 should be in stats")
	assert.Equal(t, "general", c1.ChannelName)
	assert.Equal(t, 20, c1.TotalMsgs)
	assert.Equal(t, 5, c1.UserMsgs)
	assert.Equal(t, 2, c1.Mentions)
	assert.True(t, c1.IsWatched)
	assert.False(t, c1.IsMuted)
	assert.Equal(t, 0, c1.BotMsgs)

	// Find C2 (bot-alerts)
	var c2 *ChannelStatRow
	for i := range stats {
		if stats[i].ChannelID == "C2" {
			c2 = &stats[i]
			break
		}
	}
	require.NotNil(t, c2, "C2 should be in stats")
	assert.Equal(t, 60, c2.TotalMsgs)
	assert.Equal(t, 60, c2.BotMsgs)
	assert.InDelta(t, 1.0, c2.BotRatio, 0.01)
	assert.Equal(t, 0, c2.UserMsgs)
}

func TestGetChannelStats_EmptyUserID(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetChannelStats("")
	assert.Error(t, err)
}

func TestGetChannelStats_MutedAndFavorite(t *testing.T) {
	db := openTestDB(t)
	setupStatsTestData(t, db)

	require.NoError(t, db.SetMuteForLLM("C2", true))
	require.NoError(t, db.SetFavorite("C1", true))

	stats, err := db.GetChannelStats("U1")
	require.NoError(t, err)

	for _, s := range stats {
		switch s.ChannelID {
		case "C1":
			assert.True(t, s.IsFavorite)
			assert.False(t, s.IsMuted)
		case "C2":
			assert.True(t, s.IsMuted)
			assert.False(t, s.IsFavorite)
		}
	}
}

func TestComputeRecommendations_MuteCandidate(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "bot-alerts", TotalMsgs: 60, UserMsgs: 0, Mentions: 0, BotRatio: 1.0},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "mute", recs[0].Action)
	assert.Equal(t, "C1", recs[0].ChannelID)
}

func TestComputeRecommendations_MuteSkippedIfFavorite(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "bot-alerts", TotalMsgs: 60, UserMsgs: 0, BotRatio: 1.0, IsFavorite: true},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	// Should not suggest mute for a favorite channel
	for _, r := range recs {
		assert.NotEqual(t, "mute", r.Action)
	}
}

func TestComputeRecommendations_MuteSkippedIfAlreadyMuted(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "bot-alerts", TotalMsgs: 60, UserMsgs: 0, BotRatio: 1.0, IsMuted: true},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	for _, r := range recs {
		assert.NotEqual(t, "mute", r.Action)
	}
}

func TestComputeRecommendations_LeaveCandidate(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "old-project", ChannelType: "public", IsMember: true,
			TotalMsgs: 10, UserMsgs: 0, LastUserActivity: 1600000000},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "leave", recs[0].Action)
}

func TestComputeRecommendations_LeaveSkippedForDM(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "dm", ChannelType: "dm", IsMember: true,
			TotalMsgs: 10, UserMsgs: 0},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	for _, r := range recs {
		assert.NotEqual(t, "leave", r.Action)
	}
}

func TestComputeRecommendations_LeaveSkippedIfWatched(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "watched", ChannelType: "public", IsMember: true,
			TotalMsgs: 10, UserMsgs: 0, IsWatched: true},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	for _, r := range recs {
		assert.NotEqual(t, "leave", r.Action)
	}
}

func TestComputeRecommendations_FavoriteCandidate(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "team", TotalMsgs: 100, UserMsgs: 15, Mentions: 5},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "favorite", recs[0].Action)
}

func TestComputeRecommendations_FavoriteForWatched(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "watched", TotalMsgs: 5, UserMsgs: 1, Mentions: 0, IsWatched: true},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "favorite", recs[0].Action)
	assert.Equal(t, "watched channel", recs[0].Reason)
}

func TestComputeRecommendations_FavoriteSkippedIfAlreadyFavorite(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "team", TotalMsgs: 100, UserMsgs: 15, Mentions: 5, IsFavorite: true},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	for _, r := range recs {
		assert.NotEqual(t, "favorite", r.Action)
	}
}

func TestComputeRecommendations_HighVolumeNoParticipation(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "noisy", TotalMsgs: 50, UserMsgs: 0, Mentions: 0, BotRatio: 0.3},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "mute", recs[0].Action)
	assert.Equal(t, "high volume with no participation", recs[0].Reason)
}

// --- Value Signals Tests ---

func TestGetChannelValueSignals_Empty(t *testing.T) {
	db := openTestDB(t)
	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestGetChannelValueSignals_Basic(t *testing.T) {
	db := openTestDB(t)

	// Setup: channel + workspace
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public", IsMember: true}))
	_, err := db.Exec(`INSERT INTO workspace (id, name, current_user_id) VALUES ('T1', 'test', 'U1')`)
	require.NoError(t, err)

	// Decision: insert digest + digest_topic with non-empty decisions
	_, err = db.Exec(`INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count) VALUES ('C1', ?, ?, 'channel', 'test', 5)`,
		float64(time.Now().Unix()-86400), float64(time.Now().Unix()))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO digest_topics (digest_id, idx, title, decisions) VALUES (1, 0, 'topic1', '[{"text":"do X"}]')`)
	require.NoError(t, err)

	// Track linked to C1
	_, err = db.Exec(`INSERT INTO tracks (text, channel_ids) VALUES ('track1', '["C1"]')`)
	require.NoError(t, err)

	// Task via digest
	_, err = db.Exec(`INSERT INTO tasks (text, status, source_type, source_id) VALUES ('task1', 'todo', 'digest', '1')`)
	require.NoError(t, err)

	// Pending inbox
	_, err = db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status) VALUES ('C1', '1700000001.000100', 'U2', 'mention', 'pending')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	require.Contains(t, signals, "C1")

	vs := signals["C1"]
	assert.Equal(t, 1, vs.DecisionCount)
	assert.Equal(t, 1, vs.ActiveTrackCount)
	assert.Equal(t, 1, vs.TaskCount)
	assert.Equal(t, 1, vs.PendingInboxCount)
}

func TestGetChannelValueSignals_DecisionsOnly30Days(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	// Old digest (60 days ago) — should NOT count
	_, err := db.Exec(`INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count) VALUES ('C1', ?, ?, 'channel', 'old', 5)`,
		float64(time.Now().Unix()-86400*60), float64(time.Now().Unix()-86400*31))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO digest_topics (digest_id, idx, title, decisions) VALUES (1, 0, 'old', '[{"text":"old decision"}]')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	assert.Empty(t, signals) // old digest should not count
}

func TestGetChannelValueSignals_TrackMultiChannel(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "ch1", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C2", Name: "ch2", Type: "public"}))

	_, err := db.Exec(`INSERT INTO tracks (text, channel_ids) VALUES ('cross-channel', '["C1","C2"]')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	require.Contains(t, signals, "C1")
	require.Contains(t, signals, "C2")
	assert.Equal(t, 1, signals["C1"].ActiveTrackCount)
	assert.Equal(t, 1, signals["C2"].ActiveTrackCount)
}

func TestGetChannelValueSignals_TaskViaDigest(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := db.Exec(`INSERT INTO digests (channel_id, period_from, period_to, type, summary, message_count) VALUES ('C1', 1700000000, 1700086400, 'channel', 'test', 5)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tasks (text, status, source_type, source_id) VALUES ('do X', 'in_progress', 'digest', '1')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	require.Contains(t, signals, "C1")
	assert.Equal(t, 1, signals["C1"].TaskCount)
}

func TestGetChannelValueSignals_TaskViaInbox(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status) VALUES ('C1', '1700000001.000100', 'U2', 'mention', 'resolved')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tasks (text, status, source_type, source_id) VALUES ('reply', 'blocked', 'inbox', '1')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	require.Contains(t, signals, "C1")
	assert.Equal(t, 1, signals["C1"].TaskCount)
}

func TestGetChannelValueSignals_InboxOnlyPending(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status) VALUES ('C1', '1700000001.000100', 'U2', 'mention', 'pending')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status) VALUES ('C1', '1700000002.000100', 'U3', 'mention', 'resolved')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status) VALUES ('C1', '1700000003.000100', 'U4', 'mention', 'dismissed')`)
	require.NoError(t, err)

	signals, err := db.GetChannelValueSignals()
	require.NoError(t, err)
	require.Contains(t, signals, "C1")
	assert.Equal(t, 1, signals["C1"].PendingInboxCount) // only pending
}

// --- Recommendation with Value Signals Tests ---

func TestRecommendations_MuteBlockedByInbox(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "noisy", TotalMsgs: 60, UserMsgs: 0, Mentions: 0, BotRatio: 0.8},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {PendingInboxCount: 1},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	for _, r := range recs {
		assert.NotEqual(t, "mute", r.Action, "mute should be blocked by pending inbox")
	}
}

func TestRecommendations_MuteBlockedByTask(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "noisy", TotalMsgs: 60, UserMsgs: 0, Mentions: 0, BotRatio: 0.8},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {TaskCount: 2},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	for _, r := range recs {
		assert.NotEqual(t, "mute", r.Action, "mute should be blocked by active tasks")
	}
}

func TestRecommendations_LeaveBlockedByTrack(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "old", ChannelType: "public", IsMember: true, TotalMsgs: 10, UserMsgs: 0},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {ActiveTrackCount: 1},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	for _, r := range recs {
		assert.NotEqual(t, "leave", r.Action, "leave should be blocked by active track")
	}
}

func TestRecommendations_LeaveBlockedByDecisions(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "old", ChannelType: "public", IsMember: true, TotalMsgs: 10, UserMsgs: 0},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {DecisionCount: 3},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	for _, r := range recs {
		assert.NotEqual(t, "leave", r.Action, "leave should be blocked by >=3 decisions")
	}
}

func TestRecommendations_FavoriteBoostedByDecisions(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "valuable", TotalMsgs: 20, UserMsgs: 2, Mentions: 0},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {DecisionCount: 5},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	require.Len(t, recs, 1)
	assert.Equal(t, "favorite", recs[0].Action)
	assert.Contains(t, recs[0].Reason, "high-value")
}

func TestRecommendations_FavoriteBoostedByTracks(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "tracked", TotalMsgs: 20, UserMsgs: 2, Mentions: 0},
	}
	signals := map[string]ChannelValueSignals{
		"C1": {ActiveTrackCount: 2},
	}
	recs := ComputeRecommendations(stats, "U1", signals)
	require.Len(t, recs, 1)
	assert.Equal(t, "favorite", recs[0].Action)
	assert.Contains(t, recs[0].Reason, "high-value")
}

func TestRecommendations_NilSignals(t *testing.T) {
	stats := []ChannelStatRow{
		{ChannelID: "C1", ChannelName: "bot-alerts", TotalMsgs: 60, UserMsgs: 0, Mentions: 0, BotRatio: 1.0},
	}
	recs := ComputeRecommendations(stats, "U1", nil)
	require.Len(t, recs, 1)
	assert.Equal(t, "mute", recs[0].Action)
}
