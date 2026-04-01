package codex

import (
	"testing"

	"watchtower/internal/ai"
	"watchtower/internal/digest"
)

// Verify at compile time that Client implements ai.Provider.
var _ ai.Provider = (*Client)(nil)

// Verify at compile time that CodexGenerator implements digest.Generator.
var _ digest.Generator = (*CodexGenerator)(nil)

func TestInterfaceCompliance(t *testing.T) {
	t.Log("codex.Client implements ai.Provider, codex.CodexGenerator implements digest.Generator")
}
