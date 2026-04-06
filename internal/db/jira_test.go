package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndGetJiraBoards(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertJiraBoard(JiraBoard{
		ID: 1, Name: "Sprint Board", ProjectKey: "PROJ", BoardType: "scrum", IsSelected: false, IssueCount: 42, SyncedAt: "2026-04-01T00:00:00Z",
	})
	require.NoError(t, err)

	err = db.UpsertJiraBoard(JiraBoard{
		ID: 2, Name: "Kanban Board", ProjectKey: "KAN", BoardType: "kanban", IsSelected: true, IssueCount: 10, SyncedAt: "2026-04-01T00:00:00Z",
	})
	require.NoError(t, err)

	boards, err := db.GetJiraBoards()
	require.NoError(t, err)
	assert.Len(t, boards, 2)
}

func TestGetJiraSelectedBoards(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "B1", ProjectKey: "P1", IsSelected: true, SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 2, Name: "B2", ProjectKey: "P2", IsSelected: false, SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 3, Name: "B3", ProjectKey: "P3", IsSelected: true, SyncedAt: "now"}))

	selected, err := db.GetJiraSelectedBoards()
	require.NoError(t, err)
	assert.Len(t, selected, 2)
}

func TestSetJiraBoardSelected(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "B1", ProjectKey: "P1", IsSelected: false, SyncedAt: "now"}))

	require.NoError(t, db.SetJiraBoardSelected(1, true))
	selected, err := db.GetJiraSelectedBoards()
	require.NoError(t, err)
	assert.Len(t, selected, 1)

	require.NoError(t, db.SetJiraBoardSelected(1, false))
	selected, err = db.GetJiraSelectedBoards()
	require.NoError(t, err)
	assert.Empty(t, selected)
}

func TestUpsertAndGetJiraIssue(t *testing.T) {
	db := openTestDB(t)

	issue := JiraIssue{
		Key:               "PROJ-1",
		ID:                "10001",
		ProjectKey:        "PROJ",
		BoardID:           1,
		Summary:           "Test issue",
		DescriptionText:   "Description here",
		IssueType:         "Story",
		IssueTypeCategory: "standard",
		Status:            "In Progress",
		StatusCategory:    "in_progress",
		Priority:          "Medium",
		Labels:            `["backend","urgent"]`,
		Components:        `["core"]`,
		CreatedAt:         "2026-04-01T00:00:00Z",
		UpdatedAt:         "2026-04-01T12:00:00Z",
		SyncedAt:          "2026-04-01T12:00:00Z",
	}
	require.NoError(t, db.UpsertJiraIssue(issue))

	loaded, err := db.GetJiraIssueByKey("PROJ-1")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "PROJ-1", loaded.Key)
	assert.Equal(t, "Test issue", loaded.Summary)
	assert.Equal(t, "in_progress", loaded.StatusCategory)
}

func TestGetJiraIssueByKey_NotFound(t *testing.T) {
	db := openTestDB(t)

	issue, err := db.GetJiraIssueByKey("NONEXIST-1")
	require.NoError(t, err)
	assert.Nil(t, issue)
}

func TestGetJiraIssueCount(t *testing.T) {
	db := openTestDB(t)

	count, err := db.GetJiraIssueCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "P-1", ProjectKey: "P", Summary: "S", Status: "Open", StatusCategory: "todo", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "P-2", ProjectKey: "P", Summary: "S", Status: "Open", StatusCategory: "todo", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))

	count, err = db.GetJiraIssueCount()
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestUpsertJiraIssue_UpdateOnConflict(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "P-1", ProjectKey: "P", Summary: "Old", Status: "Open", StatusCategory: "todo", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "P-1", ProjectKey: "P", Summary: "New", Status: "Done", StatusCategory: "done", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))

	loaded, err := db.GetJiraIssueByKey("P-1")
	require.NoError(t, err)
	assert.Equal(t, "New", loaded.Summary)
	assert.Equal(t, "done", loaded.StatusCategory)
}

