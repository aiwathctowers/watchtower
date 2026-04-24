package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInboxItem(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{
		ChannelID:    "C123",
		MessageTS:    "1234567890.000100",
		SenderUserID: "U456",
		TriggerType:  "mention",
		Snippet:      "Hey, can you review this?",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "C123", item.ChannelID)
	assert.Equal(t, "1234567890.000100", item.MessageTS)
	assert.Equal(t, "U456", item.SenderUserID)
	assert.Equal(t, "mention", item.TriggerType)
	assert.Equal(t, "Hey, can you review this?", item.Snippet)
	assert.Equal(t, "pending", item.Status)
	assert.Equal(t, "medium", item.Priority)
}

func TestGetInboxItemByMessage(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateInboxItem(InboxItem{
		ChannelID:    "C123",
		MessageTS:    "1234567890.000100",
		SenderUserID: "U456",
		TriggerType:  "mention",
	})
	require.NoError(t, err)

	item, err := db.GetInboxItemByMessage("C123", "1234567890.000100")
	require.NoError(t, err)
	assert.Equal(t, "C123", item.ChannelID)
	assert.Equal(t, "1234567890.000100", item.MessageTS)
}

func TestGetInboxItems_Filters(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention", Priority: "high"})
	require.NoError(t, err)
	_, err = db.CreateInboxItem(InboxItem{ChannelID: "C2", MessageTS: "2.1", SenderUserID: "U2", TriggerType: "dm", Priority: "low"})
	require.NoError(t, err)

	// All pending
	items, err := db.GetInboxItems(InboxFilter{})
	require.NoError(t, err)
	assert.Len(t, items, 2)
	// High priority first
	assert.Equal(t, "high", items[0].Priority)

	// Filter by priority
	items, err = db.GetInboxItems(InboxFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "C1", items[0].ChannelID)

	// Filter by trigger type
	items, err = db.GetInboxItems(InboxFilter{TriggerType: "dm"})
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "C2", items[0].ChannelID)

	// Filter by channel
	items, err = db.GetInboxItems(InboxFilter{ChannelID: "C1"})
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// Limit
	items, err = db.GetInboxItems(InboxFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestUpdateInboxItemStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	err = db.UpdateInboxItemStatus(int(id), "resolved")
	require.NoError(t, err)

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "resolved", item.Status)
}

func TestResolveInboxItem(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	err = db.ResolveInboxItem(int(id), "User replied in thread")
	require.NoError(t, err)

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "resolved", item.Status)
	assert.Equal(t, "User replied in thread", item.ResolvedReason)
}

func TestSnoozeAndUnsnoozeInboxItems(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	err = db.SnoozeInboxItem(int(id), "2020-01-01")
	require.NoError(t, err)

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "snoozed", item.Status)
	assert.Equal(t, "2020-01-01", item.SnoozeUntil)

	n, err := db.UnsnoozeExpiredInboxItems()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	item, err = db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "pending", item.Status)
}

func TestMarkInboxRead(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	err = db.MarkInboxRead(int(id))
	require.NoError(t, err)

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	assert.NotEmpty(t, item.ReadAt)
}

func TestLinkInboxTarget(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	err = db.LinkInboxTarget(int(id), 42)
	require.NoError(t, err)

	item, err := db.GetInboxItemByID(int(id))
	require.NoError(t, err)
	require.NotNil(t, item.TargetID)
	assert.Equal(t, 42, *item.TargetID)
}

func TestGetInboxCounts(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)
	id2, err := db.CreateInboxItem(InboxItem{ChannelID: "C2", MessageTS: "2.1", SenderUserID: "U2", TriggerType: "dm"})
	require.NoError(t, err)

	// Mark one as read
	err = db.MarkInboxRead(int(id2))
	require.NoError(t, err)

	pending, unread, err := db.GetInboxCounts()
	require.NoError(t, err)
	assert.Equal(t, 2, pending)
	assert.Equal(t, 1, unread) // one unread (the first one)
}

func TestGetInboxItemsForBriefing(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention", Priority: "high"})
	require.NoError(t, err)
	id2, err := db.CreateInboxItem(InboxItem{ChannelID: "C2", MessageTS: "2.1", SenderUserID: "U2", TriggerType: "dm", Priority: "low"})
	require.NoError(t, err)

	// Resolve one — should not appear in briefing
	err = db.ResolveInboxItem(int(id2), "done")
	require.NoError(t, err)

	items, err := db.GetInboxItemsForBriefing()
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "high", items[0].Priority)
}

