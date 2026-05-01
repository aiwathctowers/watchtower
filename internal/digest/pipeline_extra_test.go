package digest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClaudeGenerator_Initialization(t *testing.T) {
	g := NewClaudeGenerator("claude-opus", "/opt/claude")
	assert.Equal(t, "claude-opus", g.model)
	assert.Equal(t, "/opt/claude", g.claudePath)
}

func TestPipeline_SetJiraKeyDetector_Assigns(t *testing.T) {
	p := &Pipeline{}
	detector := fakeDigestKeyDetector{}
	p.SetJiraKeyDetector(detector)
	assert.NotNil(t, p.jiraKeyDetector)
}

type fakeDigestKeyDetector struct{}

func (fakeDigestKeyDetector) ProcessDigestDecision(_ int, _ string, _ string) (int, error) {
	return 0, nil
}

func TestPipeline_AccumulatedStats_ZeroByDefault(t *testing.T) {
	p := &Pipeline{}
	count, from, to := p.AccumulatedStats()
	assert.Equal(t, 0, count)
	assert.Equal(t, float64(0), from)
	assert.Equal(t, float64(0), to)
}

func TestPipeline_AccumulatedStats_AfterUpdate(t *testing.T) {
	p := &Pipeline{}
	p.totalMessageCount.Store(15)
	p.earliestPeriodFrom.Store(100)
	p.latestPeriodTo.Store(500)

	count, from, to := p.AccumulatedStats()
	assert.Equal(t, 15, count)
	assert.Equal(t, float64(100), from)
	assert.Equal(t, float64(500), to)
}

func TestPipeline_AccumulatedUsage_AfterAccumulate(t *testing.T) {
	p := &Pipeline{}
	p.accumulateUsage(&Usage{InputTokens: 10, OutputTokens: 20, TotalAPITokens: 30})
	p.accumulateUsage(&Usage{InputTokens: 5, OutputTokens: 5, TotalAPITokens: 5})
	in, out, cost, total := p.AccumulatedUsage()
	assert.Equal(t, 15, in)
	assert.Equal(t, 25, out)
	assert.Equal(t, float64(0), cost)
	assert.Equal(t, 35, total)
}

func TestPipeline_AccumulateUsage_NilNoOp(t *testing.T) {
	p := &Pipeline{}
	p.accumulateUsage(nil)
	in, _, _, _ := p.AccumulatedUsage()
	assert.Equal(t, 0, in)
}
