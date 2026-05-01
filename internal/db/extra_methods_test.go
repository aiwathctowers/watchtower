package db

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- channels.go ---

func TestUnreadDigestChannelIDs(t *testing.T) {
	db := openTestDB(t)

	// Seed two channel digests, mark one as read.
	id1, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel", PeriodFrom: 100, PeriodTo: 200,
		Summary: "s1", Topics: "[]", Decisions: "[]", ActionItems: "[]", Model: "m",
	})
	require.NoError(t, err)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C2", Type: "channel", PeriodFrom: 100, PeriodTo: 200,
		Summary: "s2", Topics: "[]", Decisions: "[]", ActionItems: "[]", Model: "m",
	})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE digests SET read_at = '2026-04-02T00:00:00Z' WHERE id = ?`, id1)
	require.NoError(t, err)

	got, err := db.UnreadDigestChannelIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"C2"}, got, "only unread channel must remain")
}

func TestUnreadDigestChannelIDs_Empty(t *testing.T) {
	db := openTestDB(t)
	got, err := db.UnreadDigestChannelIDs()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestChannelIDsWithoutLastRead(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public", IsMember: true, LastRead: ""}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C2", Name: "random", Type: "public", IsMember: true, LastRead: "1.000001"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C3", Name: "ops", Type: "public", IsMember: false, LastRead: ""}))

	got, err := db.ChannelIDsWithoutLastRead()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"C1"}, got, "only joined channels with empty last_read")
}

// --- digests.go ---

func TestDeduplicateChannelDigests(t *testing.T) {
	db := openTestDB(t)

	// Insert near-duplicates: same channel/type, same minute window, different
	// sub-second values (UNIQUE on full period values, but DeduplicateChannelDigests
	// groups by period/60 so they collapse).
	for i, fromSec := 0, 61; i < 3; i, fromSec = i+1, fromSec+1 {
		_, err := db.Exec(`INSERT INTO digests (channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model)
			VALUES ('C1', ?, ?, 'channel', 'dup', '[]', '[]', '[]', 1, 'm')`, fromSec, fromSec+60)
		require.NoError(t, err)
	}

	n, err := db.DeduplicateChannelDigests()
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "two duplicate rows should have been deleted")
}

func TestGetLatestRunningSummaryWithAge_None(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetLatestRunningSummaryWithAge("C1", "channel")
	require.NoError(t, err)
	assert.Nil(t, got, "missing summary returns nil")
}

func TestGetLatestRunningSummaryWithAge_PicksLatest(t *testing.T) {
	db := openTestDB(t)

	// Older one.
	_, err := db.UpsertDigest(Digest{
		ChannelID:      "C1",
		Type:           "channel",
		PeriodFrom:     100,
		PeriodTo:       200,
		Summary:        "x",
		Topics:         "[]",
		Decisions:      "[]",
		ActionItems:    "[]",
		Model:          "m",
		RunningSummary: "old context",
	})
	require.NoError(t, err)
	// Newer one (higher period_to).
	_, err = db.UpsertDigest(Digest{
		ChannelID:      "C1",
		Type:           "channel",
		PeriodFrom:     300,
		PeriodTo:       400,
		Summary:        "x",
		Topics:         "[]",
		Decisions:      "[]",
		ActionItems:    "[]",
		Model:          "m",
		RunningSummary: "new context",
	})
	require.NoError(t, err)

	got, err := db.GetLatestRunningSummaryWithAge("C1", "channel")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "new context", got.Summary)
	assert.GreaterOrEqual(t, got.AgeDays, 0.0)
}

func TestResetRunningSummary_AllChannels(t *testing.T) {
	db := openTestDB(t)

	for _, ch := range []string{"C1", "C2"} {
		_, err := db.UpsertDigest(Digest{
			ChannelID:      ch,
			Type:           "channel",
			PeriodFrom:     100,
			PeriodTo:       200,
			Summary:        "x",
			Topics:         "[]",
			Decisions:      "[]",
			ActionItems:    "[]",
			Model:          "m",
			RunningSummary: "to-clear",
		})
		require.NoError(t, err)
	}

	n, err := db.ResetRunningSummary("", "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	// Verify they're cleared.
	got, err := db.GetLatestRunningSummaryWithAge("C1", "channel")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestResetRunningSummary_FilteredByChannel(t *testing.T) {
	db := openTestDB(t)

	for _, ch := range []string{"C1", "C2"} {
		_, err := db.UpsertDigest(Digest{
			ChannelID:      ch,
			Type:           "channel",
			PeriodFrom:     100,
			PeriodTo:       200,
			Summary:        "x",
			Topics:         "[]",
			Decisions:      "[]",
			ActionItems:    "[]",
			Model:          "m",
			RunningSummary: "ctx",
		})
		require.NoError(t, err)
	}

	n, err := db.ResetRunningSummary("C1", "channel")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	// C2 still has a summary.
	got, err := db.GetLatestRunningSummaryWithAge("C2", "channel")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ctx", got.Summary)
}

func TestInsertDigestTopics(t *testing.T) {
	db := openTestDB(t)

	digestID, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel", PeriodFrom: 100, PeriodTo: 200,
		Summary: "x", Topics: "[]", Decisions: "[]", ActionItems: "[]", Model: "m",
	})
	require.NoError(t, err)

	require.NoError(t, db.InsertDigestTopics(digestID, []DigestTopic{
		{Title: "T1", Summary: "first", Decisions: "[]", ActionItems: "[]"},
		{Title: "T2", Summary: "second", Decisions: "[]", ActionItems: "[]"},
	}))

	// Topics persist (re-upsert replaces).
	require.NoError(t, db.InsertDigestTopics(digestID, []DigestTopic{
		{Title: "T3", Summary: "only", Decisions: "[]", ActionItems: "[]"},
	}))

	var titles []string
	rows, err := db.Query(`SELECT title FROM digest_topics WHERE digest_id = ?`, digestID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		titles = append(titles, s)
	}
	assert.Equal(t, []string{"T3"}, titles, "previous topics replaced on second insert")
}

func TestInsertDigestTopics_EmptyIsNoop(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.InsertDigestTopics(1, nil))
	require.NoError(t, db.InsertDigestTopics(1, []DigestTopic{}))
}

// --- dayplans.go ---

func TestMarkDayPlanRead(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertDayPlan(&DayPlan{
		UserID:   "U1",
		PlanDate: "2026-04-02",
		Status:   "active",
	})
	require.NoError(t, err)

	require.NoError(t, db.MarkDayPlanRead(id))

	got, err := db.GetDayPlanByID(id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.ReadAt.Valid, "read_at should be set after marking")
}

func TestMarkDayPlanRead_NoOpOnMissing(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.MarkDayPlanRead(99999))
}

func TestUpdateItemOrder(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertDayPlan(&DayPlan{
		UserID:   "U1",
		PlanDate: "2026-04-02",
		Status:   "active",
	})
	require.NoError(t, err)

	res1, err := db.Exec(`INSERT INTO day_plan_items (day_plan_id, kind, source_type, source_id, title, priority, status, order_index)
		VALUES (?, 'timeblock', 'task', 'src1', 'first', 'high', 'pending', 0)`, id)
	require.NoError(t, err)
	itemID1, err := res1.LastInsertId()
	require.NoError(t, err)

	res2, err := db.Exec(`INSERT INTO day_plan_items (day_plan_id, kind, source_type, source_id, title, priority, status, order_index)
		VALUES (?, 'timeblock', 'task', 'src2', 'second', 'medium', 'pending', 1)`, id)
	require.NoError(t, err)
	itemID2, err := res2.LastInsertId()
	require.NoError(t, err)

	// Reorder: itemID2 first.
	require.NoError(t, db.UpdateItemOrder(id, []int64{itemID2, itemID1}))

	rows, err := db.Query(`SELECT id, order_index FROM day_plan_items WHERE day_plan_id = ? ORDER BY order_index`, id)
	require.NoError(t, err)
	defer rows.Close()

	type pair struct {
		id    int64
		order int
	}
	var got []pair
	for rows.Next() {
		var p pair
		require.NoError(t, rows.Scan(&p.id, &p.order))
		got = append(got, p)
	}
	require.Len(t, got, 2)
	assert.Equal(t, itemID2, got[0].id)
	assert.Equal(t, 0, got[0].order)
	assert.Equal(t, itemID1, got[1].id)
	assert.Equal(t, 1, got[1].order)
}

func TestIncrementRegenerateCount(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertDayPlan(&DayPlan{
		UserID:   "U1",
		PlanDate: "2026-04-02",
		Status:   "active",
	})
	require.NoError(t, err)

	require.NoError(t, db.IncrementRegenerateCount(id, "first feedback"))
	require.NoError(t, db.IncrementRegenerateCount(id, "second feedback"))

	plan, err := db.GetDayPlanByID(id)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, 2, plan.RegenerateCount)

	// Newest feedback is prepended.
	assert.True(t,
		strings.Contains(plan.FeedbackHistory, "second feedback"),
		"feedback history should include latest entry: %s",
		plan.FeedbackHistory,
	)
	assert.True(t,
		strings.Contains(plan.FeedbackHistory, "first feedback"),
		"feedback history should include first entry: %s",
		plan.FeedbackHistory,
	)
}

func TestIncrementRegenerateCount_KeepsLast5(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertDayPlan(&DayPlan{
		UserID:   "U1",
		PlanDate: "2026-04-02",
		Status:   "active",
	})
	require.NoError(t, err)

	for i := 1; i <= 7; i++ {
		require.NoError(t, db.IncrementRegenerateCount(id, "feedback"+time.Now().Format("150405.000000")+"-"+string(rune(i))))
	}
	plan, err := db.GetDayPlanByID(id)
	require.NoError(t, err)
	assert.Equal(t, 7, plan.RegenerateCount)
	// History capped at 5 entries.
	assert.LessOrEqual(t, strings.Count(plan.FeedbackHistory, "feedback"), 5)
}
