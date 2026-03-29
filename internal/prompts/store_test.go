package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

// --- Store tests ---

func TestNew(t *testing.T) {
	database := openTestDB(t)

	t.Run("with logger", func(t *testing.T) {
		store := New(database, nil)
		require.NotNil(t, store)
		assert.Equal(t, database, store.db)
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		store := New(database, nil)
		require.NotNil(t, store)
		assert.NotNil(t, store.logger)
	})
}

func TestSeed(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	err := store.Seed()
	require.NoError(t, err)

	// All defaults should be in DB
	all, err := database.GetAllPrompts()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))

	// Calling Seed again should be idempotent (short-circuits via seeded flag)
	err = store.Seed()
	require.NoError(t, err)
}

func TestSeedIdempotentWithExisting(t *testing.T) {
	database := openTestDB(t)

	// Seed once with first store
	store1 := New(database, nil)
	require.NoError(t, store1.Seed())

	// Modify one prompt
	require.NoError(t, database.UpdatePrompt(DigestChannel, "custom text", "manual"))

	// Seed with a NEW store (seeded=false, so it actually checks DB)
	store2 := New(database, nil)
	require.NoError(t, store2.Seed())

	// The customized prompt should NOT be overwritten
	tmpl, version, err := store2.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "custom text", tmpl)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, version)
}

func TestGetFromDB(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, DefaultVersions[DigestChannel], version)
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetFallbackToDefault(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	// Don't seed — should fall back to built-in default

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, 0, version) // 0 = from default, not DB
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetUnknown(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	_, _, err := store.Get("nonexistent.prompt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prompt")
}

func TestGetAllPromptIDs(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	// Test each known prompt ID can be retrieved as a default
	for _, id := range AllIDs {
		tmpl, version, err := store.Get(id)
		require.NoError(t, err, "Get(%q) should not error", id)
		assert.Equal(t, 0, version, "unseeded prompt %q should have version 0", id)
		assert.NotEmpty(t, tmpl, "prompt %q should have non-empty template", id)
	}
}

func TestUpdate(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	err := store.Update(DigestChannel, "custom prompt text", "manual edit")
	require.NoError(t, err)

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "custom prompt text", tmpl)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, version)
}

func TestUpdateMultipleTimes(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	seedVer := DefaultVersions[DigestChannel]
	for i := 0; i < 5; i++ {
		text := fmt.Sprintf("version %d text", seedVer+i+1)
		require.NoError(t, store.Update(DigestChannel, text, fmt.Sprintf("edit %d", i+1)))
	}

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("version %d text", seedVer+5), tmpl)
	assert.Equal(t, seedVer+5, version)

	// History should have all versions
	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	assert.Len(t, history, 6) // 1 seed + 5 updates
}

func TestReset(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "custom text", "test")

	err := store.Reset(DigestChannel)
	require.NoError(t, err)

	tmpl, _, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Contains(t, tmpl, "analyzing Slack messages") // back to default
}

func TestResetUnknown(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	err := store.Reset("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prompt")
}

func TestRollback(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "v2 text", "tune")
	_ = store.Update(DigestChannel, "v3 text", "tune")

	err := store.Rollback(DigestChannel, DefaultVersions[DigestChannel])
	require.NoError(t, err)

	tmpl, _, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Contains(t, tmpl, "analyzing Slack messages") // original seed version
}

func TestRollbackToV2(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "v2 text", "tune")
	_ = store.Update(DigestChannel, "v3 text", "tune")

	err := store.Rollback(DigestChannel, DefaultVersions[DigestChannel]+1)
	require.NoError(t, err)

	tmpl, _, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "v2 text", tmpl)
}

func TestHistory(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "v2", "manual")

	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, history[0].Version)
	assert.Equal(t, "manual", history[0].Reason)
	assert.Equal(t, DefaultVersions[DigestChannel], history[1].Version)
}

func TestHistoryEmpty(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	history, err := store.History("nonexistent.prompt")
	require.NoError(t, err)
	assert.Len(t, history, 0)
}

