package codex

import (
	"fmt"
	"os"
	"path/filepath"
)

// mcpWorkDir creates a temporary directory with a .codex/config.toml that
// configures the SQLite MCP server. The caller must remove the returned
// directory when done (typically via defer os.RemoveAll).
// Returns the path to the temp directory and any error.
func mcpWorkDir(dbPath string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "codex-mcp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir for codex MCP config: %w", err)
	}

	codexDir := filepath.Join(tmpDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("creating .codex dir: %w", err)
	}

	configContent := fmt.Sprintf(`[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@anthropic-ai/mcp-server-sqlite", %q]
`, dbPath)

	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing codex MCP config: %w", err)
	}

	return tmpDir, nil
}
