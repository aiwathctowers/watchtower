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

func TestUpdateAndGetJiraBoardProfile(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "Board", ProjectKey: "P", SyncedAt: "now"}))

	require.NoError(t, db.UpdateJiraBoardProfile(1, `[{"name":"col1"}]`, `{"columns":[]}`, `{"stages":[]}`, "scrum workflow", "abc123", "2026-04-01T00:00:00Z"))

	board, err := db.GetJiraBoardProfile(1)
	require.NoError(t, err)
	require.NotNil(t, board)
	assert.Equal(t, `[{"name":"col1"}]`, board.RawColumnsJSON)
	assert.Equal(t, `{"columns":[]}`, board.RawConfigJSON)
	assert.Equal(t, `{"stages":[]}`, board.LLMProfileJSON)
	assert.Equal(t, "scrum workflow", board.WorkflowSummary)
	assert.Equal(t, "abc123", board.ConfigHash)
	assert.Equal(t, "2026-04-01T00:00:00Z", board.ProfileGeneratedAt)
}

func TestUpdateJiraBoardUserOverrides(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "Board", ProjectKey: "P", SyncedAt: "now"}))
	require.NoError(t, db.UpdateJiraBoardUserOverrides(1, `{"stale_thresholds":{"Review":2}}`))

	board, err := db.GetJiraBoardProfile(1)
	require.NoError(t, err)
	require.NotNil(t, board)
	assert.Equal(t, `{"stale_thresholds":{"Review":2}}`, board.UserOverridesJSON)
}

func TestGetJiraBoardProfile_NotFound(t *testing.T) {
	db := openTestDB(t)

	board, err := db.GetJiraBoardProfile(999)
	require.NoError(t, err)
	assert.Nil(t, board)
}

func TestUpsertJiraBoard_DoesNotOverwriteProfile(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "Board", ProjectKey: "P", SyncedAt: "now"}))
	require.NoError(t, db.UpdateJiraBoardProfile(1, "raw", "cfg", "profile", "summary", "hash", "time"))

	// Re-upsert the board (sync scenario).
	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "Updated Board", ProjectKey: "P", SyncedAt: "now2"}))

	board, err := db.GetJiraBoardProfile(1)
	require.NoError(t, err)
	require.NotNil(t, board)
	assert.Equal(t, "Updated Board", board.Name)
	assert.Equal(t, "profile", board.LLMProfileJSON, "profile should not be overwritten by UpsertJiraBoard")
}

func TestUpsertAndGetJiraSlackLink(t *testing.T) {
	db := openTestDB(t)

	link := JiraSlackLink{
		IssueKey:  "PROJ-123",
		ChannelID: "C1",
		MessageTS: "1000.001",
		LinkType:  "mention",
	}
	require.NoError(t, db.UpsertJiraSlackLink(link))

	links, err := db.GetJiraSlackLinksByIssue("PROJ-123")
	require.NoError(t, err)
	assert.Len(t, links, 1)
	assert.Equal(t, "PROJ-123", links[0].IssueKey)
	assert.Equal(t, "C1", links[0].ChannelID)
	assert.Equal(t, "mention", links[0].LinkType)

	links, err = db.GetJiraSlackLinksByMessage("C1", "1000.001")
	require.NoError(t, err)
	assert.Len(t, links, 1)
}

func TestUpsertJiraSlackLink_OnConflict(t *testing.T) {
	db := openTestDB(t)

	link1 := JiraSlackLink{
		IssueKey:  "PROJ-1",
		ChannelID: "C1",
		MessageTS: "100.001",
		LinkType:  "mention",
	}
	require.NoError(t, db.UpsertJiraSlackLink(link1))

	// Upsert with track_id.
	trackID := 42
	link2 := JiraSlackLink{
		IssueKey:  "PROJ-1",
		ChannelID: "C1",
		MessageTS: "100.001",
		TrackID:   &trackID,
		LinkType:  "mention",
	}
	require.NoError(t, db.UpsertJiraSlackLink(link2))

	links, err := db.GetJiraSlackLinksByIssue("PROJ-1")
	require.NoError(t, err)
	assert.Len(t, links, 1)
	require.NotNil(t, links[0].TrackID)
	assert.Equal(t, 42, *links[0].TrackID)
}

func TestGetKnownProjectKeys(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraIssue(JiraIssue{Key: "PROJ-1", ProjectKey: "PROJ", Summary: "S", Status: "O", StatusCategory: "todo", CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "Board", ProjectKey: "KAN", SyncedAt: "now"}))

	keys, err := db.GetKnownProjectKeys()
	require.NoError(t, err)
	assert.Contains(t, keys, "PROJ")
	assert.Contains(t, keys, "KAN")
}

