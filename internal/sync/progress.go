package sync

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
)

// SyncPhase tracks which phase of the sync pipeline is active.
type SyncPhase int

const (
	PhaseMetadata SyncPhase = iota
	PhaseMessages
	PhaseThreads
	PhaseDone
)

func (p SyncPhase) String() string {
	switch p {
	case PhaseMetadata:
		return "Metadata"
	case PhaseMessages:
		return "Messages"
	case PhaseThreads:
		return "Threads"
	case PhaseDone:
		return "Done"
	default:
		return "Unknown"
	}
}

// Progress tracks the state of a sync run across all phases.
// It is safe for concurrent use.
type Progress struct {
	mu sync.Mutex

	phase SyncPhase

	// Metadata phase
	usersTotal    int
	usersDone     int
	channelsTotal int
	channelsDone  int

	// Messages phase
	msgChannelsTotal int
	msgChannelsDone  int
	messagesFetched  int
	currentChannel   string

	// Threads phase
	threadsTotal   int
	threadsDone    int
	threadsFetched int

	// Timing for ETA
	phaseStartTime time.Time
}

// NewProgress creates a new progress tracker.
func NewProgress() *Progress {
	return &Progress{
		phaseStartTime: time.Now(),
	}
}

// SetPhase transitions to a new sync phase and resets the phase timer.
func (p *Progress) SetPhase(phase SyncPhase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = phase
	p.phaseStartTime = time.Now()
}

// Phase returns the current sync phase.
func (p *Progress) Phase() SyncPhase {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase
}

// SetMetadataUsers updates user sync progress.
func (p *Progress) SetMetadataUsers(total, done int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usersTotal = total
	p.usersDone = done
}

// SetMetadataChannels updates channel sync progress.
func (p *Progress) SetMetadataChannels(total, done int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.channelsTotal = total
	p.channelsDone = done
}

// SetMessageChannels sets the total number of channels for message sync.
func (p *Progress) SetMessageChannels(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgChannelsTotal = total
}

// IncMessageChannel increments the completed channels count for message sync.
func (p *Progress) IncMessageChannel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgChannelsDone++
}

// AddMessages increments the total messages fetched count.
func (p *Progress) AddMessages(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messagesFetched += count
}

// SetCurrentChannel sets the name of the channel currently being synced.
func (p *Progress) SetCurrentChannel(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentChannel = name
}

// SetThreadsTotal sets the total number of threads to sync.
func (p *Progress) SetThreadsTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.threadsTotal = total
}

// IncThread increments the completed thread count and adds the reply count.
func (p *Progress) IncThread(replies int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.threadsDone++
	p.threadsFetched += replies
}

// Snapshot captures the current state for rendering without holding the lock.
type Snapshot struct {
	Phase            SyncPhase
	UsersTotal       int
	UsersDone        int
	ChannelsTotal    int
	ChannelsDone     int
	MsgChannelsTotal int
	MsgChannelsDone  int
	MessagesFetched  int
	CurrentChannel   string
	ThreadsTotal     int
	ThreadsDone      int
	ThreadsFetched   int
	PhaseStartTime   time.Time
}

// Snapshot takes a consistent snapshot of the progress state.
func (p *Progress) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		Phase:            p.phase,
		UsersTotal:       p.usersTotal,
		UsersDone:        p.usersDone,
		ChannelsTotal:    p.channelsTotal,
		ChannelsDone:     p.channelsDone,
		MsgChannelsTotal: p.msgChannelsTotal,
		MsgChannelsDone:  p.msgChannelsDone,
		MessagesFetched:  p.messagesFetched,
		CurrentChannel:   p.currentChannel,
		ThreadsTotal:     p.threadsTotal,
		ThreadsDone:      p.threadsDone,
		ThreadsFetched:   p.threadsFetched,
		PhaseStartTime:   p.phaseStartTime,
	}
}

// Styles for terminal rendering.
var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	doneStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	waitStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Render formats the current progress for terminal display.
func (p *Progress) Render(workspace string) string {
	snap := p.Snapshot()
	return RenderSnapshot(snap, workspace)
}

// RenderSnapshot formats a snapshot for terminal display.
// Separated from Render for testability.
func RenderSnapshot(snap Snapshot, workspace string) string {
	title := titleStyle.Render(fmt.Sprintf("Syncing %s workspace...", workspace))
	meta := renderMetadata(snap)
	msgs := renderMessages(snap)
	threads := renderThreads(snap)

	return fmt.Sprintf("%s\n%s\n%s\n%s", title, meta, msgs, threads)
}

