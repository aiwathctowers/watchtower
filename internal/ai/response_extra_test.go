package ai

import (
	"strings"
	"testing"

	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSourcesSection_Present(t *testing.T) {
	rendered := "Some answer text.\n\nSources:\n- #general 2026-04-02 09:00\n"
	got := ExtractSourcesSection(rendered)
	assert.True(t, strings.HasPrefix(got, "Sources:"))
	assert.Contains(t, got, "#general")
}

func TestExtractSourcesSection_Missing(t *testing.T) {
	got := ExtractSourcesSection("Just an answer with no sources.")
	assert.Empty(t, got)
}

func TestExtractSourcesSection_TrimsTrailingWhitespace(t *testing.T) {
	rendered := "x\n\nSources:\n- #x 2026-04-02 09:00\n\n\n"
	got := ExtractSourcesSection(rendered)
	assert.False(t, strings.HasSuffix(got, "\n\n"))
}

func TestJiraPromptSection_HasJiraTables(t *testing.T) {
	got := JiraPromptSection()
	assert.Contains(t, got, "JIRA TABLES")
	assert.Contains(t, got, "jira_issues")
	assert.Contains(t, got, "key TEXT PRIMARY KEY")
}

func TestResolveSources_NoReferences(t *testing.T) {
	r := NewResponseRenderer(&db.DB{}, "acme", "T1")
	got := r.ResolveSources("plain text without refs")
	assert.Empty(t, got, "no refs → no sources block")
}

func TestResolveSources_NoMatchingChannel(t *testing.T) {
	dbi, err := db.Open(":memory:")
	require.NoError(t, err)
	defer dbi.Close()

	r := NewResponseRenderer(dbi, "acme", "T1")
	got := r.ResolveSources("see #unknown 2026-04-02 09:00 for details")
	assert.Empty(t, got, "no DB row for the channel → empty sources")
}

func TestResolveSources_ResolvedChannelMissingMessage(t *testing.T) {
	dbi, err := db.Open(":memory:")
	require.NoError(t, err)
	defer dbi.Close()

	require.NoError(t, dbi.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	r := NewResponseRenderer(dbi, "acme", "T1")
	// Channel exists but the message reference doesn't map to any row;
	// resolveRefs may still produce a placeholder ref. Just smoke-check.
	_ = r.ResolveSources("see #general 2026-04-02 09:00")
}
