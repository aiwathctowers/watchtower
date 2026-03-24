// Package repl provides an interactive REPL for Claude interaction with workspace data.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Deps holds the shared dependencies for the REPL session.
type Deps struct {
	Config    *config.Config
	DB        *db.DB
	DBPath    string
	Domain    string
	TeamID    string
	Workspace string
}

// REPL is a simple line-oriented read-eval-print loop. No alternate screen,
// no TUI framework — just stdin/stdout and the terminal's native scrollback.
type REPL struct {
	deps         Deps
	sessionID    string
	ctx          context.Context
	cancel       context.CancelFunc
	streaming    atomic.Bool
	mu           sync.Mutex // protects streamCancel
	streamCancel context.CancelFunc
}

// setStreamCancel atomically sets the stream cancel function.
func (r *REPL) setStreamCancel(fn context.CancelFunc) {
	r.mu.Lock()
	r.streamCancel = fn
	r.mu.Unlock()
}

// getStreamCancel atomically gets the stream cancel function.
func (r *REPL) getStreamCancel() context.CancelFunc {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.streamCancel
}

var (
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// Run starts the REPL. Requires an interactive terminal.
func Run(deps Deps) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("watchtower requires an interactive terminal; use a subcommand (e.g., watchtower sync)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &REPL{
		deps:   deps,
		ctx:    ctx,
		cancel: cancel,
	}

	// Ctrl+C: cancel stream if streaming, cancel REPL context if idle.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh) // unblock the goroutine's for-range loop
	}()
	go func() {
		for range sigCh {
			if cancelFn := r.getStreamCancel(); cancelFn != nil && r.streaming.Load() {
				cancelFn()
				fmt.Print("\n" + dimStyle.Render("(cancelled)") + "\n")
			} else {
				fmt.Println()
				cancel() // cancel the REPL context so defers run properly
				return
			}
		}
	}()

	fmt.Println(dimStyle.Render("Type /help for commands, Ctrl+C to quit"))
	fmt.Println()

	return r.loop(os.Stdin)
}

// loop reads from the given reader line by line, dispatching commands until
// the reader is exhausted or the context is cancelled.
func (r *REPL) loop(input io.Reader) error {
	scanner := bufio.NewScanner(input)
	for {
		select {
		case <-r.ctx.Done():
			return nil
		default:
		}
		fmt.Print(promptStyle.Render("watchtower> "))
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		r.processInput(line)
	}

	return scanner.Err()
}

func (r *REPL) processInput(input string) {
	if strings.HasPrefix(input, "/") {
		r.handleSlashCommand(input)
		return
	}
	r.runAIQuery(input)
}

// runAIQuery executes the AI pipeline: accumulates the response, renders
// markdown via glamour, and prints the formatted result.
// When sessionID is set, resumes the existing Claude session for multi-turn.
func (r *REPL) runAIQuery(question string) {
	cfg := r.deps.Config

	pq := ai.Parse(question)
	timeHints := ai.FormatTimeHints(pq)

	var systemPrompt string
	if r.sessionID == "" {
		systemPrompt = ai.BuildSystemPrompt(r.deps.Workspace, r.deps.Domain, r.deps.TeamID, r.deps.DBPath, db.Schema, cfg.Digest.Language)
	}
	userMessage := ai.AssembleUserMessage(question, timeHints)

	streamCtx, streamCancel := context.WithCancel(r.ctx)
	defer streamCancel()
	r.setStreamCancel(streamCancel)
	r.streaming.Store(true)
	defer func() {
		r.streaming.Store(false)
		r.setStreamCancel(nil)
	}()

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	if isTTY {
		fmt.Print(dimStyle.Render("Thinking..."))
	}

	aiClient := ai.NewClient(cfg.AI.Model, r.deps.DBPath, cfg.ClaudePath)
	textCh, errCh, sidCh := aiClient.Query(streamCtx, systemPrompt, userMessage, r.sessionID)

	var fullResponse strings.Builder
	for chunk := range textCh {
		fullResponse.WriteString(chunk)
	}

	// Clear "Thinking..." line.
	if isTTY {
		fmt.Print("\r\033[K")
	}

	// Capture session ID for multi-turn.
	select {
	case newSID := <-sidCh:
		if newSID != "" {
			r.sessionID = newSID
		}
	default:
	}

	if err := <-errCh; err != nil {
		fmt.Println(errorStyle.Render("Error: " + err.Error()))
		return
	}

	// Render markdown + resolve sources, print formatted output.
	renderer := ai.NewResponseRenderer(r.deps.DB, r.deps.Domain, r.deps.TeamID)
	rendered, err := renderer.Render(fullResponse.String())
	if err != nil {
		rendered = fullResponse.String()
	}
	fmt.Print(rendered)
}
