package sync

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgress(t *testing.T) {
	p := NewProgress()
	require.NotNil(t, p)
	assert.Equal(t, SyncPhase(0), p.Phase())
}

func TestSetPhase(t *testing.T) {
	p := NewProgress()

	p.SetPhase(PhaseMetadata)
	assert.Equal(t, PhaseMetadata, p.Phase())

	p.SetPhase(PhaseMessages)
	assert.Equal(t, PhaseMessages, p.Phase())

	p.SetPhase(PhaseThreads)
	assert.Equal(t, PhaseThreads, p.Phase())

	p.SetPhase(PhaseDone)
	assert.Equal(t, PhaseDone, p.Phase())
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		phase SyncPhase
		want  string
	}{
		{PhaseMetadata, "Metadata"},
		{PhaseMessages, "Messages"},
		{PhaseThreads, "Threads"},
		{PhaseDone, "Done"},
		{SyncPhase(99), "Unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.phase.String())
	}
}

func TestMetadataProgress(t *testing.T) {
	p := NewProgress()
	p.SetPhase(PhaseMetadata)

	p.SetMetadataUsers(100, 50)
	p.SetMetadataChannels(200, 100)

	snap := p.Snapshot()
	assert.Equal(t, 100, snap.UsersTotal)
	assert.Equal(t, 50, snap.UsersDone)
	assert.Equal(t, 200, snap.ChannelsTotal)
	assert.Equal(t, 100, snap.ChannelsDone)
}

func TestMessageProgress(t *testing.T) {
	p := NewProgress()
	p.SetPhase(PhaseMessages)

	p.SetMessageChannels(50)
	p.AddMessages(100)
	p.AddMessages(200)
	p.IncMessageChannel()
	p.IncMessageChannel()
	p.SetCurrentChannel("general")

	snap := p.Snapshot()
	assert.Equal(t, 50, snap.MsgChannelsTotal)
	assert.Equal(t, 2, snap.MsgChannelsDone)
	assert.Equal(t, 300, snap.MessagesFetched)
	assert.Equal(t, "general", snap.CurrentChannel)
}

func TestThreadProgress(t *testing.T) {
	p := NewProgress()
	p.SetPhase(PhaseThreads)

	p.SetThreadsTotal(30)
	p.IncThread(5)
	p.IncThread(3)

	snap := p.Snapshot()
	assert.Equal(t, 30, snap.ThreadsTotal)
	assert.Equal(t, 2, snap.ThreadsDone)
	assert.Equal(t, 8, snap.ThreadsFetched)
}

func TestConcurrentAccess(t *testing.T) {
	p := NewProgress()
	p.SetPhase(PhaseMessages)
	p.SetMessageChannels(100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.AddMessages(10)
			p.IncMessageChannel()
		}()
	}
	wg.Wait()

	snap := p.Snapshot()
	assert.Equal(t, 500, snap.MessagesFetched)
	assert.Equal(t, 50, snap.MsgChannelsDone)
}

func TestRenderSnapshotMetadataPhase(t *testing.T) {
	snap := Snapshot{
		Phase:         PhaseMetadata,
		UsersTotal:    1205,
		UsersDone:     600,
		ChannelsTotal: 342,
		ChannelsDone:  170,
	}
	output := RenderSnapshot(snap, "my-company")

	assert.Contains(t, output, "my-company")
	assert.Contains(t, output, "users 600/1,205")
	assert.Contains(t, output, "channels 170/342")
	assert.Contains(t, output, "Messages")
	assert.Contains(t, output, "waiting...")
}

func TestRenderSnapshotMessagesPhase(t *testing.T) {
	snap := Snapshot{
		Phase:            PhaseMessages,
		UsersTotal:       1205,
		UsersDone:        1205,
		ChannelsTotal:    342,
		ChannelsDone:     342,
		MsgChannelsTotal: 342,
		MsgChannelsDone:  230,
		MessagesFetched:  45230,
		PhaseStartTime:   time.Now().Add(-30 * time.Second),
	}
	output := RenderSnapshot(snap, "my-company")

	// Metadata should show done
	assert.Contains(t, output, "done")
	// Messages should show progress
	assert.Contains(t, output, "230/342 channels")
	assert.Contains(t, output, "45,230 msgs")
	assert.Contains(t, output, "67%")
	// Progress bar should have filled and empty segments
	assert.Contains(t, output, "█")
	assert.Contains(t, output, "░")
	// Threads should be waiting
	assert.Contains(t, output, "waiting...")
}

