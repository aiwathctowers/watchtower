package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCommandHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "watchtower")
	assert.Contains(t, output, "Slack workspace into a local SQLite database")
}

func TestRootCommandPersistentFlags(t *testing.T) {
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("workspace"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("config"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("verbose"))
}

func TestDefaultConfigPath(t *testing.T) {
	path := defaultConfigPath()
	assert.Contains(t, path, ".config/watchtower/config.yaml")
}

func TestWorkspaceFlagDefault(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("workspace")
	assert.Equal(t, "", flag.DefValue)
}

func TestVerboseFlagDefault(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("verbose")
	assert.Equal(t, "false", flag.DefValue)
}
