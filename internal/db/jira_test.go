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

func TestGetJiraIssueLinksByKey(t *testing.T) {
	db := openTestDB(t)

	// Insert several links.
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l1", SourceKey: "PROJ-1", TargetKey: "PROJ-2", LinkType: "Blocks", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l2", SourceKey: "PROJ-3", TargetKey: "PROJ-1", LinkType: "Relates to", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l3", SourceKey: "PROJ-4", TargetKey: "PROJ-5", LinkType: "Blocks", SyncedAt: "now"}))

	// PROJ-1 appears as source in l1, target in l2 — should find both.
	links, err := db.GetJiraIssueLinksByKey("PROJ-1")
	require.NoError(t, err)
	assert.Len(t, links, 2)
	assert.Equal(t, "l1", links[0].ID)
	assert.Equal(t, "l2", links[1].ID)

	// PROJ-5 appears only as target in l3.
	links, err = db.GetJiraIssueLinksByKey("PROJ-5")
	require.NoError(t, err)
	assert.Len(t, links, 1)
	assert.Equal(t, "l3", links[0].ID)

	// Non-existent key returns empty.
	links, err = db.GetJiraIssueLinksByKey("PROJ-999")
	require.NoError(t, err)
	assert.Empty(t, links)
}

func TestGetJiraIssueLinksByKeys(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l1", SourceKey: "A-1", TargetKey: "A-2", LinkType: "Blocks", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l2", SourceKey: "A-3", TargetKey: "A-4", LinkType: "Relates to", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraIssueLink(JiraIssueLink{ID: "l3", SourceKey: "A-5", TargetKey: "A-6", LinkType: "Blocks", SyncedAt: "now"}))

	// Batch: A-1 (source in l1) and A-4 (target in l2) → l1, l2.
	links, err := db.GetJiraIssueLinksByKeys([]string{"A-1", "A-4"})
	require.NoError(t, err)
	assert.Len(t, links, 2)
	linkIDs := []string{links[0].ID, links[1].ID}
	assert.Contains(t, linkIDs, "l1")
	assert.Contains(t, linkIDs, "l2")

	// Empty input returns empty slice.
	links, err = db.GetJiraIssueLinksByKeys([]string{})
	require.NoError(t, err)
	assert.Empty(t, links)

	// Single key matching multiple links (A-2 is target in l1 only).
	links, err = db.GetJiraIssueLinksByKeys([]string{"A-2"})
	require.NoError(t, err)
	assert.Len(t, links, 1)
	assert.Equal(t, "l1", links[0].ID)
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

	// Verify issue links are also cleared.
	issueLinks, _ := db.GetJiraIssueLinksByKey("P-1")
	assert.Empty(t, issueLinks)
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

func TestCreateTaskFromJiraIssue(t *testing.T) {
	db := openTestDB(t)

	issue := JiraIssue{
		Key:            "PROJ-10",
		ProjectKey:     "PROJ",
		Summary:        "Implement feature X",
		Priority:       "High",
		DueDate:        "2026-05-01",
		Status:         "Open",
		StatusCategory: "todo",
		Labels:         `[]`,
		Components:     `[]`,
		CreatedAt:      "now",
		UpdatedAt:      "now",
		SyncedAt:       "now",
	}

	// First call creates the task.
	task, err := db.CreateTaskFromJiraIssue(issue)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, "Implement feature X", task.Text)
	assert.Equal(t, "todo", task.Status)
	assert.Equal(t, "high", task.Priority)
	assert.Equal(t, "mine", task.Ownership)
	assert.Equal(t, "jira", task.SourceType)
	assert.Equal(t, "PROJ-10", task.SourceID)
	assert.Equal(t, "2026-05-01", task.DueDate)
	firstID := task.ID

	// Second call returns existing task (dedup).
	task2, err := db.CreateTaskFromJiraIssue(issue)
	require.NoError(t, err)
	require.NotNil(t, task2)
	assert.Equal(t, firstID, task2.ID, "should return existing task, not create duplicate")

	// Verify only one task exists.
	tasks, err := db.GetTasks(TaskFilter{SourceType: "jira", SourceID: "PROJ-10", IncludeDone: true})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
}

