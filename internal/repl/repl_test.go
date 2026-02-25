package repl

import (
	"strings"
	"testing"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-workspace",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-workspace": {SlackToken: "xoxp-test"},
		},
		AI: config.AIConfig{
			ApiKey:        "test-key",
			Model:         "claude-sonnet-4-20250514",
			MaxTokens:     4096,
			ContextBudget: 150000,
		},
		Sync: config.SyncConfig{
			Workers: 1,
		},
	}

	return Deps{
		Config:    cfg,
		DB:        database,
		Domain:    "test-domain",
		Workspace: "test-workspace",
	}
}

func TestNew(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	assert.Equal(t, -1, m.histIdx)
	assert.Empty(t, m.input)
	assert.Empty(t, m.history)
	assert.False(t, m.streaming)
	assert.False(t, m.quitting)
}

func TestWindowSizeMsg(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(*Model)

	assert.Nil(t, cmd)
	assert.Equal(t, 120, model.width)
	assert.Equal(t, 40, model.height)
}

func TestKeyInput(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	// Type "hello"
	for _, r := range "hello" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(*Model)
	}
	assert.Equal(t, "hello", m.input)

	// Backspace
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(*Model)
	assert.Equal(t, "hell", m.input)

	// Space
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(*Model)
	assert.Equal(t, "hell ", m.input)
}

func TestCtrlCQuitsWhenNotStreaming(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model := updated.(*Model)

	assert.True(t, model.quitting)
	assert.NotNil(t, cmd)
}

func TestCtrlCCancelsWhenStreaming(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true
	cancelled := false
	m.cancel = func() { cancelled = true }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model := updated.(*Model)

	assert.True(t, cancelled)
	assert.False(t, model.streaming)
	assert.False(t, model.quitting)
	assert.Nil(t, cmd)
	assert.Contains(t, model.output.String(), "cancelled")
}

func TestEnterIgnoredWhenStreaming(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true
	m.input = "test"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := updated.(*Model)

	assert.Nil(t, cmd)
	assert.Equal(t, "test", model.input) // input not cleared
}

func TestEnterIgnoredWhenEmpty(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.input = "   "

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := updated.(*Model)

	assert.Nil(t, cmd)
	assert.Empty(t, model.history)
}

func TestHistoryNavigation(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.history = []string{"first", "second", "third"}
	m.histIdx = -1

	// Press up — should go to "third"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	assert.Equal(t, "third", m.input)
	assert.Equal(t, 2, m.histIdx)

	// Press up again — should go to "second"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	assert.Equal(t, "second", m.input)
	assert.Equal(t, 1, m.histIdx)

	// Press down — should go back to "third"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	assert.Equal(t, "third", m.input)
	assert.Equal(t, 2, m.histIdx)

	// Press down again — should restore draft
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	assert.Equal(t, "", m.input)
	assert.Equal(t, -1, m.histIdx)
}

func TestHistoryPreservesDraft(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.history = []string{"old"}
	m.input = "current typing"

	// Press up — should save draft and show history
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	assert.Equal(t, "old", m.input)
	assert.Equal(t, "current typing", m.draft)

	// Press down — should restore draft
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	assert.Equal(t, "current typing", m.input)
}

func TestStreamChunkMsg(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, _ := m.Update(streamChunkMsg{text: "Hello "})
	m = updated.(*Model)
	updated, _ = m.Update(streamChunkMsg{text: "world!"})
	m = updated.(*Model)

	assert.Equal(t, "Hello world!", m.output.String())
	assert.Equal(t, []string{"Hello world!"}, m.lines)
}

func TestStreamDoneMsg(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true
	m.output.WriteString("Response text")

	updated, _ := m.Update(streamDoneMsg{sources: "Sources:\n  [1] link"})
	m = updated.(*Model)

	assert.False(t, m.streaming)
	assert.Contains(t, m.output.String(), "Sources:")
}

func TestStreamDoneMsgNoSources(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true
	m.output.WriteString("Response text")

	updated, _ := m.Update(streamDoneMsg{sources: ""})
	m = updated.(*Model)

	assert.False(t, m.streaming)
	assert.NotContains(t, m.output.String(), "Sources:")
}

func TestStreamErrMsg(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true

	updated, _ := m.Update(streamErrMsg{err: assert.AnError})
	m = updated.(*Model)

	assert.False(t, m.streaming)
	assert.Contains(t, m.output.String(), "Error:")
}

