package repl

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/ai"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize"
)

// handleSlashCommand dispatches slash commands.
func (m *Model) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/quit", "/exit":
		m.quitting = true
		return m, tea.Quit

	case "/help":
		m.output.WriteString(helpText())
		m.lines = splitLines(m.output.String())
		return m, nil

	case "/status":
		m.streaming = true
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		p := m.program
		deps := m.deps
		go func() {
			output := runStatus(deps)
			select {
			case <-ctx.Done():
				return
			default:
			}
			p.Send(commandResultMsg{output: output})
		}()
		return m, nil

	case "/sync":
		m.streaming = true
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		p := m.program
		deps := m.deps
		go func() {
			output := runSyncCommand(ctx, deps)
			p.Send(commandResultMsg{output: output})
		}()
		return m, nil

	case "/catchup":
		m.streaming = true
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		p := m.program
		deps := m.deps
		go func() {
			runCatchupStreaming(ctx, p, deps)
		}()
		return m, nil

	default:
		m.output.WriteString(errorStyle.Render(fmt.Sprintf("Unknown command: %s", cmd)) + "\n")
		m.output.WriteString(dimStyle.Render("Type /help for available commands.") + "\n")
		m.lines = splitLines(m.output.String())
		return m, nil
	}
}

func helpText() string {
	var b strings.Builder
	b.WriteString("Available commands:\n")
	b.WriteString("  /sync      Trigger an incremental sync\n")
	b.WriteString("  /status    Show workspace stats\n")
	b.WriteString("  /catchup   Get a summary of recent activity\n")
	b.WriteString("  /help      Show this help message\n")
	b.WriteString("  /quit      Exit the REPL (also Ctrl+C)\n")
	b.WriteString("\n")
	b.WriteString("Or just type a question to ask about your Slack workspace.\n")
	return b.String()
}

func runStatus(deps Deps) string {
	database := deps.DB
	cfg := deps.Config

	ws, err := database.GetWorkspace()
	if err != nil {
		return errorStyle.Render("Error: " + err.Error())
	}

	stats, err := database.GetStats()
	if err != nil {
		return errorStyle.Render("Error: " + err.Error())
	}

	lastSync, err := database.LastSyncTime()
	if err != nil {
		return errorStyle.Render("Error: " + err.Error())
	}

	var b strings.Builder

	if ws != nil {
		b.WriteString(fmt.Sprintf("Workspace: %s (%s)\n", ws.Name, ws.ID))
	} else {
		b.WriteString(fmt.Sprintf("Workspace: %s (not yet synced)\n", cfg.ActiveWorkspace))
	}

	dbPath := cfg.DBPath()
	info, statErr := os.Stat(dbPath)
	var dbSize int64
	if statErr == nil {
		dbSize = info.Size()
	}
	b.WriteString(fmt.Sprintf("Database:  %s (%s)\n", dbPath, humanize.IBytes(uint64(dbSize))))

	if lastSync != "" {
		t, err := time.Parse("2006-01-02T15:04:05Z", lastSync)
		if err == nil {
			b.WriteString(fmt.Sprintf("Last sync: %s (%s)\n", lastSync, humanize.Time(t)))
		} else {
			b.WriteString(fmt.Sprintf("Last sync: %s\n", lastSync))
		}
	} else {
		b.WriteString("Last sync: never\n")
	}

	watchedStr := ""
	if stats.WatchedCount > 0 {
		watchedStr = fmt.Sprintf(" (%s watched)", humanize.Comma(int64(stats.WatchedCount)))
	}
	b.WriteString(fmt.Sprintf("Channels: %s%s | Users: %s | Messages: %s | Threads: %s\n",
		humanize.Comma(int64(stats.ChannelCount)),
		watchedStr,
		humanize.Comma(int64(stats.UserCount)),
		humanize.Comma(int64(stats.MessageCount)),
		humanize.Comma(int64(stats.ThreadCount)),
	))

	return b.String()
}

func runSyncCommand(ctx context.Context, deps Deps) string {
	cfg := deps.Config
	database := deps.DB

	ws, err := cfg.GetActiveWorkspace()
	if err != nil {
		return errorStyle.Render("Error: " + err.Error())
	}

	if ws.SlackToken == "" {
		return errorStyle.Render("Error: Slack token not configured. Run: watchtower config set workspaces.<name>.slack_token xoxb-...")
	}

	slackClient := watchtowerslack.NewClient(ws.SlackToken)
	orch := sync.NewOrchestrator(database, slackClient, cfg)
	orch.SetLogger(log.New(io.Discard, "", 0))

	opts := sync.SyncOptions{
		Workers: cfg.Sync.Workers,
	}

	if err := orch.Run(ctx, opts); err != nil {
		return errorStyle.Render("Sync failed: " + err.Error())
	}

	snap := orch.Progress().Snapshot()
	return fmt.Sprintf("Sync complete: %d messages, %d threads synced.",
		snap.MessagesFetched, snap.ThreadsFetched)
}

func runCatchupStreaming(ctx context.Context, p *tea.Program, deps Deps) {
	cfg := deps.Config
	database := deps.DB

	if cfg.AI.ApiKey == "" {
		p.Send(commandResultMsg{output: errorStyle.Render("Anthropic API key not configured — set ANTHROPIC_API_KEY or config ai.api_key")})
		return
	}

	sinceTime, err := database.DetermineSinceTime(0)
	if err != nil {
		p.Send(streamErrMsg{err: fmt.Errorf("determining catchup window: %w", err)})
		return
	}

	p.Send(streamChunkMsg{text: dimStyle.Render(fmt.Sprintf("Catching up since %s...", sinceTime.Format("2006-01-02 15:04 MST"))) + "\n\n"})

	now := time.Now()
	pq := ai.ParsedQuery{
		RawText: "What happened since I was last here? Summarize activity by channel, highlight decisions, action items, and anything unusual.",
		TimeRange: &ai.TimeRange{
			From: sinceTime,
			To:   now,
		},
		Intent: ai.IntentCatchup,
	}

	ctxBuilder := ai.NewContextBuilder(database, cfg.AI.ContextBudget, deps.Domain)
	msgContext, err := ctxBuilder.Build(pq)
	if err != nil {
		p.Send(streamErrMsg{err: fmt.Errorf("building context: %w", err)})
		return
	}

	if strings.TrimSpace(msgContext) == "" {
		_ = database.UpdateCheckpoint(now)
		p.Send(commandResultMsg{output: "No new activity found since your last catchup."})
		return
	}

	systemPrompt := ai.BuildSystemPrompt(deps.Workspace, deps.Domain)
	question := "What happened since I was last here? Give me a structured catchup summary."
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

	renderer := ai.NewResponseRenderer(database, deps.Domain)
	rendered, err := renderer.Render(fullResponse.String())
	sources := ""
	if err == nil {
		sources = ai.ExtractSourcesSection(rendered)
	}

	_ = database.UpdateCheckpoint(now)

	p.Send(streamDoneMsg{sources: sources})
}