func TestBulkUpdateInboxPriorities(t *testing.T) {
	db := openTestDB(t)

	id1, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)
	id2, err := db.CreateInboxItem(InboxItem{ChannelID: "C2", MessageTS: "2.1", SenderUserID: "U2", TriggerType: "dm"})
	require.NoError(t, err)

	updates := map[int]struct {
		Priority string
		AIReason string
	}{
		int(id1): {Priority: "high", AIReason: "Direct request from manager"},
		int(id2): {Priority: "low", AIReason: "FYI message"},
	}
	err = db.BulkUpdateInboxPriorities(updates)
	require.NoError(t, err)

	item1, err := db.GetInboxItemByID(int(id1))
	require.NoError(t, err)
	assert.Equal(t, "high", item1.Priority)
	assert.Equal(t, "Direct request from manager", item1.AIReason)

	item2, err := db.GetInboxItemByID(int(id2))
	require.NoError(t, err)
	assert.Equal(t, "low", item2.Priority)
}

func TestInboxLastProcessedTS(t *testing.T) {
	db := openTestDB(t)

	// Need a workspace row first
	_, err := db.Exec(`INSERT INTO workspace (id, name) VALUES ('T1', 'Test')`)
	require.NoError(t, err)

	ts, err := db.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Equal(t, 0.0, ts)

	err = db.SetInboxLastProcessedTS(1234567890.0)
	require.NoError(t, err)

	ts, err = db.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Equal(t, 1234567890.0, ts)
}

func TestFindPendingMentions(t *testing.T) {
	db := openTestDB(t)

	// Insert a channel and a message that mentions our user
	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, permalink) VALUES ('C1', '1000.001', 'U_OTHER', 'Hey <@U_ME> can you check this?', 'https://slack.com/p1')`)
	require.NoError(t, err)
	// Message that doesn't mention the user
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1001.001', 'U_OTHER', 'Regular message')`)
	require.NoError(t, err)
	// Message from the user themselves (should not match)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1002.001', 'U_ME', '<@U_ME> testing self-mention')`)
	require.NoError(t, err)

	candidates, err := db.FindPendingMentions("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "C1", candidates[0].ChannelID)
	assert.Equal(t, "1000.001", candidates[0].MessageTS)
	assert.Equal(t, "mention", candidates[0].TriggerType)
	assert.Equal(t, "U_OTHER", candidates[0].SenderUserID)
}

func TestFindPendingDMs(t *testing.T) {
	db := openTestDB(t)

	// Insert a DM channel and messages
	_, err := db.Exec(`INSERT INTO channels (id, name, type, dm_user_id) VALUES ('D1', 'dm-user', 'dm', 'U_OTHER')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', '1000.001', 'U_OTHER', 'Hey, got a minute?')`)
	require.NoError(t, err)
	// Our own message — should not be a candidate
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', '1001.001', 'U_ME', 'Sure')`)
	require.NoError(t, err)

	candidates, err := db.FindPendingDMs("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "D1", candidates[0].ChannelID)
	assert.Equal(t, "dm", candidates[0].TriggerType)
}

func TestCheckUserReplied(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// Message in thread
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1000.001', 'U_OTHER', 'mention', '1000.001')`)
	require.NoError(t, err)
	// User reply in same thread
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1001.001', 'U_ME', 'reply', '1000.001')`)
	require.NoError(t, err)

	replied, err := db.CheckUserReplied("U_ME", "C1", "1000.001", "1000.001")
	require.NoError(t, err)
	assert.True(t, replied)

	// No reply in different thread
	replied, err = db.CheckUserReplied("U_ME", "C1", "2000.001", "2000.001")
	require.NoError(t, err)
	assert.False(t, replied)
}

func TestCheckUserReplied_DMThreadReply(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('D1', 'dm-olena', 'dm')`)
	require.NoError(t, err)

	// DM message from another user (top-level, no thread_ts)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', '1000.001', 'U_OTHER', 'please do this')`)
	require.NoError(t, err)

	// User replies as a thread to that DM message
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('D1', '1001.001', 'U_ME', 'done', '1000.001')`)
	require.NoError(t, err)

	// threadTS is empty because the original DM was top-level
	replied, err := db.CheckUserReplied("U_ME", "D1", "1000.001", "")
	require.NoError(t, err)
	assert.True(t, replied, "should detect thread reply to a top-level DM")
}

func TestFindThreadRepliesToUser(t *testing.T) {
	db := openTestDB(t)

	// Insert a channel
	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// Root message by our user (thread_ts == ts means it's the root)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1000.001', 'U_ME', 'Starting a thread', '1000.001')`)
	require.NoError(t, err)

	// Reply from someone else in that thread
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts, permalink) VALUES ('C1', '1001.001', 'U_OTHER', 'Replying to your thread', '1000.001', 'https://slack.com/p2')`)
	require.NoError(t, err)

	// Reply from ourselves (should not match)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1002.001', 'U_ME', 'My own reply', '1000.001')`)
	require.NoError(t, err)

	// Reply in a thread started by someone else (should not match)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '2000.001', 'U_OTHER2', 'Other root', '2000.001')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '2001.001', 'U_OTHER', 'Reply to other thread', '2000.001')`)
	require.NoError(t, err)

	candidates, err := db.FindThreadRepliesToUser("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "C1", candidates[0].ChannelID)
	assert.Equal(t, "1001.001", candidates[0].MessageTS)
	assert.Equal(t, "1000.001", candidates[0].ThreadTS)
	assert.Equal(t, "U_OTHER", candidates[0].SenderUserID)
	assert.Equal(t, "thread_reply", candidates[0].TriggerType)
	assert.Equal(t, "https://slack.com/p2", candidates[0].Permalink)
}