func TestUpsertAndGetJiraSprint(t *testing.T) {
	db := openTestDB(t)

	sprint := JiraSprint{
		ID: 100, BoardID: 1, Name: "Sprint 1", State: "active",
		Goal: "Ship MVP", StartDate: "2026-04-01", EndDate: "2026-04-14", SyncedAt: "now",
	}
	require.NoError(t, db.UpsertJiraSprint(sprint))

	sprints, err := db.GetJiraActiveSprints(1)
	require.NoError(t, err)
	assert.Len(t, sprints, 1)
	assert.Equal(t, "Sprint 1", sprints[0].Name)
	assert.Equal(t, "Ship MVP", sprints[0].Goal)
}

func TestGetJiraActiveSprints_FiltersState(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraSprint(JiraSprint{ID: 1, BoardID: 1, Name: "Active", State: "active", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraSprint(JiraSprint{ID: 2, BoardID: 1, Name: "Closed", State: "closed", SyncedAt: "now"}))

	sprints, err := db.GetJiraActiveSprints(1)
	require.NoError(t, err)
	assert.Len(t, sprints, 1)
	assert.Equal(t, "Active", sprints[0].Name)
}

func TestUpsertJiraIssueLink(t *testing.T) {
	db := openTestDB(t)

	link := JiraIssueLink{
		ID: "link-1", SourceKey: "PROJ-1", TargetKey: "PROJ-2", LinkType: "Blocks", SyncedAt: "now",
	}
	require.NoError(t, db.UpsertJiraIssueLink(link))

	// Update on conflict.
	link.LinkType = "Is blocked by"
	require.NoError(t, db.UpsertJiraIssueLink(link))
}

func TestUpsertAndGetJiraUserMap(t *testing.T) {
	db := openTestDB(t)

	mapping := JiraUserMap{
		JiraAccountID: "acc-123", Email: "user@example.com", SlackUserID: "U123",
		DisplayName: "John Doe", MatchMethod: "email", MatchConfidence: 1.0,
		ResolvedAt: "2026-04-01T00:00:00Z",
	}
	require.NoError(t, db.UpsertJiraUserMap(mapping))

	maps, err := db.GetJiraUserMaps()
	require.NoError(t, err)
	assert.Len(t, maps, 1)
	assert.Equal(t, "acc-123", maps[0].JiraAccountID)
	assert.Equal(t, "U123", maps[0].SlackUserID)

	loaded, err := db.GetJiraUserMapByAccountID("acc-123")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "email", loaded.MatchMethod)
}

func TestGetJiraUserMapByAccountID_NotFound(t *testing.T) {
	db := openTestDB(t)

	loaded, err := db.GetJiraUserMapByAccountID("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestUpdateAndGetJiraSyncState(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpdateJiraSyncState("PROJ", "2026-04-01T00:00:00Z", 100))

	state, err := db.GetJiraSyncState("PROJ")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "PROJ", state.ProjectKey)
	assert.Equal(t, "2026-04-01T00:00:00Z", state.LastSyncedAt)
	assert.Equal(t, 100, state.IssuesSynced)
}

func TestGetJiraSyncState_NotFound(t *testing.T) {
	db := openTestDB(t)

	state, err := db.GetJiraSyncState("NONEXIST")
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestGetJiraSyncStates(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpdateJiraSyncState("A", "2026-04-01T00:00:00Z", 10))
	require.NoError(t, db.UpdateJiraSyncState("B", "2026-04-01T00:00:00Z", 20))

	states, err := db.GetJiraSyncStates()
	require.NoError(t, err)
	assert.Len(t, states, 2)
}

func TestClearJiraData(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "B", ProjectKey: "P", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "P-1", ProjectKey: "P", Summary: "S", Status: "O", StatusCategory: "todo", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraSprint(JiraSprint{ID: 1, BoardID: 1, Name: "S", State: "active", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l1", SourceKey: "P-1", TargetKey: "P-2", LinkType: "Blocks", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraUserMap(JiraUserMap{JiraAccountID: "a1", DisplayName: "User"}))
	require.NoError(t, db.UpdateJiraSyncState("P", "now", 1))

	require.NoError(t, db.ClearJiraData())

	boards, _ := db.GetJiraBoards()
	assert.Empty(t, boards)

	count, _ := db.GetJiraIssueCount()
	assert.Equal(t, 0, count)

	maps, _ := db.GetJiraUserMaps()
	assert.Empty(t, maps)

	states, _ := db.GetJiraSyncStates()
	assert.Empty(t, states)
}
