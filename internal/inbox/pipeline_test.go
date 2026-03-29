package inbox

import (
	"context"
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

// recentTS returns a Slack-style timestamp string relative to now.
func recentTS(minutesAgo int) string {
	t := time.Now().Add(-time.Duration(minutesAgo) * time.Minute)
	return fmt.Sprintf("%d.000100", t.Unix())
}

type mockGenerator struct {
	response string
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, "mock-session", nil
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
		Inbox: config.InboxConfig{
			Enabled:             true,
			MaxItemsPerRun:      100,
			InitialLookbackDays: 7,
		},
	}
}

// seedWorkspaceAndUser inserts a workspace and sets the current user.
func seedWorkspaceAndUser(t *testing.T, database *db.DB, userID string) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO workspace (id, name, current_user_id) VALUES ('T1', 'Test', ?)`, userID)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO users (id, name) VALUES (?, 'testuser')`, userID)
	require.NoError(t, err)
}

func TestPipeline_Run_NoUser(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	p := New(database, cfg, nil, log.Default())

	created, resolved, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created)
	assert.Equal(t, 0, resolved)
}

func TestPipeline_Run_DetectMentions(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30) // 30 minutes ago
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text, permalink) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> review please', 'https://slack.com/p1')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "mention", items[0].TriggerType)
	assert.Equal(t, "pending", items[0].Status)
}

func TestPipeline_Run_DetectDMs(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type, dm_user_id) VALUES ('D1', 'dm-other', 'dm', 'U_OTHER')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('D1', ?, 'U_OTHER', 'Hey, got a minute?')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "dm", items[0].TriggerType)
}

func TestPipeline_Run_AutoResolveWithoutAI(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts1 := recentTS(30)
	ts2 := recentTS(20) // reply 10 minutes later
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> check this')`, ts1)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_ME', 'Done!')`, ts2)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())
	created, resolved, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)
	assert.Equal(t, 1, resolved)

	items, err := database.GetInboxItems(db.InboxFilter{IncludeResolved: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "resolved", items[0].Status)
}

func TestPipeline_Run_NoDuplicates(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> check this')`, ts)
	require.NoError(t, err)

	p := New(database, cfg, nil, log.Default())

	created1, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created1)

	// Second run — should not create duplicates (FindPendingMentions has NOT EXISTS)
	created2, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created2)
}

func TestPipeline_Run_WithAI(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	ts := recentTS(30)
	_, err := database.Exec(`INSERT INTO channels (id, name, type) VALUES ('C1', 'general', 'public')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', ?, 'U_OTHER', 'Hey <@U_ME> urgent blocker')`, ts)
	require.NoError(t, err)

	gen := &mockGenerator{
		response: `{"items": [{"id": 1, "priority": "high", "reason": "Production blocker from team lead", "resolved": false}]}`,
	}

	p := New(database, cfg, gen, log.Default())
	created, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	items, err := database.GetInboxItems(db.InboxFilter{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "high", items[0].Priority)
	assert.Equal(t, "Production blocker from team lead", items[0].AIReason)
}

func TestParseAIResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{
			name:    "valid JSON",
			input:   `{"items": [{"id": 1, "priority": "high", "reason": "urgent", "resolved": false}]}`,
			wantLen: 1,
		},
		{
			name:    "with markdown fences",
			input:   "```json\n{\"items\": [{\"id\": 1, \"priority\": \"high\", \"reason\": \"urgent\", \"resolved\": false}]}\n```",
			wantLen: 1,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAIResult(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, result.Items, tt.wantLen)
		})
	}
}

func TestPipeline_LastProcessedTS(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedWorkspaceAndUser(t, database, "U_ME")

	p := New(database, cfg, nil, log.Default())
	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	ts, err := database.GetInboxLastProcessedTS()
	require.NoError(t, err)
	assert.Greater(t, ts, float64(0))
}