func TestGetAll(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	// Don't seed — GetAll should still return defaults

	all, err := store.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))

	// Seed and verify — same count
	_ = store.Seed()
	all, err = store.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))
}

func TestGetAllMergesDBAndDefaults(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	// Seed only one prompt manually
	require.NoError(t, database.UpsertPrompt(db.Prompt{
		ID:       DigestChannel,
		Template: "custom channel prompt",
		Version:  1,
	}))

	all, err := store.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))

	// Verify the seeded prompt has custom text
	found := false
	for _, p := range all {
		if p.ID == DigestChannel {
			assert.Equal(t, "custom channel prompt", p.Template)
			assert.Equal(t, 1, p.Version)
			found = true
		}
	}
	assert.True(t, found, "DigestChannel should be in results")
}

func TestDB(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	assert.Equal(t, database, store.DB())
}

func TestDefaultsMatchPromptIDs(t *testing.T) {
	// Verify all AllIDs have defaults
	for _, id := range AllIDs {
		_, ok := Defaults[id]
		assert.True(t, ok, "prompt %q should have a default", id)
	}
	// Verify all AllIDs have descriptions
	for _, id := range AllIDs {
		_, ok := Descriptions[id]
		assert.True(t, ok, "prompt %q should have a description", id)
	}
}

func TestAllIDsMatchDefaults(t *testing.T) {
	// Every key in Defaults should be in AllIDs
	idSet := make(map[string]bool)
	for _, id := range AllIDs {
		idSet[id] = true
	}
	for id := range Defaults {
		assert.True(t, idSet[id], "Defaults key %q should be in AllIDs", id)
	}
}

// --- GetForRole tests ---

