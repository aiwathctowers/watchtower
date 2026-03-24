// Package sync provides Slack workspace synchronization orchestration and message syncing.
package sync

import (
	"encoding/json"
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
	PhaseMetadata  SyncPhase = iota
	PhaseDiscovery           // search-based channel/user discovery
	PhaseMessages
	PhaseUsers // lazy user profile fetching
	PhaseThreads
	PhaseDone
)

func (p SyncPhase) String() string {
	switch p {
	case PhaseMetadata:
		return "Metadata"
	case PhaseDiscovery:
		return "Discovery"
	case PhaseMessages:
		return "Messages"
	case PhaseUsers:
		return "Users"
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

	startTime time.Time // when the sync started (for total elapsed)
	phase     SyncPhase

	// Metadata phase
	usersTotal    int
	usersDone     int
	channelsTotal int
	channelsDone  int

	// Discovery phase
	discoveryPages      int
	discoveryTotalPages int
	discoveryChannels   int
	discoveryUsers      int

	// Users phase (lazy profile fetch)
	userProfilesTotal int
	userProfilesDone  int

	// Messages phase
	msgChannelsTotal    int
	msgChannelsDone     int
	messagesFetched     int
	channelsSkippedInfo string // human-readable breakdown of skipped channels

	// Threads phase
	threadsTotal   int
	threadsDone    int
	threadsFetched int

	// Timing for ETA
	phaseStartTime time.Time
}

// NewProgress creates a new progress tracker.
func NewProgress() *Progress {
	now := time.Now()
	return &Progress{
		startTime:      now,
		phaseStartTime: now,
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

// SetDiscovery updates discovery phase progress.
func (p *Progress) SetDiscovery(pages, totalPages, channels, users int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.discoveryPages = pages
	p.discoveryTotalPages = totalPages
	p.discoveryChannels = channels
	p.discoveryUsers = users
}

// SetUserProfiles updates user profile fetch progress.
func (p *Progress) SetUserProfiles(total, done int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.userProfilesTotal = total
	p.userProfilesDone = done
}

// SetMessageChannels sets the total number of channels for message sync.
func (p *Progress) SetMessageChannels(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgChannelsTotal = total
}

// SetChannelsSkippedInfo sets a human-readable breakdown of skipped channels.
func (p *Progress) SetChannelsSkippedInfo(info string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.channelsSkippedInfo = info
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
	Phase               SyncPhase
	StartTime           time.Time
	UsersTotal          int
	UsersDone           int
	ChannelsTotal       int
	ChannelsDone        int
	DiscoveryPages      int
	DiscoveryTotalPages int
	DiscoveryChannels   int
	DiscoveryUsers      int
	UserProfilesTotal   int
	UserProfilesDone    int
	MsgChannelsTotal    int
	MsgChannelsDone     int
	MessagesFetched     int
	ThreadsTotal        int
	ThreadsDone         int
	ThreadsFetched      int
	PhaseStartTime      time.Time
	ChannelsSkippedInfo string
}

// Snapshot takes a consistent snapshot of the progress state.
func (p *Progress) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		Phase:               p.phase,
		StartTime:           p.startTime,
		UsersTotal:          p.usersTotal,
		UsersDone:           p.usersDone,
		ChannelsTotal:       p.channelsTotal,
		ChannelsDone:        p.channelsDone,
		DiscoveryPages:      p.discoveryPages,
		DiscoveryTotalPages: p.discoveryTotalPages,
		DiscoveryChannels:   p.discoveryChannels,
		DiscoveryUsers:      p.discoveryUsers,
		UserProfilesTotal:   p.userProfilesTotal,
		UserProfilesDone:    p.userProfilesDone,
		MsgChannelsTotal:    p.msgChannelsTotal,
		MsgChannelsDone:     p.msgChannelsDone,
		MessagesFetched:     p.messagesFetched,
		ThreadsTotal:        p.threadsTotal,
		ThreadsDone:         p.threadsDone,
		ThreadsFetched:      p.threadsFetched,
		PhaseStartTime:      p.phaseStartTime,
		ChannelsSkippedInfo: p.channelsSkippedInfo,
	}
}

// JSONSnapshot is a JSON-serializable progress snapshot for external consumers (e.g. desktop app).
type JSONSnapshot struct {
	Phase               string  `json:"phase"`
	ElapsedSec          float64 `json:"elapsed_sec"`
	DiscoveryPages      int     `json:"discovery_pages"`
	DiscoveryTotalPages int     `json:"discovery_total_pages"`
	DiscoveryChannels   int     `json:"discovery_channels"`
	DiscoveryUsers      int     `json:"discovery_users"`
	MessagesFetched     int     `json:"messages_fetched"`
	MsgChannelsDone     int     `json:"msg_channels_done"`
	MsgChannelsTotal    int     `json:"msg_channels_total"`
	UserProfilesDone    int     `json:"user_profiles_done"`
	UserProfilesTotal   int     `json:"user_profiles_total"`
	ThreadsDone         int     `json:"threads_done"`
	ThreadsTotal        int     `json:"threads_total"`
	ThreadsFetched      int     `json:"threads_fetched"`
}

// JSON returns a JSON-encoded progress line.
func (p *Progress) JSON() []byte {
	snap := p.Snapshot()
	j := JSONSnapshot{
		Phase:               snap.Phase.String(),
		ElapsedSec:          time.Since(snap.StartTime).Seconds(),
		DiscoveryPages:      snap.DiscoveryPages,
		DiscoveryTotalPages: snap.DiscoveryTotalPages,
		DiscoveryChannels:   snap.DiscoveryChannels,
		DiscoveryUsers:      snap.DiscoveryUsers,
		MessagesFetched:     snap.MessagesFetched,
		MsgChannelsDone:     snap.MsgChannelsDone,
		MsgChannelsTotal:    snap.MsgChannelsTotal,
		UserProfilesDone:    snap.UserProfilesDone,
		UserProfilesTotal:   snap.UserProfilesTotal,
		ThreadsDone:         snap.ThreadsDone,
		ThreadsTotal:        snap.ThreadsTotal,
		ThreadsFetched:      snap.ThreadsFetched,
	}
	data, _ := json.Marshal(j)
	return data
}

// Styles for terminal rendering.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	doneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	waitStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Render formats the current progress for terminal display.
func (p *Progress) Render(workspace string) string {
	snap := p.Snapshot()
	return RenderSnapshot(snap, workspace)
}

// RenderSnapshot formats a snapshot for terminal display.
// Separated from Render for testability.
func RenderSnapshot(snap Snapshot, workspace string) string {
	var title string
	if snap.Phase == PhaseDone {
		elapsed := formatDuration(time.Since(snap.StartTime))
		title = doneStyle.Render(fmt.Sprintf("Synced %s workspace in %s", workspace, elapsed))
	} else {
		elapsed := formatDuration(time.Since(snap.StartTime))
		title = titleStyle.Render(fmt.Sprintf("Syncing %s workspace... (%s)", workspace, elapsed))
	}
	var lines []string
	lines = append(lines, title)

	// Metadata: show when active or done with data
	if snap.Phase == PhaseMetadata || (snap.Phase > PhaseMetadata && (snap.UsersTotal > 0 || snap.ChannelsTotal > 0)) {
		lines = append(lines, renderMetadata(snap))
	}

	// Discovery: show when active or done with results
	if snap.Phase == PhaseDiscovery || (snap.Phase > PhaseDiscovery && snap.DiscoveryPages > 0) {
		lines = append(lines, renderDiscovery(snap))
	}

	// Messages: show when active or done with results
	if snap.Phase == PhaseMessages || (snap.Phase > PhaseMessages && snap.MsgChannelsTotal > 0) {
		lines = append(lines, renderMessages(snap))
	}

	// Users: show when active or done with results
	if snap.Phase == PhaseUsers || (snap.Phase > PhaseUsers && snap.UserProfilesTotal > 0) {
		lines = append(lines, renderUsers(snap))
	}

	// Threads: show when active or done with results
	if snap.Phase == PhaseThreads || (snap.Phase > PhaseThreads && snap.ThreadsTotal > 0) {
		lines = append(lines, renderThreads(snap))
	}

	return strings.Join(lines, "\n")
}

// formatDuration formats a duration as a human-friendly string like "1m23s" or "5s".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

func renderMetadata(snap Snapshot) string {
	prefix := "  Metadata: "

	if snap.Phase > PhaseMetadata {
		users := fmt.Sprintf("users %s", humanize.Comma(int64(snap.UsersTotal)))
		channels := fmt.Sprintf("channels %s", humanize.Comma(int64(snap.ChannelsTotal)))
		return labelStyle.Render(prefix) + doneStyle.Render(fmt.Sprintf("%s, %s done", users, channels))
	}

	// Fetching users from Slack API (no saves started yet)
	if snap.UsersDone == 0 && snap.ChannelsDone == 0 {
		parts := []string{}
		if snap.UsersTotal > 0 {
			parts = append(parts, fmt.Sprintf("%s users", humanize.Comma(int64(snap.UsersTotal))))
		}
		if snap.ChannelsTotal > 0 {
			parts = append(parts, fmt.Sprintf("%s channels", humanize.Comma(int64(snap.ChannelsTotal))))
		}
		if len(parts) == 0 {
			return labelStyle.Render(prefix) + activeStyle.Render("fetching from Slack...")
		}
		return labelStyle.Render(prefix) + activeStyle.Render(fmt.Sprintf("fetched %s, saving...", strings.Join(parts, ", ")))
	}

	// Users saved, channels still fetching from API
	if snap.UsersDone > 0 && snap.UsersDone == snap.UsersTotal && snap.ChannelsDone == 0 {
		info := fmt.Sprintf("%s users saved", humanize.Comma(int64(snap.UsersTotal)))
		if snap.ChannelsTotal > 0 {
			info += fmt.Sprintf(", fetched %s channels, saving...", humanize.Comma(int64(snap.ChannelsTotal)))
		} else {
			info += ", fetching channels..."
		}
		return labelStyle.Render(prefix) + activeStyle.Render(info)
	}

	// Saving to DB (users saving, or channels saving)
	total := snap.UsersTotal + snap.ChannelsTotal
	done := snap.UsersDone + snap.ChannelsDone
	bar := progressBar(done, total)
	pct := percentage(done, total)
	return labelStyle.Render(prefix) + activeStyle.Render(fmt.Sprintf("%s %s", bar, pct))
}

func renderDiscovery(snap Snapshot) string {
	prefix := "  Search:   "
	switch {
	case snap.Phase < PhaseDiscovery:
		return labelStyle.Render(prefix) + waitStyle.Render("waiting...")
	case snap.Phase == PhaseDiscovery:
		if snap.DiscoveryPages == 0 {
			return labelStyle.Render(prefix) + activeStyle.Render("searching...")
		}
		bar := progressBar(snap.DiscoveryPages, snap.DiscoveryTotalPages)
		pct := percentage(snap.DiscoveryPages, snap.DiscoveryTotalPages)
		detail := fmt.Sprintf("%s (%s msgs, %s ch, page %s/%s)",
			pct,
			humanize.Comma(int64(snap.MessagesFetched)),
			humanize.Comma(int64(snap.DiscoveryChannels)),
			humanize.Comma(int64(snap.DiscoveryPages)),
			humanize.Comma(int64(snap.DiscoveryTotalPages)),
		)
		eta := estimateETA(snap.DiscoveryPages, snap.DiscoveryTotalPages, snap.PhaseStartTime)
		line := fmt.Sprintf("%s %s", bar, detail)
		if eta != "" {
			line += " " + eta
		}
		return labelStyle.Render(prefix) + activeStyle.Render(line)
	default:
		if snap.DiscoveryChannels == 0 && snap.DiscoveryPages == 0 {
			return labelStyle.Render(prefix) + waitStyle.Render("skipped")
		}
		detail := fmt.Sprintf("%s msgs, %s channels, %s users",
			humanize.Comma(int64(snap.MessagesFetched)),
			humanize.Comma(int64(snap.DiscoveryChannels)),
			humanize.Comma(int64(snap.DiscoveryUsers)),
		)
		return labelStyle.Render(prefix) + doneStyle.Render(detail)
	}
}

func renderUsers(snap Snapshot) string {
	prefix := "  Users:    "
	switch {
	case snap.Phase < PhaseUsers:
		return labelStyle.Render(prefix) + waitStyle.Render("waiting...")
	case snap.Phase == PhaseUsers:
		if snap.UserProfilesTotal == 0 {
			return labelStyle.Render(prefix) + activeStyle.Render("checking...")
		}
		bar := progressBar(snap.UserProfilesDone, snap.UserProfilesTotal)
		pct := percentage(snap.UserProfilesDone, snap.UserProfilesTotal)
		detail := fmt.Sprintf("%s (%s/%s profiles)",
			pct,
			humanize.Comma(int64(snap.UserProfilesDone)),
			humanize.Comma(int64(snap.UserProfilesTotal)),
		)
		return labelStyle.Render(prefix) + activeStyle.Render(fmt.Sprintf("%s %s", bar, detail))
	default:
		if snap.UserProfilesTotal == 0 {
			return labelStyle.Render(prefix) + doneStyle.Render("all known")
		}
		detail := fmt.Sprintf("%s profiles fetched",
			humanize.Comma(int64(snap.UserProfilesDone)),
		)
		return labelStyle.Render(prefix) + doneStyle.Render(detail)
	}
}

func renderMessages(snap Snapshot) string {
	prefix := "  Messages: "
	skipped := ""
	if snap.ChannelsSkippedInfo != "" {
		skipped = "\n" + labelStyle.Render("             ") + waitStyle.Render(snap.ChannelsSkippedInfo)
	}
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
		return labelStyle.Render(prefix) + activeStyle.Render(line) + skipped
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