func TestCreateTaskFromJiraIssue_PriorityMapping(t *testing.T) {
	db := openTestDB(t)

	cases := []struct {
		jiraPriority string
		expected     string
	}{
		{"Highest", "high"},
		{"High", "high"},
		{"Medium", "medium"},
		{"Low", "low"},
		{"Lowest", "low"},
		{"Unknown", "medium"},
		{"", "medium"},
	}

	for _, tc := range cases {
		issue := JiraIssue{
			Key: "MAP-" + tc.jiraPriority, ProjectKey: "MAP", Summary: "Test " + tc.jiraPriority,
			Priority: tc.jiraPriority, Status: "Open", StatusCategory: "todo",
			Labels: `[]`, Components: `[]`, CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
		}
		task, err := db.CreateTaskFromJiraIssue(issue)
		require.NoError(t, err, "priority=%s", tc.jiraPriority)
		assert.Equal(t, tc.expected, task.Priority, "jira priority %q should map to %q", tc.jiraPriority, tc.expected)
	}
}

func TestSyncJiraTaskStatuses(t *testing.T) {
	db := openTestDB(t)

	// Seed Jira issues with different statuses.
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "S-1", ProjectKey: "S", Summary: "Done in Jira",
		Status: "Done", StatusCategory: "done",
		Labels: `[]`, Components: `[]`, CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
	}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "S-2", ProjectKey: "S", Summary: "In progress in Jira",
		Status: "In Progress", StatusCategory: "in_progress",
		Labels: `[]`, Components: `[]`, CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
	}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "S-3", ProjectKey: "S", Summary: "Still todo in Jira",
		Status: "Open", StatusCategory: "todo",
		Labels: `[]`, Components: `[]`, CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
	}))

	// Create tasks linked to these issues.
	t1, err := db.CreateTaskFromJiraIssue(JiraIssue{Key: "S-1", Summary: "Done in Jira", Priority: "Medium"})
	require.NoError(t, err)
	t2, err := db.CreateTaskFromJiraIssue(JiraIssue{Key: "S-2", Summary: "In progress in Jira", Priority: "Medium"})
	require.NoError(t, err)
	t3, err := db.CreateTaskFromJiraIssue(JiraIssue{Key: "S-3", Summary: "Still todo in Jira", Priority: "Medium"})
	require.NoError(t, err)

	// All tasks start as 'todo'.
	assert.Equal(t, "todo", t1.Status)
	assert.Equal(t, "todo", t2.Status)
	assert.Equal(t, "todo", t3.Status)

	// Sync.
	synced, err := db.SyncJiraTaskStatuses()
	require.NoError(t, err)
	assert.Equal(t, 2, synced, "should update S-1 to done and S-2 to in_progress")

	// Verify statuses.
	task1, _ := db.GetTaskByID(t1.ID)
	assert.Equal(t, "done", task1.Status)

	task2, _ := db.GetTaskByID(t2.ID)
	assert.Equal(t, "in_progress", task2.Status)

	task3, _ := db.GetTaskByID(t3.ID)
	assert.Equal(t, "todo", task3.Status, "todo in Jira should stay todo")

	// Idempotent: running again should update 0 (S-1 is done/excluded, S-2 is already in_progress, S-3 is still todo).
	synced2, err := db.SyncJiraTaskStatuses()
	require.NoError(t, err)
	assert.Equal(t, 0, synced2, "idempotent run should update nothing")
}

func TestSyncJiraTaskStatuses_MissingIssue(t *testing.T) {
	db := openTestDB(t)

	// Create a task with source_type=jira but no corresponding Jira issue in DB.
	_, err := db.CreateTask(Task{
		Text: "Orphan task", Status: "todo", Priority: "medium",
		Ownership: "mine", SourceType: "jira", SourceID: "GONE-1",
		Tags: "[]", SubItems: "[]",
	})
	require.NoError(t, err)

	// Should not fail — just skip the missing issue.
	synced, err := db.SyncJiraTaskStatuses()
	require.NoError(t, err)
	assert.Equal(t, 0, synced)
}

