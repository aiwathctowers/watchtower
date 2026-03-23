package actionitems

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGenerator struct {
	response string
}

func (m *mockGenerator) Generate(_ context.Context, _, _ string) (string, *digest.Usage, error) {
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, nil
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func testConfig() *config.Config {
	return &config.Config{
		Digest: config.DigestConfig{
			Enabled: true,
			Model:   "test-model",
		},
	}
}

func TestPipelineRunForWindow(t *testing.T) {
	database := testDB(t)

	// Setup workspace with current user
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	// Setup users
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))

	// Setup channel
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Insert messages
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002",
		Text: "@alice can you review the PR?",
	}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000002.000000", UserID: "U002",
		Text: "The deploy looks good",
	}))

	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "Review the PR",
					"context": "Bob asked Alice to review a pull request",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "medium"
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify stored item
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Review the PR", items[0].Text)
	assert.Equal(t, "medium", items[0].Priority)
	assert.Equal(t, "inbox", items[0].Status)
	assert.Equal(t, "C1", items[0].ChannelID)
}

func TestPipelineNoCurrentUser(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestPipelineEmptyResult(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002", Text: "hello",
	}))

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestParseResult(t *testing.T) {
	raw := `{"items": [{"text": "do X", "context": "Y asked", "channel_id": "C1", "channel_name": "#test", "source_message_ts": "123.456", "priority": "high", "due_date": "2025-03-15"}]}`
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "do X", result.Items[0].Text)
	assert.Equal(t, "high", result.Items[0].Priority)
	assert.Equal(t, "2025-03-15", result.Items[0].DueDate)
}

func TestParseResultMarkdownFences(t *testing.T) {
	raw := "```json\n" + `{"items": [{"text": "test", "context": "ctx", "channel_id": "C1", "channel_name": "#a", "source_message_ts": "1.0", "priority": "low"}]}` + "\n```"
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "test", result.Items[0].Text)
}

func TestActionItemStatusUpdate(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "test item",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     1000,
		PeriodTo:       2000,
	})
	require.NoError(t, err)

	// Mark as done
	require.NoError(t, database.UpdateActionItemStatus(int(id), "done"))

	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "done", items[0].Status)
	assert.True(t, items[0].CompletedAt.Valid)
}

func TestDeleteActionItemsForWindow(t *testing.T) {
	database := testDB(t)

	// Insert open item
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "old item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Insert done item (should NOT be deleted)
	id2, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateActionItemStatus(int(id2), "done"))

	deleted, err := database.DeleteActionItemsForWindow("U001", 1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted) // only the open one

	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "done item", items[0].Text)
}

func TestAcceptActionItem(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "review PR",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Accept: inbox → active
	require.NoError(t, database.AcceptActionItem(int(id)))

	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "active", items[0].Status)

	// Accept again should fail (not in inbox)
	err = database.AcceptActionItem(int(id))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in inbox")
}

func TestSnoozeAndReactivate(t *testing.T) {
	database := testDB(t)

	// Create inbox item and snooze it
	id1, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Create active item and snooze it
	id2, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active item",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptActionItem(int(id2)))

	// Snooze both — set snooze_until to the past so reactivation triggers immediately
	pastTS := float64(1000000000) // well in the past
	require.NoError(t, database.SnoozeActionItem(int(id1), pastTS))
	require.NoError(t, database.SnoozeActionItem(int(id2), pastTS))

	// Both should be snoozed
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001", Status: "snoozed"})
	require.NoError(t, err)
	assert.Len(t, items, 2)

	// Reactivate
	n, err := database.ReactivateSnoozedItems()
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Check they returned to their pre-snooze statuses
	items, err = database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 2)

	statusByText := map[string]string{}
	for _, item := range items {
		statusByText[item.Text] = item.Status
	}
	assert.Equal(t, "inbox", statusByText["inbox item"])
	assert.Equal(t, "active", statusByText["active item"])
}

func TestSnoozeFutureNotReactivated(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "future snooze",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Snooze far in the future
	futureTS := float64(9999999999)
	require.NoError(t, database.SnoozeActionItem(int(id), futureTS))

	// Should not reactivate
	n, err := database.ReactivateSnoozedItems()
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "snoozed", items[0].Status)
}