func TestGetForRole_NoRole(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	tmpl, version, err := store.GetForRole(DigestChannel, "")
	require.NoError(t, err)
	assert.Equal(t, DefaultVersions[DigestChannel], version)
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetForRole_RoleVariantInDB(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	// Insert a role-specific variant
	roleVariantID := TracksExtract + "_direction_owner"
	require.NoError(t, database.UpsertPrompt(db.Prompt{
		ID:       roleVariantID,
		Template: "role-specific tracks prompt for direction owner",
		Version:  1,
	}))

	tmpl, version, err := store.GetForRole(TracksExtract, "direction_owner")
	require.NoError(t, err)
	assert.Equal(t, 1, version)
	assert.Equal(t, "role-specific tracks prompt for direction owner", tmpl)
}

func TestGetForRole_FallsBackToStandard(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	// No role variant exists — should fall back to standard
	tmpl, version, err := store.GetForRole(DigestChannel, "some_unknown_role")
	require.NoError(t, err)
	assert.Equal(t, DefaultVersions[DigestChannel], version) // from seeded DB
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetForRole_UnknownPromptID(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	_, _, err := store.GetForRole("nonexistent.prompt", "ic")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prompt")
}

// --- Role variants tests ---

func TestGetRoleInstruction(t *testing.T) {
	tests := []struct {
		role     string
		contains string
		empty    bool
	}{
		{"top_management", "Top Management", false},
		{"direction_owner", "strategic leader", false},
		{"middle_management", "Middle Management", false},
		{"senior_ic", "Senior IC", false},
		{"ic", "Individual Contributor", false},
		{"unknown_role", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			result := GetRoleInstruction(tt.role)
			if tt.empty {
				assert.Empty(t, result)
			} else {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

func TestRoleInstructionsAllPresent(t *testing.T) {
	expectedRoles := []string{"top_management", "direction_owner", "middle_management", "senior_ic", "ic"}
	for _, role := range expectedRoles {
		_, ok := RoleInstructions[role]
		assert.True(t, ok, "RoleInstructions should have key %q", role)
	}
}

// --- mockGenerator for Tuner tests ---

type mockGenerator struct {
	response string
	err      error
	// track calls
	calls []string
}

func (m *mockGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	m.calls = append(m.calls, prompt)
	return m.response, m.err
}

// --- GenerateFunc tests ---

func TestGenerateFunc(t *testing.T) {
	var calledSystem, calledUser string
	gen := GenerateFunc(func(ctx context.Context, systemPrompt, userMessage string) (string, error) {
		calledSystem = systemPrompt
		calledUser = userMessage
		return "result", nil
	})

	result, err := gen.GenerateText(context.Background(), "my prompt")
	require.NoError(t, err)
	assert.Equal(t, "result", result)
	assert.Equal(t, "", calledSystem) // GenerateFunc passes "" for system
	assert.Equal(t, "my prompt", calledUser)
}

func TestGenerateFuncError(t *testing.T) {
	gen := GenerateFunc(func(ctx context.Context, systemPrompt, userMessage string) (string, error) {
		return "", fmt.Errorf("ai error")
	})

	_, err := gen.GenerateText(context.Background(), "prompt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ai error")
}

// --- Tuner tests ---

func TestNewTuner(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	gen := &mockGenerator{}

	tuner := NewTuner(store, database, gen)
	require.NotNil(t, tuner)
}

func seedStoreWithFeedback(t *testing.T) (*db.DB, *Store) {
	t.Helper()
	database := openTestDB(t)
	store := New(database, nil)
	require.NoError(t, store.Seed())
	return database, store
}

func addFeedback(t *testing.T, database *db.DB, entityType, entityID string, rating int, comment string) {
	t.Helper()
	_, err := database.AddFeedback(db.Feedback{
		EntityType: entityType,
		EntityID:   entityID,
		Rating:     rating,
		Comment:    comment,
	})
	require.NoError(t, err)
}

func TestSuggest_NoFeedback(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), DigestChannel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no feedback found")
}

func TestSuggest_UnknownPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), "nonexistent.prompt")
	assert.Error(t, err)
}

func TestSuggest_ValidJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	// Add some feedback
	addFeedback(t, database, "digest", "1", 1, "great summary")
	addFeedback(t, database, "digest", "2", -1, "too verbose")

	response := `{"improved_prompt": "improved template %s", "explanation": "made it better", "changes": ["less verbose", "more concise"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, DigestChannel, result.PromptID)
	assert.Equal(t, DefaultVersions[DigestChannel], result.CurrentVersion)
	assert.Equal(t, "improved template %s", result.Suggestion)
	assert.Equal(t, "made it better", result.Explanation)
	assert.Len(t, result.Changes, 2)
	assert.False(t, result.Manual)
}

func TestSuggest_JSONWithPrefix(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	// AI sometimes prefixes JSON with text
	response := `Here is my suggestion:
{"improved_prompt": "better prompt", "explanation": "reason", "changes": ["change1"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "better prompt", result.Suggestion)
}

func TestSuggest_JSONWithSuffix(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	// JSON in the middle with trailing text
	response := `text before {"improved_prompt": "new prompt", "explanation": "why", "changes": []} extra text`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "new prompt", result.Suggestion)
}

func TestSuggest_EmptyImprovedPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	response := `{"improved_prompt": "", "explanation": "reason", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), DigestChannel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty improved prompt")
}

func TestSuggest_InvalidJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	response := `not json at all`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), DigestChannel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON object found")
}

func TestSuggest_BrokenJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	response := `{"improved_prompt": "text`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), DigestChannel)
	assert.Error(t, err)
}

func TestSuggest_GeneratorError(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	gen := &mockGenerator{err: fmt.Errorf("API timeout")}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.Suggest(context.Background(), DigestChannel)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI generation failed")
}

func TestSuggest_SanitizesFeedback(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	// Feedback with delimiter injection attempts
	addFeedback(t, database, "digest", "1", 1, "good --- stuff === more")
	addFeedback(t, database, "digest", "2", -1, "bad --- delimiters === here")

	response := `{"improved_prompt": "safe prompt", "explanation": "sanitized", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "safe prompt", result.Suggestion)

	// Verify the prompt passed to the generator has sanitized delimiters
	require.Len(t, gen.calls, 1)
	assert.NotContains(t, gen.calls[0], "---\nstuff")
}

func TestSuggest_EscapesPercentSigns(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good with 50% info")

	response := `{"improved_prompt": "prompt with %%s", "explanation": "kept placeholders", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "prompt with %%s", result.Suggestion)
}

func TestSuggest_MixedFeedback(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	// Multiple good and bad feedback entries
	for i := 0; i < 5; i++ {
		addFeedback(t, database, "digest", fmt.Sprintf("good-%d", i), 1, fmt.Sprintf("good example %d", i))
	}
	for i := 0; i < 3; i++ {
		addFeedback(t, database, "digest", fmt.Sprintf("bad-%d", i), -1, fmt.Sprintf("bad example %d", i))
	}

	response := `{"improved_prompt": "tuned prompt", "explanation": "based on 5 good and 3 bad", "changes": ["improvement"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "tuned prompt", result.Suggestion)
}

func TestSuggest_TracksPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "track", "1", 1, "good track")

	response := `{"improved_prompt": "improved tracks", "explanation": "better", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), TracksExtract)
	require.NoError(t, err)
	assert.Equal(t, TracksExtract, result.PromptID)
}

