package guide

import (
	"context"
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

func seedDigestWithSignals(t *testing.T, database *db.DB, channelID string, from, to float64, signals string) {
	t.Helper()
	seedDigestFull(t, database, channelID, from, to, signals, "[]")
}

func seedDigestWithSituations(t *testing.T, database *db.DB, channelID string, from, to float64, signals, situations string) {
	t.Helper()
	seedDigestFull(t, database, channelID, from, to, signals, situations)
}

func seedDigestFull(t *testing.T, database *db.DB, channelID string, from, to float64, signals, situations string) {
	t.Helper()
	_, err := database.UpsertDigest(db.Digest{
		ChannelID:     channelID,
		Type:          "channel",
		PeriodFrom:    from,
		PeriodTo:      to,
		Summary:       "test digest",
		Topics:        "[]",
		Decisions:     "[]",
		ActionItems:   "[]",
		PeopleSignals: signals,
		Situations:    situations,
		MessageCount:  10,
		Model:         "haiku",
	})
	require.NoError(t, err)
}

func TestPipeline_Run(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	logger := testLogger()

	err := database.UpsertWorkspace(db.Workspace{ID: "W1", Name: "test"})
	require.NoError(t, err)

	seedUser(t, database, "U1", "alice")
	seedChannel(t, database, "C1", "general")

	from := float64(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	to := float64(time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC).Unix())
	base := from + 86400
	for i := range 5 {
		ts := base + float64(i*60)
		tsStr := fmt.Sprintf("%.6f", ts)
		seedMessage(t, database, "C1", tsStr, "U1", "test message "+string(rune('a'+i)))
	}

	// Seed channel digest with situations for U1
	seedDigestWithSituations(t, database, "C1", from, to,
		`[]`,
		`[{"topic":"Auth refactor","type":"collaboration","participants":[{"user_id":"U1","role":"lead"}],"dynamic":"Led auth refactor with team","outcome":"Refactor completed","red_flags":[],"observations":["proactive"],"message_refs":[]},{"topic":"Deployment docs","type":"knowledge_transfer","participants":[{"user_id":"U1","role":"author"}],"dynamic":"Shared deployment docs","outcome":"Docs published","red_flags":[],"observations":[],"message_refs":[]},{"topic":"API versioning","type":"decision_deadlock","participants":[{"user_id":"U1","role":"mediator"}],"dynamic":"Resolved API versioning debate","outcome":"Agreement reached","red_flags":[],"observations":[],"message_refs":[]}]`)

	mockResp := `{
		"summary": "Alice is a proactive contributor",
		"communication_style": "driver",
		"decision_role": "decision-maker",
		"red_flags": [],
		"highlights": ["Led auth refactor"],
		"accomplishments": ["Shipped auth module"],
		"communication_guide": "Be direct, send structured asks",
		"decision_style": "Quick decisions with data backing",
		"tactics": ["If blocked, send data-backed proposal"]
	}`

	gen := &mockGenerator{response: mockResp}
	pipe := New(database, cfg, gen, logger)
	pipe.ForceRegenerate = true
	cfg.AI.Workers = 1

	n, err := pipe.RunForWindow(context.Background(), from, to)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.GreaterOrEqual(t, int(gen.calls.Load()), 1)

	// Verify stored people card
	card, err := database.GetLatestPeopleCard("U1")
	require.NoError(t, err)
	require.NotNil(t, card)
	assert.Equal(t, "Alice is a proactive contributor", card.Summary)
	assert.Equal(t, "driver", card.CommunicationStyle)
	assert.Equal(t, "decision-maker", card.DecisionRole)
	assert.Equal(t, "Be direct, send structured asks", card.CommunicationGuide)
	assert.Equal(t, 5, card.MessageCount)

	var tactics []string
	err = json.Unmarshal([]byte(card.Tactics), &tactics)
	require.NoError(t, err)
	assert.Equal(t, []string{"If blocked, send data-backed proposal"}, tactics)
}

func TestPipeline_SkipsExistingWindow(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	logger := testLogger()

	err := database.UpsertWorkspace(db.Workspace{ID: "W1", Name: "test"})
	require.NoError(t, err)

	seedUser(t, database, "U1", "bob")
	seedChannel(t, database, "C1", "general")

	from := float64(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	to := float64(time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC).Unix())
	base := from + 86400
	for i := range 5 {
		ts := base + float64(i*60)
		tsStr := fmt.Sprintf("%.6f", ts)
		seedMessage(t, database, "C1", tsStr, "U1", "message "+string(rune('a'+i)))
	}

	gen := &mockGenerator{response: `{"summary":"test","communication_style":"","decision_role":"","red_flags":[],"highlights":[],"accomplishments":[],"communication_guide":"","decision_style":"","tactics":[]}`}
	pipe := New(database, cfg, gen, logger)
	pipe.ForceRegenerate = true
	cfg.AI.Workers = 1

	n1, err := pipe.RunForWindow(context.Background(), from, to)
	require.NoError(t, err)
	assert.Equal(t, 1, n1)

	pipe2 := New(database, cfg, gen, logger)
	n2, err := pipe2.RunForWindow(context.Background(), from, to)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}

func TestPipeline_NoUsers(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	logger := testLogger()

	gen := &mockGenerator{response: "{}"}
	pipe := New(database, cfg, gen, logger)

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, int32(0), gen.calls.Load())
}

