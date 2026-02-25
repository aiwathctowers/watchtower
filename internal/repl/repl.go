package repl

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Message types for the bubbletea update loop.

type streamChunkMsg struct{ text string }
type streamDoneMsg struct{ sources string }
type streamErrMsg struct{ err error }
type commandResultMsg struct{ output string }

// Deps holds the shared dependencies for the REPL session.
type Deps struct {
	Config    *config.Config
	DB        *db.DB
	Domain    string
	Workspace string
}

// Model is the bubbletea model for the REPL.
type Model struct {
	deps    Deps
	program *tea.Program // set after program creation, used for p.Send()

	input   string
	history []string
	histIdx int
	draft   string

	output strings.Builder
	lines  []string
	scroll int

	streaming bool
	quitting  bool

	width  int
	height int

	cancel context.CancelFunc
}

// New creates a new REPL model.
func New(deps Deps) *Model {
	return &Model{
		deps:    deps,
		histIdx: -1,
	}
}

// SetProgram stores the tea.Program reference for async messaging.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
}

var (
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	userStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case streamChunkMsg:
		m.output.WriteString(msg.text)
		m.lines = splitLines(m.output.String())
		m.scroll = 0
		return m, nil

	case streamDoneMsg:
		m.streaming = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		if msg.sources != "" {
			m.output.WriteString("\n\n" + msg.sources)
		}
		m.output.WriteString("\n")
		m.lines = splitLines(m.output.String())
		m.scroll = 0
		return m, nil

	case streamErrMsg:
		m.streaming = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.output.WriteString("\n" + errorStyle.Render("Error: "+msg.err.Error()) + "\n")
		m.lines = splitLines(m.output.String())
		m.scroll = 0
		return m, nil

	case commandResultMsg:
		m.streaming = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.output.WriteString(msg.output + "\n")
		m.lines = splitLines(m.output.String())
		m.scroll = 0
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		if m.streaming && m.cancel != nil {
			m.cancel()
			m.streaming = false
			m.output.WriteString("\n" + dimStyle.Render("(cancelled)") + "\n")
			m.lines = splitLines(m.output.String())
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		if m.streaming {
			return m, nil
		}
		input := strings.TrimSpace(m.input)
		if input == "" {
			return m, nil
		}

		m.history = append(m.history, input)
		m.histIdx = -1
		m.draft = ""

		m.output.WriteString(userStyle.Render("> "+input) + "\n")
		m.lines = splitLines(m.output.String())
		m.input = ""

		return m.processInput(input)

	case tea.KeyUp:
		if len(m.history) == 0 {
			return m, nil
		}
		if m.histIdx == -1 {
			m.draft = m.input
			m.histIdx = len(m.history) - 1
		} else if m.histIdx > 0 {
			m.histIdx--
		}
		m.input = m.history[m.histIdx]
		return m, nil

	case tea.KeyDown:
		if m.histIdx == -1 {
			return m, nil
		}
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
			m.input = m.history[m.histIdx]
		} else {
			m.histIdx = -1
			m.input = m.draft
		}
		return m, nil

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.input)
			m.input = m.input[:len(m.input)-size]
		}
		return m, nil

	case tea.KeyPgUp:
		outputH := m.outputHeight()
		if len(m.lines) > outputH {
			m.scroll += outputH / 2
			max := len(m.lines) - outputH
			if m.scroll > max {
				m.scroll = max
			}
		}
		return m, nil

	case tea.KeyPgDown:
		m.scroll -= m.outputHeight() / 2
		if m.scroll < 0 {
			m.scroll = 0
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		} else if msg.Type == tea.KeySpace {
			m.input += " "
		}
		return m, nil
	}
}

func (m *Model) processInput(input string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(input, "/") {
		return m.handleSlashCommand(input)
	}

	// AI query — start streaming in a goroutine
	m.streaming = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	p := m.program
	deps := m.deps
	question := input

	go func() {
		runAIQueryStreaming(ctx, p, deps, question)
	}()

	return m, nil
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	outputH := m.outputHeight()

	// Render visible output lines
	if len(m.lines) > 0 {
		start := len(m.lines) - outputH - m.scroll
		if start < 0 {
			start = 0
		}
		end := start + outputH
		if end > len(m.lines) {
			end = len(m.lines)
		}
		for i := start; i < end; i++ {
			b.WriteString(m.lines[i])
			b.WriteString("\n")
		}
	}

	// Pad remaining output area
	rendered := strings.Count(b.String(), "\n")
	for i := rendered; i < outputH; i++ {
		b.WriteString("\n")
	}

	// Status line
	if m.streaming {
		b.WriteString(dimStyle.Render("  thinking..."))
	} else {
		b.WriteString(dimStyle.Render("  /help for commands, Ctrl+C to quit"))
	}
	b.WriteString("\n")

	// Input line
	prompt := promptStyle.Render("watchtower> ")
	b.WriteString(prompt + m.input + "█")

	return b.String()
}

func (m *Model) outputHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 20
	}
	return h
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Run starts the REPL.
func Run(deps Deps) error {
	m := New(deps)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetProgram(p)

	_, err := p.Run()
	return err
}

// runAIQueryStreaming executes the full AI pipeline, sending chunks to the
// bubbletea program via p.Send() for live streaming.
func runAIQueryStreaming(ctx context.Context, p *tea.Program, deps Deps, question string) {
	if p == nil {
		return
	}

	cfg := deps.Config

	if cfg.AI.ApiKey == "" {
		p.Send(streamErrMsg{err: fmt.Errorf("Anthropic API key not configured — set ANTHROPIC_API_KEY or config ai.api_key")})
		return
	}

	pq := ai.Parse(question)

	ctxBuilder := ai.NewContextBuilder(deps.DB, cfg.AI.ContextBudget, deps.Domain)
	msgContext, err := ctxBuilder.Build(pq)
	if err != nil {
		p.Send(streamErrMsg{err: fmt.Errorf("building context: %w", err)})
		return
	}

	systemPrompt := ai.BuildSystemPrompt(deps.Workspace, deps.Domain)
	userMessage := ai.AssembleUserMessage(msgContext, question)

	aiClient := ai.NewClient(cfg.AI.ApiKey, cfg.AI.Model, cfg.AI.MaxTokens)
	textCh, errCh := aiClient.Query(ctx, systemPrompt, userMessage)

	var fullResponse strings.Builder
	for chunk := range textCh {
		fullResponse.WriteString(chunk)
		p.Send(streamChunkMsg{text: chunk})
	}

	if err := <-errCh; err != nil {
		p.Send(streamErrMsg{err: err})
		return
	}

	// Render for sources
	renderer := ai.NewResponseRenderer(deps.DB, deps.Domain)
	rendered, err := renderer.Render(fullResponse.String())
	sources := ""
	if err == nil {
		sources = ai.ExtractSourcesSection(rendered)
	}

	p.Send(streamDoneMsg{sources: sources})
}

