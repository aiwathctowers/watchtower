package ai

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client wraps the Anthropic SDK for streaming Claude API queries.
type Client struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int
}

// NewClient creates a new AI client with the given API key, model, and max tokens.
func NewClient(apiKey, model string, maxTokens int) *Client {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &Client{
		client:    anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:     anthropic.Model(model),
		maxTokens: maxTokens,
	}
}

// NewClientWithOptions creates a client with custom SDK options (useful for testing).
func NewClientWithOptions(model string, maxTokens int, opts ...option.RequestOption) *Client {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &Client{
		client:    anthropic.NewClient(opts...),
		model:     anthropic.Model(model),
		maxTokens: maxTokens,
	}
}

// Query sends a streaming request to the Claude API and returns a channel that
// emits text chunks as they arrive. The channel is closed when the stream ends
// or an error occurs. Check the returned error channel for any errors after the
// text channel is closed.
func (c *Client) Query(ctx context.Context, systemPrompt, userMessage string) (<-chan string, <-chan error) {
	textCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)

		stream := c.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
			Model:     c.model,
			MaxTokens: int64(c.maxTokens),
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages: []anthropic.MessageParam{
				{
					Role: anthropic.MessageParamRoleUser,
					Content: []anthropic.ContentBlockParamUnion{
						{OfText: &anthropic.TextBlockParam{Text: userMessage}},
					},
				},
			},
		})
		defer stream.Close()

		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
				select {
				case textCh <- event.Delta.Text:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}

		if err := stream.Err(); err != nil {
			errCh <- classifyError(err)
		}
	}()

	return textCh, errCh
}

// QuerySync sends a non-streaming request to the Claude API and returns the
// full response text.
func (c *Client) QuerySync(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: int64(c.maxTokens),
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{OfText: &anthropic.TextBlockParam{Text: userMessage}},
				},
			},
		},
	})
	if err != nil {
		return "", classifyError(err)
	}

	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text, nil
}

// classifyError wraps API errors with user-friendly messages.
func classifyError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("ai request failed: %w", err)
	}

	switch apiErr.StatusCode {
	case 401:
		return fmt.Errorf("invalid Anthropic API key — check your ANTHROPIC_API_KEY or config ai.api_key: %w", err)
	case 429:
		return fmt.Errorf("Anthropic rate limit exceeded — please wait and try again: %w", err)
	case 400:
		return fmt.Errorf("invalid request to Claude API (context may be too long): %w", err)
	case 529:
		return fmt.Errorf("Anthropic API is overloaded — please try again shortly: %w", err)
	default:
		return fmt.Errorf("Claude API error (HTTP %d): %w", apiErr.StatusCode, err)
	}
}