func TestRenderSnapshotThreadsPhase(t *testing.T) {
	snap := Snapshot{
		Phase:            PhaseThreads,
		UsersTotal:       100,
		UsersDone:        100,
		ChannelsTotal:    50,
		ChannelsDone:     50,
		MsgChannelsTotal: 50,
		MsgChannelsDone:  50,
		MessagesFetched:  5000,
		ThreadsTotal:     200,
		ThreadsDone:      100,
		ThreadsFetched:   800,
		PhaseStartTime:   time.Now().Add(-10 * time.Second),
	}
	output := RenderSnapshot(snap, "test-ws")

	// Messages should show done
	assert.Contains(t, output, "5,000 msgs done")
	// Threads should show progress
	assert.Contains(t, output, "100/200 threads")
	assert.Contains(t, output, "800 replies")
	assert.Contains(t, output, "50%")
}

func TestRenderSnapshotDonePhase(t *testing.T) {
	snap := Snapshot{
		Phase:            PhaseDone,
		UsersTotal:       100,
		UsersDone:        100,
		ChannelsTotal:    50,
		ChannelsDone:     50,
		MsgChannelsTotal: 50,
		MsgChannelsDone:  50,
		MessagesFetched:  5000,
		ThreadsTotal:     200,
		ThreadsDone:      200,
		ThreadsFetched:   1600,
	}
	output := RenderSnapshot(snap, "test-ws")

	// All phases should show done
	assert.Contains(t, output, "done")
	assert.Contains(t, output, "5,000 msgs done")
	assert.Contains(t, output, "1,600 replies done")
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		done  int
		total int
		want  string
	}{
		{0, 100, "[" + strings.Repeat("░", 24) + "]"},
		{50, 100, "[" + strings.Repeat("█", 12) + strings.Repeat("░", 12) + "]"},
		{100, 100, "[" + strings.Repeat("█", 24) + "]"},
		{0, 0, "[" + strings.Repeat("░", 24) + "]"},
	}
	for _, tt := range tests {
		got := progressBar(tt.done, tt.total)
		assert.Equal(t, tt.want, got, "progressBar(%d, %d)", tt.done, tt.total)
	}
}

func TestPercentage(t *testing.T) {
	tests := []struct {
		done, total int
		want        string
	}{
		{0, 100, "0%"},
		{50, 100, "50%"},
		{100, 100, "100%"},
		{33, 100, "33%"},
		{0, 0, "0%"},
	}
	for _, tt := range tests {
		got := percentage(tt.done, tt.total)
		assert.Equal(t, tt.want, got)
	}
}

func TestEstimateETA(t *testing.T) {
	// No work done yet - no ETA
	assert.Equal(t, "", estimateETA(0, 100, time.Now().Add(-10*time.Second)))

	// All done - no ETA
	assert.Equal(t, "", estimateETA(100, 100, time.Now().Add(-10*time.Second)))

	// Too early (less than 1 second elapsed) - no ETA
	assert.Equal(t, "", estimateETA(10, 100, time.Now()))

	// Normal case: 50 done out of 100 in 10 seconds = ~10s remaining
	eta := estimateETA(50, 100, time.Now().Add(-10*time.Second))
	assert.Contains(t, eta, "~")
	assert.Contains(t, eta, "left")

	// Longer ETA should include minutes
	eta = estimateETA(1, 100, time.Now().Add(-10*time.Second))
	assert.Contains(t, eta, "m")
}

func TestSetPhaseResetsTimer(t *testing.T) {
	p := NewProgress()

	p.SetPhase(PhaseMetadata)
	snap1 := p.Snapshot()

	time.Sleep(10 * time.Millisecond)
	p.SetPhase(PhaseMessages)
	snap2 := p.Snapshot()

	// The phase start time should be later for the second phase
	assert.True(t, snap2.PhaseStartTime.After(snap1.PhaseStartTime))
}