func TestGetJiraTeamWorkload(t *testing.T) {
	db := openTestDB(t)

	sp3 := 3.0
	sp5 := 5.0

	// User U1: 2 open issues (1 overdue, 1 blocked), 1 done issue.
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-1", ProjectKey: "P", Summary: "Open normal",
		Status: "In Progress", StatusCategory: "in_progress",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		StoryPoints: &sp3,
		Labels:      `[]`, Components: `[]`,
		CreatedAt: "2026-03-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z", SyncedAt: "now",
	}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-2", ProjectKey: "P", Summary: "Overdue task",
		Status: "Open", StatusCategory: "todo",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		StoryPoints: &sp5,
		DueDate:     "2025-01-01",
		Labels:      `[]`, Components: `[]`,
		CreatedAt: "2026-03-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z", SyncedAt: "now",
	}))
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-3", ProjectKey: "P", Summary: "Blocked task",
		Status: "Blocked", StatusCategory: "in_progress",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		Labels: `[]`, Components: `[]`,
		CreatedAt: "2026-03-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z", SyncedAt: "now",
	}))
	// Done issue for U1 (should NOT count as open/overdue/blocked, but contributes to avg cycle time).
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-4", ProjectKey: "P", Summary: "Done task",
		Status: "Done", StatusCategory: "done",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		Labels: `[]`, Components: `[]`,
		CreatedAt:  "2026-03-20T00:00:00Z",
		ResolvedAt: "2026-03-30T00:00:00Z",
		UpdatedAt:  "2026-03-30T00:00:00Z", SyncedAt: "now",
	}))

	// User U2: 1 open issue, no overdue, no blocked.
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-5", ProjectKey: "P", Summary: "U2 open",
		Status: "To Do", StatusCategory: "todo",
		AssigneeSlackID: "U2", AssigneeDisplayName: "Bob",
		Labels: `[]`, Components: `[]`,
		CreatedAt: "2026-04-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z", SyncedAt: "now",
	}))

	// Issue with empty assignee_slack_id (should be filtered out).
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-6", ProjectKey: "P", Summary: "Unassigned",
		Status: "Open", StatusCategory: "todo",
		Labels: `[]`, Components: `[]`,
		CreatedAt: "2026-04-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z", SyncedAt: "now",
	}))

	// Deleted issue (should be filtered out).
	require.NoError(t, db.UpsertJiraIssue(JiraIssue{
		Key: "P-7", ProjectKey: "P", Summary: "Deleted",
		Status: "Open", StatusCategory: "todo",
		AssigneeSlackID: "U1", AssigneeDisplayName: "Alice",
		Labels: `[]`, Components: `[]`,
		CreatedAt: "2026-04-01T00:00:00Z", UpdatedAt: "2026-04-01T00:00:00Z",
		SyncedAt: "now", IsDeleted: true,
	}))

	rows, err := db.GetJiraTeamWorkload()
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Rows ordered by open_issues DESC, so U1 first.
	u1 := rows[0]
	assert.Equal(t, "U1", u1.SlackUserID)
	assert.Equal(t, "Alice", u1.DisplayName)
	assert.Equal(t, 3, u1.OpenIssues)       // P-1, P-2, P-3 (not P-4 done, not P-7 deleted)
	assert.Equal(t, 8.0, u1.StoryPoints)    // 3 + 5 (P-3 has nil story_points)
	assert.Equal(t, 1, u1.OverdueCount)     // P-2
	assert.Equal(t, 1, u1.BlockedCount)     // P-3
	assert.True(t, u1.AvgCycleTimeDays > 0) // P-4: 10 days

	u2 := rows[1]
	assert.Equal(t, "U2", u2.SlackUserID)
	assert.Equal(t, "Bob", u2.DisplayName)
	assert.Equal(t, 1, u2.OpenIssues)
	assert.Equal(t, 0.0, u2.StoryPoints)
	assert.Equal(t, 0, u2.OverdueCount)
	assert.Equal(t, 0, u2.BlockedCount)
	assert.Equal(t, 0.0, u2.AvgCycleTimeDays) // no resolved issues for U2
}