func TestHasUpdatesFlag(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "track updates",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000",
	})
	require.NoError(t, err)

	// Initially no updates
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.False(t, items[0].HasUpdates)

	// Set has_updates
	require.NoError(t, database.SetActionItemHasUpdates(int(id), true))

	items, err = database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.True(t, items[0].HasUpdates)

	// Filter by has_updates
	hasUpdates := true
	items, err = database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001", HasUpdates: &hasUpdates})
	require.NoError(t, err)
	assert.Len(t, items, 1)

	noUpdates := false
	items, err = database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001", HasUpdates: &noUpdates})
	require.NoError(t, err)
	assert.Len(t, items, 0)

	// Mark as read
	require.NoError(t, database.MarkActionItemUpdateRead(int(id)))
	items, err = database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.False(t, items[0].HasUpdates)
}

func TestGetActionItemsForUpdateCheck(t *testing.T) {
	database := testDB(t)

	// Item with source_message_ts — should be returned
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "with ts",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000",
	})
	require.NoError(t, err)

	// Item without source_message_ts — should NOT be returned
	_, err = database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "without ts",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Done item — should NOT be returned
	id3, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000003.000000",
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateActionItemStatus(int(id3), "done"))

	items, err := database.GetActionItemsForUpdateCheck()
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "with ts", items[0].Text)
}

func TestDeleteWindowPreservesActive(t *testing.T) {
	database := testDB(t)

	// Inbox item — will be deleted
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Active item — should NOT be deleted
	id2, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active item",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptActionItem(int(id2)))

	deleted, err := database.DeleteActionItemsForWindow("U001", 1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "active item", items[0].Text)
	assert.Equal(t, "active", items[0].Status)
}

func TestCheckForUpdates(t *testing.T) {
	database := testDB(t)

	// Setup
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Original message that generated the action item
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002",
		Text: "@alice review the PR please",
	}))

	// Create action item for this message
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "review PR",
		Status: "active", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000", SourceChannelName: "general",
	})
	require.NoError(t, err)

	// Add a thread reply (new update)
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000005.000000", UserID: "U002",
		Text:     "actually this is now urgent, we need it by EOD",
		ThreadTS: nullString("1000000001.000000"),
	}))

	gen := &mockGenerator{
		response: `{"has_update": true, "updated_context": "Bob says this is now urgent, need by EOD", "status_hint": "active"}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify has_updates flag is set
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.True(t, items[0].HasUpdates)
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func TestCheckForUpdatesChannelMessage(t *testing.T) {
	database := testDB(t)

	// Setup
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "denis"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))

	// Original message that generated the action item
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "need to whitelist IP 136.226.198.1 for EY consultants",
	}))

	// Create action item
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "whitelist IP for EY",
		Status: "active", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000", SourceChannelName: "dev-europe",
	})
	require.NoError(t, err)

	// Denis posts a CHANNEL message (not a thread reply!) confirming the task is done
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000010.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
		// No ThreadTS — this is a standalone channel message
	}))

	gen := &mockGenerator{
		response: `{"has_update": true, "updated_context": "Denis opened access for the IP on prod EU", "status_hint": "done"}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify item is marked done
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "done", items[0].Status)
	assert.True(t, items[0].HasUpdates)
	assert.True(t, items[0].CompletedAt.Valid)
}

func TestDayWindow(t *testing.T) {
	// Two times on the same day should produce the same window.
	t1, _ := time.Parse(time.RFC3339, "2026-03-12T13:00:00+03:00")
	t2, _ := time.Parse(time.RFC3339, "2026-03-12T13:15:00+03:00")

	from1, to1 := DayWindow(t1)
	from2, to2 := DayWindow(t2)

	assert.Equal(t, from1, from2, "same day should have same from")
	assert.Equal(t, to1, to2, "same day should have same to")

	// Window should be exactly 24h.
	assert.Equal(t, float64(86400), to1-from1, "window should be 24h")

	// Different day should produce different window.
	t3, _ := time.Parse(time.RFC3339, "2026-03-13T01:00:00+03:00")
	from3, _ := DayWindow(t3)
	assert.NotEqual(t, from1, from3, "different days should have different from")
}

