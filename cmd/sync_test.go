package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "sync" {
			found = true
			break
		}
	}
	assert.True(t, found, "sync command should be registered")
}

func TestSyncCommandFlags(t *testing.T) {
	f := syncCmd.Flags()

	fullFlag := f.Lookup("full")
	assert.NotNil(t, fullFlag)
	assert.Equal(t, "false", fullFlag.DefValue)

	daemonFlag := f.Lookup("daemon")
	assert.NotNil(t, daemonFlag)
	assert.Equal(t, "false", daemonFlag.DefValue)

	channelsFlag := f.Lookup("channels")
	assert.NotNil(t, channelsFlag)
	assert.Equal(t, "[]", channelsFlag.DefValue)

	workersFlag := f.Lookup("workers")
	assert.NotNil(t, workersFlag)
	assert.Equal(t, "0", workersFlag.DefValue)
}

func TestSyncCommandRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := syncCmd.RunE(syncCmd, nil)
	assert.Error(t, err)
}