func TestSuggest_PeopleReducePrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "user_analysis", "1", -1, "bad analysis")

	response := `{"improved_prompt": "improved analysis", "explanation": "better", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), PeopleReduce)
	require.NoError(t, err)
	assert.Equal(t, PeopleReduce, result.PromptID)
}

// --- Apply tests ---

func TestApply(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: DefaultVersions[DigestChannel],
		Suggestion:     "applied prompt text",
		Explanation:    "improved based on feedback",
		Changes:        []string{"change 1"},
	}

	err := tuner.Apply(result)
	require.NoError(t, err)

	// Verify it was saved
	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "applied prompt text", tmpl)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, version)

	// Verify history has the reason
	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	require.True(t, len(history) >= 2)
	assert.Contains(t, history[0].Reason, "auto-tune")
}

func TestApply_ManualTune(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: DefaultVersions[DigestChannel],
		Suggestion:     "manually tuned",
		Explanation:    "user wanted changes",
		Changes:        []string{"change 1"},
		Manual:         true,
	}

	err := tuner.Apply(result)
	require.NoError(t, err)

	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	assert.Contains(t, history[0].Reason, "manual-tune")
}

func TestApply_VersionMismatch(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	// First update the prompt to v2
	require.NoError(t, store.Update(DigestChannel, "v2 text", "manual"))

	// Try to apply a result that was generated for v1
	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: 1,
		Suggestion:     "stale suggestion",
		Explanation:    "based on v1",
	}

	err := tuner.Apply(result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "was modified")
}

func TestApply_LongExplanationTruncated(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	longExplanation := strings.Repeat("x", 300)
	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: DefaultVersions[DigestChannel],
		Suggestion:     "new prompt",
		Explanation:    longExplanation,
	}

	err := tuner.Apply(result)
	require.NoError(t, err)

	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	// Reason should be truncated to ~200 runes
	assert.LessOrEqual(t, len([]rune(history[0].Reason)), 200)
}

func TestApply_UnknownPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	result := &TuneResult{
		PromptID:       "nonexistent.prompt",
		CurrentVersion: 0,
		Suggestion:     "text",
		Explanation:    "reason",
	}

	err := tuner.Apply(result)
	assert.Error(t, err)
}

// --- SuggestManual tests ---

func TestSuggestManual(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	response := `{"improved_prompt": "manually improved", "explanation": "user asked for changes", "changes": ["added X"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.SuggestManual(context.Background(), DigestChannel, "make it shorter")
	require.NoError(t, err)
	assert.Equal(t, DigestChannel, result.PromptID)
	assert.Equal(t, "manually improved", result.Suggestion)
	assert.True(t, result.Manual)
}

func TestSuggestManual_EmptyInstructions(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instructions cannot be empty")

	_, err = tuner.SuggestManual(context.Background(), DigestChannel, "   ")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "instructions cannot be empty")
}

func TestSuggestManual_UnknownPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), "nonexistent", "do something")
	assert.Error(t, err)
}

func TestSuggestManual_GeneratorError(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{err: fmt.Errorf("timeout")}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "do something")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI generation failed")
}

func TestSuggestManual_InvalidJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{response: "not json"}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "improve it")
	assert.Error(t, err)
}

