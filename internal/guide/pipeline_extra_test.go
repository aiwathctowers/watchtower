package guide

import (
	"testing"

	"watchtower/internal/prompts"

	"github.com/stretchr/testify/assert"
)

func TestPipeline_SetPromptStore_Assigns(t *testing.T) {
	p := &Pipeline{}
	store := &prompts.Store{}
	p.SetPromptStore(store)
	assert.Same(t, store, p.promptStore)
}

func TestPipeline_AccumulatedUsage_ZeroByDefault(t *testing.T) {
	p := &Pipeline{}
	in, out, cost, total := p.AccumulatedUsage()
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, float64(0), cost)
	assert.Equal(t, 0, total)
}

func TestPipeline_AccumulatedUsage_AfterUpdate(t *testing.T) {
	p := &Pipeline{}
	p.totalInputTokens.Store(10)
	p.totalOutputTokens.Store(20)
	p.totalAPITokens.Store(30)

	in, out, cost, total := p.AccumulatedUsage()
	assert.Equal(t, 10, in)
	assert.Equal(t, 20, out)
	assert.Equal(t, float64(0), cost)
	assert.Equal(t, 30, total)
}

func TestChannelName_FromMap(t *testing.T) {
	p := &Pipeline{
		channelNames: map[string]string{
			"C1": "general",
			"C2": "ops-fire",
		},
	}
	assert.Equal(t, "general", p.channelName("C1"))
	assert.Equal(t, "ops-fire", p.channelName("C2"))
}

func TestChannelName_FallsBackToID(t *testing.T) {
	p := &Pipeline{
		channelNames: map[string]string{"C1": "general"},
	}
	assert.Equal(t, "C99", p.channelName("C99"))
}

func TestChannelName_NilMapFallsBackToID(t *testing.T) {
	p := &Pipeline{}
	assert.Equal(t, "C1", p.channelName("C1"))
}

func TestUserName_FromMap(t *testing.T) {
	p := &Pipeline{
		userNames: map[string]string{
			"U1": "Alice",
		},
	}
	assert.Equal(t, "Alice", p.userName("U1"))
	assert.Equal(t, "U99", p.userName("U99"))
}