func renderMetadata(snap Snapshot) string {
	prefix := "  Metadata: "
	switch {
	case snap.Phase == PhaseMetadata:
		users := fmt.Sprintf("users %s/%s", humanize.Comma(int64(snap.UsersDone)), humanize.Comma(int64(snap.UsersTotal)))
		channels := fmt.Sprintf("channels %s/%s", humanize.Comma(int64(snap.ChannelsDone)), humanize.Comma(int64(snap.ChannelsTotal)))
		return labelStyle.Render(prefix) + activeStyle.Render(fmt.Sprintf("%s, %s", users, channels))
	default:
		users := fmt.Sprintf("users %s/%s", humanize.Comma(int64(snap.UsersDone)), humanize.Comma(int64(snap.UsersTotal)))
		channels := fmt.Sprintf("channels %s/%s", humanize.Comma(int64(snap.ChannelsDone)), humanize.Comma(int64(snap.ChannelsTotal)))
		return labelStyle.Render(prefix) + doneStyle.Render(fmt.Sprintf("%s, %s done", users, channels))
	}
}

func renderMessages(snap Snapshot) string {
	prefix := "  Messages: "
	switch {
	case snap.Phase < PhaseMessages:
		return labelStyle.Render(prefix) + waitStyle.Render("waiting...")
	case snap.Phase == PhaseMessages:
		bar := progressBar(snap.MsgChannelsDone, snap.MsgChannelsTotal)
		pct := percentage(snap.MsgChannelsDone, snap.MsgChannelsTotal)
		detail := fmt.Sprintf("%s (%s/%s channels, %s msgs)",
			pct,
			humanize.Comma(int64(snap.MsgChannelsDone)),
			humanize.Comma(int64(snap.MsgChannelsTotal)),
			humanize.Comma(int64(snap.MessagesFetched)),
		)
		eta := estimateETA(snap.MsgChannelsDone, snap.MsgChannelsTotal, snap.PhaseStartTime)
		line := fmt.Sprintf("%s %s", bar, detail)
		if eta != "" {
			line += " " + eta
		}
		return labelStyle.Render(prefix) + activeStyle.Render(line)
	default:
		detail := fmt.Sprintf("%s/%s channels, %s msgs done",
			humanize.Comma(int64(snap.MsgChannelsDone)),
			humanize.Comma(int64(snap.MsgChannelsTotal)),
			humanize.Comma(int64(snap.MessagesFetched)),
		)
		return labelStyle.Render(prefix) + doneStyle.Render(detail)
	}
}

func renderThreads(snap Snapshot) string {
	prefix := "  Threads:  "
	switch {
	case snap.Phase < PhaseThreads:
		return labelStyle.Render(prefix) + waitStyle.Render("waiting...")
	case snap.Phase == PhaseThreads:
		bar := progressBar(snap.ThreadsDone, snap.ThreadsTotal)
		pct := percentage(snap.ThreadsDone, snap.ThreadsTotal)
		detail := fmt.Sprintf("%s (%s/%s threads, %s replies)",
			pct,
			humanize.Comma(int64(snap.ThreadsDone)),
			humanize.Comma(int64(snap.ThreadsTotal)),
			humanize.Comma(int64(snap.ThreadsFetched)),
		)
		eta := estimateETA(snap.ThreadsDone, snap.ThreadsTotal, snap.PhaseStartTime)
		line := fmt.Sprintf("%s %s", bar, detail)
		if eta != "" {
			line += " " + eta
		}
		return labelStyle.Render(prefix) + activeStyle.Render(line)
	default:
		detail := fmt.Sprintf("%s/%s threads, %s replies done",
			humanize.Comma(int64(snap.ThreadsDone)),
			humanize.Comma(int64(snap.ThreadsTotal)),
			humanize.Comma(int64(snap.ThreadsFetched)),
		)
		return labelStyle.Render(prefix) + doneStyle.Render(detail)
	}
}

// progressBar renders a text-based progress bar: [████████░░░░░░░░]
func progressBar(done, total int) string {
	const barWidth = 24
	if total <= 0 {
		return "[" + strings.Repeat("░", barWidth) + "]"
	}
	filled := (done * barWidth) / total
	if filled > barWidth {
		filled = barWidth
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
}

func percentage(done, total int) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (done*100)/total)
}

// estimateETA calculates remaining time based on throughput so far.
func estimateETA(done, total int, startTime time.Time) string {
	if done <= 0 || total <= 0 || done >= total {
		return ""
	}
	elapsed := time.Since(startTime)
	if elapsed < time.Second {
		return ""
	}
	rate := float64(done) / elapsed.Seconds()
	remaining := float64(total-done) / rate
	if remaining < 1 {
		return ""
	}
	eta := time.Duration(remaining * float64(time.Second))
	if eta < time.Minute {
		return fmt.Sprintf("~%ds left", int(eta.Seconds()))
	}
	return fmt.Sprintf("~%dm%ds left", int(eta.Minutes()), int(eta.Seconds())%60)
}
