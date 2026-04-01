package ai

import "context"

// Provider is the interface for AI query clients (both streaming and sync).
// ai.Client (Claude) and codex.Client both implement this interface.
type Provider interface {
	Query(ctx context.Context, systemPrompt, userMessage, sessionID string) (<-chan string, <-chan error, <-chan string)
	QuerySync(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *Usage, error)
}
