package codex

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("gpt-5.4", "/tmp/test.db", "/usr/local/bin/codex")
	if c.model != "gpt-5.4" {
		t.Errorf("model = %q, want %q", c.model, "gpt-5.4")
	}
	if c.dbPath != "/tmp/test.db" {
		t.Errorf("dbPath = %q, want %q", c.dbPath, "/tmp/test.db")
	}
}

func TestClient_BuildArgs(t *testing.T) {
	c := NewClient("gpt-5.4", "", "codex")
	args := c.buildArgs("you are a helper", "what is 2+2", "")

	// Check required args are present.
	assertContains(t, args, "exec")
	assertContains(t, args, "--model")
	assertContains(t, args, "gpt-5.4")
	assertContains(t, args, "--json")
	assertContains(t, args, "--ephemeral")
	assertContains(t, args, "approval_policy=never")
	assertContains(t, args, "sandbox_mode=read-only")

	// System prompt passed via developer_instructions.
	found := false
	for _, a := range args {
		if a == "developer_instructions=you are a helper" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected developer_instructions arg with system prompt")
	}

	// User message should be the last argument.
	if args[len(args)-1] != "what is 2+2" {
		t.Errorf("last arg = %q, want user message", args[len(args)-1])
	}

	// No --cd when workDir is empty.
	assertNotContains(t, args, "--cd")
}

func TestClient_BuildArgs_WithWorkDir(t *testing.T) {
	c := NewClient("gpt-5.4", "", "codex")
	args := c.buildArgs("sys", "msg", "/tmp/mcp-dir")

	assertContains(t, args, "--cd")
	assertContains(t, args, "/tmp/mcp-dir")
}

func TestClient_BuildArgs_NoSystemPrompt(t *testing.T) {
	c := NewClient("gpt-5.4", "", "codex")
	args := c.buildArgs("", "hello", "")

	// Should not contain developer_instructions when system prompt is empty.
	for _, a := range args {
		if a == "developer_instructions=" {
			t.Error("should not include developer_instructions with empty system prompt")
		}
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v should contain %q", args, want)
}

func assertNotContains(t *testing.T, args []string, unwanted string) {
	t.Helper()
	for _, a := range args {
		if a == unwanted {
			t.Errorf("args %v should not contain %q", args, unwanted)
			return
		}
	}
}
