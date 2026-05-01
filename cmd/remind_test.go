package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemindWhen_RelativeMinutes(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseRemindWhen("", "30m", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-01T12:30", got)
}

func TestParseRemindWhen_RelativeHours(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseRemindWhen("", "4h", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-01T16:00", got)
}

func TestParseRemindWhen_RelativeDays(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseRemindWhen("", "2d", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-03T12:00", got)
}

func TestParseRemindWhen_AbsoluteRFC3339(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseRemindWhen("2026-05-01T16:00:00Z", "", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-01T16:00", got)
}

func TestParseRemindWhen_AbsoluteShort(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseRemindWhen("2026-05-01T16:00", "", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-01T16:00", got)
}

func TestParseRemindWhen_BothFlagsRejected(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	_, err := parseRemindWhen("2026-05-01T16:00", "4h", now)
	assert.Error(t, err)
}

func TestParseRemindWhen_NeitherFlagRejected(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	_, err := parseRemindWhen("", "", now)
	assert.Error(t, err)
}

func TestParseRemindWhen_GarbageDuration(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	_, err := parseRemindWhen("", "soon", now)
	assert.Error(t, err)
}
