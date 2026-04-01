package codex

// CodexEvent represents a single JSONL event from the Codex CLI --json output.
type CodexEvent struct {
	Type     string      `json:"type"` // thread.started, turn.started, turn.completed, item.started, item.completed, error
	ThreadID string      `json:"thread_id"`
	Item     *CodexItem  `json:"item"`
	Usage    *CodexUsage `json:"usage"`
	Error    *CodexError `json:"error"`
}

// CodexItem represents an item within a Codex event.
type CodexItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // agent_message, command_execution, mcp_tool_call
	Content string `json:"content"`
}

// CodexUsage holds token usage metrics from a Codex CLI call.
type CodexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CodexError holds error information from a Codex CLI call.
type CodexError struct {
	Message string `json:"message"`
}
