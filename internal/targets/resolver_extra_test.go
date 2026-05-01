package targets

import (
	"testing"
	"time"

	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
)

func TestNewResolver_DefaultsTimeout(t *testing.T) {
	r := NewResolver(nil, nil, 0)
	assert.Equal(t, 10*time.Second, r.timeout, "non-positive timeout falls back to 10s")
	assert.Len(t, r.resolvers, 2, "Slack + Jira resolvers registered")
}

func TestNewResolver_CustomTimeout(t *testing.T) {
	r := NewResolver(nil, nil, 3*time.Second)
	assert.Equal(t, 3*time.Second, r.timeout)
}

func TestSlackResolver_CanResolve(t *testing.T) {
	s := &SlackResolver{}
	assert.True(t, s.CanResolve("https://acme.slack.com/archives/C123/p1700000000123456"))
	assert.True(t, s.CanResolve("https://acme.slack.com/archives/C123/p1700000000123456?thread_ts=1700000000.000123"))
	assert.False(t, s.CanResolve("https://atlassian.net/browse/X-1"))
	assert.False(t, s.CanResolve("not a url"))
	assert.False(t, s.CanResolve(""))
}

func TestJiraResolver_CanResolve(t *testing.T) {
	j := &JiraResolver{}
	assert.True(t, j.CanResolve("https://acme.atlassian.net/browse/PROJ-123"))
	assert.True(t, j.CanResolve("https://acme.atlassian.net/browse/AB12-9"))
	assert.False(t, j.CanResolve("https://acme.atlassian.net/dashboard"))
	assert.False(t, j.CanResolve("https://acme.slack.com/archives/C1/p1"))
}

func TestRefForMatch_Slack(t *testing.T) {
	got := refForMatch(URLMatch{Source: "slack", ChannelID: "C1", TS: "1.000001"})
	assert.Equal(t, "slack:C1:1.000001", got)
}

func TestRefForMatch_Jira(t *testing.T) {
	got := refForMatch(URLMatch{Source: "jira", Key: "PROJ-123"})
	assert.Equal(t, "jira:PROJ-123", got)
}

func TestRefForMatch_Unknown(t *testing.T) {
	got := refForMatch(URLMatch{Source: "other", RawURL: "https://x.com"})
	assert.Equal(t, "https://x.com", got)
}

// Smoke-test Resolve with no matches — must not panic and return empty list.
func TestResolver_Resolve_EmptyInput(t *testing.T) {
	r := NewResolver(&db.DB{}, nil, time.Second)
	got := r.Resolve(t.Context(), nil)
	assert.Empty(t, got)
}

// Smoke-test Resolve with a match nobody can resolve — returns empty enrichments.
func TestResolver_Resolve_NoResolverMatches(t *testing.T) {
	r := NewResolver(&db.DB{}, nil, time.Second)
	got := r.Resolve(t.Context(), []URLMatch{{Source: "other", RawURL: "https://example.com"}})
	assert.Empty(t, got, "resolvers refuse the URL → no enrichment")
}
