package inbox

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
	p := &Pipeline{
		totalInputTokens:  10,
		totalOutputTokens: 20,
		totalAPITokens:    30,
	}
	in, out, cost, total := p.AccumulatedUsage()
	assert.Equal(t, 10, in)
	assert.Equal(t, 20, out)
	assert.Equal(t, float64(0), cost)
	assert.Equal(t, 30, total)
}
