// Package ui provides UI components and formatting utilities for CLI output.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	doneIcon     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓")
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows an animated spinner with a status message.
type Spinner struct {
	w       io.Writer
	mu      sync.Mutex
	status  string
	done    int
	total   int
	stopped bool
	stopCh  chan struct{}
	started time.Time
	isTTY   bool
}

// NewSpinner creates and starts a spinner writing to w.
func NewSpinner(w io.Writer, status string) *Spinner {
	isTTY := false
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		isTTY = true
	}
	s := &Spinner{
		w:       w,
		status:  status,
		stopCh:  make(chan struct{}),
		started: time.Now(),
		isTTY:   isTTY,
	}
	if isTTY {
		go s.run()
	}
	return s
}

// Update changes the spinner status message (no progress bar).
func (s *Spinner) Update(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// UpdateProgress changes the spinner status with progress info.
func (s *Spinner) UpdateProgress(done, total int, status string) {
	s.mu.Lock()
	s.done = done
	s.total = total
	s.status = status
	s.mu.Unlock()
}

// Stop stops the spinner and shows a final done message.
func (s *Spinner) Stop(message string) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	close(s.stopCh)

	elapsed := time.Since(s.started).Round(time.Second)
	s.clearLine()
	fmt.Fprintf(s.w, "%s %s (%s)\n", doneIcon, message, elapsed)
}

func (s *Spinner) run() {
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	i := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-tick.C:
			s.mu.Lock()
			status := s.status
			done := s.done
			total := s.total
			s.mu.Unlock()

			elapsed := time.Since(s.started).Round(time.Second)
			frame := spinnerStyle.Render(frames[i%len(frames)])
			s.clearLine()

			if total > 0 {
				bar := progressBar(done, total)
				eta := estimateETA(done, total, s.started)
				pct := fmt.Sprintf("%d/%d", done, total)
				if eta != "" {
					fmt.Fprintf(s.w, "%s %s %s %s %s %s", frame, bar, pct, status, dimStyle.Render(eta), dimStyle.Render(elapsed.String()))
				} else {
					fmt.Fprintf(s.w, "%s %s %s %s %s", frame, bar, pct, status, dimStyle.Render(elapsed.String()))
				}
			} else {
				fmt.Fprintf(s.w, "%s %s %s", frame, status, dimStyle.Render(elapsed.String()))
			}
			i++
		}
	}
}

func (s *Spinner) clearLine() {
	if f, ok := s.w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(s.w, "\r\033[K")
	}
}

func progressBar(done, total int) string {
	const barWidth = 20
	if total <= 0 {
		return "[" + strings.Repeat("░", barWidth) + "]"
	}
	filled := (done * barWidth) / total
	if filled > barWidth {
		filled = barWidth
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
}

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
