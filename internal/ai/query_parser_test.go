package ai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fixedNow returns a fixed "now" for deterministic tests.
// Wednesday, 2025-02-26 14:30:00 UTC
func fixedNow() time.Time {
	return time.Date(2025, 2, 26, 14, 30, 0, 0, time.UTC)
}

func withFixedTime(t *testing.T) {
	t.Helper()
	orig := nowFunc
	nowFunc = fixedNow
	t.Cleanup(func() { nowFunc = orig })
}

// --- Time range parsing ---

func TestParse_TimeRange_Yesterday(t *testing.T) {
	withFixedTime(t)
	pq := Parse("what happened yesterday")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 25, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, time.Date(2025, 2, 25, 23, 59, 59, 0, time.UTC), pq.TimeRange.To)
}

func TestParse_TimeRange_Today(t *testing.T) {
	withFixedTime(t)
	pq := Parse("what's new today")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 26, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, fixedNow(), pq.TimeRange.To)
}

func TestParse_TimeRange_ThisMorning(t *testing.T) {
	withFixedTime(t)
	pq := Parse("what happened this morning")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 26, 6, 0, 0, 0, time.UTC), pq.TimeRange.From)
	// now (14:30) is after noon, so To should be noon
	assert.Equal(t, time.Date(2025, 2, 26, 12, 0, 0, 0, time.UTC), pq.TimeRange.To)
}

func TestParse_TimeRange_ThisMorning_BeforeNoon(t *testing.T) {
	orig := nowFunc
	nowFunc = func() time.Time {
		return time.Date(2025, 2, 26, 10, 0, 0, 0, time.UTC)
	}
	defer func() { nowFunc = orig }()

	pq := Parse("this morning activity")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 26, 6, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, time.Date(2025, 2, 26, 10, 0, 0, 0, time.UTC), pq.TimeRange.To)
}

func TestParse_TimeRange_LastWeek(t *testing.T) {
	withFixedTime(t)
	// 2025-02-26 is Wednesday. Last week = Mon Feb 17 - Sun Feb 23
	pq := Parse("show me last week")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 17, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, time.Date(2025, 2, 23, 23, 59, 59, 0, time.UTC), pq.TimeRange.To)
}

func TestParse_TimeRange_RelativeDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		duration time.Duration
	}{
		{"last 2h", "what happened last 2h", 2 * time.Hour},
		{"past 2 hours", "past 2 hours activity", 2 * time.Hour},
		{"last 30 minutes", "last 30 minutes", 30 * time.Minute},
		{"last 30 min", "last 30 min", 30 * time.Minute},
		{"past 3 days", "past 3 days", 3 * 24 * time.Hour},
		{"last 1 week", "last 1 week", 7 * 24 * time.Hour},
		{"last 2 weeks", "last 2 weeks", 14 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFixedTime(t)
			pq := Parse(tt.input)
			assert.NotNil(t, pq.TimeRange, "expected time range for %q", tt.input)
			now := fixedNow()
			assert.Equal(t, now.Add(-tt.duration), pq.TimeRange.From)
			assert.Equal(t, now, pq.TimeRange.To)
		})
	}
}

func TestParse_TimeRange_SinceWeekday(t *testing.T) {
	withFixedTime(t)
	// 2025-02-26 is Wednesday. "since Monday" = Monday Feb 24
	pq := Parse("since Monday")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 24, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, fixedNow(), pq.TimeRange.To)
}

func TestParse_TimeRange_SinceToday(t *testing.T) {
	withFixedTime(t)
	// "since Wednesday" when it is Wednesday = today
	pq := Parse("since Wednesday")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 26, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Equal(t, fixedNow(), pq.TimeRange.To)
}

func TestParse_TimeRange_NoTimeRange(t *testing.T) {
	pq := Parse("tell me about deployments")
	assert.Nil(t, pq.TimeRange)
}

// --- Channel extraction ---

func TestParse_Channels_Literal(t *testing.T) {
	pq := Parse("summarize #engineering")
	assert.Contains(t, pq.Channels, "engineering")
}

func TestParse_Channels_MultipleLiterals(t *testing.T) {
	pq := Parse("compare #engineering and #design")
	assert.Contains(t, pq.Channels, "engineering")
	assert.Contains(t, pq.Channels, "design")
}

func TestParse_Channels_InChannel(t *testing.T) {
	pq := Parse("what happened in engineering channel")
	assert.Contains(t, pq.Channels, "engineering")
}

func TestParse_Channels_InPrefix(t *testing.T) {
	pq := Parse("search in dev-ops")
	assert.Contains(t, pq.Channels, "dev-ops")
}

