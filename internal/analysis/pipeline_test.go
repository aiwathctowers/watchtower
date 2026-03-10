package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
)

type mockGenerator struct {
	response string
	err      error
	calls    atomic.Int32
}

func (m *mockGenerator) Generate(_ context.Context, _, _ string) (string, *digest.Usage, error) {
	m.calls.Add(1)
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, m.err
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
			Enabled:     true,
			Model:       "haiku",
			MinMessages: 3,
		},
	}
}

func testLogger() *log.Logger {
	return log.New(os.Stderr, "test: ", 0)
}

func seedUser(t *testing.T, database *db.DB, id, name string) {
	t.Helper()
	err := database.UpsertUser(db.User{ID: id, Name: name, DisplayName: name})
	require.NoError(t, err)
}

func seedChannel(t *testing.T, database *db.DB, id, name string) {
	t.Helper()
	err := database.UpsertChannel(db.Channel{ID: id, Name: name, Type: "public"})
	require.NoError(t, err)
}

func seedMessage(t *testing.T, database *db.DB, channelID, ts, userID, text string) {
	t.Helper()
	err := database.UpsertMessage(db.Message{
		ChannelID: channelID,
		TS:        ts,
		UserID:    userID,
		Text:      text,
	})
	require.NoError(t, err)
}

func seedThreadReply(t *testing.T, database *db.DB, channelID, ts, threadTS, userID, text string) {
	t.Helper()
	err := database.UpsertMessage(db.Message{
		ChannelID: channelID,
		TS:        ts,
		UserID:    userID,
		Text:      text,
		ThreadTS:  sql.NullString{String: threadTS, Valid: true},
	})
	require.NoError(t, err)
}

func TestParseSingleResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid JSON",
			input: `{"summary":"Active user","communication_style":"driver","decision_role":"decision-maker","red_flags":[],"highlights":["good"]}`,
		},
		{
			name:  "JSON in markdown fences",
			input: "```json\n" + `{"summary":"s","communication_style":"c","decision_role":"d","red_flags":[],"highlights":[]}` + "\n```",
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:  "with red flags",
			input: `{"summary":"going quiet","communication_style":"observer","decision_role":"observer","red_flags":["volume dropped 60%","only in DMs"],"highlights":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSingleResult(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, result.Summary)
		})
	}
}

func TestPipelineRun(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedUser(t, database, "U1", "alice")
	seedUser(t, database, "U2", "bob")
	seedChannel(t, database, "C1", "general")
	seedChannel(t, database, "C2", "random")

	now := time.Now()
	for i := range 10 {
		ts := fmt.Sprintf("%d.%06d", now.Add(-time.Duration(i)*time.Hour).Unix(), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("message %d from alice", i))
	}
	for i := range 5 {
		ts := fmt.Sprintf("%d.%06d", now.Add(-time.Duration(i)*time.Hour).Unix(), i+100)
		seedMessage(t, database, "C2", ts, "U2", fmt.Sprintf("message %d from bob", i))
	}

	mockResult := UserResult{
		Summary:            "Active user",
		CommunicationStyle: "driver",
		DecisionRole:       "decision-maker",
		RedFlags:           []string{},
		Highlights:         []string{"active"},
	}
	mockJSON, _ := json.Marshal(mockResult)

	gen := &mockGenerator{response: string(mockJSON)}
	pipe := New(database, cfg, gen, testLogger())
	pipe.Workers = 1 // sequential for deterministic test

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, int32(3), gen.calls.Load()) // one call per user + 1 period summary

	// Verify stored analyses
	analyses, err := database.GetUserAnalysesForWindow(
		float64(now.AddDate(0, 0, -7).Unix()),
		float64(now.Unix()),
	)
	require.NoError(t, err)
	assert.Len(t, analyses, 2)

	var alice *db.UserAnalysis
	for i := range analyses {
		if analyses[i].UserID == "U1" {
			alice = &analyses[i]
		}
	}
	require.NotNil(t, alice)
	assert.Equal(t, "Active user", alice.Summary)
	assert.Equal(t, "driver", alice.CommunicationStyle)
	assert.Equal(t, 10, alice.MessageCount)
}

func TestPipelineSkipsExistingWindow(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedUser(t, database, "U1", "alice")
	seedChannel(t, database, "C1", "general")

	now := time.Now()
	for i := range 5 {
		ts := fmt.Sprintf("%d.%06d", now.Add(-time.Duration(i)*time.Hour).Unix(), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	mockResult := UserResult{
		Summary:            "s",
		CommunicationStyle: "c",
		DecisionRole:       "d",
		RedFlags:           []string{},
		Highlights:         []string{},
	}
	mockJSON, _ := json.Marshal(mockResult)
	gen := &mockGenerator{response: string(mockJSON)}
	pipe := New(database, cfg, gen, testLogger())
	pipe.Workers = 1

	n1, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n1)

	n2, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
	assert.Equal(t, int32(2), gen.calls.Load()) // 1 user + 1 period summary
}

func TestPipelineDisabledConfig(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Enabled = false

	gen := &mockGenerator{}
	pipe := New(database, cfg, gen, testLogger())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, int32(0), gen.calls.Load())
}

func TestComputeUserStats(t *testing.T) {
	database := testDB(t)

	seedUser(t, database, "U1", "alice")
	seedChannel(t, database, "C1", "general")
	seedChannel(t, database, "C2", "random")

	now := time.Now()
	from := float64(now.Add(-24 * time.Hour).Unix())
	to := float64(now.Unix())

	for i := range 5 {
		ts := fmt.Sprintf("%d.%06d", now.Add(-time.Duration(i)*time.Hour).Unix(), i)
		seedMessage(t, database, "C1", ts, "U1", "hello world")
	}
	for i := range 3 {
		ts := fmt.Sprintf("%d.%06d", now.Add(-time.Duration(i)*time.Hour).Unix(), i+100)
		seedMessage(t, database, "C2", ts, "U1", "message in random channel")
	}
	parentTS := fmt.Sprintf("%d.%06d", now.Add(-2*time.Hour).Unix(), 200)
	seedMessage(t, database, "C1", parentTS, "U1", "parent message")
	seedThreadReply(t, database, "C1", fmt.Sprintf("%d.%06d", now.Add(-1*time.Hour).Unix(), 201), parentTS, "U1", "reply")

	stats, err := database.ComputeUserStats("U1", from, to)
	require.NoError(t, err)
	assert.Equal(t, "U1", stats.UserID)
	assert.True(t, stats.MessageCount >= 8, "expected >= 8 messages, got %d", stats.MessageCount)
	assert.Equal(t, 2, stats.ChannelsActive)
	assert.True(t, stats.AvgMessageLength > 0)
}
