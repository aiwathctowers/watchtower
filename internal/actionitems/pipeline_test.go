package actionitems

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"testing"

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
		Text: "actually this is now urgent, we need it by EOD",
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