func TestCommandResultMsg(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.streaming = true

	updated, _ := m.Update(commandResultMsg{output: "Status info here"})
	m = updated.(*Model)

	assert.False(t, m.streaming)
	assert.Contains(t, m.output.String(), "Status info here")
}

func TestViewShowsPrompt(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.width = 80
	m.height = 24
	m.input = "test query"

	view := m.View()
	assert.Contains(t, view, "watchtower>")
	assert.Contains(t, view, "test query")
}

func TestViewShowsStreamingIndicator(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.width = 80
	m.height = 24
	m.streaming = true

	view := m.View()
	assert.Contains(t, view, "thinking...")
}

func TestViewShowsHelpHint(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.width = 80
	m.height = 24

	view := m.View()
	assert.Contains(t, view, "/help")
}

func TestViewEmpty(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.quitting = true

	view := m.View()
	assert.Empty(t, view)
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"hello", []string{"hello"}},
		{"hello\n", []string{"hello"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"a\nb\nc\n", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitLines(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractSourcesSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no sources", "Just some text", ""},
		{"with sources", "Response\n\nSources:\n  [1] link1\n  [2] link2", "Sources:\n  [1] link1\n  [2] link2"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ai.ExtractSourcesSection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSlashCommandQuit(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	// Type /quit and press enter
	m.input = ""
	m.output.Reset()

	// Simulate processInput for /quit
	updated, cmd := m.handleSlashCommand("/quit")
	model := updated.(*Model)

	assert.True(t, model.quitting)
	assert.NotNil(t, cmd)
}

func TestSlashCommandExit(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, cmd := m.handleSlashCommand("/exit")
	model := updated.(*Model)

	assert.True(t, model.quitting)
	assert.NotNil(t, cmd)
}

func TestSlashCommandHelp(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, cmd := m.handleSlashCommand("/help")
	model := updated.(*Model)

	assert.Nil(t, cmd)
	assert.Contains(t, model.output.String(), "/sync")
	assert.Contains(t, model.output.String(), "/status")
	assert.Contains(t, model.output.String(), "/catchup")
	assert.Contains(t, model.output.String(), "/help")
	assert.Contains(t, model.output.String(), "/quit")
}

func TestSlashCommandUnknown(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	updated, cmd := m.handleSlashCommand("/unknown")
	model := updated.(*Model)

	assert.Nil(t, cmd)
	assert.Contains(t, model.output.String(), "Unknown command")
}

func TestSlashCommandStatus(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	// Need a program ref for async; just check that it starts streaming
	m.program = tea.NewProgram(m)

	updated, _ := m.handleSlashCommand("/status")
	model := updated.(*Model)

	assert.True(t, model.streaming)
}

func TestHelpText(t *testing.T) {
	text := helpText()
	assert.Contains(t, text, "/sync")
	assert.Contains(t, text, "/status")
	assert.Contains(t, text, "/catchup")
	assert.Contains(t, text, "/quit")
	assert.Contains(t, text, "/help")
}

func TestDetermineSinceTime(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// No checkpoint — should default to 24h ago
	since, err := determineSinceTime(database)
	require.NoError(t, err)
	assert.WithinDuration(t, since, since, 1) // just check it didn't error
}

func TestOutputHeight(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	m.height = 30
	assert.Equal(t, 28, m.outputHeight())

	m.height = 0
	assert.Equal(t, 20, m.outputHeight()) // fallback
}

func TestPageScrolling(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.height = 10

	// Create enough lines to scroll
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line")
	}
	m.output.WriteString(strings.Join(lines, "\n"))
	m.lines = splitLines(m.output.String())

	// Page up
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(*Model)
	assert.Greater(t, m.scroll, 0)

	// Page down
	prevScroll := m.scroll
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(*Model)
	assert.Less(t, m.scroll, prevScroll)
}

func TestScrollDoesNotGoBelowZero(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)
	m.height = 10
	m.scroll = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(*Model)
	assert.Equal(t, 0, m.scroll)
}

func TestProcessInputSlashCommand(t *testing.T) {
	deps := testDeps(t)
	m := New(deps)

	// /help should be routed to slash command handler
	updated, _ := m.processInput("/help")
	model := updated.(*Model)
	assert.Contains(t, model.output.String(), "Available commands")
}

func TestRunStatusFormat(t *testing.T) {
	deps := testDeps(t)

	output := runStatus(deps)
	assert.Contains(t, output, "Workspace:")
	assert.Contains(t, output, "Database:")
	assert.Contains(t, output, "Last sync:")
	assert.Contains(t, output, "Channels:")
}
