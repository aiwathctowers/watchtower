package codex

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNewCodexGenerator(t *testing.T) {
	gen := NewCodexGenerator("gpt-5.4", "/usr/local/bin/codex")
	if gen.model != "gpt-5.4" {
		t.Errorf("model = %q, want %q", gen.model, "gpt-5.4")
	}
	if gen.codexPath != "/usr/local/bin/codex" {
		t.Errorf("codexPath = %q, want %q", gen.codexPath, "/usr/local/bin/codex")
	}
}

func TestNewCodexGenerator_EmptyPath(t *testing.T) {
	gen := NewCodexGenerator(ModelDefault, "")
	if gen.model != ModelDefault {
		t.Errorf("model = %q, want %q", gen.model, ModelDefault)
	}
	if gen.codexPath != "" {
		t.Errorf("codexPath = %q, want empty", gen.codexPath)
	}
}

func TestClassifyError_NotFound(t *testing.T) {
	err := classifyError(&exec.Error{Name: "codex", Err: exec.ErrNotFound}, "", "/usr/bin/codex")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("error = %q, want to contain 'codex CLI not found'", err.Error())
	}
}

func TestClassifyError_ExitError(t *testing.T) {
	// We can't easily create a real exec.ExitError, so test the generic path.
	err := classifyError(exec.ErrDot, "", "/usr/bin/codex")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "codex CLI error") {
		t.Errorf("error = %q, want to contain 'codex CLI error'", err.Error())
	}
}

func TestClassifyError_WithStderr(t *testing.T) {
	// Generic error wrapping.
	err := classifyError(exec.ErrDot, "something went wrong", "/usr/bin/codex")
	if err == nil {
		t.Fatal("expected error")
	}
	// Generic errors don't use stderr, only ExitError does.
	if !strings.Contains(err.Error(), "codex CLI error") {
		t.Errorf("error = %q, want to contain 'codex CLI error'", err.Error())
	}
}

func TestLimitedWriter(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 5}

	n, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 11 {
		t.Errorf("Write returned %d, want 11", n)
	}
	if buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", buf.String(), "hello")
	}

	// Second write should be discarded.
	n, err = lw.Write([]byte("more"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 4 {
		t.Errorf("Write returned %d, want 4", n)
	}
	if buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", buf.String(), "hello")
	}
}
