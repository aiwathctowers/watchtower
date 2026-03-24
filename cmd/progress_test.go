package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/sync"
)

func TestPrintProgressJSON_Success(t *testing.T) {
	buf := new(bytes.Buffer)
	snap := sync.Snapshot{
		Phase:           sync.PhaseMessages,
		StartTime:       time.Now().Add(-30 * time.Second),
		UsersTotal:      100,
		UsersDone:       50,
		ChannelsTotal:   20,
		ChannelsDone:    10,
		MessagesFetched: 500,
		ThreadsTotal:    30,
		ThreadsDone:     15,
		ThreadsFetched:  45,
	}

	printProgressJSON(buf, snap, nil)

	output := buf.String()
	var pj progressJSON
	err := json.Unmarshal([]byte(output), &pj)
	require.NoError(t, err)
	assert.Equal(t, "Messages", pj.Phase)
	assert.Equal(t, 100, pj.UsersTotal)
	assert.Equal(t, 50, pj.UsersDone)
	assert.Equal(t, 500, pj.MessagesFetched)
	assert.Greater(t, pj.ElapsedSec, float64(0))
	assert.Empty(t, pj.Error)
}

func TestPrintProgressJSON_WithError(t *testing.T) {
	buf := new(bytes.Buffer)
	snap := sync.Snapshot{
		Phase:     sync.PhaseDone,
		StartTime: time.Now(),
	}

	syncErr := assert.AnError
	printProgressJSON(buf, snap, syncErr)

	output := buf.String()
	var pj progressJSON
	err := json.Unmarshal([]byte(output), &pj)
	require.NoError(t, err)
	assert.NotEmpty(t, pj.Error)
	assert.Equal(t, "Done", pj.Phase)
}

func TestIsTerminal_NonTerminal(t *testing.T) {
	// Create a temp file - should not be a terminal
	tmpFile, err := os.CreateTemp(t.TempDir(), "test")
	require.NoError(t, err)
	defer tmpFile.Close()
	assert.False(t, isTerminal(tmpFile))
}

func TestPrintProgress_Basic(t *testing.T) {
	buf := new(bytes.Buffer)
	p := sync.NewProgress()

	// Just verify it doesn't panic and produces output
	printProgress(buf, p, "test-ws")
	assert.NotEmpty(t, buf.String())
}

func TestProgressJSONStruct(t *testing.T) {
	pj := progressJSON{
		Phase:             "Discovery",
		ElapsedSec:        5.0,
		DiscoveryPages:    3,
		DiscoveryChannels: 50,
		DiscoveryUsers:    100,
		Error:             "test error",
	}

	data, err := json.Marshal(pj)
	require.NoError(t, err)

	var parsed progressJSON
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "Discovery", parsed.Phase)
	assert.Equal(t, 50, parsed.DiscoveryChannels)
	assert.Equal(t, "test error", parsed.Error)
}
