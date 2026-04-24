package targets

import (
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseExtractResponse_DropsInvalidExternalRef verifies that secondary links
// with external_ref values outside the jira:/slack: allowlist are silently dropped.
func TestParseExtractResponse_DropsInvalidExternalRef(t *testing.T) {
	raw := `{
		"extracted": [
			{
				"text": "Ship feature X",
				"intent": "deliver value",
				"level": "week",
				"priority": "high",
				"period_start": "2026-04-21",
				"period_end": "2026-04-27",
				"secondary_links": [
					{"external_ref": "jira:PROJ-1",  "relation": "contributes_to", "confidence": 0.9},
					{"external_ref": "notion:abc123", "relation": "related",        "confidence": 0.5},
					{"external_ref": "slack:C123:1714567890.123456", "relation": "related", "confidence": 0.7}
				]
			}
		],
		"omitted_count": 0,
		"notes": ""
	}`

	result, err := parseExtractResponse(raw, nil, log.Default())
	require.NoError(t, err)
	require.Len(t, result.Extracted, 1)

	links := result.Extracted[0].SecondaryLinks
	// notion: must be dropped; jira: and slack: must survive.
	assert.Len(t, links, 2, "invalid external_ref (notion:) should be dropped")
	for _, l := range links {
		assert.True(t, IsValidExternalRef(l.ExternalRef),
			"all surviving links must have a valid external_ref prefix, got %q", l.ExternalRef)
	}
}

// TestIsValidExternalRef covers the allowlist helper.
func TestIsValidExternalRef(t *testing.T) {
	valid := []string{"jira:PROJ-1", "jira:", "slack:C123:ts", "slack:"}
	for _, ref := range valid {
		assert.True(t, IsValidExternalRef(ref), "expected valid: %q", ref)
	}

	invalid := []string{"notion:abc", "github:issue/1", "http://example.com", "", "JIRA:PROJ-1"}
	for _, ref := range invalid {
		assert.False(t, IsValidExternalRef(ref), "expected invalid: %q", ref)
	}
}
