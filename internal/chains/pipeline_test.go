package chains

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

type mockGenerator struct {
	response string
	err      error
	calls    atomic.Int32
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	m.calls.Add(1)
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, "mock-session", m.err
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func testConfig() *config.Config {
	return &config.Config{
		Digest: config.DigestConfig{
			Enabled: true,
			Model:   "haiku",
		},
	}
}

func testLogger() *log.Logger {
	return log.New(os.Stderr, "test-chains: ", 0)
}

func seedDigestWithDecisions(t *testing.T, database *db.DB, channelID string, decisions []digest.Decision) int {
	t.Helper()
	decJSON, _ := json.Marshal(decisions)
	d := db.Digest{
		ChannelID:  channelID,
		Type:       "channel",
		PeriodFrom: float64(time.Now().Add(-24 * time.Hour).Unix()),
		PeriodTo:   float64(time.Now().Unix()),
		Summary:    "test digest",
		Topics:     "[]",
		Decisions:  string(decJSON),
	}
	id, err := database.UpsertDigest(d)
	require.NoError(t, err)
	return int(id)
}

func TestRunNoUnlinkedDecisions(t *testing.T) {
	database := testDB(t)
	gen := &mockGenerator{response: "[]"}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, int32(0), gen.calls.Load(), "should not call AI when no unlinked decisions")
}

func TestRunCreatesNewChain(t *testing.T) {
	database := testDB(t)

	// Seed a channel and digest with decisions.
	require.NoError(t, database.EnsureChannel("C1", "engineering", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Migrate to PostgreSQL", By: "@alice", Importance: "high"},
	})

	gen := &mockGenerator{
		response: `[{"index": 0, "action": "NEW", "title": "PostgreSQL Migration", "slug": "postgres-migration", "summary": "Migrating the database to PostgreSQL"}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, int32(1), gen.calls.Load())

	// Verify chain was created.
	chains, err := database.GetActiveChains(14)
	require.NoError(t, err)
	require.Len(t, chains, 1)
	assert.Equal(t, "PostgreSQL Migration", chains[0].Title)
	assert.Equal(t, "postgres-migration", chains[0].Slug)
	assert.Equal(t, "active", chains[0].Status)

	// Verify chain ref was created.
	refs, err := database.GetChainRefs(chains[0].ID)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "decision", refs[0].RefType)
	assert.Equal(t, "C1", refs[0].ChannelID)
}

func TestRunLinksToExistingChain(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "engineering", "public", ""))

	// Create an existing chain.
	chainID, err := database.CreateChain(db.Chain{
		Title:      "PostgreSQL Migration",
		Slug:       "postgres-migration",
		Status:     "active",
		Summary:    "Ongoing migration",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Add(-48 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Add(-24 * time.Hour).Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Seed a new digest with a related decision.
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Chose RDS for PostgreSQL hosting", By: "@bob", Importance: "high"},
	})

	gen := &mockGenerator{
		response: `[{"index": 0, "action": "EXISTING", "chain_id": ` + itoa(int(chainID)) + `}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify ref was added to existing chain.
	refs, err := database.GetChainRefs(int(chainID))
	require.NoError(t, err)
	assert.Len(t, refs, 1)
}

func TestRunSkipsDecisions(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "random", "public", ""))

	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Order pizza on Friday", By: "@charlie", Importance: "low"},
	})

	gen := &mockGenerator{
		response: `[{"index": 0, "action": "SKIP"}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	chains, err := database.GetActiveChains(14)
	require.NoError(t, err)
	assert.Len(t, chains, 0)
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"plain JSON", `[{"index":0,"action":"NEW","title":"Test","slug":"test","summary":"s"}]`, 1},
		{"markdown fenced", "```json\n[{\"index\":0,\"action\":\"SKIP\"}]\n```", 1},
		{"multiple items", `[{"index":0,"action":"NEW","title":"A","slug":"a","summary":"s"},{"index":1,"action":"SKIP"}]`, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseResponse(tt.input)
			require.NoError(t, err)
			assert.Len(t, result, tt.want)
		})
	}
}

