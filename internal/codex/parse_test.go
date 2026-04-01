package codex

import (
	"testing"
)

func TestParseJSONLOutput_Success(t *testing.T) {
	jsonl := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","content":"Hello world"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
`
	result, usage, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello world" {
		t.Errorf("result = %q, want %q", result, "Hello world")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
}

func TestParseJSONLOutput_LastAgentMessage(t *testing.T) {
	// Multiple agent messages — last one wins.
	jsonl := `{"type":"item.completed","item":{"id":"i1","type":"agent_message","content":"first"}}
{"type":"item.completed","item":{"id":"i2","type":"agent_message","content":"second"}}
`
	result, _, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "second" {
		t.Errorf("result = %q, want %q", result, "second")
	}
}

func TestParseJSONLOutput_NoAgentMessage(t *testing.T) {
	jsonl := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.completed"}
`
	_, _, err := parseJSONLOutput([]byte(jsonl))
	if err == nil {
		t.Error("expected error for missing agent_message")
	}
}

func TestParseJSONLOutput_Error(t *testing.T) {
	jsonl := `{"type":"error","error":{"message":"rate limit exceeded","code":"rate_limit"}}
`
	_, _, err := parseJSONLOutput([]byte(jsonl))
	if err == nil {
		t.Error("expected error for codex error event")
	}
}

func TestParseJSONLOutput_IgnoresNonAgentItems(t *testing.T) {
	jsonl := `{"type":"item.completed","item":{"id":"i1","type":"command_execution","content":"ls output"}}
{"type":"item.completed","item":{"id":"i2","type":"agent_message","content":"result"}}
`
	result, _, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("result = %q, want %q", result, "result")
	}
}

func TestParseJSONLOutput_AccumulatesUsage(t *testing.T) {
	jsonl := `{"type":"turn.completed","usage":{"input_tokens":50,"output_tokens":20}}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","content":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":30,"output_tokens":10}}
`
	_, usage, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.InputTokens != 80 {
		t.Errorf("InputTokens = %d, want 80", usage.InputTokens)
	}
	if usage.OutputTokens != 30 {
		t.Errorf("OutputTokens = %d, want 30", usage.OutputTokens)
	}
}

func TestParseJSONLOutput_EmptyInput(t *testing.T) {
	_, _, err := parseJSONLOutput([]byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseJSONLOutput_InvalidJSON(t *testing.T) {
	// Invalid JSON lines are silently skipped.
	jsonl := `not json
{"type":"item.completed","item":{"id":"i1","type":"agent_message","content":"ok"}}
`
	result, _, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

func TestParseJSONLOutput_TextFieldFallback(t *testing.T) {
	// Newer Codex CLI uses "text" instead of "content".
	jsonl := `{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"hello from text"}}
`
	result, _, err := parseJSONLOutput([]byte(jsonl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello from text" {
		t.Errorf("result = %q, want %q", result, "hello from text")
	}
}

func TestCodexItem_MessageText(t *testing.T) {
	// Text takes precedence over Content.
	item := &CodexItem{Text: "from text", Content: "from content"}
	if got := item.MessageText(); got != "from text" {
		t.Errorf("MessageText() = %q, want %q", got, "from text")
	}

	// Falls back to Content when Text is empty.
	item2 := &CodexItem{Content: "from content"}
	if got := item2.MessageText(); got != "from content" {
		t.Errorf("MessageText() = %q, want %q", got, "from content")
	}
}
