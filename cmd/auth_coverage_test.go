package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/auth"
)

func TestSaveAuthResult_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	result := &auth.OAuthResult{
		AccessToken: "xoxp-test-token-12345",
		TeamID:      "T123",
		TeamName:    "My Test Team",
		UserID:      "U456",
	}

	info, err := saveAuthResult(result)
	require.NoError(t, err)
	assert.Equal(t, "my-test-team", info.Workspace)
	assert.Equal(t, "T123", info.TeamID)
	assert.Equal(t, "U456", info.UserID)

	// Verify config file was created
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "my-test-team")
	assert.Contains(t, content, "xoxp-test-token-12345")
}

func TestSaveAuthResult_EmptyTeamName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	result := &auth.OAuthResult{
		AccessToken: "xoxp-test-token",
		TeamID:      "T789",
		TeamName:    "",
		UserID:      "U101",
	}

	info, err := saveAuthResult(result)
	require.NoError(t, err)
	// When team name sanitizes to empty, should use TeamID
	assert.Equal(t, "T789", info.Workspace)
}

func TestSaveAuthResult_ExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create existing config
	existingConfig := `active_workspace: old-workspace
ai:
  model: "custom-model"
`
	require.NoError(t, os.WriteFile(configPath, []byte(existingConfig), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	result := &auth.OAuthResult{
		AccessToken: "xoxp-new-token",
		TeamID:      "T001",
		TeamName:    "New Team",
		UserID:      "U001",
	}

	info, err := saveAuthResult(result)
	require.NoError(t, err)
	assert.Equal(t, "new-team", info.Workspace)

	// Verify config was updated (active_workspace should be new)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "new-team")
	assert.Contains(t, content, "xoxp-new-token")
}

func TestSaveAuthResult_WithExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	result := &auth.OAuthResult{
		AccessToken: "xoxp-expiring-token",
		TeamID:      "T001",
		TeamName:    "Expiry Team",
		UserID:      "U001",
		ExpiresIn:   3600,
	}

	// Should succeed but print warning to stderr
	info, err := saveAuthResult(result)
	require.NoError(t, err)
	assert.Equal(t, "expiry-team", info.Workspace)
}

func TestAuthResultInfo_Fields(t *testing.T) {
	info := authResultInfo{
		Workspace: "test-ws",
		TeamID:    "T001",
		UserID:    "U001",
	}
	assert.Equal(t, "test-ws", info.Workspace)
	assert.Equal(t, "T001", info.TeamID)
	assert.Equal(t, "U001", info.UserID)
}