func TestFormatActiveChainsForPrompt(t *testing.T) {
	database := testDB(t)

	// No chains → empty string.
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())
	ctx := context.Background()
	result, err := pipe.FormatActiveChainsForPrompt(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)

	// Create a chain.
	_, err = database.CreateChain(db.Chain{
		Title:      "Test Chain",
		Slug:       "test-chain",
		Status:     "active",
		Summary:    "A test chain",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  2,
	})
	require.NoError(t, err)

	result, err = pipe.FormatActiveChainsForPrompt(ctx)
	require.NoError(t, err)
	assert.Contains(t, result, "ACTIVE CHAINS")
	assert.Contains(t, result, "Test Chain")
}

func TestMarkStaleChains(t *testing.T) {
	database := testDB(t)

	// Create a chain with old last_seen.
	_, err := database.CreateChain(db.Chain{
		Title:      "Old Chain",
		Slug:       "old-chain",
		Status:     "active",
		Summary:    "stale",
		ChannelIDs: `[]`,
		FirstSeen:  float64(time.Now().Add(-30 * 24 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Add(-30 * 24 * time.Hour).Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	cutoff := float64(time.Now().Add(-14 * 24 * time.Hour).Unix())
	n, err := database.MarkStaleChains(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	chains, err := database.GetChains(db.ChainFilter{Status: "stale"})
	require.NoError(t, err)
	assert.Len(t, chains, 1)
	assert.Equal(t, "Old Chain", chains[0].Title)
}

func TestDBChainOperations(t *testing.T) {
	database := testDB(t)

	// Create chain.
	id, err := database.CreateChain(db.Chain{
		Title:      "Test",
		Slug:       "test",
		Status:     "active",
		ChannelIDs: `["C1"]`,
		FirstSeen:  100,
		LastSeen:   200,
		ItemCount:  0,
	})
	require.NoError(t, err)
	assert.True(t, id > 0)

	// Get by ID.
	chain, err := database.GetChainByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Test", chain.Title)

	// Insert ref.
	err = database.InsertChainRef(db.ChainRef{
		ChainID:     int(id),
		RefType:     "decision",
		DigestID:    1,
		DecisionIdx: 0,
		ChannelID:   "C1",
		Timestamp:   150,
	})
	require.NoError(t, err)

	// Get refs.
	refs, err := database.GetChainRefs(int(id))
	require.NoError(t, err)
	assert.Len(t, refs, 1)

	// Item count.
	count, err := database.GetChainItemCount(int(id))
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// IsDecisionChained.
	chainID := database.IsDecisionChained(1, 0)
	assert.Equal(t, int(id), chainID)

	// Not chained.
	assert.Equal(t, 0, database.IsDecisionChained(999, 0))

	// Add channel.
	err = database.AddChannelToChain(int(id), "C2")
	require.NoError(t, err)
	chain, _ = database.GetChainByID(int(id))
	assert.Contains(t, chain.ChannelIDs, "C2")

	// Add same channel — no duplicate.
	err = database.AddChannelToChain(int(id), "C2")
	require.NoError(t, err)

	// Update status.
	err = database.UpdateChainStatus(int(id), "resolved")
	require.NoError(t, err)
	chain, _ = database.GetChainByID(int(id))
	assert.Equal(t, "resolved", chain.Status)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// --- Additional tests to improve coverage ---

func TestSetPromptStore(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	assert.Nil(t, pipe.promptStore)

	store := &prompts.Store{}
	pipe.SetPromptStore(store)
	assert.Equal(t, store, pipe.promptStore)
}

func TestRunAIError(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Use Redis for caching", By: "@dave", Importance: "medium"},
	})

	gen := &mockGenerator{
		err: fmt.Errorf("AI service unavailable"),
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI chain linking")
	assert.Equal(t, 0, n)
}

func TestRunContextCancelled(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Deploy to prod", By: "@eve", Importance: "high"},
		{Text: "Switch to gRPC", By: "@frank", Importance: "high"},
	})

	// Return two NEW assignments; cancel context before processing.
	gen := &mockGenerator{
		response: `[
			{"index": 0, "action": "NEW", "title": "Prod Deploy", "slug": "prod-deploy", "summary": "Deploy to production"},
			{"index": 1, "action": "NEW", "title": "gRPC Migration", "slug": "grpc-migration", "summary": "Move to gRPC"}
		]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately — but AI call already returned (mock), so cancellation checked in loop.

	n, err := pipe.Run(ctx)
	// Either 0 linked (cancelled before first iteration) or context.Canceled returned.
	assert.True(t, err == nil || err == context.Canceled, "expected nil or context.Canceled, got: %v", err)
	_ = n
}

func TestRunOutOfBoundsDecisionIndex(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Only one decision", By: "@grace", Importance: "low"},
	})

	gen := &mockGenerator{
		response: `[
			{"index": 5, "action": "NEW", "title": "Bad Index", "slug": "bad-idx", "summary": "Out of range"},
			{"index": -1, "action": "NEW", "title": "Negative", "slug": "neg", "summary": "Negative index"}
		]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "out-of-bounds indices should be skipped")

	chains, err := database.GetActiveChains(14)
	require.NoError(t, err)
	assert.Len(t, chains, 0, "no chains should be created for bad indices")
}

func TestRunUnknownChainID(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Some decision", By: "@heidi", Importance: "medium"},
	})

	// AI returns EXISTING with a chain_id that does not exist.
	gen := &mockGenerator{
		response: `[{"index": 0, "action": "EXISTING", "chain_id": 9999}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "unknown chain_id should be skipped")
}

func TestRunStaleMarkingWithNoDecisions(t *testing.T) {
	database := testDB(t)

	// Create a stale chain (old last_seen).
	_, err := database.CreateChain(db.Chain{
		Title:      "Very Old Topic",
		Slug:       "old-topic",
		Status:     "active",
		Summary:    "Ancient discussion",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Add(-60 * 24 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Add(-30 * 24 * time.Hour).Unix()),
		ItemCount:  3,
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: "[]"}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// The chain should now be stale.
	chains, err := database.GetChains(db.ChainFilter{Status: "stale"})
	require.NoError(t, err)
	assert.Len(t, chains, 1)
	assert.Equal(t, "Very Old Topic", chains[0].Title)
}

func TestRunLinksToExistingChainUpdatesMetadata(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "engineering", "public", ""))
	require.NoError(t, database.EnsureChannel("C2", "backend", "public", ""))

	// Create an existing chain with C1 — use recent LastSeen so it's active.
	chainID, err := database.CreateChain(db.Chain{
		Title:      "API Redesign",
		Slug:       "api-redesign",
		Status:     "active",
		Summary:    "Redesigning the API",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Add(-72 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Seed a digest in C2 with a related decision.
	seedDigestWithDecisions(t, database, "C2", []digest.Decision{
		{Text: "API v3 will use REST+GraphQL", By: "@ivan", Importance: "high"},
	})

	gen := &mockGenerator{
		response: `[{"index": 0, "action": "EXISTING", "chain_id": ` + itoa(int(chainID)) + `}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify ref was inserted.
	refs, err := database.GetChainRefs(int(chainID))
	require.NoError(t, err)
	assert.Len(t, refs, 1)
	assert.Equal(t, "C2", refs[0].ChannelID)
}

func TestRunMultipleDecisionsMixed(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	require.NoError(t, database.EnsureChannel("C2", "design", "public", ""))

	// Create an existing chain.
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Design System",
		Slug:       "design-system",
		Status:     "active",
		Summary:    "Building a design system",
		ChannelIDs: `["C2"]`,
		FirstSeen:  float64(time.Now().Add(-72 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  2,
	})
	require.NoError(t, err)

	// Seed digests with multiple decisions.
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Switch to TypeScript", By: "@jack", Importance: "high"},
		{Text: "Adopt Figma tokens", By: "@kate", Importance: "medium"},
		{Text: "Office plants", By: "@larry", Importance: "low"},
	})

	gen := &mockGenerator{
		response: `[
			{"index": 0, "action": "NEW", "title": "TypeScript Adoption", "slug": "ts-adopt", "summary": "Moving to TypeScript"},
			{"index": 1, "action": "EXISTING", "chain_id": ` + itoa(int(chainID)) + `},
			{"index": 2, "action": "SKIP"}
		]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n) // NEW + EXISTING linked, SKIP not counted.

	chains, err := database.GetActiveChains(14)
	require.NoError(t, err)
	assert.Len(t, chains, 2) // Design System + TypeScript Adoption
}

func TestParseResponseInvalidJSON(t *testing.T) {
	_, err := parseResponse("this is not json at all")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing chain assignments JSON")
}

func TestParseResponseEmpty(t *testing.T) {
	result, err := parseResponse("[]")
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestParseResponseWithPrefixText(t *testing.T) {
	resp := `Here are the assignments:
[{"index": 0, "action": "SKIP"}]
Some trailing text`
	result, err := parseResponse(resp)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "SKIP", result[0].Action)
}

func TestMarshalStringSlice(t *testing.T) {
	assert.Equal(t, `["a","b"]`, marshalStringSlice([]string{"a", "b"}))
	assert.Equal(t, `[]`, marshalStringSlice([]string{}))
	assert.Equal(t, `["single"]`, marshalStringSlice([]string{"single"}))
}

func TestBuildPromptWithChains(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	chains := []db.Chain{
		{
			ID:         1,
			Title:      "Infra Overhaul",
			Slug:       "infra-overhaul",
			Summary:    "Overhauling infrastructure",
			ChannelIDs: `["C1","C2"]`,
			ItemCount:  3,
		},
	}
	unlinked := []db.UnlinkedDecision{
		{
			DigestID:     10,
			DecisionIdx:  0,
			ChannelID:    "C1",
			ChannelName:  "engineering",
			DecisionText: "Switch to Kubernetes",
			DecisionBy:   "@mike",
			Importance:   "high",
		},
	}

	prompt := pipe.buildPrompt(chains, unlinked, nil)
	assert.Contains(t, prompt, "ACTIVE CHAINS")
	assert.Contains(t, prompt, "Infra Overhaul")
	assert.Contains(t, prompt, "infra-overhaul")
	assert.Contains(t, prompt, "C1, C2")
	assert.Contains(t, prompt, "UNLINKED DECISIONS")
	assert.Contains(t, prompt, "Switch to Kubernetes")
	assert.Contains(t, prompt, "@mike")
	assert.Contains(t, prompt, "#engineering")
}

func TestBuildPromptNoChains(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	unlinked := []db.UnlinkedDecision{
		{
			DigestID:     1,
			DecisionIdx:  0,
			ChannelID:    "C1",
			ChannelName:  "",
			DecisionText: "Use Terraform",
			DecisionBy:   "@nancy",
			Importance:   "medium",
		},
	}

	prompt := pipe.buildPrompt(nil, unlinked, nil)
	assert.Contains(t, prompt, "No active chains yet")
	assert.Contains(t, prompt, "Use Terraform")
	// When ChannelName is empty, fallback to ChannelID.
	assert.Contains(t, prompt, "#C1")
}

func TestFormatChainedDecisionsForRollupNoChains(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	chained, standalone, err := pipe.FormatChainedDecisionsForRollup(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, chained)
	assert.Empty(t, standalone)
}

func TestFormatChainedDecisionsForRollupWithChains(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))

	// Create a chain and link a decision to it.
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Migration Project",
		Slug:       "migration",
		Status:     "active",
		Summary:    "DB migration",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  2,
	})
	require.NoError(t, err)

	digestID := seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Migrate users table", By: "@alice", Importance: "high"},
		{Text: "Standalone thing", By: "@bob", Importance: "low"},
	})

	// Link first decision to chain.
	require.NoError(t, database.InsertChainRef(db.ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    digestID,
		DecisionIdx: 0,
		ChannelID:   "C1",
		Timestamp:   float64(time.Now().Unix()),
	}))

	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	allDecisions := []rollupDecision{
		{DigestID: digestID, DecisionIdx: 0, ChannelName: "eng", Text: "Migrate users table", Importance: "high"},
		{DigestID: digestID, DecisionIdx: 1, ChannelName: "eng", Text: "Standalone thing", Importance: "low"},
	}

	chained, standalone, err := pipe.FormatChainedDecisionsForRollup(nil, allDecisions)
	require.NoError(t, err)

	assert.Contains(t, chained, "CHAIN UPDATES")
	assert.Contains(t, chained, "Migration Project")
	assert.Contains(t, chained, "Migrate users table")

	assert.Contains(t, standalone, "STANDALONE DECISIONS")
	assert.Contains(t, standalone, "Standalone thing")
}

func TestFormatChainedDecisionsForRollupAllChained(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))

	chainID, err := database.CreateChain(db.Chain{
		Title:      "Auth Rewrite",
		Slug:       "auth-rewrite",
		Status:     "active",
		Summary:    "Rewriting auth",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	digestID := seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Use OAuth2", By: "@carol", Importance: "high"},
	})

	require.NoError(t, database.InsertChainRef(db.ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    digestID,
		DecisionIdx: 0,
		ChannelID:   "C1",
		Timestamp:   float64(time.Now().Unix()),
	}))

	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	allDecisions := []rollupDecision{
		{DigestID: digestID, DecisionIdx: 0, ChannelName: "eng", Text: "Use OAuth2", Importance: "high"},
	}

	chained, standalone, err := pipe.FormatChainedDecisionsForRollup(nil, allDecisions)
	require.NoError(t, err)

	assert.Contains(t, chained, "Auth Rewrite")
	assert.Empty(t, standalone, "no standalone decisions expected")
}

func TestFormatChainedDecisionsForRollupAllStandalone(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))

	// Create a chain but don't link any of the rollup decisions to it.
	_, err := database.CreateChain(db.Chain{
		Title:      "Unrelated Chain",
		Slug:       "unrelated",
		Status:     "active",
		Summary:    "Something else",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	allDecisions := []rollupDecision{
		{DigestID: 999, DecisionIdx: 0, ChannelName: "eng", Text: "Random decision", Importance: "low"},
	}

	chained, standalone, err := pipe.FormatChainedDecisionsForRollup(nil, allDecisions)
	require.NoError(t, err)

	assert.Empty(t, chained, "no chained decisions expected")
	assert.Contains(t, standalone, "STANDALONE DECISIONS")
	assert.Contains(t, standalone, "Random decision")
}

func TestFormatActiveChainsForPromptMultipleChains(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, testLogger())

	now := float64(time.Now().Unix())
	for _, c := range []db.Chain{
		{Title: "Chain Alpha", Slug: "alpha", Status: "active", Summary: "Alpha summary", ChannelIDs: `["C1"]`, FirstSeen: now, LastSeen: now, ItemCount: 1},
		{Title: "Chain Beta", Slug: "beta", Status: "active", Summary: "Beta summary", ChannelIDs: `["C2"]`, FirstSeen: now, LastSeen: now, ItemCount: 3},
	} {
		_, err := database.CreateChain(c)
		require.NoError(t, err)
	}

	result, err := pipe.FormatActiveChainsForPrompt(context.Background())
	require.NoError(t, err)
	assert.Contains(t, result, "Chain Alpha")
	assert.Contains(t, result, "Chain Beta")
	assert.Contains(t, result, "Alpha summary")
	assert.Contains(t, result, "Beta summary")
}

func TestUpdateChainSummariesTriggered(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))

	// Create an existing chain with a pre-existing ref.
	chainID, err := database.CreateChain(db.Chain{
		Title:      "Existing Chain",
		Slug:       "existing",
		Status:     "active",
		Summary:    "Original summary",
		ChannelIDs: `["C1"]`,
		FirstSeen:  float64(time.Now().Add(-72 * time.Hour).Unix()),
		LastSeen:   float64(time.Now().Unix()),
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Add a pre-existing ref (from a previous run).
	require.NoError(t, database.InsertChainRef(db.ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    0, // dummy, won't conflict with real digests
		DecisionIdx: 99,
		ChannelID:   "C1",
		Timestamp:   float64(time.Now().Add(-24 * time.Hour).Unix()),
	}))

	// Seed a new unlinked decision.
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Redis sentinel for HA", By: "@emma", Importance: "medium"},
	})

	// AI links the decision to the existing chain — triggers updateChainSummaries.
	gen := &mockGenerator{
		response: `[{"index": 0, "action": "EXISTING", "chain_id": ` + itoa(int(chainID)) + `}]`,
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify updateChainSummaries updated the item count.
	chain, err := database.GetChainByID(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, 2, chain.ItemCount) // 1 pre-existing + 1 new
}

func TestRunParseResponseError(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.EnsureChannel("C1", "eng", "public", ""))
	seedDigestWithDecisions(t, database, "C1", []digest.Decision{
		{Text: "Something", By: "@user", Importance: "low"},
	})

	gen := &mockGenerator{
		response: "This is not JSON at all, no brackets",
	}
	pipe := New(database, testConfig(), gen, testLogger())

	n, err := pipe.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing chain response")
	assert.Equal(t, 0, n)
}