func TestSuggestManual_EmptyResult(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{response: `{"improved_prompt": "", "explanation": "oops", "changes": []}`}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "improve it")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty improved prompt")
}

func TestSuggestManual_SanitizesInstructions(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	response := `{"improved_prompt": "safe prompt", "explanation": "done", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "instructions with --- and === delimiters and 50% numbers")
	require.NoError(t, err)

	// Check that delimiters within user instructions were sanitized
	require.Len(t, gen.calls, 1)
	prompt := gen.calls[0]
	// The user text "---" should have been replaced with "- - -"
	assert.Contains(t, prompt, "- - -")
	assert.Contains(t, prompt, "= = =")
	// Percent signs in user instructions should be escaped
	assert.Contains(t, prompt, "50%%")
}

// --- SuggestImportance tests ---

func addImportanceCorrection(t *testing.T, database *db.DB, digestID, decisionIdx int, text, original, corrected string) {
	t.Helper()
	_, err := database.AddImportanceCorrection(db.ImportanceCorrection{
		DigestID:           digestID,
		DecisionIdx:        decisionIdx,
		DecisionText:       text,
		OriginalImportance: original,
		NewImportance:      corrected,
	})
	require.NoError(t, err)
}

func TestSuggestImportance_NoCorrections(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestImportance(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no importance corrections found")
}

func TestSuggestImportance_Success(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	// Need a digest to add corrections (add dummy digest first)
	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decided to use Go", "low", "high")
	addImportanceCorrection(t, database, 1, 1, "renamed variable", "high", "low")

	response := `{"improved_prompt": "improved importance criteria", "explanation": "adjusted thresholds", "changes": ["raised bar for high"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.SuggestImportance(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DigestChannel, result.PromptID) // always uses digest.channel
	assert.Equal(t, "improved importance criteria", result.Suggestion)
}

func TestSuggestImportance_GeneratorError(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	gen := &mockGenerator{err: fmt.Errorf("api error")}
	tuner := NewTuner(store, database, gen)

	_, err = tuner.SuggestImportance(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI generation failed")
}

func TestSuggestImportance_InvalidJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	gen := &mockGenerator{response: "not json"}
	tuner := NewTuner(store, database, gen)

	_, err = tuner.SuggestImportance(context.Background())
	assert.Error(t, err)
}

func TestSuggestImportance_EmptyResult(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	gen := &mockGenerator{response: `{"improved_prompt": "", "explanation": "oops", "changes": []}`}
	tuner := NewTuner(store, database, gen)

	_, err = tuner.SuggestImportance(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty improved prompt")
}

func TestSuggestImportance_SanitizesDecisionText(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision with --- and === delimiters", "low", "high")

	response := `{"improved_prompt": "safe prompt", "explanation": "done", "changes": []}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.SuggestImportance(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "safe prompt", result.Suggestion)
}

// --- ApplyImportance tests ---

func TestApplyImportance(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: DefaultVersions[DigestChannel],
		Suggestion:     "improved importance",
		Explanation:    "tuned",
	}

	err = tuner.ApplyImportance(result)
	require.NoError(t, err)

	// Verify corrections were cleared
	has, err := database.HasImportanceCorrections()
	require.NoError(t, err)
	assert.False(t, has)

	// Verify prompt was updated
	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "improved importance", tmpl)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, version)
}

func TestApplyImportance_VersionMismatch(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	// Update the prompt first to make version mismatch
	require.NoError(t, store.Update(DigestChannel, "updated", "manual"))

	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	result := &TuneResult{
		PromptID:       DigestChannel,
		CurrentVersion: 1, // stale version
		Suggestion:     "improved importance",
		Explanation:    "tuned",
	}

	err = tuner.ApplyImportance(result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "was modified")

	// Verify corrections were NOT cleared (since apply failed)
	has, err := database.HasImportanceCorrections()
	require.NoError(t, err)
	assert.True(t, has)
}

// --- HasImportanceCorrections tests ---

func TestHasImportanceCorrections_False(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	assert.False(t, tuner.HasImportanceCorrections())
}

func TestHasImportanceCorrections_True(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	_, err := database.Exec(`INSERT INTO digests (id, channel_id, period_from, period_to, type, summary, message_count)
		VALUES (1, 'C123', 1000, 2000, 'channel', 'test', 10)`)
	require.NoError(t, err)

	addImportanceCorrection(t, database, 1, 0, "decision", "low", "high")

	gen := &mockGenerator{}
	tuner := NewTuner(store, database, gen)

	assert.True(t, tuner.HasImportanceCorrections())
}

// --- JSON parsing edge cases ---

func TestSuggest_JSONWithMarkdownFences(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	// Some LLMs wrap JSON in markdown code fences
	response := "```json\n{\"improved_prompt\": \"fenced prompt\", \"explanation\": \"reason\", \"changes\": []}\n```"
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "fenced prompt", result.Suggestion)
}

func TestSuggest_NestedJSONInPrompt(t *testing.T) {
	database, store := seedStoreWithFeedback(t)
	addFeedback(t, database, "digest", "1", 1, "good")

	// Improved prompt contains JSON example
	inner := `You should return {\"key\": \"value\"}`
	resp := map[string]any{
		"improved_prompt": inner,
		"explanation":     "updated",
		"changes":         []string{"added example"},
	}
	respBytes, _ := json.Marshal(resp)
	gen := &mockGenerator{response: string(respBytes)}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, inner, result.Suggestion)
}

// --- Full end-to-end Suggest → Apply flow ---

func TestSuggestAndApplyFlow(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	addFeedback(t, database, "digest", "1", 1, "great")
	addFeedback(t, database, "digest", "2", -1, "too long")

	response := `{"improved_prompt": "concise channel analysis %s", "explanation": "made more concise", "changes": ["shorter summaries"]}`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	// Suggest
	result, err := tuner.Suggest(context.Background(), DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, DefaultVersions[DigestChannel], result.CurrentVersion)

	// Apply
	err = tuner.Apply(result)
	require.NoError(t, err)

	// Verify
	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "concise channel analysis %s", tmpl)
	assert.Equal(t, DefaultVersions[DigestChannel]+1, version)

	// History should record both versions
	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 2)
}

