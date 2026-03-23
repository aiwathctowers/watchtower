package db

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertMessage(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msg := Message{
		ChannelID:  "C001",
		TS:         "1700000000.000001",
		UserID:     "U001",
		Text:       "Hello world",
		ReplyCount: 0,
		RawJSON:    `{"ts":"1700000000.000001"}`,
	}
	err = db.UpsertMessage(msg)
	require.NoError(t, err)

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Hello world", msgs[0].Text)
	assert.Equal(t, "U001", msgs[0].UserID)
	assert.Equal(t, float64(1700000000), msgs[0].TSUnix)
}

func TestUpsertMessageUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msg := Message{ChannelID: "C001", TS: "1700000000.000001", UserID: "U001", Text: "original", RawJSON: "{}"}
	require.NoError(t, db.UpsertMessage(msg))

	msg.Text = "edited"
	msg.IsEdited = true
	require.NoError(t, db.UpsertMessage(msg))

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "edited", msgs[0].Text)
	assert.True(t, msgs[0].IsEdited)
}

func TestUpsertMessageWithThread(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	parent := Message{ChannelID: "C001", TS: "1700000000.000001", UserID: "U001", Text: "parent", ReplyCount: 2, RawJSON: "{}"}
	require.NoError(t, db.UpsertMessage(parent))

	reply := Message{
		ChannelID: "C001",
		TS:        "1700000001.000001",
		UserID:    "U002",
		Text:      "reply",
		ThreadTS:  sql.NullString{String: "1700000000.000001", Valid: true},
		RawJSON:   "{}",
	}
	require.NoError(t, db.UpsertMessage(reply))

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
}

func TestGetMessages(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert messages across channels and users
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000001.000001", UserID: "U001", Text: "msg1", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000002.000001", UserID: "U002", Text: "msg2", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000003.000001", UserID: "U001", Text: "msg3", RawJSON: "{}"}))

	// No filter - all messages, newest first
	msgs, err := db.GetMessages(MessageOpts{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
	assert.Equal(t, "msg3", msgs[0].Text) // Newest first

	// Filter by channel
	msgs, err = db.GetMessages(MessageOpts{ChannelID: "C001", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Filter by user
	msgs, err = db.GetMessages(MessageOpts{UserID: "U001", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Filter by both
	msgs, err = db.GetMessages(MessageOpts{ChannelID: "C001", UserID: "U001", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "msg1", msgs[0].Text)
}

func TestGetMessagesDefaultLimit(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert more than 100 messages
	for i := 0; i < 110; i++ {
		ts := fmt.Sprintf("17000%05d.000001", i)
		require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: ts, UserID: "U001", Text: "msg", RawJSON: "{}"}))
	}

	msgs, err := db.GetMessages(MessageOpts{ChannelID: "C001"})
	require.NoError(t, err)
	assert.Len(t, msgs, 100) // Default limit is 100
}

func TestGetMessagesByTimeRange(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000001.000001", UserID: "U001", Text: "early", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000500.000001", UserID: "U001", Text: "middle", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700001000.000001", UserID: "U001", Text: "late", RawJSON: "{}"}))

	// Get middle range
	msgs, err := db.GetMessagesByTimeRange("C001", 1700000100, 1700000600)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "middle", msgs[0].Text)

	// Get all
	msgs, err = db.GetMessagesByTimeRange("C001", 1700000000, 1700002000)
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
	// Should be sorted descending (newest first)
	assert.Equal(t, "late", msgs[0].Text)
	assert.Equal(t, "early", msgs[2].Text)
}

func TestGetMessagesByTimeRangeEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msgs, err := db.GetMessagesByTimeRange("C001", 1700000000, 1700001000)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetMessagesByChannel(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000001.000001", UserID: "U001", Text: "old", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000002.000001", UserID: "U001", Text: "new", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000003.000001", UserID: "U001", Text: "other", RawJSON: "{}"}))

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	// Newest first
	assert.Equal(t, "new", msgs[0].Text)
	assert.Equal(t, "old", msgs[1].Text)

	// Test limit
	msgs, err = db.GetMessagesByChannel("C001", 1)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "new", msgs[0].Text)
}

func TestGetThreadReplies(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	threadTS := "1700000000.000001"
	// Parent message
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: threadTS, UserID: "U001", Text: "parent", ReplyCount: 2, RawJSON: "{}"}))
	// Replies
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000001.000001", UserID: "U002", Text: "reply1", ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000002.000001", UserID: "U003", Text: "reply2", ThreadTS: sql.NullString{String: threadTS, Valid: true}, RawJSON: "{}"}))
	// Unrelated message
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000003.000001", UserID: "U001", Text: "unrelated", RawJSON: "{}"}))

	msgs, err := db.GetThreadReplies("C001", threadTS)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	// Sorted ascending
	assert.Equal(t, "parent", msgs[0].Text)
	assert.Equal(t, "reply1", msgs[1].Text)
	assert.Equal(t, "reply2", msgs[2].Text)
}

func TestGetThreadRepliesEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msgs, err := db.GetThreadReplies("C001", "9999999999.000001")
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetAllThreadParents(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Channel 1: needs sync
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000000.000001", UserID: "U001", Text: "ch1 thread", ReplyCount: 2, RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000001.000001", UserID: "U002", Text: "reply", ThreadTS: sql.NullString{String: "1700000000.000001", Valid: true}, RawJSON: "{}"}))

	// Channel 2: needs sync
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000010.000001", UserID: "U001", Text: "ch2 thread", ReplyCount: 5, RawJSON: "{}"}))

	// Channel 3: fully synced — should not appear
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C003", TS: "1700000020.000001", UserID: "U001", Text: "synced", ReplyCount: 1, RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C003", TS: "1700000021.000001", UserID: "U002", Text: "reply", ThreadTS: sql.NullString{String: "1700000020.000001", Valid: true}, RawJSON: "{}"}))

	parents, err := db.GetAllThreadParents(1000)
	require.NoError(t, err)
	require.Len(t, parents, 2)

	// Should be sorted by ts_unix DESC
	assert.Equal(t, "C002", parents[0].ChannelID)
	assert.Equal(t, "C001", parents[1].ChannelID)
}

func TestGetAllThreadParentsEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	parents, err := db.GetAllThreadParents(1000)
	require.NoError(t, err)
	assert.Empty(t, parents)
}

func TestUpsertMessageUnicode(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msg := Message{
		ChannelID: "C001",
		TS:        "1700000000.000001",
		UserID:    "U001",
		Text:      "Hello 世界! 🚀 café naïve Ñoño デプロイメント",
		RawJSON:   "{}",
	}
	require.NoError(t, db.UpsertMessage(msg))

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg.Text, msgs[0].Text)
}

func TestUpsertMessageEmptyText(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	msg := Message{
		ChannelID: "C001",
		TS:        "1700000000.000001",
		UserID:    "U001",
		Text:      "",
		RawJSON:   "{}",
	}
	require.NoError(t, db.UpsertMessage(msg))

	msgs, err := db.GetMessagesByChannel("C001", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "", msgs[0].Text)
}

func TestGetMessageNearEdgeCases(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert two messages close together
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000000.000001", UserID: "U001", Text: "first", RawJSON: "{}"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000030.000001", UserID: "U001", Text: "second", RawJSON: "{}"}))

	// Closest to first message
	msg, err := db.GetMessageNear("C001", 1700000010)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "first", msg.Text)

	// Closest to second message
	msg, err = db.GetMessageNear("C001", 1700000025)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "second", msg.Text)

	// Outside tolerance (>60s from any message)
	msg, err = db.GetMessageNear("C001", 1700000200)
	require.NoError(t, err)
	assert.Nil(t, msg)
}
