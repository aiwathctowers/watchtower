package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultFor_KnownKey(t *testing.T) {
	got := DefaultFor(DigestChannel)
	assert.NotEmpty(t, got, "known prompt key should return a non-empty default")
}

func TestDefaultFor_UnknownKey(t *testing.T) {
	got := DefaultFor("nonexistent.prompt.key")
	assert.Empty(t, got)
}

func TestDefaultFor_AllKnownKeysHaveDefaults(t *testing.T) {
	// Every key listed in CurrentVersions must have a non-empty default.
	for key := range DefaultVersions {
		assert.NotEmpty(t, DefaultFor(key), "missing default for known key %q", key)
	}
}