func TestGetJiraTeamWorkload_Empty(t *testing.T) {
	db := openTestDB(t)

	rows, err := db.GetJiraTeamWorkload()
	require.NoError(t, err)
	assert.Empty(t, rows)
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

// --- Jira Releases tests ---

func TestUpsertAndGetJiraReleases(t *testing.T) {
	db := openTestDB(t)

	r1 := JiraRelease{
		ID: 10001, ProjectKey: "PROJ", Name: "v1.0",
		Description: "First release", ReleaseDate: "2026-04-15",
		Released: false, Archived: false, SyncedAt: "2026-04-01T00:00:00Z",
	}
	r2 := JiraRelease{
		ID: 10002, ProjectKey: "PROJ", Name: "v2.0",
		Description: "Second release", ReleaseDate: "2026-05-01",
		Released: true, Archived: false, SyncedAt: "2026-04-01T00:00:00Z",
	}
	require.NoError(t, db.UpsertJiraRelease(r1))
	require.NoError(t, db.UpsertJiraRelease(r2))

	releases, err := db.GetJiraReleases("PROJ")
	require.NoError(t, err)
	assert.Len(t, releases, 2)
	assert.Equal(t, "v1.0", releases[0].Name)
	assert.Equal(t, "v2.0", releases[1].Name)
	assert.False(t, releases[0].Released)
	assert.True(t, releases[1].Released)
}

func TestUpsertJiraRelease_UpdateOnConflict(t *testing.T) {
	db := openTestDB(t)

	r := JiraRelease{
		ID: 10001, ProjectKey: "PROJ", Name: "v1.0",
		Description: "Old desc", ReleaseDate: "2026-04-15",
		Released: false, SyncedAt: "2026-04-01T00:00:00Z",
	}
	require.NoError(t, db.UpsertJiraRelease(r))

	// Update via upsert.
	r.Description = "New desc"
	r.Released = true
	require.NoError(t, db.UpsertJiraRelease(r))

	releases, err := db.GetJiraReleases("PROJ")
	require.NoError(t, err)
	require.Len(t, releases, 1)
	assert.Equal(t, "New desc", releases[0].Description)
	assert.True(t, releases[0].Released)
}

func TestGetJiraReleases_Empty(t *testing.T) {
	db := openTestDB(t)

	releases, err := db.GetJiraReleases("NONEXIST")
	require.NoError(t, err)
	assert.Empty(t, releases)
}

func TestGetJiraReleases_SortedByReleaseDate(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 3, ProjectKey: "P", Name: "v3", ReleaseDate: "2026-06-01", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 1, ProjectKey: "P", Name: "v1", ReleaseDate: "2026-04-01", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 2, ProjectKey: "P", Name: "v2", ReleaseDate: "2026-05-01", SyncedAt: "now"}))

	releases, err := db.GetJiraReleases("P")
	require.NoError(t, err)
	require.Len(t, releases, 3)
	assert.Equal(t, "v1", releases[0].Name)
	assert.Equal(t, "v2", releases[1].Name)
	assert.Equal(t, "v3", releases[2].Name)
}

func TestGetJiraReleasesByName(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 1, ProjectKey: "A", Name: "v1.0", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 2, ProjectKey: "B", Name: "v1.0", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 3, ProjectKey: "C", Name: "v2.0", SyncedAt: "now"}))

	releases, err := db.GetJiraReleasesByName("v1.0")
	require.NoError(t, err)
	assert.Len(t, releases, 2)
	assert.Equal(t, "A", releases[0].ProjectKey)
	assert.Equal(t, "B", releases[1].ProjectKey)

	releases, err = db.GetJiraReleasesByName("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, releases)
}

func TestJiraIssueFixVersions(t *testing.T) {
	db := openTestDB(t)

	issue := JiraIssue{
		Key: "P-1", ProjectKey: "P", Summary: "With fix versions",
		Status: "Open", StatusCategory: "todo",
		Labels: `[]`, Components: `[]`, FixVersions: `["v1.0","v2.0"]`,
		CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
	}
	require.NoError(t, db.UpsertJiraIssue(issue))

	loaded, err := db.GetJiraIssueByKey("P-1")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, `["v1.0","v2.0"]`, loaded.FixVersions)
}

func TestJiraIssueFixVersions_DefaultEmpty(t *testing.T) {
	db := openTestDB(t)

	// Issue without setting FixVersions — should default to "[]".
	issue := JiraIssue{
		Key: "P-2", ProjectKey: "P", Summary: "No fix versions",
		Status: "Open", StatusCategory: "todo",
		Labels: `[]`, Components: `[]`,
		CreatedAt: "now", UpdatedAt: "now", SyncedAt: "now",
	}
	require.NoError(t, db.UpsertJiraIssue(issue))

	loaded, err := db.GetJiraIssueByKey("P-2")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	// Either empty string or "[]" is acceptable; the DB default is "[]".
	assert.Contains(t, []string{"", "[]"}, loaded.FixVersions)
}

func TestClearJiraData_IncludesReleases(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertJiraBoard(JiraBoard{ID: 1, Name: "B", ProjectKey: "P", SyncedAt: "now"}))
	require.NoError(t, db.UpsertJiraRelease(JiraRelease{ID: 1, ProjectKey: "P", Name: "v1.0", SyncedAt: "now"}))

	require.NoError(t, db.ClearJiraData())

	releases, _ := db.GetJiraReleases("P")
	assert.Empty(t, releases)
}
