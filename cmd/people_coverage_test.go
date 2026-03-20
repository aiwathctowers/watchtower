package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestShowPeopleList_WithVolumeChange(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       100,
		ChannelsActive:     5,
		ThreadsInitiated:   10,
		ThreadsReplied:     20,
		VolumeChangePct:    -25.0,
		Summary:            "Activity dropped significantly",
		CommunicationStyle: "terse",
		DecisionRole:       "observer",
		RedFlags:           `["Unresponsive in code reviews"]`,
		Highlights:         `["Quick bug fixes"]`,
		Accomplishments:    `[]`,
		HowToCommunicate:   "Be direct and concise",
		DecisionStyle:      "Prefers to observe before deciding",
		Tactics:            `[]`,
		Model:              "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 1

	err = peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
}

func TestShowPeopleList_WithTeamSummaryAndAttention(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       50,
		ChannelsActive:     3,
		Summary:            "Normal week",
		CommunicationStyle: "balanced",
		DecisionRole:       "contributor",
		RedFlags:           `[]`,
		Highlights:         `["Shipped feature"]`,
		Accomplishments:    `["Completed project"]`,
		HowToCommunicate:   "Keep communication balanced",
		DecisionStyle:      "Collaborative contributor",
		Tactics:            `["If blocked, escalate early"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	// Add people card summary with attention items
	err = database.UpsertPeopleCardSummary(db.PeopleCardSummary{
		PeriodFrom: from,
		PeriodTo:   to,
		Summary:    "Team had a productive sprint",
		Attention:  `["Unreviewed PRs piling up","Deployment deadline approaching"]`,
		Tips:       `["Schedule regular 1:1s"]`,
		Model:      "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showPeopleList(peopleCmd, buf, database, nil, from, to,
		time.Unix(int64(from), 0), time.Unix(int64(to), 0))
	require.NoError(t, err)

	// Output goes through ui.RenderMarkdown - check for partial strings
	output := buf.String()
	assert.Contains(t, output, "alice")

	database.Close()
}

func TestShowUserDetail_WithAllFields(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       200,
		ChannelsActive:     8,
		ThreadsInitiated:   30,
		ThreadsReplied:     50,
		AvgMessageLength:   120.5,
		VolumeChangePct:    40.0,
		Summary:            "Alice had an exceptionally productive week",
		CommunicationStyle: "detailed",
		DecisionRole:       "driver",
		RedFlags:           `["Working past midnight regularly"]`,
		Highlights:         `["Shipped auth service","Mentored two juniors"]`,
		Accomplishments:    `["Completed OAuth integration","Published architecture doc"]`,
		HowToCommunicate:   "Tends to write long, well-structured messages. Provide detailed context.",
		DecisionStyle:      "Drives decisions with data-backed arguments",
		Tactics:            `["If overloaded, delegate code reviews","If working late, set boundaries for work hours"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showUserDetail(buf, database, "alice", from, to)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "200")
	assert.Contains(t, output, "detailed")

	database.Close()
}

func TestRunPeopleGenerate_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := peopleGenerateCmd.RunE(peopleGenerateCmd, nil)
	assert.Error(t, err)
}

func TestRunPeople_WeeksClampedToOne(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = -5 // should be clamped to 1

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No people cards available")
}
