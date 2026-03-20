package ai

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// setupIntegrationDB creates an in-memory DB populated with realistic workspace
// data suitable for AI pipeline integration tests.
func setupIntegrationDB(t *testing.T) (*db.DB, time.Time) {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Reference time: 2025-02-26 14:00:00 UTC
	refTime := time.Date(2025, 2, 26, 14, 0, 0, 0, time.UTC)

	// Workspace
	err = database.UpsertWorkspace(db.Workspace{
		ID:     "T024BE7LD",
		Name:   "my-company",
		Domain: "my-company",
	})
	require.NoError(t, err)

	// Users
	users := []db.User{
		{ID: "U001", Name: "alice", DisplayName: "Alice Smith", RealName: "Alice Smith", Email: "alice@company.com"},
		{ID: "U002", Name: "bob", DisplayName: "Bob Jones", RealName: "Bob Jones", Email: "bob@company.com"},
		{ID: "U003", Name: "carol", DisplayName: "Carol White", RealName: "Carol White", Email: "carol@company.com"},
	}
	for _, u := range users {
		require.NoError(t, database.UpsertUser(u))
	}

	// Channels
	channels := []db.Channel{
		{ID: "C001", Name: "general", Type: "public", NumMembers: 50, IsMember: true, Topic: "General chat"},
		{ID: "C002", Name: "engineering", Type: "public", NumMembers: 20, IsMember: true, Topic: "Engineering discussion"},
		{ID: "C003", Name: "design", Type: "public", NumMembers: 15, IsMember: true},
	}
	for _, ch := range channels {
		require.NoError(t, database.UpsertChannel(ch))
	}

	// Messages in #general (C001) — recent deployment discussion
	generalMsgs := []struct {
		ts     float64
		userID string
		text   string
		reply  int
	}{
		{float64(refTime.Add(-2 * time.Hour).Unix()), "U001", "We're deploying v2.3 to production today. Heads up everyone.", 2},
		{float64(refTime.Add(-90 * time.Minute).Unix()), "U002", "Monitoring dashboards now, everything looks stable.", 0},
		{float64(refTime.Add(-60 * time.Minute).Unix()), "U001", "Deployment successful! No errors in logs.", 0},
		{float64(refTime.Add(-30 * time.Minute).Unix()), "U003", "Great work team! The new features are looking good.", 0},
	}
	for _, m := range generalMsgs {
		ts := fmt.Sprintf("%.6f", m.ts)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID:  "C001",
			TS:         ts,
			UserID:     m.userID,
			Text:       m.text,
			ReplyCount: m.reply,
			TSUnix:     m.ts,
		}))
	}

	// Thread replies for the deployment message
	parentTS := fmt.Sprintf("%.6f", float64(refTime.Add(-2*time.Hour).Unix()))
	reply1TS := fmt.Sprintf("%.6f", float64(refTime.Add(-110*time.Minute).Unix()))
	reply2TS := fmt.Sprintf("%.6f", float64(refTime.Add(-100*time.Minute).Unix()))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        reply1TS,
		UserID:    "U002",
		Text:      "I'll keep an eye on the metrics during rollout",
		ThreadTS:  sql.NullString{String: parentTS, Valid: true},
		TSUnix:    float64(refTime.Add(-110 * time.Minute).Unix()),
	}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        reply2TS,
		UserID:    "U003",
		Text:      "No breaking changes expected for my service",
		ThreadTS:  sql.NullString{String: parentTS, Valid: true},
		TSUnix:    float64(refTime.Add(-100 * time.Minute).Unix()),
	}))

	// Messages in #engineering (C002)
	engMsgs := []struct {
		ts     float64
		userID string
		text   string
	}{
		{float64(refTime.Add(-3 * time.Hour).Unix()), "U002", "CI pipeline optimization reduced build time by 40%"},
		{float64(refTime.Add(-150 * time.Minute).Unix()), "U001", "Nice! What changes did you make?"},
		{float64(refTime.Add(-120 * time.Minute).Unix()), "U002", "Switched to parallel test execution and better caching strategy"},
	}
	for _, m := range engMsgs {
		ts := fmt.Sprintf("%.6f", m.ts)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID: "C002",
			TS:        ts,
			UserID:    m.userID,
			Text:      m.text,
			TSUnix:    m.ts,
		}))
	}

	// Messages in #design (C003)
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C003",
		TS:        fmt.Sprintf("%.6f", float64(refTime.Add(-4*time.Hour).Unix())),
		UserID:    "U003",
		Text:      "New mockups for the settings page are ready for review",
		TSUnix:    float64(refTime.Add(-4 * time.Hour).Unix()),
	}))

	return database, refTime
}

