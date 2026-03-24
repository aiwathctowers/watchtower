package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertPrompt_New(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertPrompt(Prompt{
		ID:       "digest.channel",
		Template: "Summarize: {{.Messages}}",
		Version:  1,
		Language: "en",
	})
	require.NoError(t, err)

	p, err := db.GetPrompt("digest.channel")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "Summarize: {{.Messages}}", p.Template)
	assert.Equal(t, 1, p.Version)
	assert.Equal(t, "en", p.Language)

	// History should have initial seed
	history, err := db.GetPromptHistory("digest.channel")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "initial seed", history[0].Reason)
}

func TestUpsertPrompt_Update(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1})
	require.NoError(t, err)

	// Upsert again
	err = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v2", Version: 2})
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "v2", p.Template)
	assert.Equal(t, 2, p.Version)

	// History should have 2 entries
	history, err := db.GetPromptHistory("test.prompt")
	require.NoError(t, err)
	assert.Len(t, history, 2)
}

func TestUpdatePrompt_WithHistory(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1}))

	err := db.UpdatePrompt("test.prompt", "updated template", "manual edit")
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "updated template", p.Template)
	assert.Equal(t, 2, p.Version)

	history, err := db.GetPromptHistory("test.prompt")
	require.NoError(t, err)
	require.Len(t, history, 2)
	// Newest first
	assert.Equal(t, "manual edit", history[0].Reason)
	assert.Equal(t, 2, history[0].Version)
}

func TestUpdatePrompt_NotFound(t *testing.T) {
	db := openTestDB(t)

	err := db.UpdatePrompt("nonexistent", "template", "reason")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetPrompt_NotFound(t *testing.T) {
	db := openTestDB(t)

	p, err := db.GetPrompt("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestGetAllPrompts_Sorted(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertPrompt(Prompt{ID: "b.prompt", Template: "b", Version: 1}))
	require.NoError(t, db.UpsertPrompt(Prompt{ID: "a.prompt", Template: "a", Version: 1}))

	prompts, err := db.GetAllPrompts()
	require.NoError(t, err)
	require.Len(t, prompts, 2)
	// Sorted by ID
	assert.Equal(t, "a.prompt", prompts[0].ID)
	assert.Equal(t, "b.prompt", prompts[1].ID)
}

func TestGetPromptAtVersion_AllVersions(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1}))
	require.NoError(t, db.UpdatePrompt("test.prompt", "v2", "update"))

	// Get v1
	h, err := db.GetPromptAtVersion("test.prompt", 1)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "v1", h.Template)

	// Get v2
	h, err = db.GetPromptAtVersion("test.prompt", 2)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "v2", h.Template)

	// Non-existent version
	h, err = db.GetPromptAtVersion("test.prompt", 99)
	require.NoError(t, err)
	assert.Nil(t, h)
}

func TestRollbackPrompt_FullCycle(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1}))
	require.NoError(t, db.UpdatePrompt("test.prompt", "v2", "update"))

	// Rollback to v1
	err := db.RollbackPrompt("test.prompt", 1)
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "v1", p.Template)
	assert.Equal(t, 3, p.Version) // bumped to 3

	history, err := db.GetPromptHistory("test.prompt")
	require.NoError(t, err)
	// Should have: initial seed, update, rollback
	assert.Len(t, history, 3)
	assert.Contains(t, history[0].Reason, "rollback to v1")
}

func TestRollbackPrompt_VersionNotFound(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1}))

	err := db.RollbackPrompt("test.prompt", 99)
	assert.Error(t, err)
}
