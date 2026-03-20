package db

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertMessageBatch(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)

	msgs := []Message{
		{ChannelID: "C1", TS: "1700000001.000001", UserID: "U1", Text: "msg1", RawJSON: "{}"},
		{ChannelID: "C1", TS: "1700000002.000001", UserID: "U2", Text: "msg2", RawJSON: "{}"},
		{ChannelID: "C2", TS: "1700000003.000001", UserID: "U1", Text: "msg3", RawJSON: "{}"},
	}

	count, err := db.UpsertMessageBatch(tx, msgs)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.NoError(t, tx.Commit())

	all, err := db.GetMessages(MessageOpts{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestUpsertMessageBatch_NilTx(t *testing.T) {
	db := openTestDB(t)
	_, err := db.UpsertMessageBatch(nil, nil)
	assert.Error(t, err)
}

func TestCountMessagesByTimeRange(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000001", UserID: "U1", Text: "a", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1600000.000001", UserID: "U1", Text: "b", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C2", TS: "1700000.000001", UserID: "U1", Text: "c", RawJSON: "{}"}))

	count, err := db.CountMessagesByTimeRange(1400000, 1650000)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = db.CountMessagesByTimeRange(1400000, 1800000)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	count, err = db.CountMessagesByTimeRange(9000000, 9900000)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetChannelActivityCounts(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C2", Name: "random", Type: "public"}))

	// C1: 3 messages, C2: 1 message
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000001", UserID: "U1", Text: "a", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000002", UserID: "U1", Text: "b", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000003", UserID: "U1", Text: "c", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C2", TS: "1500000.000004", UserID: "U1", Text: "d", RawJSON: "{}"}))

	counts, err := db.GetChannelActivityCounts(1400000, 1600000, 10)
	require.NoError(t, err)
	require.Len(t, counts, 2)
	// Ordered by count DESC
	assert.Equal(t, "C1", counts[0].ChannelID)
	assert.Equal(t, "general", counts[0].Name)
	assert.Equal(t, 3, counts[0].Count)
	assert.Equal(t, "C2", counts[1].ChannelID)
	assert.Equal(t, 1, counts[1].Count)

	// With limit
	counts, err = db.GetChannelActivityCounts(1400000, 1600000, 1)
	require.NoError(t, err)
	assert.Len(t, counts, 1)
}

func TestGetUserActivityCounts(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000001", UserID: "U1", Text: "a", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000002", UserID: "U1", Text: "b", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000003", UserID: "U2", Text: "c", RawJSON: "{}"}))

	counts, err := db.GetUserActivityCounts(1400000, 1600000, 10)
	require.NoError(t, err)
	require.Len(t, counts, 2)
	assert.Equal(t, "U1", counts[0].UserID)
	assert.Equal(t, 2, counts[0].Count)
	assert.Equal(t, "U2", counts[1].UserID)
	assert.Equal(t, 1, counts[1].Count)
}

func TestGetMessagesExcludeDMs(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "D1", Name: "dm", Type: "dm"}))

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000001", UserID: "U1", Text: "public msg", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "D1", TS: "1500000.000002", UserID: "U1", Text: "dm msg", RawJSON: "{}"}))

	msgs, err := db.GetMessages(MessageOpts{ExcludeDMs: true, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "public msg", msgs[0].Text)
}

func TestGetMessagesTimeFilter(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000001", UserID: "U1", Text: "early", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "2500000.000001", UserID: "U1", Text: "late", RawJSON: "{}"}))

	msgs, err := db.GetMessages(MessageOpts{FromUnix: 2000000, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "late", msgs[0].Text)

	msgs, err = db.GetMessages(MessageOpts{ToUnix: 2000000, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "early", msgs[0].Text)
}

func TestGetThreadRepliesAfterTS(t *testing.T) {
	db := openTestDB(t)

	threadTS := "1700000000.000001"
	require.NoError(t, db.UpsertMessage(Message{
		ChannelID: "C1", TS: threadTS, UserID: "U1", Text: "parent", ReplyCount: 3, RawJSON: "{}",
	}))
	require.NoError(t, db.UpsertMessage(Message{
		ChannelID: "C1", TS: "1700000001.000001", UserID: "U2", Text: "reply1",
		ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}",
	}))
	require.NoError(t, db.UpsertMessage(Message{
		ChannelID: "C1", TS: "1700000002.000001", UserID: "U3", Text: "reply2",
		ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}",
	}))
	require.NoError(t, db.UpsertMessage(Message{
		ChannelID: "C1", TS: "1700000003.000001", UserID: "U2", Text: "reply3",
		ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}",
	}))

	// Only replies after reply1
	msgs, err := db.GetThreadRepliesAfterTS("C1", threadTS, "1700000001.000001")
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "reply2", msgs[0].Text)
	assert.Equal(t, "reply3", msgs[1].Text)
}

func TestGetChannelMessagesAfterTS(t *testing.T) {
	db := openTestDB(t)

	threadTS := "1700000000.000001"
	// Standalone messages
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1700000001.000001", UserID: "U1", Text: "msg1", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1700000002.000001", UserID: "U2", Text: "msg2", RawJSON: "{}"}))
	// Thread reply — should be excluded
	require.NoError(t, db.UpsertMessage(Message{
		ChannelID: "C1", TS: "1700000003.000001", UserID: "U3", Text: "reply",
		ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}",
	}))
	// Deleted message — should be excluded
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1700000004.000001", UserID: "U1", Text: "deleted", IsDeleted: true, RawJSON: "{}"}))
	// Empty text — should be excluded
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1700000005.000001", UserID: "U1", Text: "", RawJSON: "{}"}))

	msgs, err := db.GetChannelMessagesAfterTS("C1", "1700000000.000001", 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg1", msgs[0].Text)
	assert.Equal(t, "msg2", msgs[1].Text)
}

func TestGetMessageNear_NotFound(t *testing.T) {
	db := openTestDB(t)

	msg, err := db.GetMessageNear("C1", 1700000000)
	require.NoError(t, err)
	assert.Nil(t, msg)
}

func TestEnsureChannel(t *testing.T) {
	db := openTestDB(t)

	err := db.EnsureChannel("C1", "general", "public", "")
	require.NoError(t, err)

	ch, err := db.GetChannelByID("C1")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, "general", ch.Name)
	assert.Equal(t, "public", ch.Type)

	// Re-ensure should not overwrite
	err = db.EnsureChannel("C1", "different-name", "public", "")
	require.NoError(t, err)

	ch, err = db.GetChannelByID("C1")
	require.NoError(t, err)
	assert.Equal(t, "general", ch.Name) // not changed
}

func TestEnsureChannel_WithDMUserID(t *testing.T) {
	db := openTestDB(t)

	err := db.EnsureChannel("D1", "dm-user", "dm", "U123")
	require.NoError(t, err)

	ch, err := db.GetChannelByID("D1")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.True(t, ch.DMUserID.Valid)
	assert.Equal(t, "U123", ch.DMUserID.String)
}