func TestFindThreadRepliesToUser_ExcludesExistingInbox(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1000.001', 'U_ME', 'Root', '1000.001')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1001.001', 'U_OTHER', 'Reply', '1000.001')`)
	require.NoError(t, err)

	// Create an existing inbox item for this message
	_, err = db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1001.001", SenderUserID: "U_OTHER", TriggerType: "thread_reply"})
	require.NoError(t, err)

	candidates, err := db.FindThreadRepliesToUser("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 0)
}

func TestFindThreadRepliesToUser_SinceTS(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1000.001', 'U_ME', 'Root', '1000.001')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1001.001', 'U_OTHER', 'Old reply', '1000.001')`)
	require.NoError(t, err)

	// sinceTS after the reply — should find nothing
	candidates, err := db.FindThreadRepliesToUser("U_ME", 2000.0)
	require.NoError(t, err)
	assert.Len(t, candidates, 0)
}

func TestFindThreadRepliesToUser_ParticipantNotRoot(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// Root message by someone else
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '3000.001', 'U_OTHER', 'Other starts a thread', '3000.001')`)
	require.NoError(t, err)

	// U_ME replies in that thread (participant, not root author)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '3001.001', 'U_ME', 'I chime in', '3000.001')`)
	require.NoError(t, err)

	// Third person replies after U_ME — should be detected
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts, permalink) VALUES ('C1', '3002.001', 'U_THIRD', 'Follow-up for you', '3000.001', 'https://slack.com/p3')`)
	require.NoError(t, err)

	// U_ME's own reply should NOT appear
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '3003.001', 'U_ME', 'My second reply', '3000.001')`)
	require.NoError(t, err)

	candidates, err := db.FindThreadRepliesToUser("U_ME", 0)
	require.NoError(t, err)
	// Only U_THIRD's reply — root (thread_ts==ts) is filtered, U_ME's own replies are excluded
	assert.Len(t, candidates, 1)
	assert.Equal(t, "3002.001", candidates[0].MessageTS)
	assert.Equal(t, "U_THIRD", candidates[0].SenderUserID)
	assert.Equal(t, "thread_reply", candidates[0].TriggerType)
	assert.Equal(t, "https://slack.com/p3", candidates[0].Permalink)
}

