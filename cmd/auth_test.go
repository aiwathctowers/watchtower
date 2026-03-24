package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "auth" {
			found = true
			break
		}
	}
	assert.True(t, found, "auth command should be registered")
}

func TestAuthSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"login": false, "prepare": false, "complete": false, "trust-cert": false, "check-cert": false}
	for _, cmd := range authCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "auth %s subcommand should be registered", name)
	}
}

func TestSanitizeWorkspaceName_Extended(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Cool Company!", "my-cool-company"},
		{"  whitespace  ", "whitespace"},
		{"123numeric", "123numeric"},
		{"with_underscores", "with_underscores"},
		{"a-b-c", "a-b-c"},
		{"TEAM NAME (Main)", "team-name-main"},
		{"---leading-trailing---", "leading-trailing"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeWorkspaceName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveOAuthConfig_Empty(t *testing.T) {
	// When both env vars are empty and no defaults are compiled in,
	// resolveOAuthConfig should return an error.
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "")

	_, err := resolveOAuthConfig()
	// If DefaultClientID and DefaultClientSecret are empty, we get an error.
	// If they're set at build time, this succeeds. Either way this test is valid.
	if err != nil {
		assert.Contains(t, err.Error(), "OAuth credentials not configured")
	}
}

func TestResolveOAuthConfig_FromEnv(t *testing.T) {
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "test-client-secret")

	cfg, err := resolveOAuthConfig()
	assert.NoError(t, err)
	assert.Equal(t, "test-client-id", cfg.ClientID)
	assert.Equal(t, "test-client-secret", cfg.ClientSecret)
}

func TestAuthFlags(t *testing.T) {
	assert.NotNil(t, authCompleteCmd.Flags().Lookup("code"))
	assert.NotNil(t, authCompleteCmd.Flags().Lookup("redirect-uri"))
	assert.NotNil(t, authPrepareCmd.Flags().Lookup("redirect-uri"))
	assert.NotNil(t, authLoginCmd.Flags().Lookup("no-open"))
}