func TestExistingIDParsing(t *testing.T) {
	raw := `{"items": [
		{"existing_id": 42, "text": "updated item", "context": "new ctx", "source_message_ts": "1.0", "priority": "high"},
		{"existing_id": null, "text": "new item", "context": "ctx", "source_message_ts": "2.0", "priority": "medium"}
	]}`
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	// First item: existing_id = 42
	require.NotNil(t, result.Items[0].ExistingID)
	assert.Equal(t, 42, *result.Items[0].ExistingID)

	// Second item: existing_id = null (new item)
	assert.Nil(t, result.Items[1].ExistingID)
}

func TestUpdateActionItemFromExtraction(t *testing.T) {
	database := testDB(t)

	// Create original item.
	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:       "C1",
		AssigneeUserID:  "U001",
		Text:            "original task",
		Context:         "old context",
		Status:          "active",
		Priority:        "low",
		PeriodFrom:      1000,
		PeriodTo:        2000,
		DecisionSummary: "old decision",
	})
	require.NoError(t, err)

	// Update with new extraction data.
	changed, err := database.UpdateActionItemFromExtraction(int(id), db.ActionItem{
		Context:         "updated context with new info",
		Priority:        "high",
		DecisionSummary: "evolved decision",
		Tags:            `["infra"]`,
	})
	require.NoError(t, err)
	assert.True(t, changed)

	// Verify changes.
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "updated context with new info", items[0].Context)
	assert.Equal(t, "high", items[0].Priority)
	assert.Equal(t, "active", items[0].Status) // preserved
	assert.Equal(t, "evolved decision", items[0].DecisionSummary)
	assert.Equal(t, `["infra"]`, items[0].Tags)

	// Verify history.
	history, err := database.GetActionItemHistory(int(id))
	require.NoError(t, err)
	require.True(t, len(history) >= 3) // re_extracted, priority_changed, decision_evolved

	events := make(map[string]bool)
	for _, h := range history {
		events[h.Event] = true
	}
	assert.True(t, events["re_extracted"])
	assert.True(t, events["priority_changed"])
	assert.True(t, events["decision_evolved"])
}

func TestUpdateNoChange(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "task",
		Context:        "context",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     1000,
		PeriodTo:       2000,
	})
	require.NoError(t, err)

	// Update with same values → no change.
	changed, err := database.UpdateActionItemFromExtraction(int(id), db.ActionItem{
		Context:  "context",
		Priority: "medium",
	})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestPipelineExistingIDUpdate(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Create existing action item.
	existingID, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "prepare migration plan",
		Context:        "old context about migration",
		Status:         "active",
		Priority:       "medium",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// Insert message.
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U002",
		Text: "migration vendor confirmed Q2 timeline",
	}))

	// AI returns existing_id pointing to existing item.
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"text": "prepare migration plan",
					"context": "vendor confirmed Q2 timeline, plan needs update",
					"source_message_ts": "1000000050.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Should have updated the existing item, not created a new one.
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "vendor confirmed Q2 timeline, plan needs update", items[0].Context)
	assert.Equal(t, "high", items[0].Priority)
	assert.Equal(t, "active", items[0].Status) // preserved
}