func TestFindReactionRequests(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// Message by our user
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, permalink) VALUES ('C1', '1000.001', 'U_ME', 'Here is my proposal', 'https://slack.com/p1')`)
	require.NoError(t, err)

	// Question reaction from someone else
	_, err = db.Exec(`INSERT INTO reactions (channel_id, message_ts, user_id, emoji) VALUES ('C1', '1000.001', 'U_OTHER', 'question')`)
	require.NoError(t, err)

	candidates, err := db.FindReactionRequests("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "C1", candidates[0].ChannelID)
	assert.Equal(t, "1000.001", candidates[0].MessageTS)
	assert.Equal(t, "U_OTHER", candidates[0].SenderUserID)
	assert.Equal(t, "reaction", candidates[0].TriggerType)
	assert.Equal(t, "https://slack.com/p1", candidates[0].Permalink)
}

func TestFindReactionRequests_IgnoresNonAttentionEmoji(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000.001', 'U_ME', 'Nice work')`)
	require.NoError(t, err)

	// Thumbs up reaction (not an attention emoji)
	_, err = db.Exec(`INSERT INTO reactions (channel_id, message_ts, user_id, emoji) VALUES ('C1', '1000.001', 'U_OTHER', '+1')`)
	require.NoError(t, err)

	candidates, err := db.FindReactionRequests("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 0)
}

func TestFindReactionRequests_IgnoresOwnReaction(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000.001', 'U_ME', 'My message')`)
	require.NoError(t, err)

	// Own reaction should not match
	_, err = db.Exec(`INSERT INTO reactions (channel_id, message_ts, user_id, emoji) VALUES ('C1', '1000.001', 'U_ME', 'question')`)
	require.NoError(t, err)

	candidates, err := db.FindReactionRequests("U_ME", 0)
	require.NoError(t, err)
	assert.Len(t, candidates, 0)
}

func TestFindReactionRequests_DeduplicatesMultipleReactions(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000.001', 'U_ME', 'My message')`)
	require.NoError(t, err)

	// Multiple attention reactions from different users on the same message
	_, err = db.Exec(`INSERT INTO reactions (channel_id, message_ts, user_id, emoji) VALUES ('C1', '1000.001', 'U_A', 'question')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO reactions (channel_id, message_ts, user_id, emoji) VALUES ('C1', '1000.001', 'U_B', 'eyes')`)
	require.NoError(t, err)

	candidates, err := db.FindReactionRequests("U_ME", 0)
	require.NoError(t, err)
	// Should return only one candidate per message
	assert.Len(t, candidates, 1)
	assert.Equal(t, "reaction", candidates[0].TriggerType)
}

func TestGetInboxItems_IncludeResolved(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)
	err = db.ResolveInboxItem(int(id), "done")
	require.NoError(t, err)

	// Default: exclude resolved
	items, err := db.GetInboxItems(InboxFilter{})
	require.NoError(t, err)
	assert.Len(t, items, 0)

	// Include resolved
	items, err = db.GetInboxItems(InboxFilter{IncludeResolved: true})
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestCheckUserRepliedBefore(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)

	// Thread: user replied before the "thanks" message.
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1000.001', 'U_OTHER', 'Can you help?', '1000.001')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1001.001', 'U_ME', 'Sure, done', '1000.001')`)
	require.NoError(t, err)
	// "Thanks" message at ts=1002.001
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, thread_ts) VALUES ('C1', '1002.001', 'U_OTHER', 'Thanks!', '1000.001')`)
	require.NoError(t, err)

	// User replied before the "thanks" message.
	replied, err := db.CheckUserRepliedBefore("U_ME", "C1", "1002.001", "1000.001")
	require.NoError(t, err)
	assert.True(t, replied)

	// User did NOT reply before the first message.
	replied, err = db.CheckUserRepliedBefore("U_ME", "C1", "1000.001", "1000.001")
	require.NoError(t, err)
	assert.False(t, replied)

	// Non-threaded: user replied in channel before.
	_, err = db.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '2000.001', 'U_ME', 'Channel message')`)
	require.NoError(t, err)

	replied, err = db.CheckUserRepliedBefore("U_ME", "C1", "2001.001", "")
	require.NoError(t, err)
	assert.True(t, replied)

	// No reply in a completely different context.
	replied, err = db.CheckUserRepliedBefore("U_ME", "C_NONE", "1000.001", "")
	require.NoError(t, err)
	assert.False(t, replied)
}

func TestCreateInboxItem_UniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	require.NoError(t, err)

	// Duplicate should fail
	_, err = db.CreateInboxItem(InboxItem{ChannelID: "C1", MessageTS: "1.1", SenderUserID: "U1", TriggerType: "mention"})
	assert.Error(t, err)
}
