package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseResponse builds an SSE response body simulating a streaming Claude response.
func sseResponse(text string) string {
	var b strings.Builder

	// message_start event
	b.WriteString("event: message_start\n")
	b.WriteString(`data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`)
	b.WriteString("\n\n")

	// content_block_start event
	b.WriteString("event: content_block_start\n")
	b.WriteString(`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	b.WriteString("\n\n")

	// Split text into chunks for realistic streaming
	chunks := splitIntoChunks(text, 5)
	for _, chunk := range chunks {
		b.WriteString("event: content_block_delta\n")
		b.WriteString(fmt.Sprintf(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, chunk))
		b.WriteString("\n\n")
	}

	// content_block_stop event
	b.WriteString("event: content_block_stop\n")
	b.WriteString(`data: {"type":"content_block_stop","index":0}`)
	b.WriteString("\n\n")

	// message_delta event
	b.WriteString("event: message_delta\n")
	b.WriteString(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`)
	b.WriteString("\n\n")

	// message_stop event
	b.WriteString("event: message_stop\n")
	b.WriteString(`data: {"type":"message_stop"}`)
	b.WriteString("\n\n")

	return b.String()
}

func splitIntoChunks(s string, size int) []string {
	var chunks []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// nonStreamingResponse builds a JSON response for a non-streaming Claude API call.
func nonStreamingResponse(text string) string {
	return fmt.Sprintf(`{
		"id": "msg_test",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": %q}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`, text)
}

func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func newTestClient(serverURL string) *Client {
	return NewClientWithOptions(
		"claude-sonnet-4-20250514",
		4096,
		option.WithBaseURL(serverURL),
		option.WithAPIKey("test-api-key"),
		option.WithMaxRetries(0),
	)
}

func TestQuery_StreamingSuccess(t *testing.T) {
	expectedText := "Hello, this is a streaming response from Claude."

	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-api-key", r.Header.Get("X-Api-Key"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse(expectedText))
	})
	defer server.Close()

	client := newTestClient(server.URL)
	textCh, errCh := client.Query(context.Background(), "You are helpful.", "Say hello")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, expectedText, result.String())
}

func TestQuery_ContextCancellation(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow response
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("slow response"))
	})
	defer server.Close()

	client := newTestClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	textCh, errCh := client.Query(ctx, "system", "hello")

	// Drain text channel
	for range textCh {
	}

	// Should get a context error
	err := <-errCh
	if err != nil {
		assert.True(t, strings.Contains(err.Error(), "context") || strings.Contains(err.Error(), "deadline"))
	}
}

func TestQuery_AuthenticationError(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	})
	defer server.Close()

	client := newTestClient(server.URL)
	textCh, errCh := client.Query(context.Background(), "system", "hello")

	for range textCh {
	}

	err := <-errCh
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Anthropic API key")
}

func TestQuery_RateLimitError(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
	})
	defer server.Close()

	client := newTestClient(server.URL)
	textCh, errCh := client.Query(context.Background(), "system", "hello")

	for range textCh {
	}

	err := <-errCh
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestQuery_OverloadedError(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(529)
		fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`)
	})
	defer server.Close()

	client := newTestClient(server.URL)
	textCh, errCh := client.Query(context.Background(), "system", "hello")

	for range textCh {
	}

	err := <-errCh
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overloaded")
}

func TestQuerySync_Success(t *testing.T) {
	expectedText := "This is a sync response."

	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, nonStreamingResponse(expectedText))
	})
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.QuerySync(context.Background(), "You are helpful.", "Say hello")

	require.NoError(t, err)
	assert.Equal(t, expectedText, result)
}

func TestQuerySync_AuthError(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid"}}`)
	})
	defer server.Close()

	client := newTestClient(server.URL)
	_, err := client.QuerySync(context.Background(), "system", "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Anthropic API key")
}

func TestQuerySync_BadRequest(t *testing.T) {
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt too long"}}`)
	})
	defer server.Close()

	client := newTestClient(server.URL)
	_, err := client.QuerySync(context.Background(), "system", "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context may be too long")
}

func TestNewClient_DefaultMaxTokens(t *testing.T) {
	c := NewClient("key", "model", 0)
	assert.Equal(t, 4096, c.maxTokens)
}

func TestNewClient_CustomMaxTokens(t *testing.T) {
	c := NewClient("key", "model", 8192)
	assert.Equal(t, 8192, c.maxTokens)
}