func TestClearJiraData_IncludesSlackLinks(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "B", ProjectKey: "P", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-1", ChannelID: "C1", MessageTS: "100", LinkType: "mention"}))

	require.NoError(t, db.ClearJiraData())

	links, _ := db.GetJiraSlackLinksByIssue("P-1")
	assert.Empty(t, links)
}

// --- Phase 1 query tests ---

func seedIssue(t *testing.T, db *DB, key, projectKey, status, statusCat, assigneeSlackID, priority string) {
	t.Helper()
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: key, ProjectKey: projectKey, Summary: "Summary " + key,
		Status: status, StatusCategory: statusCat,
		AssigneeSlackID: assigneeSlackID, Priority: priority,
		Labels: `[]`, Components: `[]`,
		CreatedAt: "2026-04-01T00:00:00Z", UpdatedAt: "2026-04-01T12:00:00Z", SyncedAt: "2026-04-01T12:00:00Z",
	}))
}

func TestGetJiraIssuesForTrack(t *testing.T) {
	db := openTestDB(t)

	seedIssue(t, db, "P-1", "P", "Open", "todo", "U1", "High")
	seedIssue(t, db, "P-2", "P", "Open", "todo", "U2", "Low")

	trackID := 10
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-1", ChannelID: "C1", MessageTS: "100", TrackID: &trackID, LinkType: "mention"}))
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-2", ChannelID: "C1", MessageTS: "200", TrackID: &trackID, LinkType: "mention"}))

	issues, err := db.GetJiraIssuesForTrack(10)
	require.NoError(t, err)
	assert.Len(t, issues, 2)
}