// --- Suggest for each prompt type to ensure entity type mapping ---

func TestSuggest_AllPromptTypes(t *testing.T) {
	tests := []struct {
		promptID   string
		entityType string
	}{
		{DigestChannel, "digest"},
		{DigestDaily, "digest"},
		{DigestWeekly, "digest"},
		{DigestPeriod, "digest"},
		{TracksExtract, "track"},
		{TracksUpdate, "track"},
		{PeopleReduce, "user_analysis"},
		{PeopleTeam, "user_analysis"},
	}

	for _, tt := range tests {
		t.Run(tt.promptID, func(t *testing.T) {
			database, store := seedStoreWithFeedback(t)
			addFeedback(t, database, tt.entityType, "1", 1, "good")

			response := `{"improved_prompt": "improved", "explanation": "better", "changes": []}`
			gen := &mockGenerator{response: response}
			tuner := NewTuner(store, database, gen)

			result, err := tuner.Suggest(context.Background(), tt.promptID)
			require.NoError(t, err)
			assert.Equal(t, tt.promptID, result.PromptID)
		})
	}
}

// --- SuggestManual JSON parsing edge cases ---

func TestSuggestManual_JSONWithExtraText(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	response := `Here is the improved prompt:
{"improved_prompt": "better", "explanation": "improved", "changes": ["updated"]}
I hope this helps!`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	result, err := tuner.SuggestManual(context.Background(), DigestChannel, "make it better")
	require.NoError(t, err)
	assert.Equal(t, "better", result.Suggestion)
	assert.True(t, result.Manual)
}

func TestSuggestManual_BrokenJSON(t *testing.T) {
	database, store := seedStoreWithFeedback(t)

	response := `{"improved_prompt": "broken`
	gen := &mockGenerator{response: response}
	tuner := NewTuner(store, database, gen)

	_, err := tuner.SuggestManual(context.Background(), DigestChannel, "improve")
	assert.Error(t, err)
}
