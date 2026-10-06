package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OpenCodeConfigFile represents the configuration written to opencode.json
type OpenCodeConfigFile struct {
	Skills     []string `json:"skills"`
	MCPServers []string `json:"mcp_servers"`
}

// ClaudeConfigFile represents the configuration written to .claude/config.json
type ClaudeConfigFile struct {
	Skills     []string `json:"skills"`
	MCPServers []string `json:"mcpServers"`
}

// InjectSandboxedConfig writes sanitized agent config files inside worktreePath.
// API keys and secrets are NEVER written to these files; credentials must be injected
// purely via container runtime environment variables.
func InjectSandboxedConfig(worktreePath string, skills []string, mcpServers []string) error {
	if worktreePath == "" {
		return fmt.Errorf("worktreePath cannot be empty")
	}

	if skills == nil {
		skills = []string{}
	}
	if mcpServers == nil {
		mcpServers = []string{}
	}

	// Ensure worktree directory exists
	if err := os.MkdirAll(worktreePath, 0777); err != nil {
		return fmt.Errorf("failed to create worktree directory: %w", err)
	}

	// 1. Write opencode.json
	opencodeConfig := OpenCodeConfigFile{
		Skills:     skills,
		MCPServers: mcpServers,
	}
	opencodeData, err := json.MarshalIndent(opencodeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal opencode config: %w", err)
	}
	opencodePath := filepath.Join(worktreePath, "opencode.json")
	if err := os.WriteFile(opencodePath, opencodeData, 0666); err != nil {
		return fmt.Errorf("failed to write opencode.json: %w", err)
	}

	// 2. Write .claude/config.json
	claudeDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0777); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}
	claudeConfig := ClaudeConfigFile{
		Skills:     skills,
		MCPServers: mcpServers,
	}
	claudeData, err := json.MarshalIndent(claudeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal claude config: %w", err)
	}
	claudePath := filepath.Join(claudeDir, "config.json")
	if err := os.WriteFile(claudePath, claudeData, 0666); err != nil {
		return fmt.Errorf("failed to write .claude/config.json: %w", err)
	}

	return nil
}
