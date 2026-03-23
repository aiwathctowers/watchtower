package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertPromptNew(t *testing.T) {
	db := openTestDB(t)
	err := db.UpsertPrompt(Prompt{
		ID:       "digest.channel",
		Template: "Analyze the following messages...",
		Version:  1,
		Language: "en",
	})
	require.NoError(t, err)

	p, err := db.GetPrompt("digest.channel")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "digest.channel", p.ID)
	assert.Equal(t, "Analyze the following messages...", p.Template)
	assert.Equal(t, 1, p.Version)
	assert.Equal(t, "en", p.Language)

	// Check history was recorded
	history, err := db.GetPromptHistory("digest.channel")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, 1, history[0].Version)
	assert.Equal(t, "initial seed", history[0].Reason)
}

func TestUpsertPromptUpdate(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1 text", Version: 1})

	// Upsert with same ID bumps version
	err := db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v2 text", Version: 1})
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "v2 text", p.Template)
	assert.Equal(t, 2, p.Version)
}

func TestUpdatePrompt(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "original", Version: 1})

	err := db.UpdatePrompt("test.prompt", "improved version", "tuned: 5 negative feedbacks")
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "improved version", p.Template)
	assert.Equal(t, 2, p.Version)

	// Check history
	history, err := db.GetPromptHistory("test.prompt")
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, 2, history[0].Version)
	assert.Equal(t, "tuned: 5 negative feedbacks", history[0].Reason)
	assert.Equal(t, 1, history[1].Version)
}

func TestUpdatePromptNotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdatePrompt("nonexistent", "text", "reason")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetAllPrompts(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "b.prompt", Template: "b", Version: 1})
	_ = db.UpsertPrompt(Prompt{ID: "a.prompt", Template: "a", Version: 1})

	all, err := db.GetAllPrompts()
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "a.prompt", all[0].ID)
	assert.Equal(t, "b.prompt", all[1].ID)
}

func TestRollbackPrompt(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1 original", Version: 1})
	_ = db.UpdatePrompt("test.prompt", "v2 tuned", "auto-tune")
	_ = db.UpdatePrompt("test.prompt", "v3 broken", "auto-tune")

	// Rollback to v1
	err := db.RollbackPrompt("test.prompt", 1)
	require.NoError(t, err)

	p, err := db.GetPrompt("test.prompt")
	require.NoError(t, err)
	assert.Equal(t, "v1 original", p.Template)
	assert.Equal(t, 4, p.Version) // new version, old content

	// History has 4 entries
	history, err := db.GetPromptHistory("test.prompt")
	require.NoError(t, err)
	assert.Len(t, history, 4)
	assert.Equal(t, "rollback to v1", history[0].Reason)
}

func TestRollbackPromptBadVersion(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "v1", Version: 1})

	err := db.RollbackPrompt("test.prompt", 99)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetPromptAtVersion(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertPrompt(Prompt{ID: "test.prompt", Template: "version one", Version: 1})
	_ = db.UpdatePrompt("test.prompt", "version two", "edit")

	h, err := db.GetPromptAtVersion("test.prompt", 1)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "version one", h.Template)

	h, err = db.GetPromptAtVersion("test.prompt", 2)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "version two", h.Template)

	h, err = db.GetPromptAtVersion("test.prompt", 99)
	require.NoError(t, err)
	assert.Nil(t, h)
}

func TestGetPromptNil(t *testing.T) {
	db := openTestDB(t)
	p, err := db.GetPrompt("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, p)
}