func TestDeleteActionItemsForWindowRangeBased(t *testing.T) {
	database := testDB(t)

	// Item from earlier run with slightly different window (old sliding behavior).
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "old sliding item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Delete with a window that encompasses the old item's period.
	deleted, err := database.DeleteActionItemsForWindow("U001", 900, 2100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Item outside the window should NOT be deleted.
	_, err = database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "outside window",
		Status: "inbox", Priority: "medium", PeriodFrom: 3000, PeriodTo: 4000,
	})
	require.NoError(t, err)

	deleted, err = database.DeleteActionItemsForWindow("U001", 900, 2100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestGetExistingActionItemsForChannel(t *testing.T) {
	database := testDB(t)

	// Inbox item — should be returned.
	_, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox item",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Active item — should be returned.
	id2, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active item",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptActionItem(int(id2)))

	// Done item — should NOT be returned.
	id3, err := database.UpsertActionItem(db.ActionItem{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done item",
		Status: "inbox", Priority: "low", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateActionItemStatus(int(id3), "done"))

	// Different channel — should NOT be returned.
	_, err = database.UpsertActionItem(db.ActionItem{
		ChannelID: "C2", AssigneeUserID: "U001", Text: "other channel",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	items, err := database.GetExistingActionItemsForChannel("C1", "U001")
	require.NoError(t, err)
	assert.Len(t, items, 2)

	texts := map[string]bool{}
	for _, item := range items {
		texts[item.Text] = true
	}
	assert.True(t, texts["inbox item"])
	assert.True(t, texts["active item"])
}

func TestPipelineExistingIDStatusHintDone(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))

	// Create existing active action item (e.g., "whitelist IP for EY consultants").
	existingID, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "whitelist IP 136.226.198.1 for EY consultants",
		Context:        "EY consultants need access before March 19 meeting",
		Status:         "active",
		Priority:       "high",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// New message: someone confirms the task is done.
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000200.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
	}))

	// AI returns existing_id with status_hint "done".
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"status_hint": "done",
					"text": "whitelist IP 136.226.198.1 for EY consultants",
					"context": "Denis opened access for IP 136.226.198.1/32 on prod EU. Task completed.",
					"source_message_ts": "1000000200.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000150, 1000000300)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// The existing item should be marked as done with has_updates flag.
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "done", items[0].Status)
	assert.True(t, items[0].HasUpdates)
	assert.True(t, items[0].CompletedAt.Valid)
	assert.Contains(t, items[0].Context, "Denis opened access")
}

func TestPipelineCrossChannelCompletion(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "denis", DisplayName: "Denis"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C2", Name: "devops", Type: "public"}))

	// Action item created in dev-europe.
	existingID, err := database.UpsertActionItem(db.ActionItem{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "whitelist IP 136.226.198.1 for EY consultants",
		Context:        "EY needs access before March 19 meeting",
		Status:         "active",
		Priority:       "high",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// Completion message appears in devops channel (different channel!).
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C2", TS: "1000000200.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
	}))

	// AI recognizes cross-channel completion.
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"status_hint": "done",
					"text": "whitelist IP 136.226.198.1 for EY consultants",
					"context": "Denis confirmed access opened in devops channel",
					"source_message_ts": "1000000200.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000150, 1000000300)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Item from C1 should be marked done via message from C2.
	items, err := database.GetActionItems(db.ActionItemFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "done", items[0].Status)
	assert.Equal(t, "C1", items[0].ChannelID) // still in original channel
	assert.True(t, items[0].CompletedAt.Valid)
}

// Verify JSON structure matches what the AI produces
func TestAIResultStructure(t *testing.T) {
	sample := aiResult{
		Items: []aiItem{
			{
				Text:            "Review PR #42",
				Context:         "Bob needs Alice to approve",
				ChannelID:       "C123",
				ChannelName:     "#dev",
				SourceMsgTS:     "1234567890.123456",
				Priority:        "high",
				DueDate:         "2025-01-15",
				Requester:       &aiRequester{Name: "@bob", UserID: "U1"},
				Category:        "code_review",
				Blocking:        "Release v2.0 is blocked",
				Tags:            json.RawMessage(`["backend","pr"]`),
				DecisionSummary: "Team agreed this needs review before merge",
				DecisionOptions: json.RawMessage(`[]`),
				Participants:    json.RawMessage(`[{"name":"@bob","user_id":"U1","stance":"needs review"}]`),
				SourceRefs:      json.RawMessage(`[{"ts":"1234567890.123456","author":"@bob","text":"Please review PR #42"}]`),
				SubItems:        json.RawMessage(`[{"text":"Check code quality","status":"open"},{"text":"Run tests","status":"open"}]`),
			},
		},
	}
	data, err := json.Marshal(sample)
	require.NoError(t, err)

	var parsed aiResult
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, sample, parsed)
}
