package jira

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	// Initially does not exist.
	assert.False(t, store.Exists())

	_, err := store.Load()
	assert.Error(t, err)

	// Save.
	token := &OAuthToken{
		AccessToken:  "access123",
		TokenType:    "Bearer",
		RefreshToken: "refresh456",
		ExpiresIn:    3600,
		Scope:        "read:jira-work",
		Expiry:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	require.NoError(t, store.Save(token))
	assert.True(t, store.Exists())

	// Load.
	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "access123", loaded.AccessToken)
	assert.Equal(t, "Bearer", loaded.TokenType)
	assert.Equal(t, "refresh456", loaded.RefreshToken)

	// Delete.
	require.NoError(t, store.Delete())
	assert.False(t, store.Exists())

	// Delete non-existent is OK.
	require.NoError(t, store.Delete())
}

func TestTokenStore_Path(t *testing.T) {
	store := NewTokenStore("/tmp/test-workspace")
	assert.Equal(t, filepath.Join("/tmp/test-workspace", "jira_token.json"), store.Path())
}

func TestTokenStore_SaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub", "dir")
	store := NewTokenStore(subDir)

	token := &OAuthToken{AccessToken: "test"}
	require.NoError(t, store.Save(token))

	_, err := os.Stat(filepath.Join(subDir, "jira_token.json"))
	assert.NoError(t, err)
}

func TestOAuthToken_IsExpired(t *testing.T) {
	tests := []struct {
		name    string
		token   OAuthToken
		expired bool
	}{
		{
			name:    "empty expiry",
			token:   OAuthToken{Expiry: ""},
			expired: true,
		},
		{
			name:    "invalid expiry",
			token:   OAuthToken{Expiry: "not-a-date"},
			expired: true,
		},
		{
			name:    "future expiry",
			token:   OAuthToken{Expiry: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)},
			expired: false,
		},
		{
			name:    "past expiry",
			token:   OAuthToken{Expiry: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)},
			expired: true,
		},
		{
			name:    "within 60s buffer",
			token:   OAuthToken{Expiry: time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)},
			expired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expired, tt.token.IsExpired())
		})
	}
}

func TestListenLocal_PortRange(t *testing.T) {
	// Should be able to listen on one of the preferred ports.
	ln, err := listenLocal()
	require.NoError(t, err)
	defer ln.Close()

	addr := ln.Addr().String()
	assert.Contains(t, addr, "127.0.0.1:")
}
