package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestPeopleCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "people" {
			found = true
			break
		}
	}
	assert.True(t, found, "people command should be registered")
}

func TestPeopleSubcommandsRegistered(t *testing.T) {
	found := false
	for _, cmd := range peopleCmd.Commands() {
		if cmd.Name() == "generate" {
			found = true
			break
		}
	}
	assert.True(t, found, "people generate subcommand should be registered")
}

func TestRunPeople_NoCards(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 1

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No people cards available")
}

func TestRunPeople_WithCards(t *testing.T) {
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
		ThreadsInitiated:   5,
		ThreadsReplied:     10,
		AvgMessageLength:   45.5,
		VolumeChangePct:    15.0,
		Summary:            "Alice is a strong communicator",
		CommunicationStyle: "collaborator",
		DecisionRole:       "decision-maker",
		RedFlags:           `[]`,
		Highlights:         `["led deployment"]`,
		Accomplishments:    `["shipped auth refactor"]`,
		CommunicationGuide:   "Alice prefers async communication",
		DecisionStyle:      "Quick decisions in #frontend",
		Tactics:            `["If urgent, DM her directly"]`,
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
	assert.Contains(t, output, "collaborator")
	assert.Contains(t, output, "decision-maker")
}

func TestRunPeople_UserFilter(t *testing.T) {
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
		MessageCount:       25,
		ChannelsActive:     2,
		Summary:            "Alice's detailed card",
		CommunicationStyle: "driver",
		DecisionRole:       "approver",
		RedFlags:           `["missed deadline"]`,
		Highlights:         `["great code review"]`,
		Accomplishments:    `["shipped feature"]`,
		CommunicationGuide:   "Be direct with Alice",
		DecisionStyle:      "Quick approver",
		Tactics:            `["If blocked, escalate to her manager"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = "@alice"
	peopleFlagPrevious = false
	peopleFlagWeeks = 1
	defer func() { peopleFlagUser = "" }()

	err = peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
}

func TestRunPeople_UserNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	peopleFlagUser = "@nonexistent"
	peopleFlagPrevious = false
	peopleFlagWeeks = 1
	defer func() { peopleFlagUser = "" }()

	err := peopleCmd.RunE(peopleCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunPeople_UserAsArg(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 1

	err := peopleCmd.RunE(peopleCmd, []string{"@alice"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No people card available")
}

func TestRunPeople_Previous(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = true
	peopleFlagWeeks = 1
	defer func() { peopleFlagPrevious = false }()

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No people cards available")
}

func TestRunPeople_MultipleWeeks(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 3

	err := peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(buf.String()), 50)
}

func TestRunPeople_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	peopleFlagUser = ""
	peopleFlagWeeks = 1

	err := peopleCmd.RunE(peopleCmd, nil)
	assert.Error(t, err)
}

func TestPeopleFlags(t *testing.T) {
	assert.NotNil(t, peopleCmd.Flags().Lookup("user"))
	assert.NotNil(t, peopleCmd.Flags().Lookup("previous"))
	assert.NotNil(t, peopleCmd.Flags().Lookup("weeks"))
	assert.NotNil(t, peopleGenerateCmd.Flags().Lookup("workers"))
	assert.NotNil(t, peopleGenerateCmd.Flags().Lookup("progress-json"))
}

func TestShowPeopleList_WithSummary(t *testing.T) {
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
		MessageCount:       30,
		ChannelsActive:     2,
		Summary:            "Alice is active",
		CommunicationStyle: "collaborator",
		DecisionRole:       "contributor",
		RedFlags:           `[]`,
		Highlights:         `[]`,
		Accomplishments:    `[]`,
		CommunicationGuide:   "Prefers threads",
		Tactics:            `[]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	err = database.UpsertPeopleCardSummary(db.PeopleCardSummary{
		PeriodFrom: from,
		PeriodTo:   to,
		Summary:    "Team was productive this week",
		Attention:  `["Review overdue PRs"]`,
		Tips:       `["Use threads more"]`,
		Model:      "haiku",
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