// TestIntegrationAIQueryPipeline verifies the full AI query pipeline:
// parse query -> build context -> verify context contains correct messages.
func TestIntegrationAIQueryPipeline(t *testing.T) {
	database, refTime := setupIntegrationDB(t)

	t.Run("channel query includes correct messages", func(t *testing.T) {
		query := Parse("summarize #general")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		assert.Equal(t, IntentChannel, query.Intent)
		assert.Contains(t, query.Channels, "general")

		cb := NewContextBuilder(database, 150000, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		assert.Contains(t, ctx, "deploying v2.3")
		assert.Contains(t, ctx, "Deployment successful")
		assert.Contains(t, ctx, "#general")
		assert.Contains(t, ctx, "Workspace Summary")
		assert.Contains(t, ctx, "my-company")
	})

	t.Run("user query includes correct messages", func(t *testing.T) {
		query := Parse("what did @alice say")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		assert.Equal(t, IntentPerson, query.Intent)
		assert.Contains(t, query.Users, "alice")

		cb := NewContextBuilder(database, 150000, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		assert.Contains(t, ctx, "@alice")
		assert.Contains(t, ctx, "deploying v2.3")
	})

	t.Run("search query finds FTS5 matches", func(t *testing.T) {
		query := Parse("find messages about deployment")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		assert.Equal(t, IntentSearch, query.Intent)
		assert.Contains(t, query.Topics, "deployment")

		cb := NewContextBuilder(database, 150000, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		assert.Contains(t, ctx, "Search Results")
	})

	t.Run("catchup query with watch list includes priority context", func(t *testing.T) {
		require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
		t.Cleanup(func() { database.RemoveWatch("channel", "C001") })

		query := Parse("what happened")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		assert.Equal(t, IntentCatchup, query.Intent)

		cb := NewContextBuilder(database, 150000, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		assert.Contains(t, ctx, "Priority Context")
		assert.Contains(t, ctx, "#general")
		assert.Contains(t, ctx, "general [high]")
	})

	t.Run("context builder respects token budget", func(t *testing.T) {
		query := Parse("summarize everything")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		cb := NewContextBuilder(database, 500, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		tokens := estimateTokens(ctx)
		assert.Less(t, tokens, 1000, "token budget should be approximately respected")
	})

	t.Run("broad context shows activity overview", func(t *testing.T) {
		query := Parse("what's going on")
		query.TimeRange = &TimeRange{
			From: refTime.Add(-4 * time.Hour),
			To:   refTime,
		}

		cb := NewContextBuilder(database, 150000, "my-company", "T001")
		ctx, err := cb.Build(query)
		require.NoError(t, err)

		assert.Contains(t, ctx, "Activity Overview")
		assert.Contains(t, ctx, "#general")
	})
}

// TestIntegrationAIPromptAssembly verifies the system prompt and user message
// assembly produce well-formed prompts with data from the context builder.
func TestIntegrationAIPromptAssembly(t *testing.T) {
	_, refTime := setupIntegrationDB(t)

	query := Parse("summarize #general")
	query.TimeRange = &TimeRange{
		From: refTime.Add(-4 * time.Hour),
		To:   refTime,
	}

	systemPrompt := BuildSystemPrompt("my-company", "my-company", "T001", "/tmp/test.db", db.Schema)
	assert.Contains(t, systemPrompt, "Watchtower")
	assert.Contains(t, systemPrompt, "my-company")
	assert.Contains(t, systemPrompt, "sqlite3")
	assert.Contains(t, systemPrompt, "CREATE TABLE")

	timeHints := FormatTimeHints(query)
	userMessage := AssembleUserMessage("summarize #general", timeHints)
	assert.Contains(t, userMessage, "summarize #general")
	assert.Contains(t, userMessage, "ts_unix BETWEEN")
}

// TestIntegrationResponseRenderer verifies the response renderer can detect
// message references and resolve them to Slack permalinks.
func TestIntegrationResponseRenderer(t *testing.T) {
	database, refTime := setupIntegrationDB(t)

	renderer := NewResponseRenderer(database, "my-company", "T001")

	// Format a timestamp that matches a message in the test DB
	msgTime := refTime.Add(-2 * time.Hour).UTC()
	timeStr := msgTime.Format("2006-01-02 15:04")

	response := fmt.Sprintf("Alice deployed v2.3 at #general %s", timeStr)
	rendered, err := renderer.Render(response)
	require.NoError(t, err)

	// The renderer should resolve the reference to a Slack permalink
	assert.Contains(t, rendered, "slack://channel?team=T001")
	assert.Contains(t, rendered, "Sources")
}

// TestIntegrationEndToEnd verifies the complete pipeline:
// pre-populated DB -> parse query -> build context -> call Claude (mocked) ->
// render response -> verify output references correct data.
func TestIntegrationEndToEnd(t *testing.T) {
	database, refTime := setupIntegrationDB(t)

	// Step 1: Verify the DB is populated (simulating post-sync state)
	ws, err := database.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "my-company", ws.Name)

	users, err := database.GetUsers(db.UserFilter{})
	require.NoError(t, err)
	assert.Len(t, users, 3)

	channels, err := database.GetChannels(db.ChannelFilter{})
	require.NoError(t, err)
	assert.Len(t, channels, 3)

	// Step 2: Parse a question and build workspace summary
	question := "what happened in #general today"
	query := Parse(question)
	query.TimeRange = &TimeRange{
		From: refTime.Add(-4 * time.Hour),
		To:   refTime,
	}

	assert.Contains(t, query.Channels, "general")

	// Step 3: Build prompts (DB path + schema in system prompt, no pre-loaded context)
	systemPrompt := BuildSystemPrompt("my-company", "my-company", "T001", "/tmp/test.db", db.Schema)
	timeHints := FormatTimeHints(query)
	userMessage := AssembleUserMessage(question, timeHints)

	assert.Contains(t, systemPrompt, "Watchtower")
	assert.Contains(t, systemPrompt, "sqlite3")
	assert.Contains(t, userMessage, question)

	// Step 4: Mock Claude CLI and send the query
	mockResponseText := "Here's what happened in #general:\n\nAlice deployed v2.3 to production. Bob monitored the dashboards and confirmed everything was stable. Carol praised the team."

	mockPath := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\n", strings.ReplaceAll(mockResponseText, "'", "'\\''"))
	require.NoError(t, os.WriteFile(mockPath, []byte(script), 0o755))

	client := NewClient("claude-sonnet-4-6", "", "")
	client.claudeCmd = mockPath

	response, err := client.QuerySync(context.Background(), systemPrompt, userMessage, "")
	require.NoError(t, err)
	assert.Contains(t, response, "v2.3")
	assert.Contains(t, response, "Alice")
	assert.Contains(t, response, "Bob")

	// Step 5: Render the response
	renderer := NewResponseRenderer(database, "my-company", "T001")
	rendered, err := renderer.Render(response)
	require.NoError(t, err)
	assert.NotEmpty(t, rendered)
	assert.True(t, strings.Contains(rendered, "v2.3") || strings.Contains(rendered, "Alice"),
		"rendered output should contain key information from the response")
}

// TestIntegrationEndToEndStreaming verifies the streaming variant of the
// end-to-end pipeline works correctly.
func TestIntegrationEndToEndStreaming(t *testing.T) {
	_, refTime := setupIntegrationDB(t)

	question := "summarize #engineering"
	query := Parse(question)
	query.TimeRange = &TimeRange{
		From: refTime.Add(-4 * time.Hour),
		To:   refTime,
	}

	systemPrompt := BuildSystemPrompt("my-company", "my-company", "T001", "/tmp/test.db", db.Schema)
	timeHints := FormatTimeHints(query)
	userMessage := AssembleUserMessage(question, timeHints)

	mockResponseText := "Bob optimized the CI pipeline, reducing build time by 40% through parallel test execution and better caching."

	mockPath := filepath.Join(t.TempDir(), "claude")
	escapedText := strings.ReplaceAll(mockResponseText, `"`, `\"`)
	script := fmt.Sprintf("#!/bin/sh\necho '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"%s\"}]}}'\necho '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"%s\"}'\n",
		escapedText, escapedText)
	require.NoError(t, os.WriteFile(mockPath, []byte(script), 0o755))

	client := NewClient("claude-sonnet-4-6", "", "")
	client.claudeCmd = mockPath

	textCh, errCh, _ := client.Query(context.Background(), systemPrompt, userMessage, "")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, mockResponseText, result.String())
}