func TestParsePeopleCardResult(t *testing.T) {
	raw := `{
		"summary": "Good communicator",
		"communication_style": "collaborator",
		"decision_role": "contributor",
		"red_flags": ["Missed deadline"],
		"highlights": ["Led deployment"],
		"accomplishments": ["Shipped v2"],
		"communication_guide": "Be concise",
		"decision_style": "Data-driven",
		"tactics": ["If blocked, escalate"]
	}`
	result, err := parsePeopleCardResult(raw)
	require.NoError(t, err)
	assert.Equal(t, "Good communicator", result.Summary)
	assert.Equal(t, "collaborator", result.CommunicationStyle)
	assert.Equal(t, []string{"If blocked, escalate"}, result.Tactics)
}

func TestParsePeopleCardResult_WithMarkdownFences(t *testing.T) {
	raw := "```json\n{\"summary\":\"test\",\"communication_style\":\"\",\"decision_role\":\"\",\"red_flags\":[],\"highlights\":[],\"accomplishments\":[],\"communication_guide\":\"\",\"decision_style\":\"\",\"tactics\":[]}\n```"
	result, err := parsePeopleCardResult(raw)
	require.NoError(t, err)
	assert.Equal(t, "test", result.Summary)
}

func TestParseTeamSummaryResult(t *testing.T) {
	raw := `{"summary": "Team is healthy", "attention": ["Check on Bob"], "tips": ["Use threads more"]}`
	result, err := parseTeamSummaryResult(raw)
	require.NoError(t, err)
	assert.Equal(t, "Team is healthy", result.Summary)
	assert.Equal(t, []string{"Check on Bob"}, result.Attention)
	assert.Equal(t, []string{"Use threads more"}, result.Tips)
}

func TestDBPeopleCardOperations(t *testing.T) {
	database := testDB(t)

	card := db.PeopleCard{
		UserID:             "U1",
		PeriodFrom:         1000,
		PeriodTo:           2000,
		MessageCount:       10,
		ChannelsActive:     3,
		Summary:            "Test card",
		CommunicationStyle: "driver",
		DecisionRole:       "decision-maker",
		RedFlags:           `["flag1"]`,
		Highlights:         `["highlight1"]`,
		Accomplishments:    `["shipped v2"]`,
		CommunicationGuide: "Be direct",
		DecisionStyle:      "Quick",
		Tactics:            `["tactic1"]`,
		ActiveHoursJSON:    `{"9":5}`,
		Model:              "test",
	}

	id, err := database.UpsertPeopleCard(card)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	latest, err := database.GetLatestPeopleCard("U1")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "Test card", latest.Summary)
	assert.Equal(t, "driver", latest.CommunicationStyle)
	assert.Equal(t, "Be direct", latest.CommunicationGuide)

	cards, err := database.GetPeopleCardsForWindow(1000, 2000)
	require.NoError(t, err)
	assert.Len(t, cards, 1)

	filtered, err := database.GetPeopleCards(db.PeopleCardFilter{UserID: "U1"})
	require.NoError(t, err)
	assert.Len(t, filtered, 1)

	card.Summary = "Updated card"
	_, err = database.UpsertPeopleCard(card)
	require.NoError(t, err)
	latest2, err := database.GetLatestPeopleCard("U1")
	require.NoError(t, err)
	assert.Equal(t, "Updated card", latest2.Summary)
}

func TestDBPeopleCardSummaryOperations(t *testing.T) {
	database := testDB(t)

	s := db.PeopleCardSummary{
		PeriodFrom: 1000,
		PeriodTo:   2000,
		Summary:    "Team is healthy",
		Attention:  `["Check on Bob"]`,
		Tips:       `["Use threads"]`,
		Model:      "test",
	}
	err := database.UpsertPeopleCardSummary(s)
	require.NoError(t, err)

	got, err := database.GetPeopleCardSummary(1000, 2000)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Team is healthy", got.Summary)
	assert.Equal(t, `["Check on Bob"]`, got.Attention)

	missing, err := database.GetPeopleCardSummary(9999, 9999)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestGetPeopleSignals(t *testing.T) {
	database := testDB(t)

	seedChannel(t, database, "C1", "general")
	seedChannel(t, database, "C2", "random")

	// Seed digests with signals
	seedDigestWithSignals(t, database, "C1", 1000, 2000,
		`[{"user_id":"U1","signals":[{"type":"initiative","detail":"Started discussion"}]},{"user_id":"U2","signals":[{"type":"bottleneck","detail":"Blocked PR"}]}]`)
	seedDigestWithSignals(t, database, "C2", 1000, 2000,
		`[{"user_id":"U1","signals":[{"type":"accomplishment","detail":"Shipped feature"}]}]`)

	// Get signals for U1
	signals, err := database.GetPeopleSignalsForUser("U1", 1000, 2000)
	require.NoError(t, err)
	assert.Len(t, signals, 2) // from C1 and C2
	assert.Equal(t, "initiative", signals[0].Signals[0].Type)

	// Get all signals
	allSignals, err := database.GetAllPeopleSignals(1000, 2000)
	require.NoError(t, err)
	assert.Len(t, allSignals, 2) // U1 and U2
	assert.Len(t, allSignals["U1"], 2)
	assert.Len(t, allSignals["U2"], 1)
}

func TestComputeTeamNorms(t *testing.T) {
	stats := []db.UserStats{
		{UserID: "U1", MessageCount: 20, ChannelsActive: 4, AvgMessageLength: 100, ThreadsInitiated: 3},
		{UserID: "U2", MessageCount: 40, ChannelsActive: 6, AvgMessageLength: 80, ThreadsInitiated: 7},
	}
	norms := computeTeamNorms(stats)
	assert.Equal(t, 2, norms.TotalUsers)
	assert.InDelta(t, 30, norms.AvgMessages, 0.01)
	assert.InDelta(t, 5, norms.AvgChannels, 0.01)
	assert.InDelta(t, 90, norms.AvgMsgLength, 0.01)
	assert.InDelta(t, 5, norms.AvgThreadsStart, 0.01)
}