func TestGetJiraIssuesForTrack_Empty(t *testing.T) {
	db := openTestDB(t)

	issues, err := db.GetJiraIssuesForTrack(999)
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestGetJiraIssuesForDigest(t *testing.T) {
	db := openTestDB(t)

	seedIssue(t, db, "P-1", "P", "Done", "done", "U1", "Medium")

	digestID := 5
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-1", ChannelID: "C1", MessageTS: "100", DigestID: &digestID, LinkType: "mention"}))

	issues, err := db.GetJiraIssuesForDigest(5)
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	assert.Equal(t, "P-1", issues[0].Key)
}

func TestGetJiraIssuesForDigest_Empty(t *testing.T) {
	db := openTestDB(t)

	issues, err := db.GetJiraIssuesForDigest(999)
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestGetJiraIssuesByAssigneeSlackID(t *testing.T) {
	db := openTestDB(t)

	seedIssue(t, db, "P-1", "P", "In Progress", "in_progress", "U1", "High")
	seedIssue(t, db, "P-2", "P", "Open", "todo", "U1", "Low")
	seedIssue(t, db, "P-3", "P", "Done", "done", "U1", "Medium") // excluded

	issues, err := db.GetJiraIssuesByAssigneeSlackID("U1")
	require.NoError(t, err)
	assert.Len(t, issues, 2)
	// High priority should come first.
	assert.Equal(t, "P-1", issues[0].Key)
}

func TestGetJiraIssuesByAssigneeSlackID_Empty(t *testing.T) {
	db := openTestDB(t)

	issues, err := db.GetJiraIssuesByAssigneeSlackID("NONEXIST")
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestGetJiraIssuesByKeys(t *testing.T) {
	db := openTestDB(t)

	seedIssue(t, db, "A-1", "A", "Open", "todo", "", "Medium")
	seedIssue(t, db, "A-2", "A", "Open", "todo", "", "Medium")
	seedIssue(t, db, "B-1", "B", "Open", "todo", "", "Medium")

	issues, err := db.GetJiraIssuesByKeys([]string{"A-1", "B-1"})
	require.NoError(t, err)
	assert.Len(t, issues, 2)
}

func TestGetJiraIssuesByKeys_Empty(t *testing.T) {
	db := openTestDB(t)

	issues, err := db.GetJiraIssuesByKeys([]string{})
	require.NoError(t, err)
	assert.Empty(t, issues)

	issues, err = db.GetJiraIssuesByKeys([]string{"NONEXIST-1"})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestGetJiraActiveSprintStats(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraSprint(JiraSprint{
		ID: 1, BoardID: 10, Name: "Sprint 5", State: "active",
		StartDate: "2026-04-01", EndDate: "2026-12-31", SyncedAt: "now",
	}))

	// Issues in the sprint.
	for _, tc := range []struct{ key, cat string }{
		{"P-1", "done"}, {"P-2", "done"},
		{"P-3", "in_progress"},
		{"P-4", "todo"}, {"P-5", "todo"},
	} {
		require.NoError(t, db.UpsertJiraIssue(JiraIssue{
			Key: tc.key, ProjectKey: "P", Summary: "S", Status: "X",
			StatusCategory: tc.cat, SprintID: 1,
			Labels: `[]`, Components: `[]`,
			CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
		}))
	}

	stats, err := db.GetJiraActiveSprintStats(10)
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, "Sprint 5", stats.SprintName)
	assert.Equal(t, 5, stats.Total)
	assert.Equal(t, 2, stats.Done)
	assert.Equal(t, 1, stats.InProgress)
	assert.Equal(t, 2, stats.Todo)
	assert.True(t, stats.DaysLeft > 0)
}

func TestGetJiraActiveSprintStats_NoSprint(t *testing.T) {
	db := openTestDB(t)

	stats, err := db.GetJiraActiveSprintStats(999)
	require.NoError(t, err)
	assert.Nil(t, stats)
}

func TestGetJiraIssuesForUser(t *testing.T) {
	db := openTestDB(t)

	seedIssue(t, db, "P-1", "P", "In Progress", "in_progress", "U1", "Medium")
	seedIssue(t, db, "P-2", "P", "Done", "done", "U1", "Medium")
	seedIssue(t, db, "P-3", "P", "Open", "todo", "U1", "Medium")

	// All issues for user.
	issues, err := db.GetJiraIssuesForUser("U1", "")
	require.NoError(t, err)
	assert.Len(t, issues, 3)

	// Filtered by status.
	issues, err = db.GetJiraIssuesForUser("U1", "in_progress")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	assert.Equal(t, "P-1", issues[0].Key)
}

func TestGetJiraIssuesForUser_Empty(t *testing.T) {
	db := openTestDB(t)

	issues, err := db.GetJiraIssuesForUser("NONEXIST", "")
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestGetJiraSlackLinksByTrackID(t *testing.T) {
	db := openTestDB(t)

	trackID := 42
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-1", ChannelID: "C1", MessageTS: "100", TrackID: &trackID, LinkType: "mention"}))
	require.NoError(t, db.UpsertJiraSlackLink(JiraSlackLink{IssueKey: "P-2", ChannelID: "C1", MessageTS: "200", TrackID: &trackID, LinkType: "mention"}))

	links, err := db.GetJiraSlackLinksByTrackID(42)
	require.NoError(t, err)
	assert.Len(t, links, 2)
}

func TestGetJiraSlackLinksByTrackID_Empty(t *testing.T) {
	db := openTestDB(t)

	links, err := db.GetJiraSlackLinksByTrackID(999)
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestGetJiraDeliveryStats(t *testing.T) {
	db := openTestDB(t)

	// Closed issue with story points.
	sp := 5.0
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-1", ProjectKey: "P", Summary: "Done task",
		Status: "Done", StatusCategory: "done",
		AssigneeSlackID: "U1", Priority: "Medium",
		StoryPoints: &sp,
		Labels:      `["backend"]`,
		Components:  `["core"]`,
		CreatedAt:   "2026-03-25T00:00:00Z",
		UpdatedAt:   "2026-04-02T00:00:00Z",
		ResolvedAt:  "2026-04-02T00:00:00Z",
		SyncedAt:    "now",
	}))

	// Open issue (overdue).
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-2", ProjectKey: "P", Summary: "Open task",
		Status: "Open", StatusCategory: "todo",
		AssigneeSlackID: "U1", Priority: "High",
		DueDate:    "2026-01-01",
		Labels:     `[]`,
		Components: `[]`,
		CreatedAt:  "2026-03-01T00:00:00Z",
		UpdatedAt:  "2026-04-01T00:00:00Z",
		SyncedAt:   "now",
	}))

	stats, err := db.GetJiraDeliveryStats("U1", "2026-04-01", "2026-04-30")
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.IssuesClosed)
	assert.True(t, stats.AvgCycleTimeDays > 0)
	assert.Equal(t, 5.0, stats.StoryPointsCompleted)
	assert.Equal(t, 1, stats.OpenIssues) // P-2
	assert.Equal(t, 1, stats.OverdueIssues)
	assert.Contains(t, stats.Components, "core")
	assert.Contains(t, stats.Labels, "backend")
}

func TestGetJiraDeliveryStats_NoData(t *testing.T) {
	db := openTestDB(t)

	stats, err := db.GetJiraDeliveryStats("NONEXIST", "2026-01-01", "2026-12-31")
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 0, stats.IssuesClosed)
	assert.Equal(t, 0, stats.OpenIssues)
}

func TestTaskSourceTypeJira(t *testing.T) {
	db := openTestDB(t)

	// Verify 'jira' is accepted as source_type in tasks.
	_, err := db.Exec(`INSERT INTO tasks (text, source_type) VALUES ('from jira', 'jira')`)
	require.NoError(t, err)

	var st string
	err = db.QueryRow(`SELECT source_type FROM tasks WHERE text = 'from jira'`).Scan(&st)
	require.NoError(t, err)
	assert.Equal(t, "jira", st)
}
