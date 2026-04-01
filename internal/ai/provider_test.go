package ai

import "testing"

// Verify at compile time that Client implements Provider.
var _ Provider = (*Client)(nil)

func TestProviderInterface(t *testing.T) {
	// This test verifies that ai.Client satisfies the Provider interface.
	// The actual compliance check is done by the var _ line above.
	t.Log("ai.Client implements ai.Provider")
}