func TestParse_Channels_NoDuplicates(t *testing.T) {
	pq := Parse("search #engineering in engineering channel")
	count := 0
	for _, ch := range pq.Channels {
		if ch == "engineering" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// --- User extraction ---

func TestParse_Users_Literal(t *testing.T) {
	pq := Parse("what did @alice say")
	assert.Contains(t, pq.Users, "alice")
}

func TestParse_Users_FromUser(t *testing.T) {
	pq := Parse("messages from bob.smith")
	assert.Contains(t, pq.Users, "bob.smith")
}

func TestParse_Users_UserSaid(t *testing.T) {
	pq := Parse("alice said something about deployment")
	assert.Contains(t, pq.Users, "alice")
}

func TestParse_Users_WhatDidUser(t *testing.T) {
	pq := Parse("what did carol say about the release")
	assert.Contains(t, pq.Users, "carol")
}

func TestParse_Users_NoDuplicates(t *testing.T) {
	pq := Parse("what did @alice say, from alice")
	count := 0
	for _, u := range pq.Users {
		if u == "alice" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// --- Intent detection ---

func TestParse_Intent_Catchup(t *testing.T) {
	tests := []string{
		"what happened while I was gone",
		"what's new",
		"catch me up",
		"summarize everything",
		"update me",
		"bring me up to speed",
		"what did I miss",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			pq := Parse(input)
			assert.Equal(t, IntentCatchup, pq.Intent, "expected catchup for %q", input)
		})
	}
}

func TestParse_Intent_Search(t *testing.T) {
	tests := []string{
		"find messages about deployment",
		"search for kubernetes errors",
		"look for database migration",
		"show me messages about the outage",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			pq := Parse(input)
			assert.Equal(t, IntentSearch, pq.Intent, "expected search for %q", input)
		})
	}
}

func TestParse_Intent_Person(t *testing.T) {
	pq := Parse("what did @alice say about the release")
	assert.Equal(t, IntentPerson, pq.Intent)
}

func TestParse_Intent_Channel(t *testing.T) {
	pq := Parse("summarize #engineering")
	assert.Equal(t, IntentChannel, pq.Intent)
}

func TestParse_Intent_General(t *testing.T) {
	pq := Parse("how does the deployment pipeline work")
	assert.Equal(t, IntentGeneral, pq.Intent)
}

// --- Topic extraction ---

func TestParse_Topics_ExtractsKeywords(t *testing.T) {
	pq := Parse("tell me about deployment pipeline errors")
	assert.Contains(t, pq.Topics, "deployment")
	assert.Contains(t, pq.Topics, "pipeline")
	assert.Contains(t, pq.Topics, "errors")
}

func TestParse_Topics_FiltersStopWords(t *testing.T) {
	pq := Parse("what is the status of the project")
	// "what", "is", "the", "of" are stop words
	assert.Contains(t, pq.Topics, "status")
	assert.Contains(t, pq.Topics, "project")
	assert.NotContains(t, pq.Topics, "what")
	assert.NotContains(t, pq.Topics, "is")
	assert.NotContains(t, pq.Topics, "the")
	assert.NotContains(t, pq.Topics, "of")
}

func TestParse_Topics_NoDuplicates(t *testing.T) {
	pq := Parse("deployment deployment deployment")
	count := 0
	for _, topic := range pq.Topics {
		if topic == "deployment" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// --- Combined parsing ---

func TestParse_Combined_FullQuery(t *testing.T) {
	withFixedTime(t)
	pq := Parse("what did @alice say about deployment in #engineering yesterday")

	assert.Equal(t, IntentPerson, pq.Intent)
	assert.Contains(t, pq.Users, "alice")
	assert.Contains(t, pq.Channels, "engineering")
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 25, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
	assert.Contains(t, pq.Topics, "deployment")
}

func TestParse_Combined_CatchupSince(t *testing.T) {
	withFixedTime(t)
	pq := Parse("catch me up since Monday")

	assert.Equal(t, IntentCatchup, pq.Intent)
	assert.NotNil(t, pq.TimeRange)
	assert.Equal(t, time.Date(2025, 2, 24, 0, 0, 0, 0, time.UTC), pq.TimeRange.From)
}

func TestParse_RawTextPreserved(t *testing.T) {
	input := "what happened in #general yesterday"
	pq := Parse(input)
	assert.Equal(t, input, pq.RawText)
}

// --- QueryIntent.String ---

func TestQueryIntent_String(t *testing.T) {
	assert.Equal(t, "general", IntentGeneral.String())
	assert.Equal(t, "catchup", IntentCatchup.String())
	assert.Equal(t, "search", IntentSearch.String())
	assert.Equal(t, "person", IntentPerson.String())
	assert.Equal(t, "channel", IntentChannel.String())
}

// --- Edge cases ---

func TestParse_EmptyInput(t *testing.T) {
	pq := Parse("")
	assert.Equal(t, "", pq.RawText)
	assert.Nil(t, pq.TimeRange)
	assert.Nil(t, pq.Channels)
	assert.Nil(t, pq.Users)
	assert.Nil(t, pq.Topics)
	assert.Equal(t, IntentGeneral, pq.Intent)
}

func TestParse_OnlyStopWords(t *testing.T) {
	pq := Parse("what is the")
	assert.Nil(t, pq.Topics)
}
